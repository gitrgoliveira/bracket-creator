// Dedicated table-operator console for a single shiaijo (court): Direction C.
// Route: /admin/shiaijo/:court
//
// Two-column layout: a queue on the left (Up Next / Upcoming / Completed) and
// an inline scoring panel + collapsible context on the right. The operator
// runs their court all day without leaving the page: Call to Court → Start
// Match → score inline → Submit → auto-advance. Scoring renders the shared
// ScoreEditorModal with variant="inline" (no overlay). Cross-competition: the
// queue interleaves matches from every competition assigned to this court.
//
// Data comes from window.tournamentMatches(tournament) (already cross-comp and
// normalized in the admin console) filtered by court. The "Called" state is
// local-only UI (per the brief; backend persistence is a follow-up).

import { createTimerPool } from './timer_pool.jsx';
import { applyPatch, keepNewerCompetitions, matchInComp } from './patch.jsx';
import { SideCell, WinnerTick } from './side_cell.jsx';
import { useOpenedTapGuard, acceptTap } from './tap_guard.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
// Imported DIRECTLY from the leaf rather than read off `window`. Two of the
// call sites below sit inside a `try { } catch (_e) { }` that swallows, so a
// missing global there would degrade into exactly the silent not-saved failure
// these predicates exist to remove. write_result.jsx is import-only (never
// script-tagged), so the double-module-eval trap that keeps this file off
// api_client.jsx does not apply to it — the same move admin_scoring_shared.jsx
// already makes.
import {
    writeDidNotLand, writeKeepsEditorOpen, writeWasSuperseded,
    startWhileCorrectingMessage, startWhileStartingMessage, startWasBlockedByStartMessage,
    courtChangeWhileStartingMessage,
    correctWhileRunningMessage, wasToasted, BRONZE_MATCH_ID,
    writePending, writeWasRefused, writeQueueKey,
} from './write_result.jsx';
// What a Start came back with (a clock_skew refusal, a thrown 409) is
// start_match.jsx's to classify, shared with the Scores tab's own automatic
// start (admin_schedule_score_editor.jsx). startPatch, the write itself, comes
// from the same leaf, not from `window.startPatch`. The state wiring stays here:
// startingRef/pickingRef carry the pick exemption (pickMatch), which that
// leaf's guard has no word for.
import { startPatch, classifyStartOutcome, startFailureMessage, startRefusalStands } from './start_match.jsx';
// swissRoundLabel: single owner is pool_ids.jsx (mp-dej2); this file used to
// carry its own copy. scoreRowMatchName names a match in the refusal notices.
import { swissRoundLabel, scoreRowMatchName, scoreRowMatchLabel } from './pool_ids.jsx';
// NumberedName: single owner of the number-chip-on-the-outer-side rule
// (bc-dnst); see that file's header for why this stays an ES import.
import { NumberedName } from './numbered_name.jsx';
// mp-jnvl: the recency rule is shared with the public viewer's Recent
// results, so it lives in its own leaf rather than in this page.
import { resultRecencyDesc } from './result_recency.jsx';
// bc-cse: a scheduled match a competitor is barred from (withdrew earlier)
// cannot be fought, so no auto-pick may offer it and the queue row shows the
// default-win action instead of Start. One leaf owns the question
// (ineligible_match.jsx); BarredMatchNotice (admin_scoring_shared.jsx) is the
// one component that renders the note plus that action across every surface.
import { isBarredMatch, sideBarredByDecision, involvesCompetitor, sideKeyForDecisionBy } from './ineligible_match.jsx';
// Straight from its own leaf, NOT admin_scoring_shared.jsx: that module also
// imports bracket.jsx (for sideMarks), and admin_shiaijo.jsx's render suite
// stubs window.BracketTree before importing this file -- routing through
// admin_scoring_shared.jsx pulled bracket.jsx's module body in ahead of that
// stub taking effect and silently overwrote it. See barred_match_notice.jsx's
// header. The same rule is why winnerSideLR and teamMatchMarks come from the
// side_marks.jsx leaf below, not from bracket.jsx.
import { BarredMatchNotice } from './barred_match_notice.jsx';
import { holdsUnsaved, confirmLeave, leaveThen, discardUnsaved, UNSAVED_ENTRIES_DISCARDED } from './unsaved_entries.jsx';
import { confirmMatchLabel, SCORE_KEPT_SENTENCE } from './court_move.jsx';
// The winner cue and a withdrawn team's mark, imported from their leaf (its one
// import is competitor_identity.jsx), never read off `window`: a missing global
// used to paint nothing, with no error to say why.
import { winnerSideLR, teamMatchMarks } from './side_marks.jsx';
// bc-tmwn: team match marks (Kiken, Fus.) are placed beside the withdrawn
// side's name via teamNameMark.
import { teamNameMark } from './match_scoreboard.jsx';

const { useState: useStateSh, useMemo: useMemoSh, useEffect: useEffectSh, useRef: useRefSh, useCallback: useCallbackSh } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const ScoreEditorModal = window.ScoreEditorModal;
const CourtPicker = window.CourtPicker;
const BracketTree = window.BracketTree;
const Icon = window.Icon;

const QUEUE_OPEN_KEY = "bc_shiaijo_queue_open";
const Modal = window.Modal;
const hasBothSides = window.hasBothSides;

// Pure ordering/partition helpers (exported for unit tests). Matches sort
// running → scheduled → completed, then by scheduled time, then queue
// position within a group.
export function sortShiaijoMatches(matches) {
    const order = { running: 0, scheduled: 1, completed: 2 };
    return [...matches].sort((a, b) => {
        const ao = order[a.status] ?? 99;
        const bo = order[b.status] ?? 99;
        if (ao !== bo) return ao - bo;
        const ta = (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
        if (ta !== 0) return ta;
        // Final tie-break on queuePosition so the order matches the backend
        // court queue and stays deterministic for same- or untimed matches
        // (e.g. the ""-scheduledAt rows that Skip produces).
        return (Number(a.queuePosition) || 0) - (Number(b.queuePosition) || 0);
    });
}

// Completed bouts read in the order they were PLAYED, oldest first, which is
// only the schedule order on a court that ran to schedule (mp-jnvl). That makes
// the newest result the TAIL, so the context strip's anchor and the Completed
// preview are both a plain tail again: the preview stays a contiguous window
// and "Show all" extends the list instead of inserting rows into the middle of
// it (operator ruling 2026-09-19). Running and scheduled keep schedule order,
// which is the order they will be fought in.
export function partitionShiaijoMatches(matches) {
    const sorted = sortShiaijoMatches(matches);
    const running = [], scheduled = [], completed = [];
    for (const m of sorted) {
        if (m.status === "running") running.push(m);
        else if (m.status === "scheduled") scheduled.push(m);
        else if (m.status === "completed") completed.push(m);
    }
    // Oldest write first: resultRecencyDesc is newest-first, so reverse it. An
    // all-unstamped list falls back to scheduled time and keeps exactly the
    // order it had before the stamp existed.
    completed.sort((a, b) => resultRecencyDesc(b, a));
    return { sorted, running, scheduled, completed };
}

const matchKey = (m) => `${m.compId}:${m.id}`;

// The match id and the court a match event names, read off either the event's
// own fields or its `result` row; court is null when it names none.
const eventMatchCourt = (d) => {
    const res = d && d.result;
    const court = d && typeof d.court === "string" ? d.court : (res && typeof res.court === "string" ? res.court : null);
    const mid = d && (d.matchId != null ? d.matchId : (res && res.id));
    return { court, mid };
};

// _bracketSideName reads a bracket side that may be a normalized {id,name}
// object or a bare string. Mirrors admin_helpers.sideName without importing it
// (admin_shiaijo reads its helpers off window; this stays pure for testing).
function _bracketSideName(side) {
    if (!side) return "";
    return typeof side === "object" ? (side.name || "") : side;
}

// ownStatusMoveHolds: whether the console still shows match `m` with the
// status move this device made (`mv`, from the outbox's ONE owner,
// API.ownStatusMove: a send back, a finish or a decision; stage 6 review 8,
// M4) over the feed's copy. Not once the feed shows that status or no longer
// holds the match. While the move is queued it holds: the copy may move under
// this device's own earlier writes landing, which the move follows. Once the
// server has answered it, a copy stamped after the move's own stamp is a later
// change made elsewhere (a reopen, a start), which shows (X3, Z2, stage 6
// review 7).
export function ownStatusMoveHolds(mv, m) {
    if (!mv || !m || m.status === mv.status) return false;
    if (!mv.landed) return true;
    // A feed copy past the answer's score version is newer whatever its stamp
    // (stage 6 review 9, F7).
    if (Number.isFinite(m.scoreVersion) && Number.isFinite(mv.version) && m.scoreVersion > mv.version) return false;
    return !(m.modifiedAt > mv.stamp);
}

// withOwnStatusMove: match `m` as the court console shows it under the move
// `mv`: sent back reads scheduled, its score kept; finished reads completed
// with the winner the finish names (an engi finish, which names none: the side
// its flags decide), or for a decision the side that did not withdraw, the
// finish's counts laid on, so the court row never shows a finished match
// without its winner. The same after a reload or with the console mounted
// again: the move is read from the outbox, never from the page's memory.
export function withOwnStatusMove(m, mv) {
    if (!ownStatusMoveHolds(mv, m)) return m;
    if (mv.status === "scheduled") return { ...m, status: "scheduled" };
    const p = mv.payload || {};
    let winner;
    if (mv.kind === "decision") {
        const lost = sideKeyForDecisionBy(p.decisionBy);
        winner = lost === "a" ? m.sideB : lost === "b" ? m.sideA : null;
    } else {
        winner = finishWinnerOf(m, p);
        // The wire names the winner by name: the side it names.
        if (typeof winner === "string") {
            if (winner === _bracketSideName(m.sideA)) winner = m.sideA;
            else if (winner === _bracketSideName(m.sideB)) winner = m.sideB;
        }
    }
    return { ...m, ...finishFieldsOf(p), status: "completed", ...(winner ? { winner } : {}) };
}

// finishWinnerOf: the winner a finish names, or, for an engi finish, which
// names none (the server decides it from the flags, engine.engiWinnerSide),
// the side with more flags in it. The console reads a match finished here as
// finished with this winner (withOwnStatusMove) and advances its local bracket on
// it, so the court row never shows a finished match without its winner
// (stage 6).
export function finishWinnerOf(match, patch) {
    if (!match || !patch) return null;
    if (patch.winner) return patch.winner;
    const a = Number(patch.flagsA);
    const b = Number(patch.flagsB);
    if (!Number.isFinite(a) || !Number.isFinite(b) || a === b) return null;
    return a > b ? match.sideA : match.sideB;
}

// finishFieldsOf: what a finish states beside its verdict that the court row
// shows (an engi finish's counts), laid on the match while it reads as
// finished here, so the row's flags and its winner come from the same finish.
function finishFieldsOf(patch) {
    if (!patch) return null;
    const a = Number(patch.flagsA);
    const b = Number(patch.flagsB);
    return Number.isFinite(a) && Number.isFinite(b) ? { flagsA: a, flagsB: b } : null;
}

const _WINNER_OF_RE = /^Winner of r(\d+)-m(\d+)$/;

// pendingFeederSlots maps a pending placeholder final to the feeder matches whose
// winners would make it runnable (mp-y3nk Phase 3). For each side still reading "Winner of
// rX-mY" it locates the feeder POSITIONALLY, mirroring the Go parseWinnerOf:
// roundIndex = rounds.length - X, matchIndex = Y. A slot is `resolvable` only
// when the feeder exists and both of ITS sides are real competitors (so the
// operator picks a winner from the two `options`); a feeder that is itself
// unresolved is returned non-resolvable so the caller disables it. Exported for
// unit testing; returns [] for a resolved match or a missing bracket.
export function pendingFeederSlots(finalMatch, rounds) {
    if (!finalMatch || !Array.isArray(rounds)) return [];
    const slots = [];
    for (const side of ["A", "B"]) {
        const name = _bracketSideName(side === "A" ? finalMatch.sideA : finalMatch.sideB);
        const mm = _WINNER_OF_RE.exec(name);
        if (!mm) continue; // resolved competitor (or empty) → not a slot
        const roundIndex = rounds.length - Number(mm[1]);
        const matchIndex = Number(mm[2]);
        const feeder = (rounds[roundIndex] && rounds[roundIndex][matchIndex]) || null;
        const a = feeder ? _bracketSideName(feeder.sideA) : "";
        const b = feeder ? _bracketSideName(feeder.sideB) : "";
        const feederResolved = !!feeder && !!a && !!b &&
            !_WINNER_OF_RE.test(a) && !_WINNER_OF_RE.test(b);
        slots.push({
            side,
            placeholder: name,
            feeder,
            resolvable: feederResolved,
            options: feederResolved ? [feeder.sideA, feeder.sideB] : [],
        });
    }
    return slots;
}

// _findMatchInRounds locates a match by id, returning its {R (round), M (index)}
// position, or {R:-1, M:-1} if absent. Shared by propagateBracketWinnerLocal and
// applyBronzeLoserLocal so the search logic can't drift between them.
function _findMatchInRounds(rounds, matchId) {
    for (let r = 0; r < rounds.length; r++) {
        const idx = (rounds[r] || []).findIndex((x) => x && x.id === matchId);
        if (idx >= 0) return { R: r, M: idx };
    }
    return { R: -1, M: -1 };
}

// propagateBracketWinnerLocal advances a winner into the next round's placeholder
// side on the CLIENT (mp-y3nk offline console), so a court running fully offline
// can complete a bout and have the next match: including the final: become
// runnable without a server round-trip. Mirrors the Go engine
// propagateBracketWinner positional rule: a completed match at rounds[r][m] feeds
// rounds[r+1][floor(m/2)], filling sideA when m is even and sideB when m is odd.
// The winning SIDE object (carrying id + name, not just the name) is copied up so
// downstream highlight/scoring keys keep working. Pure + immutable: returns fresh
// rounds and never mutates the input. The server remains authoritative; on
// reconnect a refetch replaces this optimistic tree with the real one.
export function propagateBracketWinnerLocal(rounds, matchId, winnerName) {
    if (!Array.isArray(rounds) || !matchId) return rounds;
    const { R, M } = _findMatchInRounds(rounds, matchId);
    if (R < 0) return rounds; // unknown id → no-op (identity preserved)

    const match = rounds[R][M];
    const aName = _bracketSideName(match.sideA);
    const bName = _bracketSideName(match.sideB);
    // The winning side OBJECT, so id-based highlighting survives advancement.
    const winnerSide = winnerName === aName ? match.sideA
        : winnerName === bName ? match.sideB
        : winnerName;

    // Shallow-clone every match so the returned tree shares no references with
    // the input (immutability for React state).
    const next = rounds.map((round) => round.map((x) => ({ ...x })));
    next[R][M].winner = winnerSide;
    next[R][M].status = "completed";

    if (R < rounds.length - 1) {
        const nm = next[R + 1][Math.floor(M / 2)];
        if (nm) {
            if (M % 2 === 0) nm.sideA = winnerSide; else nm.sideB = winnerSide;
            // Both sides now real (no "Winner of" placeholder) → the match is
            // runnable; promote a pending/blank status to scheduled.
            const na = _bracketSideName(nm.sideA);
            const nb = _bracketSideName(nm.sideB);
            const bothResolved = na && nb && !_WINNER_OF_RE.test(na) && !_WINNER_OF_RE.test(nb);
            if (bothResolved && (nm.status === "pending" || nm.status === "scheduled" || !nm.status)) {
                nm.status = "scheduled";
            }
        }
    }
    return next;
}

// applyBronzeLoserLocal places the LOSER of a completed semifinal into the
// client-side thirdPlaceMatch, mirroring the Go engine's bronze-seeding rule
// (naginata only). Only acts when the scored match is in the SEMIFINAL round
// (R === rounds.length - 2) and a thirdPlaceMatch object is provided. Assigns
// to thirdPlaceMatch.sideA when M is even, sideB when M is odd. Returns an
// immutably cloned thirdPlaceMatch, or null when conditions are not met (not a
// semifinal, no thirdPlaceMatch, unresolved/placeholder loser). Pure + immutable:
// caller merges the result into the bracket; this function never mutates inputs.
export function applyBronzeLoserLocal(rounds, matchId, winnerName, thirdPlaceMatch) {
    if (!thirdPlaceMatch || !Array.isArray(rounds) || !matchId) return null;
    const { R, M } = _findMatchInRounds(rounds, matchId);
    // Only act for the semifinal round (one step before the final).
    if (R < 0 || R !== rounds.length - 2) return null;
    const match = rounds[R][M];
    const aName = _bracketSideName(match.sideA);
    const bName = _bracketSideName(match.sideB);
    // Derive the loser side object (carrying id + name, not just the name).
    let loserSide;
    if (winnerName === aName) {
        loserSide = match.sideB;
    } else if (winnerName === bName) {
        loserSide = match.sideA;
    } else {
        return null; // winner doesn't match either side → no-op
    }
    const loserName = _bracketSideName(loserSide);
    // Reject empty or still-placeholder losers: a "Winner of rX-mY" loser means
    // the semifinal side itself was unresolved and should not be seeded.
    if (!loserName || _WINNER_OF_RE.test(loserName)) return null;
    // Immutably assign the loser to the correct bronze side.
    if (M % 2 === 0) {
        return { ...thirdPlaceMatch, sideA: loserSide };
    }
    return { ...thirdPlaceMatch, sideB: loserSide };
}

// makeReconnectRefetcher builds an SSE onStatus handler that fires onReconnect()
// only on an 'open' that FOLLOWS an 'error' (a genuine reconnect), never on the
// first connect (mp-y3nk Phase 2). A court whose tablet dropped offline can miss
// events while disconnected (e.g. a feeder resolved on another court); refetching
// on reconnect self-heals its queue instead of waiting for the next ordinary
// event. Gating on a prior error avoids a redundant double-fetch on page load,
// where the mount fetch already runs. Exported for unit testing.
//
// markLost() counts as such an error. The stream does not always error when the
// device loses its connection: app.jsx reopens it when the device comes back
// online (reconnectEvents closes the old one, which fires no error), so that
// reopen arrives as an 'open' with nothing before it, and a court that went
// offline kept its stale queue (offering Start for a match another device had
// started) until a manual refresh. The console marks the stream lost on
// 'online', and the reopen that follows refetches. The reopen, not 'online'
// itself, is the moment to fetch: it is when the device is known to reach the
// server again.
export function makeReconnectRefetcher(onReconnect) {
    let sawError = false;
    const onStatus = (status) => {
        if (status === "error") { sawError = true; return; }
        if (status === "open" && sawError) { sawError = false; onReconnect(); }
    };
    onStatus.markLost = () => { sawError = true; };
    return onStatus;
}

// How many of the most-recent completed bouts the Completed section shows
// before the "Show all N" toggle. Keeps the live queue + standings above the
// fold on a full-day court without hiding the recent record.
const COMPLETED_PREVIEW = 8;

// A team encounter (vs an individual bout): team matches carry a lineup the
// operator can set before the bout starts. Exported for unit tests (it gates
// the "Enter lineup" affordance).
export const isTeamMatch = (m) => !!m && (m.compKind === "team" || m.teamSize > 0);

// (addMinuteHHMM and deferTimeFor removed: queue reordering now works by
// swapping scheduledAt between adjacent rows via moveMatch, which calls
// updateMatchTime for both the moved match and its neighbour.)

// shiaijoMatchNo: the match's identity on the Up next card and a queue row,
// after the time and the competition. A pool bout keeps the console's own
// "Match N of M". A knockout match is named by the owner the Scores list and
// the refusal dialogs use (scoreRowMatchLabel -> matchLabel), so the 3rd-place
// match, which has no number, reads "the 3rd-place match" (bc-bzlb, operator
// decision 2026-10-11) rather than nothing.
function shiaijoMatchNo(m) {
    if (m.phase === "pool") {
        return m.poolPosition > 0 && m.poolCount > 0 ? ` · Match ${m.poolPosition} of ${m.poolCount}` : "";
    }
    if (m.phase !== "bracket") return "";
    const label = scoreRowMatchLabel(m);
    return label ? ` · ${label}` : "";
}

// shiaijoScoreCell: decide what the queue row's middle score column shows.
// Exported so the team-vs-individual routing is unit-testable. A team
// encounter's headline number is Individual Victories (IV); it must always
// carry the IV label and never appear as a bare figure (which could be read
// as wins or points). Engi (flag-count scoring) is the ONLY competition type
// where the headline figure is a number at all; it also carries an explicit
// label so it isn't mistaken for an ippon count. Every other individual bout
// shows the self-explanatory ippon LETTERS, never digits.
// Returns one of: {kind:"team",iv} | {kind:"engi",flags} | {kind:"ippon",ippon} | {kind:"vs"} | {kind:"none"}.
export function shiaijoScoreCell(m) {
    if (!m) return { kind: "none" };
    if (m.status === "scheduled") return { kind: "vs" };
    if (m.status !== "completed" && m.status !== "running") return { kind: "none" };
    const flags = window.engiFlagScore ? window.engiFlagScore(m) : null;
    if (flags) return { kind: "engi", flags };
    const isTeam = isTeamMatch(m);
    if (isTeam) {
        const iv = window.teamIVPWScore ? window.teamIVPWScore(m) : (window.teamIVScore ? window.teamIVScore(m) : null);
        return iv ? { kind: "team", iv } : { kind: "none" };
    }
    const ipponsA = m.ipponsA || [];
    const ipponsB = m.ipponsB || [];
    const s = window.formatIpponsScore
        ? window.formatIpponsScore(ipponsB, ipponsA, m.score, m.decision, m.encho, m.decidedByHantei,
            winnerSideLR(m))
        : "";
    return s ? { kind: "ippon", ippon: s } : { kind: "none" };
}

// The match with id in one competition of the court feed, pool or knockout.

function AdminShiaijoPage({ tournament, court: routeCourt, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password, showToast, tweaks, onSwitchCourt }) {
    // Normalize once: filterMatchesByCourt trims its param, so a bookmarked URL
    // with stray whitespace must use the trimmed value everywhere.
    const court = (routeCourt || "").trim();

    const mountedRef = useRefSh(true);
    // The mounted score editor's unsaved entries (R1, stage 5 review 8): every
    // console action that unmounts the editor or puts another match in it asks
    // first (leaveEditorConfirmed), and Send back to queue says so in its own
    // confirm.
    const editorUnsavedRef = useRefSh(null);
    // The latest render's pick and correct and the court's matches, for a tap
    // judged again once the operator has answered the unsaved-entries
    // question (pickMatch, correctMatch).
    const latestRef = useRefSh(null);
    useEffectSh(() => {
        if (typeof window.scrollTo === "function") window.scrollTo(0, 0);
        return () => { mountedRef.current = false; };
    }, []);

    // mp-9h1f: the operator console is court-first AND cross-competition (the
    // running bout stays across comps per AC7, the switch nudge watches OTHER
    // comps on the court per AC6, Submit+Next advances within the submitted
    // comp). It therefore sources every competition with a match on THIS court
    // from the dedicated court feed (GET /api/viewer/court/:court/matches) rather
    // than the whole-tournament aggregate. app.jsx skips its per-event aggregate
    // refetch while this view is active (so the operator's tablet stops
    // re-downloading all courts on every score); this page instead subscribes to
    // the tournament-wide /api/events SSE stream, filters to the event TYPES that
    // can change a court's queue (see REFRESH_EVENTS below), and refetches its
    // court feed on those: the scoping to one court happens server-side in the
    // feed, not in the subscription. Until the first fetch resolves it falls back
    // to the prop aggregate so the queue is never momentarily blank.
    const [courtComps, setCourtComps] = useStateSh(null);
    // The feed as of this render, for the event handler below, whose closure
    // outlives renders.
    const courtCompsRef = useRefSh(null);
    courtCompsRef.current = courtComps;
    // R4 (bc-evts stage 3 review 2): a court move or time change this device
    // makes shows in the court feed at once, as a push would, and a refusal
    // puts the old value back (told by the outbox, subscribeLocalMatchPatch).
    useEffectSh(() => {
        if (!window.API || typeof window.API.subscribeLocalMatchPatch !== "function") return undefined;
        return window.API.subscribeLocalMatchPatch((event) => {
            const cid = event && event.data && event.data.competitionId;
            if (!cid) return;
            setCourtComps((prev) => (Array.isArray(prev) ? prev.map((c) => (c.id === cid ? applyPatch(c, event) : c)) : prev));
        });
    }, []);
    // True while a manual "Refresh" is in flight (button feedback only).
    const [refreshing, setRefreshing] = useStateSh(false);
    // Refetch this court's live feed. Hoisted (not an effect-local closure) so
    // the SSE effect, the reconnect handler, AND the manual Refresh button all
    // drive the same fetch. Guarded by mountedRef so a late resolve after
    // unmount is dropped. Returns the promise so callers can await completion.
    const refreshCourt = useCallbackSh(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return Promise.resolve();
        return window.API.fetchCourtMatches(court)
            // A refetch never puts a row back to an older state
            // (keepNewerCompetitions, patch.jsx): the rule showRunningPush
            // applies to a push.
            .then(comps => { if (mountedRef.current) setCourtComps((prev) => keepNewerCompetitions(prev, comps)); })
            .catch(err => console.error("Failed to fetch court matches", err));
    }, [court]);
    // Operator-triggered re-sync: the last-resort recovery when a court's tablet
    // has fallen behind (dropped SSE, flaky venue WiFi) and the queue looks
    // stale: e.g. a downstream final still shows placeholder feeders that have
    // in fact resolved on the server. One tap re-pulls the authoritative feed.
    const manualRefresh = useCallbackSh(() => {
        setRefreshing(true);
        Promise.resolve(refreshCourt()).finally(() => { if (mountedRef.current) setRefreshing(false); });
    }, [refreshCourt]);
    // Offline bracket advancement (mp-y3nk offline console): optimistically apply
    // a feeder's winner to the LOCAL court feed so a downstream match
    // becomes runnable immediately: even with no server. The server stays
    // authoritative: a later refetch/reconnect replaces this optimistic tree
    // with the real one.
    // `onApplied(before, after)`, when given, is told the competition's bracket
    // before and after the advance, so a caller can take it back.
    const applyLocalBracketWin = useCallbackSh((compId, feederId, winnerName, onApplied) => {
        setCourtComps((prev) => {
            // Seed from the prop aggregate when the live feed hasn't materialised
            // (e.g. offline before the first fetch resolved), so the optimistic
            // advance still lands and courtComps takes over from the fallback.
            const base = Array.isArray(prev) ? prev : (tournament.competitions || []);
            if (!base.length) return prev;
            let changed = false;
            const next = base.map((c) => {
                if (c.id !== compId || !c.bracket || !Array.isArray(c.bracket.rounds)) return c;
                const rounds = propagateBracketWinnerLocal(c.bracket.rounds, feederId, winnerName);
                const thirdPlaceMatch = applyBronzeLoserLocal(
                    c.bracket.rounds, feederId, winnerName, c.bracket.thirdPlaceMatch || null
                );
                if (rounds === c.bracket.rounds && thirdPlaceMatch === null) return c;
                changed = true;
                const bracket = {
                    ...c.bracket,
                    rounds,
                    ...(thirdPlaceMatch !== null ? { thirdPlaceMatch } : {}),
                };
                if (onApplied) onApplied(c.bracket, bracket);
                return { ...c, bracket };
            });
            return changed ? next : prev;
        });
    }, [tournament.competitions]);
    // advanceLocalUntilRefused: the ONE local bracket advance of a result this
    // console wrote (a finish of any kind, a decision). A result the outbox
    // still holds advances at once, so an offline court keeps moving; when it
    // is refused later (match_closed, a newer stored result) or discarded from
    // the held-writes list, nothing of it will land, so the advance is taken
    // back: the competition's bracket is put back as it was when nothing has
    // changed it since, and the court's feed is read again either way (stage 6
    // review 1).
    const advanceLocalUntilRefused = useCallbackSh((match, winnerName, res) => {
        if (!match || match.phase !== "bracket" || !winnerName) return;
        let made = null;
        applyLocalBracketWin(match.compId, match.id, winnerName, (before, after) => { made = { before, after }; });
        if (!writePending(res)) return;
        const takeBack = () => {
            if (!mountedRef.current) return;
            // The court's feed is read again whatever the local advance was.
            refreshCourt();
            if (!made) return;
            const { before, after } = made;
            setCourtComps((prev) => {
                if (!Array.isArray(prev)) return prev;
                let changed = false;
                const next = prev.map((c) => {
                    if (c.id !== match.compId || c.bracket !== after) return c;
                    changed = true;
                    return { ...c, bracket: before };
                });
                return changed ? next : prev;
            });
        };
        res.settled.then(
            (body) => { if (!body || body.discarded || writeWasRefused(body)) takeBack(); },
            takeBack,
        );
    }, [applyLocalBracketWin, refreshCourt]);
    // Advance the local bracket when a bracket bout is scored to completion, so a
    // court running offline sees the NEXT match (incl. the final) become runnable
    // without the server round-trip. Belt-and-suspenders online (the refetch also
    // reflects the server's propagation); the ONLY path that resolves it offline.
    // Draws/no-winner (patch.winner null) never apply: knockout bouts always
    // resolve to a winner before completing.
    const maybeAdvanceLocal = useCallbackSh((match, patch, res) => {
        if (!match || match.phase !== "bracket" || !patch || patch.status !== "completed") return;
        const w = finishWinnerOf(match, patch);
        advanceLocalUntilRefused(match, _bracketSideName(w), res);
    }, [advanceLocalUntilRefused]);
    // The last Start the server refused, or the console refused for a start still
    // out that has since landed, as { key, compId, msg } for the match it was
    // refused for (null = none). It belongs to that match: it shows on that
    // match's Up next card or queue row for as long as the match is still
    // scheduled on this court, and goes when the match leaves scheduled or the
    // court's (non-empty) list no longer holds it (the effect beside upNext
    // below), when any competitor's eligibility changes in that competition (the
    // competitor_status_updated handler just below), which is what restoring a
    // withdrawn competitor broadcasts, or when the operator starts a match
    // (startMatch). Up next changing by itself does not drop it. A refusal that
    // still applies comes straight back on the next tap. Before this it was a
    // bare string nothing cleared, so "kiken-voluntary at Pool A-2" outlived
    // the restore and then sat under the NEXT match (UAT, bc-tmfn). Declared
    // above the feed effect that clears it.
    const [startError, setStartError] = useStateSh(null);
    useEffectSh(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return;
        let cancelled = false;
        const timerPool = createTimerPool();
        // One refetch per burst of events, not one per event: a running match
        // broadcasts every saved point, and each used to schedule its own
        // full fetch of the court's feed.
        let refreshPending = false;
        const scheduleRefresh = () => {
            if (refreshPending) return;
            refreshPending = true;
            timerPool.schedule(() => { refreshPending = false; if (!cancelled) refreshCourt(); }, 200 + Math.random() * 400);
        };
        // A running match's score update changes only its own row, so the
        // pushed result is shown at once rather than after the refetch. The
        // refetch still follows (once per burst): it carries what a push
        // cannot, such as another row a finish moves, and a running update the
        // server coalesced away. A push older than the row it would replace is
        // not applied: a newer state is already shown.
        const showRunningPush = (event) => {
            const cid = event.data && event.data.competitionId;
            const r = event.data && event.data.result;
            const comp = cid && r && r.status === "running" && Array.isArray(courtCompsRef.current)
                ? courtCompsRef.current.find((c) => c.id === cid) : null;
            const row = comp && matchInComp(comp, r.id);
            if (!row || row.status !== "running" || (r.modifiedAt || 0) < (row.modifiedAt || 0)) return;
            setCourtComps((prev) => (Array.isArray(prev) ? prev.map((c) => (c.id === cid ? applyPatch(c, event) : c)) : prev));
        };
        refreshCourt();
        let unsub = () => {};
        if (typeof window.API.subscribeToEvents === "function") {
            // The feed is court-scoped server-side, so any match/schedule/comp
            // transition may change this court's queue. Refetch (jittered to
            // avoid a thundering herd when many operators share the venue LAN).
            const REFRESH_EVENTS = new Set([
                "match_updated", "schedule_updated", "competition_started",
                "competition_completed", "draw_generated", "draw_discarded",
                "swiss_round_generated", "competitor_status_updated", "participants_updated",
                // resync_required: server signalled the SSE replay was unsatisfiable
                // (ring eviction / restart): treat as a refresh so this court's queue
                // doesn't stay stale until the next ordinary event happens to arrive.
                "resync_required",
            ]);
            // A RECONNECT (an 'open' following an 'error') forces an immediate
            // refetch so a court that missed events while disconnected self-heals.
            const onStatus = makeReconnectRefetcher(() => { if (!cancelled) scheduleRefresh(); });
            const off = window.API.subscribeToEvents(
                (event) => {
                    if (cancelled || !event || !REFRESH_EVENTS.has(event.type)) return;
                    if (event.type === "competitor_status_updated") {
                        // Eligibility moved in that competition (a withdrawal
                        // cleared, a competitor reinstated): a Start refused
                        // there may now succeed, so the refusal stops claiming
                        // otherwise. An event naming no competition clears too.
                        const cid = event.data && event.data.competitionId;
                        setStartError((prev) => (prev && (!cid || prev.compId === cid) ? null : prev));
                    }
                    if (event.type === "match_updated") {
                        showRunningPush(event);
                        // A push naming another court takes this console's call down.
                        const d = event.data;
                        const { court: pushedCourt, mid } = eventMatchCourt(d);
                        if (pushedCourt !== null && d.competitionId != null && mid != null) {
                            const key = matchKey({ compId: d.competitionId, id: mid });
                            if (callIdsRef.current.has(key) && pushedCourt !== String(court || "")) {
                                if (withdrawCallRef.current) withdrawCallRef.current(key);
                            }
                        }
                    }
                    scheduleRefresh();
                },
                onStatus
            );
            // Back online: the reopen app.jsx makes refetches (see
            // makeReconnectRefetcher.markLost).
            const onOnline = () => onStatus.markLost();
            window.addEventListener("online", onOnline);
            unsub = () => {
                window.removeEventListener("online", onOnline);
                if (typeof off === "function") off();
            };
        }
        // Back from the background (an iPad locked or switched app): events
        // may have been missed meanwhile, and admin.jsx's own resume refresh
        // reloads the competition, not this court's feed.
        const onVisible = () => { if (!document.hidden && !cancelled) scheduleRefresh(); };
        document.addEventListener("visibilitychange", onVisible);
        return () => {
            cancelled = true;
            timerPool.clearAll();
            document.removeEventListener("visibilitychange", onVisible);
            unsub();
        };
    }, [court, refreshCourt]);

    // Court-scoped competitions: the live feed once loaded, else the prop
    // aggregate as a transient fallback. All competition/match derivations below
    // read from this so the page operates only on THIS court's competitions.
    const courtCompetitions = courtComps || tournament.competitions || [];

    // Selected match for the inline scoring panel. `calledKey` marks the match
    // the operator has announced this session (local cue only); `callingKey`
    // guards the in-flight announce request. `callTapRef` guards against double
    // taps on Call to court (bc-cdbl).
    const [calledKey, setCalledKey] = useStateSh(null);
    const [callingKey, setCallingKey] = useStateSh(null);
    const [startingKey, setStartingKey] = useStateSh(null);
    // The start in flight, as { key, match } (null = none). The guards READ this
    // ref, never startingKey: an advance awaits its finish write and then starts
    // the next match from the render that began the await, so a closure's state
    // is stale by then (a start begun meanwhile was not seen; one that has
    // landed still was). The state above only drives the rendering.
    const startingRef = useRefSh(null);
    // The scheduled match a pick (pickMatch) is starting, from the moment its
    // guards pass until the pick settles: { key, match } or null. It is a start in
    // flight for the one-start guard, because the pick awaits the defer of the
    // running bout before it reaches startMatch (and so before startingRef is
    // set). startMatch is handed its own pick, which is exempt from the guard.
    const pickingRef = useRefSh(null);
    const callTapRef = useRefSh(null);
    // bc-sdtp: one bounce guard for every Start match on this console (Up next
    // and the queue rows), keyless, because the bounce lands on a DIFFERENT
    // button: the first start can land inside the bounce window, the card then
    // shows the next match, and a per-match key would let the bounce start it
    // and send the match just started back to the queue.
    const startTapRef = useRefSh(null);
    // The newest court call this console made per match, keyed by matchKey, as
    // { id, msg }: the announcement id (a promise of it, null if the send failed)
    // and the text sent, so the call can be withdrawn once its match leaves
    // scheduled (bc-cdbl). A reload forgets them; the 5-minute expiry is the
    // backstop.
    const callIdsRef = useRefSh(new Map());
    const withdrawCallRef = useRefSh(null);
    const [contextOpen, setContextOpen] = useStateSh(true);
    // The whole queue column folds away so the scorer can take the full width;
    // the choice is per device, like the operator's other console preferences.
    const [queueOpen, setQueueOpen] = useStateSh(() => {
        try { return localStorage.getItem(QUEUE_OPEN_KEY) !== "0"; } catch (_) { return true; }
    });
    // Set only by an operator click in toggleQueue below, never true on mount
    // or on a server-driven rerender, so the focus effect it gates fires
    // solely for an explicit toggle.
    const queueToggledByUser = useRefSh(false);
    // The two controls that swap places across the fold: the rail button only
    // exists while collapsed, the header button only exists while expanded.
    const queueShowBtnRef = useRefSh(null);
    const queueHideBtnRef = useRefSh(null);
    const toggleQueue = () => {
        queueToggledByUser.current = true;
        // Persist from INSIDE the updater, off the live value. Reading the
        // render-time `queueOpen` here while updating functionally let two taps
        // in one batch cancel each other in state while both wrote the same
        // stale value, so the operator's choice and what was stored disagreed
        // (bc-dnst). This console runs on tablets, where a double-tap is one
        // batch.
        setQueueOpen((open) => {
            const next = !open;
            try { localStorage.setItem(QUEUE_OPEN_KEY, next ? "1" : "0"); } catch (_) { /* private mode */ }
            return next;
        });
    };
    // Collapsing hides the header "Hide" button (it stays mounted; its
    // .shiaijo__queue parent is hidden via .shiaijo--queue-collapsed) and
    // mounts the rail "Show queue" button in its place (and vice versa on
    // expand: the rail unmounts and the header button becomes visible
    // again), so a click or keyboard toggle would otherwise drop focus to
    // the document body either way, hidden or unmounted. Move it to the
    // counterpart control once the DOM has settled, but only for an
    // operator-driven toggle: this must never steal focus on mount or when
    // `queueOpen` merely reflects a rerender the operator didn't trigger.
    useEffectSh(() => {
        if (!queueToggledByUser.current) return;
        queueToggledByUser.current = false;
        const target = queueOpen ? queueHideBtnRef.current : queueShowBtnRef.current;
        // preventScroll: the counterpart sits where the pressed control was,
        // already in view, so the focus move must not scroll the page.
        if (target) target.focus({ preventScroll: true });
    }, [queueOpen]);
    // Completed list stays expanded (it's the operator's running record), but a
    // full-day court accumulates many bouts that would bury the live queue on
    // mobile. Show the most recent COMPLETED_PREVIEW by default; the rest fold
    // behind an inline "Show all N" toggle. The recent ones are never hidden.
    const [showAllCompleted, setShowAllCompleted] = useStateSh(false);
    // The match the operator has explicitly picked to score from the queue.
    // null = follow the running bout (running[0]). A picked match drives the
    // scoring panel instead; the find() in pickedMatch guards staleness, so a
    // pick that completes or vanishes falls back to running[0] automatically.
    const [pickedKey, setPickedKey] = useStateSh(null);
    // The match pickMatch started, and the scheduled snapshot it started
    // from: see ScoreEditorModal's `started` (bc-strt).
    const [startedFrom, setStartedFrom] = useStateSh(null);
    // The status moves this device made (a send back, a finish, a decision)
    // show at once over the court's feed, offline included, until the feed
    // shows them (withOwnStatusMove): the panel moves on, a bout sent back
    // leaves the live slot and the editor. They are read from the outbox
    // through ONE owner (API.ownStatusMove, stage 6 review 8, M4), never kept
    // in this page's state, so a reload or the console mounted again shows
    // what the operator did; a refused or discarded move leaves the outbox,
    // and the match shows as the server holds it. Re-read whenever a move
    // changes.
    const [ownMovesSeq, setOwnMovesSeq] = useStateSh(0);
    useEffectSh(() => {
        if (!window.API || typeof window.API.subscribeOwnStatusMoves !== "function") return undefined;
        return window.API.subscribeOwnStatusMoves(() => setOwnMovesSeq((n) => n + 1));
    }, []);
    const ownMoveOf = (m) => (window.API && typeof window.API.ownStatusMove === "function"
        ? window.API.ownStatusMove(m.compId, m.id) : null);
    // The COMPLETED match the operator has opened to correct in place (parity
    // with the Scores page's "Correct" button). Kept separate from pickedKey so
    // it takes priority over the running bout WITHOUT disturbing it, and so it
    // never interferes with the finish-advance fallback that pickedKey drives.
    // Cleared by "Back to court", and handed over to pickedKey once the match
    // is reopened (see the effect beside correctingMatch below): a running
    // match is the live bout, not a correction. null = not correcting anything.
    const [correctingKey, setCorrectingKey] = useStateSh(null);
    // The last tap the console refused because a match is live (a Correct), a
    // correction is open (a Start) or another start is still out (a Start):
    // { key, compId, why: "running" | "correcting" | "starting", against, label, start }
    // (`against` is the blocker's key, `label` its name as the operator sees it, and
    // `start` the in-flight start or pick record for "starting", null otherwise).
    // Only the tap is stored; the notice text is derived at render while the
    // condition still holds (refusalNotice below), so it cannot go stale. The one
    // that outlives its condition is "starting": when the start it named lands,
    // the effect beside startMatch turns it into a stored startError for the
    // refused match, in the past tense.
    const [refusedTap, setRefusedTap] = useStateSh(null);
    // Pending court reassignment, awaiting operator confirmation. Moving a match
    // off this shiaijo is disruptive (it leaves the court and joins another's
    // queue), so it's gated behind a confirm step. {compId, matchId, to, label, from}.
    const [pendingMove, setPendingMove] = useStateSh(null);
    const [movingCourt, setMovingCourt] = useStateSh(false);
    // A scheduled team match whose lineup the operator is entering before start.
    // Opens the team scoresheet as a modal; positions persist via putMatchLineup
    // independent of scoring, so the operator can set the lineup and close
    // without starting (or hit Start from inside the modal). Held as a key and
    // resolved from the court's matches below, like pickedKey: a snapshot would
    // keep the panel's editors on the teams the match had when it was opened,
    // when a feeding match decided on another device has since seated another
    // team on a side.
    const [lineupKey, setLineupKey] = useStateSh(null);
    // Pending revert-to-queue confirmation. Set when the operator clicks
    // "Send back to queue" on a running bout. Cleared on confirm or cancel.
    // Shape: {compId, matchId, label}.
    const [pendingRevert, setPendingRevert] = useStateSh(null);
    // bc-stpf (operator ruling 2026-10-27, Q1): the question a pick of another
    // bout asks when the app cannot check that the picked match can start (no
    // answer in time, offline, or writes of its competition not sent yet):
    // send the running bout back and start the pick, or keep it running. It is
    // local and never waits on the network, so the court keeps working without
    // the server. Shape: {curLabel, pickLabel}; the answer goes to the promise
    // askStartWithoutCheck handed out (startAskResolveRef).
    const [pendingStartAsk, setPendingStartAsk] = useStateSh(null);
    const startAskResolveRef = useRefSh(null);
    const answerStartAsk = (sendBack) => {
        const resolve = startAskResolveRef.current;
        startAskResolveRef.current = null;
        if (mountedRef.current) setPendingStartAsk(null);
        if (resolve) resolve(sendBack);
    };
    const askStartWithoutCheck = (cur, m) => new Promise((resolve) => {
        startAskResolveRef.current = resolve;
        setPendingStartAsk({ curLabel: confirmMatchLabel(cur), pickLabel: confirmMatchLabel(m) });
    });
    // A console that goes away with the question open answers it "keep it
    // running": nothing is sent, and the pick's finally runs.
    useEffectSh(() => () => answerStartAsk(false), []);
    // bc-cfbd: the two confirms below open from a tap; the bounce of that tap
    // must not land on the fresh backdrop or its buttons. One ref serves both
    // (only one is ever open).
    const { openedRef: confirmOpenedRef, onClickCapture: confirmOnClickCapture } = useOpenedTapGuard();
    // The Confirm button of either confirm: its bounce is not a second confirm
    // (acceptTap in both handlers below). One ref serves both, as above.
    const confirmTapRef = useRefSh(null);
    const [reverting, setReverting] = useStateSh(false);
    // The two confirms are this page's own dialogs, so they do what DialogHost does
    // for confirmDialog: focus moves into the open one (its first button, Cancel),
    // Escape cancels it while its request is not out, and focus goes back to the
    // control that opened it once it closes, without scrolling the page.
    const confirmOpen = Boolean(pendingMove || pendingRevert || pendingStartAsk);
    const confirmBoxRef = useRefSh(null);
    // Escape on the pick's question (bc-stpf) is "keep it running".
    window.useEscapeToClose(confirmOpen && !movingCourt && !reverting
        ? () => { setPendingMove(null); setPendingRevert(null); if (pendingStartAsk) answerStartAsk(false); }
        : undefined);
    useDialogFocus(confirmBoxRef, confirmOpen);
    // Selected competition for filtering the queue. Default: running match's comp,
    // else first comp with scheduled matches here, else any comp with matches here.
    const [selectedCompId, setSelectedCompId] = useStateSh(null);

    // Short-circuit to [] for a blank/unknown court so an accidental landing
    // (e.g. /admin/shiaijo/%20) never sorts/partitions the whole tournament.
    // Same condition as courtKnown below.
    // The court's full match set BEFORE the hasBothSides split. Actionable rows
    // (allMatches) and pending placeholder finals (pendingPlaceholder) are both
    // derived from this so the two are computed from one filtered pass.
    const courtMatchesRaw = useMemoSh(
        () => (tournament.courts || []).includes(court)
            ? window.filterMatchesByCourt(window.tournamentMatches({ competitions: courtCompetitions }), court)
            : [],
        [courtCompetitions, tournament.courts, court]
    );
    // A match this device finished or sent back reads so, until the feed says
    // so itself (withOwnStatusMove): every list below (running, Up next,
    // allDone, the advance target) agrees on it.
    const allMatches = useMemoSh(
        () => courtMatchesRaw.filter(hasBothSides).map((m) => withOwnStatusMove(m, ownMoveOf(m))),
        // ownMovesSeq: a move queued, answered, refused or discarded.
        // oxlint-disable-next-line react-hooks/exhaustive-deps
        [courtMatchesRaw, ownMovesSeq]
    );
    // A move the server answered ends once the feed shows it, or a later
    // change made elsewhere: the owner forgets it.
    useEffectSh(() => {
        if (!window.API || typeof window.API.forgetOwnStatusMove !== "function") return;
        for (const m of courtMatchesRaw) {
            const mv = ownMoveOf(m);
            if (mv && mv.landed && !ownStatusMoveHolds(mv, m)) window.API.forgetOwnStatusMove(m.compId, m.id);
        }
        // A match the tournament no longer holds (gone with its draw) keeps no
        // answered move (stage 6 review 9, F7). The moves are the tab's, so
        // the list is the tournament's, never this court's alone (stage 6
        // review 10, m4: opening court B wiped court A's answered moves, and a
        // finished match moved to another court is not gone).
        const held = [...courtMatchesRaw, ...window.tournamentMatches({ competitions: tournament.competitions || [] })];
        if (held.length > 0 && typeof window.API.forgetOwnStatusMovesNotIn === "function") {
            window.API.forgetOwnStatusMovesNotIn(held);
        }
        // oxlint-disable-next-line react-hooks/exhaustive-deps
    }, [courtMatchesRaw, tournament.competitions, ownMovesSeq]);
    // Scheduled knockout bouts still waiting on a "Winner of rX-mY" feeder
    // (mp-y3nk). Kept SEPARATE from allMatches / filteredScheduled so they never
    // become upNext, never enter moveMatch reordering, and never carry a Start
    // button: a placeholder side has no participant to call to the court. They
    // are rendered as non-actionable "later" rows so a court whose only remaining
    // bout is a downstream final does not show a falsely-empty Upcoming list.
    // Each competition's bracket rounds, by id: the 3rd-place match is pending
    // only while a semi-final that feeds an empty side is unplayed, so the
    // pending split and the slot labels below both read them.
    const roundsFor = useMemoSh(() => {
        const byComp = new Map();
        for (const c of courtCompetitions) byComp.set(c.id, (c.bracket && c.bracket.rounds) || []);
        return (compId) => byComp.get(compId) || [];
    }, [courtCompetitions]);
    const pendingPlaceholder = useMemoSh(
        () => courtMatchesRaw.filter((m) => window.isPendingBracketMatch(m, roundsFor(m.compId))),
        [courtMatchesRaw, roundsFor]
    );
    // A "Later" row shows the placeholder sides of a bout whose feeders are not
    // in yet, so its names go through the shared slot rule (bracket.jsx) and read
    // "Winner of M3" — the number on the bracket card — instead of the internal
    // "r2-m0". One labeller per competition, memoised with the court feed it is
    // derived from. NOTE: the labeller is DISPLAY only; the queue is still split
    // by isPendingBracketMatch / hasBothSides on the RAW sides above, so a
    // relabelled row stays non-actionable exactly as before.
    //
    // The 3rd-place match's empty sides have no slot value to label, so a
    // "Later" bronze row names them after the semi-finals that will fill them
    // ("Loser of M1"), through the same slot-text owner (bracket.jsx). Both
    // labellers read the numbers of ONE display model per competition, built
    // here, so a slot and a bronze side can never name different numbers.
    const bracketLabelsFor = useMemoSh(() => {
        const cache = new Map();
        return (compId) => {
            if (!cache.has(compId)) {
                const rounds = roundsFor(compId);
                const nums = window.buildDisplayModel ? window.buildDisplayModel(rounds).matchNumById : undefined;
                cache.set(compId, {
                    slot: window.makeSlotLabeller ? window.makeSlotLabeller(rounds, nums) : (n) => n,
                    bronze: window.bronzeSlotLabels ? window.bronzeSlotLabels(rounds, nums) : undefined,
                });
            }
            return cache.get(compId);
        };
    }, [roundsFor]);
    const slotLabelFor = useCallbackSh((compId) => bracketLabelsFor(compId).slot, [bracketLabelsFor]);
    const bronzeLabelsFor = useCallbackSh((compId) => bracketLabelsFor(compId).bronze, [bracketLabelsFor]);
    const { sorted, running, scheduled, completed } = useMemoSh(
        () => partitionShiaijoMatches(allMatches),
        [allMatches]
    );

    const courts = tournament.courts || [];
    const courtKnown = courts.includes(court);

    // All competitions that have at least one match on this court (AC3/AC4).
    // Pending placeholder finals count too, so a court whose only remaining bout
    // is a downstream final still surfaces its competition in the selector and
    // is not treated as empty.
    const courtsComps = useMemoSh(() => {
        const seen = new Set();
        const out = [];
        for (const m of [...allMatches, ...pendingPlaceholder]) {
            if (!seen.has(m.compId)) {
                seen.add(m.compId);
                const comp = courtCompetitions.find(c => c.id === m.compId);
                out.push({ id: m.compId, name: m.compName || (comp && comp.name) || m.compId });
            }
        }
        return out;
    }, [allMatches, pendingPlaceholder, courtCompetitions]);

    // Effective selected competition: default logic runs when selectedCompId is
    // null or no longer present (e.g. a comp was removed). Priority: running
    // match's comp; else first comp with scheduled matches here; else first comp
    // with any matches. AC3/AC7.
    const effectiveCompId = useMemoSh(() => {
        if (!courtsComps.length) return null;
        // If explicitly selected and still valid, keep it
        if (selectedCompId && courtsComps.some(c => c.id === selectedCompId)) return selectedCompId;
        // Default: running match's comp
        if (running.length > 0) return running[0].compId;
        // Else first comp with scheduled matches
        for (const c of courtsComps) {
            if (allMatches.some(m => m.compId === c.id && m.status === "scheduled")) return c.id;
        }
        // Else any comp
        return courtsComps[0].id;
    }, [selectedCompId, courtsComps, running, allMatches]);
    // One operator per court (operator ruling 2026-09-26): the console never
    // switches competition by itself. The default above only CHOOSES the first
    // one shown; it is then held as if picked, so a finished bout or a match
    // moved onto this court cannot swap the competition under the operator.
    // From then on only the picker and the banner below change it (and a
    // competition that leaves this court falls back to the default again).
    useEffectSh(() => {
        if (effectiveCompId && effectiveCompId !== selectedCompId) setSelectedCompId(effectiveCompId);
    }, [effectiveCompId, selectedCompId]);

    // The scoring panel shows the match the operator is officiating. By default
    // that's the running (NOW) bout, but the operator may pick any upcoming
    // match to run out of order via its "Score" button (pickMatch). A running
    // current bout is sent back to the queue first, keeping any score entered
    // for it (bc-sbq), so the court never has two running bouts.
    // A pickedMatch that completes (or vanishes) falls back to running[0]: the
    // find() filters out completed matches and the `pickedMatch || running[0]`
    // expression covers the null case. This keeps the finish-advance flow clean
    // (a just-scored pick hands off to the next bout, not to itself). Correcting
    // a completed match is a DELIBERATE, separate action via correctingKey /
    // correctMatch below — not a pickedMatch, so it never disturbs this fallback.
    const pickedMatch = useMemoSh(
        () => pickedKey ? sorted.find((x) => matchKey(x) === pickedKey && x.status !== "completed") || null : null,
        [pickedKey, sorted]
    );
    // The completed match being corrected. Takes priority over the running
    // match so the panel follows the deliberate correction the operator asked
    // for. "Back to court" clears correctingKey to fall back to the live bout.
    // No status filter here, on purpose: a filter would leave the key set, and
    // the key would re-pin the match the moment it completed again. The
    // effect below ends the correction instead.
    const correctingMatch = useMemoSh(
        () => correctingKey ? sorted.find((x) => matchKey(x) === correctingKey) || null : null,
        [correctingKey, sorted]
    );
    const lineupMatch = useMemoSh(
        () => lineupKey ? sorted.find((x) => matchKey(x) === lineupKey) || null : null,
        [lineupKey, sorted]
    );
    // A correction that is REOPENED (a kachinuki Reopen, or Clear withdrawal
    // and reopen: completed -> running) is no longer a correction of a past
    // result. It is the bout being fought on this court, so it becomes the
    // live match: the pick takes it over (same key, so the editor does not
    // remount and nothing entered is lost) and the correction ends. Without
    // this, Finish + Start Next left the panel pinned on the old match in
    // CORRECTION mode with the match it had just started hidden until "Back
    // to court" (UAT, bc-tmfn). Keyed on the status VALUE, never the match
    // object, which every refetch re-creates.
    //
    // A cleared match-level fusensho whose competitor is still barred
    // reopens to "scheduled" instead (engine.reopenTargetStatus): there is
    // no bout to pick, but the correction still ends, so the panel returns
    // to the court's ordinary view rather than staying pinned to it.
    const correctingStatus = correctingMatch ? correctingMatch.status : null;
    useEffectSh(() => {
        if (!correctingKey) return;
        if (correctingStatus === "running") {
            setPickedKey(correctingKey);
            setCorrectingKey(null);
        } else if (correctingStatus === "scheduled") {
            setCorrectingKey(null);
        }
    }, [correctingKey, correctingStatus]);
    // A match the operator deliberately holds open: a correction. It outranks
    // the pick and the live bout, so while one is set it IS selectedMatch.
    const selectedMatch = useMemoSh(
        () => correctingMatch || pickedMatch || running[0] || null,
        [correctingMatch, pickedMatch, running]
    );

    // bc-strt: the match pickMatch started, until the refetch shows it
    // running. ONE predicate for the editor's `started` prop and for liveBout
    // below, so the two cannot drift.
    const isStartedSnapshot = (m) => !!m && !!startedFrom && startedFrom.key === matchKey(m) && startedFrom.at === m.modifiedAt;
    // The match whose start request is still in flight (startingKey), found on
    // the court, for the render (liveBout below). The guards read startingRef.
    const startingBlocker = startingKey ? sorted.find((x) => matchKey(x) === startingKey) || null : null;
    // The bout live on this court right now (bc-crpn): the pick when it is
    // running or is the start this console just made (the feed still reads
    // "scheduled" until the refetch; a queued offline start too), else
    // running[0] (court-wide, never filtered by the competition selector),
    // else the match whose start request is still in flight.
    const liveBout = (pickedMatch && (pickedMatch.status === "running" || isStartedSnapshot(pickedMatch)))
        ? pickedMatch
        : running[0] || startingBlocker;
    // The correction the console holds open (completed), if any. A reopened
    // one is the live bout and is handed over by the effect above.
    const openCorrection = correctingMatch && correctingMatch.status === "completed" ? correctingMatch : null;
    // The "Another bout is running" list: the bouts beyond the one the panel
    // shows, plus the running one itself while a correction holds the panel.
    const alsoRunning = openCorrection ? running : running.slice(1);
    // What refuses a tap right now, by kind: a live bout refuses a Correct, an
    // open correction refuses a Start, and a start still out refuses a Start of
    // another match. ONE place builds each sentence, for the toast and the
    // notice alike.
    // "starting" reads the refs, so a refusal made from an advance's async
    // continuation names the start that is out NOW (the render's copy may be
    // stale there), and a refusal made during a pick's defer names that pick. At
    // render time the refs and the state agree.
    const refusalBlocker = (why) => {
        if (why === "running") return liveBout;
        if (why === "starting") {
            const s = startingRef.current || pickingRef.current;
            return s ? s.match : null;
        }
        return openCorrection;
    };
    const refusalText = (why) => {
        const blocker = refusalBlocker(why);
        if (!blocker) return null;
        if (why === "running") return correctWhileRunningMessage({ court, label: scoreRowMatchName(blocker) });
        if (why === "starting") return startWhileStartingMessage({ label: scoreRowMatchName(blocker) });
        return startWhileCorrectingMessage({ label: scoreRowMatchName(blocker) });
    };
    // The notice for a refused tap, derived: shown only while the match that
    // refused it still does, so it goes by itself when that match is finished,
    // sent back or cancelled, and a later, unrelated one never revives it.
    const refusalNotice = (key) => {
        if (!refusedTap || refusedTap.key !== key) return null;
        const blocker = refusalBlocker(refusedTap.why);
        return blocker && matchKey(blocker) === refusedTap.against ? refusalText(refusedTap.why) : null;
    };
    // Refuse a tap: remember it against the match that blocks it, and toast.
    // leaveEditorConfirmed: true when the console may unmount the editor or put
    // another match in it: nothing unsaved, or the operator chose Discard
    // entries (R1). With nothing unsaved it answers `true` at once.
    const leaveEditorConfirmed = () => confirmLeave(editorUnsavedRef, () => mountedRef.current);
    // leaving: a page exit (the Dashboard crumb, Sign out, Public viewer, the
    // court switcher) behind the same question; with nothing unsaved it goes
    // in the same tick (stage 5 review 9, M1).
    const leaving = (go) => (...args) => leaveThen(editorUnsavedRef, () => go(...args), () => mountedRef.current);
    // The editor shows the match a host confirm drops (Send back to queue, a
    // court move) and holds unsaved entries: the confirm says they are
    // discarded, and drops them on confirm (m1).
    const editorUnsavedFor = (compId, matchId) => !!selectedMatch && selectedMatch.compId === compId
        && selectedMatch.id === matchId && holdsUnsaved(editorUnsavedRef);
    const refuseTap = (m, why) => {
        const blocker = refusalBlocker(why);
        setRefusedTap({
            key: matchKey(m), compId: m.compId, why, against: matchKey(blocker), label: scoreRowMatchName(blocker),
            // The start (or pick) record itself, which startMatch marks `landed` once the
            // start went out: the effect beside startMatch asks it before writing the
            // past-tense sentence.
            start: why === "starting" ? (startingRef.current || pickingRef.current) : null,
        });
        if (showToast) showToast(refusalText(why), "error");
    };
    // The stored refusal is read against the refused match's LIVE row in the
    // court's list (the same list the court-call withdrawal reads), never against
    // a snapshot: the notice shows only while that row is still scheduled, so it
    // cannot outlive the match for the render before the effect below drops it.
    const startErrorKey = startError ? startError.key : null;
    const startErrorRow = startErrorKey ? courtMatchesRaw.find((x) => matchKey(x) === startErrorKey) : null;
    const startErrorStatus = startErrorRow ? startErrorRow.status : null;
    // The Start notice a scheduled match's card or queue row shows: a refused
    // tap's derived notice, else the stored Start refusal for that match.
    const startNoticeFor = (key) => refusalNotice(key)
        || (startError && startError.key === key && startErrorStatus === "scheduled" ? startError.msg : null);

    // For pool daihyosen/tiebreaker bouts, enrich the selected match with
    // rep-player roster data so ScoreEditorModal renders the rep-picker dropdowns
    // (repIsTeam / repRosterA / repRosterB). Regular matches pass through
    // unchanged. enrichPoolMatchWithComp is exposed by admin_pools.jsx via
    // window.enrichPoolMatchWithComp; isSupplementaryBout via window.isSupplementaryBout.
    // These globals are assigned at module evaluation time, so they are always
    // present when admin_shiaijo.jsx executes (admin_pools.js loads first per
    // the <script> order in index.html).
    const editorMatch = useMemoSh(() => {
        if (!selectedMatch) return selectedMatch;
        if (window.isSupplementaryBout && window.isSupplementaryBout(selectedMatch.id) && window.enrichPoolMatchWithComp) {
            const comp = courtCompetitions.find(c => c.id === selectedMatch.compId);
            return window.enrichPoolMatchWithComp(selectedMatch, comp);
        }
        return selectedMatch;
    }, [selectedMatch, courtCompetitions]);

    // Filtered to the selected competition (AC4).
    // Running matches are NEVER filtered: the panel must stay on the running
    // bout even if the operator switches comp (AC7).
    // The match this console's own Start began is not waiting any more: while
    // the start is out (isStartedSnapshot, until the feed shows it running) the
    // editor runs it, so it leaves Upcoming and Up next as a running match does.
    // A start refused or discarded takes startedFrom back (startMatch's unpin),
    // and the match is in the list again.
    const filteredScheduled = useMemoSh(
        () => scheduled.filter((m) => (!effectiveCompId || m.compId === effectiveCompId) && !isStartedSnapshot(m)),
        // isStartedSnapshot reads startedFrom alone.
        // eslint-disable-next-line react-hooks/exhaustive-deps
        [scheduled, effectiveCompId, startedFrom]
    );
    const filteredCompleted = useMemoSh(
        () => effectiveCompId ? completed.filter(m => m.compId === effectiveCompId) : completed,
        [completed, effectiveCompId]
    );
    // Pending placeholder finals for the selected competition, rendered as
    // non-actionable "later" rows below Upcoming. Sorted by scheduled time so a
    // multi-round pending tail reads in play order.
    const filteredPending = useMemoSh(
        () => {
            const list = effectiveCompId ? pendingPlaceholder.filter(m => m.compId === effectiveCompId) : pendingPlaceholder;
            return sortShiaijoMatches(list);
        },
        [pendingPlaceholder, effectiveCompId]
    );

    // The standings/context panel follows the court's current focus: not
    // strictly the running match: so it stays visible (and updates) after a
    // bout is finished, instead of collapsing to the empty state. Priority:
    // the running match; else the bout just played, which is the TAIL because
    // partitionShiaijoMatches orders completed by play time, so the operator
    // sees their result land in the standings; else the next scheduled bout
    // (before the court's first match).
    const contextMatch = useMemoSh(
        () => selectedMatch || filteredCompleted[filteredCompleted.length - 1] || filteredScheduled[0] || null,
        [selectedMatch, filteredCompleted, filteredScheduled]
    );

    // What the panel is describing, when that is not the live bout. The heading
    // says so outright, because the panel keeps the bout on screen (and, for
    // knockout, highlights it in the bracket) while the operator's attention is
    // elsewhere (mp-jnvl). Three cases: before the court's first bout the
    // anchor is the one about to be FOUGHT ("Up next", operator ruling
    // 2026-09-19 - the highlight means "play this next", so say that); after a
    // bout it is the last result; and the operator can open a finished bout
    // deliberately to correct it, which is a different fact from falling back
    // to it.
    const contextLead = useMemoSh(() => {
        if (contextMatch?.status === "scheduled") return "Up next";
        if (contextMatch?.status !== "completed") return "";
        return contextMatch === correctingMatch ? "Correcting" : (selectedMatch ? "" : "Just played");
    }, [contextMatch, correctingMatch, selectedMatch]);

    // The rows the Completed section shows: the whole list once the operator
    // expands it, else the most recently played preview. Memoised because this
    // console re-renders on every SSE broadcast and a full-day court can hold
    // a hundred completed bouts.
    const completedShown = useMemoSh(
        () => (showAllCompleted ? filteredCompleted : filteredCompleted.slice(-COMPLETED_PREVIEW)),
        [showAllCompleted, filteredCompleted]
    );

    // Up Next = the first scheduled match in the selected competition that
    // can actually be fought. A barred match (isBarredMatch, bc-cse) is
    // skipped by every auto-pick: the operator resolves it directly from its
    // queue row (the default-win action, or Reinstate) rather than having it
    // offered as the next thing to start.
    const upNext = filteredScheduled.find((m) => !isBarredMatch(m)) || null;
    // The Up next card's alert line (startNoticeFor).
    const upNextNotice = upNext ? startNoticeFor(matchKey(upNext)) : null;
    // Everything else in Upcoming: the whole scheduled list minus whichever
    // match became Up Next (by key, not index: Up Next may not be [0] when a
    // barred match sits ahead of it). Any barred match stays here, rendered
    // as a normal row that shows its own resolution instead of Start.
    const upcomingQueueMatches = upNext
        ? filteredScheduled.filter((m) => matchKey(m) !== matchKey(upNext))
        : filteredScheduled;
    // A stored Start refusal describes ONE match at one moment, so it is judged
    // against that match's live row and nothing else (startErrorStatus above;
    // start_match.jsx startRefusalStands owns the rule, and the Scores tab asks
    // the same one, admin_schedule_score_editor.jsx). It is
    // dropped when the match leaves scheduled (started here or elsewhere, closed
    // by a decision), or when the court's list is non-empty and no longer holds
    // it (the match moved). Up next changing by itself drops nothing: a refusal
    // for a match picked from further down the queue stays on its row, and is
    // there on the card if the match later reaches Up next. A refusal is about
    // one match at one moment, so a stale one never revives when that match is
    // later sent back to the queue: the next tap asks the server again. A start
    // by the operator clears it too (startMatch), and so does an eligibility
    // change in its competition (the competitor_status_updated handler).
    //
    // The list is EMPTY while it loads and across a transient empty refetch, and
    // an empty list says nothing about the match, so the refusal is left where it
    // is then (the court-call withdrawal below leaves an absent key alone for the
    // same reason). Only a list that holds matches yet lacks this one counts.
    const startErrorStands = startRefusalStands({ row: startErrorRow, listHoldsMatches: courtMatchesRaw.length > 0 });
    useEffectSh(() => {
        if (!startErrorKey) return;
        if (!startErrorStands) setStartError(null);
    }, [startErrorKey, startErrorStands]);

    // "Which pool is next" for the context panel: the first upcoming pool on
    // this court (within the selected comp) whose pool differs from the one
    // currently in focus.
    const nextPoolName = useMemoSh(() => {
        const cur = (contextMatch && contextMatch.phase === "pool") ? (contextMatch.poolName || "") : "";
        for (const m of filteredScheduled) {
            const pn = m.poolName || "";
            if (m.phase === "pool" && pn && pn !== cur) return pn;
        }
        return null;
    }, [contextMatch, filteredScheduled]);

    // Auto-advance target after a submit: next non-completed match in the
    // SUBMITTED match's competition (not the selected one). The scoring panel may
    // be on a running bout from a different comp than the selector (AC7), so
    // keying off m.compId keeps Submit+Next within the competition the operator
    // just scored instead of hopping to the selected comp.
    const nextActiveAfter = (m, withdrawn = null) => {
        const pool = [...running, ...scheduled].filter((x) => x.compId === m.compId);
        const idx = pool.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return null;
        // bc-cse: skip a barred match. Both Finish + Start Next and the
        // after-decision advance feed this straight into a Start write, which
        // the server would just refuse (409 ineligible_competitor); the
        // barred match is left for its own queue row to resolve. `withdrawn`
        // is a competitor the decision just barred (sideBarredByDecision):
        // this court's list does not show their matches barred until it is
        // refetched.
        return pool.slice(idx + 1).find((x) => x.status !== "completed" && !isBarredMatch(x) && !involvesCompetitor(x, withdrawn)) || null;
    };

    // Amber nudge banner logic (AC6): fires ONLY when the SELECTED competition
    // has no more matches to run on this court (it has finished, or hasn't
    // started yet: no running and no scheduled bouts here) AND another
    // competition still has scheduled matches on the court. That's the "you're
    // on the wrong competition, switch" case. It deliberately does NOT turn
    // amber just because another competition has an earlier match while this
    // one is still active: the operator runs their current competition to
    // completion first. In that case it is the quiet variant (alsoWaiting):
    // neutral, "N <comp> matches also waiting on this court", still a tap to
    // switch and never a switch by itself (operator request 2026-10-03, after
    // a match moved here from another court was visible only through the
    // picker). Never red/navy: the amber case uses --warn-* tokens only.
    const nudgeBanner = useMemoSh(() => {
        if (!effectiveCompId || !courtKnown) return null;

        // Selected comp still has a running or scheduled match here: no
        // "switch" nudge, but the other competition's matches are still named
        // (alsoWaiting), quietly, because a match moved onto this court from
        // another one was otherwise visible only through the competition
        // picker (operator request 2026-10-03).
        // The bout this console just started counts as running (liveBout: the
        // started snapshot, which has left the scheduled list).
        const selHasActive = running.some(m => m.compId === effectiveCompId) || (!!liveBout && liveBout.compId === effectiveCompId) || filteredScheduled.length > 0;

        // Other competitions' scheduled matches still on this court. The bout
        // this console just started (its started snapshot, still scheduled in
        // the feed) is a live bout, never one waiting (stage 6 review 7).
        const otherScheduled = allMatches.filter(
            m => m.compId !== effectiveCompId && m.status === "scheduled" && !isStartedSnapshot(m)
        );
        if (otherScheduled.length === 0) return null;

        // Null-prototype: compId is user-controlled (a comp id of "__proto__"
        // must not pollute the map or collide with inherited keys).
        const byComp = Object.create(null);
        for (const m of otherScheduled) {
            if (!byComp[m.compId]) byComp[m.compId] = { id: m.compId, name: m.compName, count: 0 };
            byComp[m.compId].count++;
        }
        // EVERY other competition waiting here is named, each with its own
        // switch, most matches first: a match moved onto this court may
        // belong to the smaller one.
        const entries = Object.values(byComp);
        entries.sort((a, b) => b.count - a.count);
        return { entries, alsoWaiting: selHasActive };
        // isStartedSnapshot reads startedFrom alone.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [allMatches, effectiveCompId, running, liveBout, filteredScheduled, courtKnown, startedFrom]);

    // The one place the Starting… state follows the refs: the start in flight,
    // else the pick still deferring the running bout (so a start settling inside
    // a pick does not blank the button the pick is still holding).
    const syncStartingKey = () => {
        const s = startingRef.current || pickingRef.current;
        if (mountedRef.current) setStartingKey(s ? s.key : null);
    };
    // A refusal for a start that was still out ("starting") is derived while that
    // start is out: "X is still being started". When it has landed (or failed),
    // the sentence stops being true, but nothing else would say the match the
    // operator asked for was never started (a 2.7s toast was the only sign,
    // bc-aadv). So it becomes a STORED refusal for the refused match, in the past
    // tense, bound to that match like every stored refusal (the effect beside
    // upNext). The refused match is NOT started on its own: the court now has a
    // running bout, so a second automatic start would draw court_busy or put two
    // bouts on the court unasked, and one start at a time is the rule (fce5866c).
    // The operator starts it, which clears the sentence. The effect re-runs when
    // the start in flight changes (startingKey), and reads the refs for the truth.
    //
    // A start that did NOT land leaves the court free, so the past-tense sentence
    // would be false, and that start's own refusal (a thrown 409, a clock_skew
    // answer or a superseded one that held its result group) is what the
    // operator needs. It is stored for the
    // blocker before this runs (startMatch's refusal branch or its catch, ahead
    // of its finally), and startMatch clears
    // startError when a start passes the guard, so any value present when the
    // blocker ends is the blocker's own: it is kept, and the waiting match shows
    // nothing, as it did before the sentence existed.
    //
    // The sentence also needs a start that WENT OUT. The blocker can be a pick whose
    // deferral of the running bout failed (the revert threw or was unavailable): it
    // never started, nothing was written and the court is as it was, so "was being
    // started" would be false. The start record the refusal holds says whether a start
    // landed (`landed`, set by startMatch, also on the pick's record), not the court's
    // list: the list is refreshed a moment after the write and may still show the match
    // scheduled, and a start queued offline stays scheduled until the connection
    // returns, yet both were sent. Otherwise the refused tap is dropped, with no sentence.
    useEffectSh(() => {
        if (!refusedTap || refusedTap.why !== "starting") return;
        if (startingRef.current || pickingRef.current) return;
        if (refusedTap.start && refusedTap.start.landed) {
            const blocked = { key: refusedTap.key, compId: refusedTap.compId, msg: startWasBlockedByStartMessage({ label: refusedTap.label }) };
            setStartError((prev) => prev ?? blocked);
        }
        setRefusedTap(null);
    }, [refusedTap, startingKey]);
    // The stored Start refusal for match `m` (startError), which
    // startRefusalStands binds to that match: a start's own refusal and a
    // pick's refused start check (bc-stpf) store it alike.
    const refusalFor = (m, msg) => ({ key: matchKey(m), compId: m.compId, msg });
    // Returns true when the start went out (landed, or was queued offline), false
    // otherwise: pickMatch relies on this so it only pins pickedKey for a match that
    // actually started (a blocked-by-eligibility start, a clock_skew refusal or a
    // superseded answer that held the result group must not steal the panel; a
    // superseded answer that held nothing is an echo of a start that landed and
    // does pin it).
    // `pick` is the pickingRef entry pickMatch made for THIS start, if it is one:
    // that pick is the start itself, not a start in flight against it.
    const startMatch = async (m, { pick = null, after = null } = {}) => {
        const key = matchKey(m);
        // bc-evts: the start is the outbox's (a running score write), answered
        // pending at once; a refusal arrives later, through `settled`. It is
        // stored on the refused match and toasted once, here, as a refusal the
        // start came back with always was, and claimed so the queue's own
        // alert does not toast it a second time.
        //
        // The pending answer pins the panel on the started match at once
        // (`startedFrom`, `pickedKey`, below); a start refused AFTER that takes
        // its pin back, so the console shows what the server holds and a tap
        // is not aimed at a match that never started (Z1). `refused` also
        // covers a refusal that comes before the pin is set.
        let refused = false;
        const unpin = () => {
            refused = true;
            if (pick) pick.refused = true;
            if (!mountedRef.current) return;
            setStartedFrom((s) => (s && s.key === key ? null : s));
            setPickedKey((k) => (k === key ? null : k));
        };
        const refusedLater = (msg) => {
            unpin();
            if (mountedRef.current) setStartError(refusalFor(m, msg));
            if (showToast) showToast(msg, "error");
        };
        // Claimed only when it was said, on the row or by the (app-wide) toast (R2).
        const onRefusal = (e) => { refusedLater(startFailureMessage(e)); return mountedRef.current || !!showToast; };
        const inFlight = startingRef.current || (pickingRef.current && pickingRef.current !== pick ? pickingRef.current : null);
        if (inFlight) {
            // One start at a time. An advance (Finish + Start Next, or the one
            // after a decision) that asks for another match while a start is out
            // is refused, and the refusal says so where the refused match is
            // shown (refuseTap: its Up next card or its queue row) and toasts,
            // rather than doing nothing (bc-aadv). A repeat for the match already
            // being started is silent: it is on its way.
            if (inFlight.key !== key && mountedRef.current) refuseTap(m, "starting");
            return false;
        }
        setStartError(null);
        const started = { key, match: m };
        startingRef.current = started;
        setStartingKey(key);
        try {
            // Starting makes the match running; the scoring panel shows
            // running[0], so it picks the match up on the next refetch.
            // A pick's start follows its send back of the running bout (`after`,
            // request-only: never sent, deferAndStart).
            const res = await onEditScore(m.compId, m.id, after ? { ...startPatch(), after } : startPatch(), m, { onRefusal });
            // A clock_skew answer, or a superseded one that held the result
            // group, means the server stored NOTHING of the start and, unlike a
            // queued start, nothing will land later, so returning true here would
            // pin the panel on a match that never started while the tap looked
            // like it worked. Found in browser verification: this card button is
            // a start path none of the review sweeps enumerated (it is not one of
            // the editor call sites). (A superseded answer that held NOTHING is an
            // echo of a start that already landed: classifyStartOutcome answers
            // ok for it and it takes the landed path below.)
            // Both are a REFUSED start (start_match.jsx classifyStartOutcome): the
            // sentence is stored on the refused match and toasted once, and
            // `landed` stays unset, so a refusal that waited on this start is not
            // worded as "was being started". The sentences differ in their advice
            // (after a clock refusal a SECOND tap normally succeeds; after a
            // held-group superseded one the operator checks the match first), which is why
            // the toast and the card carry the sentence and not a bare failure.
            //
            // For a PICK the court is not "as it was": the running bout was
            // already sent back to the queue (deferAndStart), and this refusal
            // leaves the court idle with that bout waiting in the queue.
            const outcome = classifyStartOutcome(res);
            if (!outcome.ok) {
                if (mountedRef.current) setStartError(refusalFor(m, outcome.msg));
                if (showToast) showToast(outcome.msg, "error");
                return false;
            }
            // The start went out. A refusal that waited on it (or on the pick that
            // made it) reads this, whichever record it holds.
            started.landed = true;
            if (pick) pick.landed = true;
            // The started match is the court's live bout from here, before any
            // push or refetch shows it running (bc-strt; X3: an advance after a
            // finish or a decision too, not only a pick): the editor treats this
            // snapshot as running, so a point struck at once saves at once. A
            // start that answered direct came back after its refresh, which
            // already shows it; a pick pins itself (deferAndStart).
            if (!refused && mountedRef.current && (pick || writePending(res))) {
                setStartedFrom({ key, at: m.modifiedAt });
                if (!pick) setPickedKey(key);
            }
            if (writePending(res)) {
                res.settled.then((body) => {
                    // A discarded start (the operator took it off the held-writes
                    // list) reads as ok, but the server never started the match.
                    if (body && body.discarded) { unpin(); return; }
                    const late = classifyStartOutcome(body);
                    if (!late.ok) refusedLater(late.msg);
                }, () => {});
            }
            return true;
        } catch (e) {
            const msg = startFailureMessage(e);
            if (mountedRef.current) setStartError(refusalFor(m, msg));
            // A thrown refusal onEditScore (editMatchScore, admin.jsx) already
            // toasted is not toasted again: the toast is a single slot, so a
            // second one only restarts its timer. The card or row says it either way.
            if (showToast && !wasToasted(e)) showToast(msg, "error");
            return false;
        } finally {
            // Only the start that holds the ref clears it; the state follows the refs.
            if (startingRef.current && startingRef.current.key === key) startingRef.current = null;
            syncStartingKey();
        }
    };

    // pickMatch: run an upcoming match out of order. Rules:
    //   • Completed matches are never picked here — correcting them is a
    //     separate deliberate action (see correctMatch), so this returns early.
    //   • If a DIFFERENT bout is running, defer it: send it back to the queue
    //     so the court is never left with two running bouts. It keeps its
    //     score (operator ruling 2026-09-26, bc-sbq: switch, keeping the
    //     score), so a mistaken switch is undone with one tap on its Start,
    //     and it runs after the picked one.
    //   • A scheduled pick is started (through the eligibility gate); a pick
    //     that's already running just takes the panel.
    //   • While a correction is open nothing starts (operator ruling
    //     2026-09-27, bc-crpn): the tap is refused with a notice, BEFORE the
    //     defer above, so a refused start sends nothing back to the queue.
    //   • One start at a time (bc-aadv): while another match's start is still
    //     out, a Start of a different scheduled match is refused with a notice
    //     on its row, also BEFORE the defer, so the running bout is not sent
    //     back for a start that never happens. A repeat for the match already
    //     being started is silent: it is on its way.
    const pickMatch = async (m, asked = false) => {
        if (!m || m.status === "completed") return;
        if (openCorrection) {
            refuseTap(m, "correcting");
            return;
        }
        const inFlight = startingRef.current || pickingRef.current;
        if (inFlight && m.status === "scheduled") {
            if (inFlight.key !== matchKey(m)) refuseTap(m, "starting");
            return;
        }
        // A pick that puts another match in the editor asks first over its
        // unsaved entries (R1). The court may move while the operator answers
        // (another device finishes the running bout, a start lands), so the
        // pick is judged again from the top on the court as it is then, by
        // the latest render's pickMatch, never on this tap's copy (stage 5
        // review 9, m3).
        if (!asked && selectedMatch && matchKey(selectedMatch) !== matchKey(m) && holdsUnsaved(editorUnsavedRef)) {
            if (!(await leaveEditorConfirmed())) return;
            const latest = latestRef.current;
            const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
            if (fresh) await latest.pickMatch(fresh, true);
            return;
        }
        setRefusedTap(null);
        // A scheduled pick is a start in flight from here, before the defer below
        // awaits: a second Start tapped during that wait is refused, and does not
        // send the running bout back a second time.
        const pick = m.status === "scheduled" ? { key: matchKey(m), match: m } : null;
        if (pick) {
            pickingRef.current = pick;
            syncStartingKey();
        }
        try {
            await deferAndStart(m, pick);
        } finally {
            if (pick && pickingRef.current === pick) pickingRef.current = null;
            syncStartingKey();
        }
    };

    // A Start match tap: the bounce of the previous Start tap is ignored
    // (startTapRef, bc-sdtp), anything else goes to pickMatch.
    const tapStart = (m, ev) => {
        if (!acceptTap(startTapRef, ev)) return;
        pickMatch(m);
    };

    // bc-stpf (operator ruling 2026-10-12, option A): before a pick sends the
    // running bout `cur` back, ask the server whether the picked match `m`
    // would start once it has gone (API.checkStart, at most
    // START_CHECK_TIMEOUT_MS; pickMatch's pickingRef holds the pick, so the
    // button reads "Starting…" and a second Start is refused as in flight
    // meanwhile). True goes on with today's send back and start; false leaves
    // the court as it is. A refusal is stored on the picked match and toasted
    // once, and the running bout keeps running with its score. With no answer,
    // or when the device already knows an answer cannot be trusted (offline,
    // or writes of the pick's competition not sent yet that move a match or a
    // bar, API.startCheckUntrusted), the operator is asked
    // (Q1, 2026-10-27: the court must keep working without the server).
    // Console stubs without the check keep today's path.
    const startMayFollowSendBack = async (m, cur, pick) => {
        const api = window.API;
        if (typeof api.checkStart !== "function") return true;
        const untrusted = typeof api.startCheckUntrusted === "function" && api.startCheckUntrusted(m.compId);
        const answer = untrusted ? { unknown: true } : await api.checkStart(m.compId, m.id, court, password);
        if (!mountedRef.current) return false;
        if (answer && answer.ok) return true;
        if (answer && answer.refused) {
            const msg = startFailureMessage(answer.refused);
            if (pick) pick.refused = true;
            setStartError(refusalFor(m, msg));
            if (showToast) showToast(msg, "error");
            return false;
        }
        return askStartWithoutCheck(cur, m);
    };
    // The defer of the running bout and the start of the picked one, after
    // pickMatch's guards and its pickingRef entry (see pickMatch).
    const deferAndStart = async (m, pick) => {
        let cur = running[0] || null;
        const isSame = cur && matchKey(cur) === matchKey(m);
        // The queue key of the send back the start must follow, if any.
        let after = null;
        if (cur && !isSame) {
            // Send the current bout back to the queue, otherwise the pick below
            // would leave the court with two running bouts. Route through the
            // dedicated revert-to-queue endpoint (not a scheduled /score write):
            // it keeps the bout's score server-side AND skips the StartMatchTx
            // eligibility gate, so a stale cross-match ineligibility can't
            // silently 409 the defer. If the revert fails we surface a toast and
            // must NOT start the pick.
            if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
            // bc-stpf: a start the server would refuse leaves the running bout
            // running, so the check comes before anything is sent back.
            if (m.status === "scheduled") {
                if (!(await startMayFollowSendBack(m, cur, pick))) return;
                if (!mountedRef.current) return;
                // The check can wait on the operator for as long as they take
                // to answer (askStartWithoutCheck), and the court may move
                // meanwhile (another device finishes the running bout, or
                // starts another): the bout sent back and the match started
                // are the court's as it is now, by the latest render, never
                // this tap's copies, as pickMatch does after its own question.
                // A court that changed is judged again from the top.
                const latest = latestRef.current;
                const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
                const freshCur = latest.running[0] || null;
                if (!fresh || fresh.status !== "scheduled" || !freshCur || matchKey(freshCur) !== matchKey(cur)) {
                    if (pick && pickingRef.current === pick) pickingRef.current = null;
                    syncStartingKey();
                    if (fresh) await latest.pickMatch(fresh, true);
                    return;
                }
                cur = freshCur;
                m = fresh;
            }
            if (!mountedRef.current) return;
            // bc-evts (the governing rule): the send back is the outbox's and
            // nothing waits for its answer. The pick's start is queued right
            // after it, naming it (`after`), so the outbox sends the start only
            // once the send back has settled: this device never asks the
            // server to start a bout while its own send back of the running
            // one is still out. A send back the server refuses is said here
            // (claimed, so the queue raises no second alert), and the start
            // then goes and the server judges it.
            const deferFailed = (e) => {
                if (showToast) showToast((e && e.message) || "Could not defer the current bout", "error");
                // Claimed only when said (R2): with no toast it is the outbox's.
                return !!showToast;
            };
            try {
                const sent = await window.API.revertMatchToQueue(cur.compId, cur.id, password, { onRefusal: deferFailed, seen: cur.scoreVersion, match: cur });
                after = writeQueueKey(sent);
                // Shown queued at once, as the Send back confirm's is.
                if (mountedRef.current) markSentBackHere(matchKey(cur), sent);
            } catch (e) {
                deferFailed(e);
                return;
            }
            if (!mountedRef.current) return;
        }
        if (m.status === "scheduled") {
            // A start that went out pins the panel and its started snapshot
            // itself (startMatch, bc-strt).
            const ok = await startMatch(m, { pick, after });
            // A start refused before this line (pick.refused) left nothing to pin.
            if (!ok || !mountedRef.current || (pick && pick.refused)) return;
        }
        setPickedKey(matchKey(m));
    };

    // correctMatch: open a COMPLETED match to correct it in place, mirroring the
    // Scores page's "Correct" button. The court operator often spots a wrong
    // score after the match closed (frequently once the court is otherwise
    // done), so they must be able to fix it here rather than leaving for the
    // competition admin view. It is REFUSED while a bout is live on this court
    // (operator ruling 2026-09-27, bc-crpn: the console never holds a running
    // match and an open correction together): the tap gets a notice naming
    // the bout, and the operator finishes it or sends it back to the queue
    // first. A correction never defers or reverts the live match. The
    // editor then offers Reopen (kachinuki: back to running, keep the bout log;
    // a withdrawal-decided match: Clear withdrawal and reopen) or a direct
    // score re-write (other formats), same as the Scores page. A reopen makes
    // the match the court's live bout and ends the correction (the effect
    // beside correctingMatch).
    const correctMatch = async (m, asked = false) => {
        if (!m || m.status !== "completed") return;
        if (liveBout) {
            refuseTap(m, "running");
            return;
        }
        // Judged again after the ask on the court as it is then (m3, as pickMatch).
        if (!asked && selectedMatch && matchKey(selectedMatch) !== matchKey(m) && holdsUnsaved(editorUnsavedRef)) {
            if (!(await leaveEditorConfirmed())) return;
            const latest = latestRef.current;
            const fresh = latest.allMatches.find((x) => matchKey(x) === matchKey(m));
            if (fresh) latest.correctMatch(fresh, true);
            return;
        }
        setRefusedTap(null);
        setCorrectingKey(matchKey(m));
    };
    // Cancel the correction without saving and return to the court's ordinary
    // view. Only offered on a COMPLETED correction: once it is reopened it is
    // the live bout, finished via End match / Finish or sent back to the
    // queue like any other.
    const stopCorrecting = async () => {
        if (holdsUnsaved(editorUnsavedRef) && !(await leaveEditorConfirmed())) return;
        setCorrectingKey(null);
    };
    // End the correction of the match keyed `key` only if it is still the one
    // open: a write's answer may arrive after another correction was opened.
    const endCorrectionOf = (key) => { setCorrectingKey((k) => (k === key ? null : k)); };
    latestRef.current = { pickMatch, correctMatch, allMatches, running };

    // Call to court: optional. Broadcasts a tournament announcement so the
    // competitors (and anyone watching the public app) are notified they're
    // being summoned to this shiaijo. It does NOT start the match; Start is
    // always available on its own.
    const callToCourt = async (m, ev) => {
        // bc-cdbl: double tap guard. acceptTap returns false for a bounce and
        // true otherwise, stamping the tap as it goes.
        if (!acceptTap(callTapRef, ev, matchKey(m))) return;
        if (!window.API || typeof window.API.sendAnnouncement !== "function") return;
        const a = (m.sideA && m.sideA.name) || "Aka";
        const b = (m.sideB && m.sideB.name) || "Shiro";
        const msg = `Now calling ${b} and ${a} to Shiaijo ${court}.`.slice(0, 200);
        setCallingKey(matchKey(m));
        // The call already up for this match, if any: { id, msg }, where id is a
        // promise of its announcement id. It comes down only once the new call is
        // answered (below), so a send that fails leaves it up. The server replaces
        // only an announcement with identical text, so a call whose text changed
        // (a side's name resolved) would otherwise leave the earlier banner up
        // until it expired (bc-cdbl).
        const earlier = callIdsRef.current.get(matchKey(m)) || null;
        try {
            const sent = window.API.sendAnnouncement(msg, 5, password);
            const key = matchKey(m);
            // Keep the newest call per match, with its text, so starting the match
            // can withdraw it (bc-cdbl). The id is kept as a promise: a start that
            // lands before the answer must still withdraw the call once its id
            // arrives. If the send fails, the earlier call stays up, and so does
            // its entry.
            const entry = {
                compId: m.compId,
                matchId: m.id,
                msg,
                id: sent.then((ann) => (ann && ann.id) || null, () => (earlier ? earlier.id : null)),
            };
            callIdsRef.current.set(key, entry);
            sent.catch(() => {
                if (callIdsRef.current.get(key) !== entry) return;
                if (earlier) callIdsRef.current.set(key, earlier);
                else callIdsRef.current.delete(key);
            });
            // The earlier call comes down once this one is answered, but only when
            // its text differs. An identical text already replaced it on the server
            // (AnnouncementStore.Add), so a DELETE of it would 404; a failed send
            // leaves it up (its id comes back unchanged, so nothing is taken down).
            if (earlier && earlier.msg !== msg) {
                takeDown(Promise.all([earlier.id, entry.id]).then(([oldId, newId]) => (oldId !== newId ? oldId : null)));
            }
            await sent;
            if (!mountedRef.current) return;
            setCalledKey(matchKey(m));
            if (showToast) showToast(`Called ${b} and ${a} to Shiaijo ${court}`);
        } catch (e) {
            if (showToast) showToast((e && e.message) || "Could not send the call announcement", "error");
        } finally {
            if (mountedRef.current) setCallingKey(null);
        }
    };

    // withdrawCall: take this console's court call for the match keyed `key`
    // back down once that match has left scheduled (bc-cdbl). Fire
    // and forget: a failed delete is logged, never allowed to fail the start,
    // and the 5-minute expiry still clears the banner.
    const withdrawCall = (key) => {
        const entry = callIdsRef.current.get(key);
        if (!entry) return;
        callIdsRef.current.delete(key);
        takeDown(entry.id);
    };
    withdrawCallRef.current = withdrawCall;
    // takeDown: delete the announcement whose id `pending` resolves to (none
    // when it resolves to null). Fire and forget, shared by withdrawCall and the
    // earlier call callToCourt replaces.
    const takeDown = (pending) => {
        pending
            .then((id) => {
                if (!id || !window.API || typeof window.API.deleteAnnouncement !== "function") return;
                return window.API.deleteAnnouncement(id, password);
            })
            .catch((e) => console.warn("Could not withdraw the court call", e));
    };

    // A court call comes down once its match has left scheduled, however that
    // happened (started here, started elsewhere, closed by a decision), once the
    // match shows on another court, whatever its status, or once the loaded
    // court feed no longer holds the match (its competition dropped from this
    // court); a key no list holds is left alone until the court feed has loaded.
    useEffectSh(() => {
        if (callIdsRef.current.size === 0) return;
        const feedMatches = window.tournamentMatches({ competitions: courtCompetitions }) || [];
        for (const [key, entry] of callIdsRef.current.entries()) {
            const m = courtMatchesRaw.find((x) => matchKey(x) === key);
            if (m && m.status !== "scheduled") {
                withdrawCall(key);
                continue;
            }
            const moved = feedMatches.find((x) => matchKey(x) === key);
            // A court move still queued on this device alone does not withdraw
            // the call, however the feed came by it (a patch or a refetch
            // overlaid with the queue): only the server's court may.
            if (moved && (moved.court || "") !== (court || "")
                && !(window.API && window.API.courtMoveQueued(moved.compId, moved.id))) {
                withdrawCall(key);
                continue;
            }
            if (!m && !moved && courtComps !== null) {
                if (!(window.API && window.API.courtMoveQueued(entry.compId, entry.matchId))) {
                    withdrawCall(key);
                }
            }
        }
    }, [courtMatchesRaw, courtCompetitions, courtComps]);

    // moveMatch: reorder by swapping scheduledAt with the adjacent row.
    // direction: "up" (earlier) or "down" (later). Only scheduled matches
    // can be reordered; running/completed are stable.
    //
    // The swap is two separate updateMatchTime PUTs (no server-side swap API),
    // so it is NOT atomic. Each is an entry of its match's outbox, handed over at
    // once (bc-evts M5), so nothing here waits; when the server refuses one, the
    // other match is put back to its original time, so a partial failure doesn't
    // leave both rows with the same/incorrect time. Times are sent as strings (""
    // for an unset time): never null, which the Go handler's `scheduledAt string`
    // rejects (400).
    const moveMatch = async (m, direction) => {
        if (!window.API || typeof window.API.updateMatchTime !== "function") return;
        if (m.status === "running" || m.status === "completed") return;
        // Reorder WITHIN the filtered (selected-competition) queue: the same list
        // the rows render and compute first/last against. Swapping against the full
        // court list would exchange ScheduledAt with a hidden match from another
        // competition, silently reordering a queue the operator can't even see.
        const idx = filteredScheduled.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return;
        const neighbour = direction === "up" ? filteredScheduled[idx - 1] : filteredScheduled[idx + 1];
        if (!neighbour) return; // already first/last
        const label = (m.sideB && m.sideB.name) || (m.sideA && m.sideA.name) || "Match";
        const myTime = m.scheduledAt || "";
        const neighbourTime = neighbour.scheduledAt || "";
        // Says a refusal, and answers whether it was said (R2: an unsaid one is
        // left for the outbox's alert).
        const failed = (e) => {
            if (showToast) showToast((e && e.message) || "Could not reorder the match", "error");
            return !!showToast;
        };
        // A refusal of either half puts the OTHER match back to its own time, so
        // the queue isn't left half-swapped (a put-back the server refuses too
        // is toasted like any refusal).
        // Both halves show at once and a refused one shows its old time again
        // (`was`, R4); the put-back of the other half does the same.
        const putBack = (match, time, shown) => {
            Promise.resolve(window.API.updateMatchTime(match.compId, match.id, time, password, {
                onRefusal: (e) => failed(e), was: { scheduledAt: shown },
            })).catch(failed);
        };
        try {
            await window.API.updateMatchTime(m.compId, m.id, neighbourTime, password, {
                onRefusal: (e) => { putBack(neighbour, neighbourTime, myTime); return failed(e); },
                was: { scheduledAt: myTime },
            });
            await window.API.updateMatchTime(neighbour.compId, neighbour.id, myTime, password, {
                onRefusal: (e) => { putBack(m, myTime, neighbourTime); return failed(e); },
                was: { scheduledAt: neighbourTime },
            });
            if (showToast) showToast(`Moved ${label} ${direction === "up" ? "earlier" : "later"} in the queue`);
        } catch (e) {
            failed(e);
        }
    };

    // Revert a running bout back to the queue. Gated behind a confirm dialog
    // (same pattern as court reassignment) because discarding the running state
    // is disruptive: viewers drop the match from "Now" and it reappears in the
    // upcoming list. Always offered on a running bout, and the match keeps its
    // score (operator ruling 2026-09-26, bc-sbq): starting it again carries on
    // from it, and the operator removes a wrong mark themselves.
    // A pick is checking the start of another match (bc-stpf): it holds the
    // running bout it may send back, so a Send back or a court move tapped
    // now would race it. The tap is refused with a toast naming the pick.
    const refusedWhilePicking = () => {
        const pick = pickingRef.current;
        if (!pick) return false;
        if (showToast) showToast(courtChangeWhileStartingMessage({ label: scoreRowMatchName(pick.match) }), "error");
        return true;
    };
    const requestRevert = (m) => {
        if (refusedWhilePicking()) return;
        // The editor shows this match: its unsaved entries go with the send
        // back, and the confirm says so (R1, the editor's local Mark draw or
        // hantei pick of a running match included).
        const unsaved = editorUnsavedFor(m.compId, m.id);
        setPendingRevert({ compId: m.compId, matchId: m.id, match: m, label: confirmMatchLabel(m), seen: m.scoreVersion, at: m.modifiedAt, key: matchKey(m), unsaved });
    };
    // markSentBackHere: the outbox holds a send back of the match keyed `key`
    // (its pending answer `res`): the console shows it queued from the outbox
    // itself (withOwnStatusMove), and a start pin of the match (startedFrom)
    // goes with it: the bout is queued.
    const markSentBackHere = (key, res) => {
        if (!key || !writePending(res)) return;
        setStartedFrom((prev) => (prev && prev.key === key ? null : prev));
    };
    const confirmRevert = async (ev) => {
        if (!pendingRevert || reverting) return;
        if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
        // A bounce of the tap that turned this confirm into the warned one must
        // not confirm the warned version the operator has not read yet.
        if (!acceptTap(confirmTapRef, ev)) return;
        const { compId, matchId, label } = pendingRevert;
        const heldNow = editorUnsavedFor(compId, matchId);
        // The confirm's wording was decided when it opened. Entries made while
        // it was open (a scoring key reaching a console whose focus fell to
        // the body) were never named in it: show the confirm again WITH the
        // warning instead of dropping them silently (R1, stage 5 review 11,
        // minor 3).
        if (heldNow && !pendingRevert.unsaved) { setPendingRevert({ ...pendingRevert, unsaved: true }); return; }
        // The confirm said the editor's unsaved entries are discarded: they go
        // now, as Discard entries does everywhere (m1), not once the feed
        // shows the match queued.
        if (heldNow) discardUnsaved(editorUnsavedRef);
        setReverting(true);
        try {
            // bc-evts: the outbox's, never awaited: the console moves on at
            // once, and a refusal the server gives later is toasted.
            const res = await window.API.revertMatchToQueue(compId, matchId, password, {
                seen: pendingRevert.seen,
                match: pendingRevert.match,
                onRefusal: (e) => {
                    if (!mountedRef.current || !showToast) return false;
                    showToast((e && e.message) || "Could not send match back to queue", "error");
                    return true;
                },
            });
            if (!mountedRef.current) return;
            // Queued (offline included): the bout leaves the live slot and the
            // editor at once and shows in the queue, as a finish shows finished
            // (withOwnStatusMove); a refused or discarded send back puts it back.
            markSentBackHere(pendingRevert.key, res);
            if (showToast) showToast(`${label} sent back to queue`);
            setPendingRevert(null);
            setPickedKey(null);
            // Release a correction pin too. A reopened correction hands its key
            // to pickedKey (cleared above) once the refetch shows it running, but
            // one sent back before that refetch landed would otherwise re-pin the
            // panel to a now-scheduled match with no exit. Harmless (no-op) when
            // reverting a plain running bout.
            setCorrectingKey(null);
        } catch (e) {
            if (mountedRef.current) {
                if (showToast) showToast((e && e.message) || "Could not send match back to queue", "error");
            }
        } finally {
            if (mountedRef.current) setReverting(false);
        }
    };

    // Court reassignment is gated behind a confirm: a CourtPicker change opens a
    // confirmation rather than moving immediately, because the match leaves this
    // shiaijo for another court's queue. `onMoveCourt` (the real move) only runs
    // once the operator confirms.
    const requestMoveCourt = (compId, matchId, toCourt) => {
        if (refusedWhilePicking()) return;
        const mm = sorted.find((x) => x.compId === compId && x.id === matchId);
        setPendingMove({
            compId, matchId, to: toCourt,
            shown: mm,
            label: mm ? confirmMatchLabel(mm) : "this match",
            from: (mm && mm.court) || court,
            unsaved: editorUnsavedFor(compId, matchId),
        });
    };
    const confirmMoveCourt = async (ev) => {
        if (!pendingMove || movingCourt) return;
        // The bounce guard of confirmRevert, for the court move's Confirm.
        if (!acceptTap(confirmTapRef, ev)) return;
        const { compId, matchId, to } = pendingMove;
        const heldNow = editorUnsavedFor(compId, matchId);
        // Entries made while a confirm without the warning was open are asked
        // about, never dropped silently (see confirmRevert).
        if (heldNow && !pendingMove.unsaved) { setPendingMove({ ...pendingMove, unsaved: true }); return; }
        if (heldNow) discardUnsaved(editorUnsavedRef);
        setMovingCourt(true);
        try {
            // The host hands the move to the outbox and answers at once with
            // `{ landed }`, a promise of whether the move landed (the host
            // reports its own failure); an older host answers the boolean. A
            // move that did not land leaves this console's call for the match
            // up. The call names the court the match is leaving, so it comes
            // down once the move has landed (bc-cdbl).
            const handed = await onMoveCourt(compId, matchId, to, { confirmed: true, shown: pendingMove.shown });
            const withdraw = () => withdrawCall(matchKey({ compId, id: matchId }));
            if (handed === true) withdraw();
            else if (handed && handed.landed && typeof handed.landed.then === "function") {
                handed.landed.then((ok) => { if (ok === true) withdraw(); });
            }
            if (mountedRef.current) setPendingMove(null);
        } finally {
            if (mountedRef.current) setMovingCourt(false);
        }
    };

    // allDone: selected comp has no more matches to run on this court (AC4).
    // Scoped to the SELECTED competition, not the whole court: another comp may
    // still have matches here (the nudge banner surfaces that).
    // A pending placeholder final for this comp means the court is NOT done: it
    // has a downstream bout waiting on its feeders, so hold off the done state.
    // A match this console just started (liveBout: the started snapshot, which
    // has left the scheduled list) is not done either.
    const allDone = courtKnown && allMatches.length > 0 && running.length === 0 && !liveBout && filteredScheduled.length === 0 && filteredPending.length === 0;
    const selectedCompName = (courtsComps.find((c) => c.id === effectiveCompId) || {}).name || "";

    return (
        <div className="app">
            <AdminTopbar onLogout={leaving(onLogout)} onViewerMode={leaving(onViewerMode)} beforeLeave={leaveEditorConfirmed} tournament={tournament} hideRunningStrip />
            <div className="page page--wide">
                <Breadcrumbs items={[{ label: "Dashboard", onClick: leaving(onBack) }, { label: `Shiaijo ${court}` }]} />
                <div className="page-head page-head--oneline">
                    {/* Title, court switcher and Refresh on ONE row, no subtitle:
                        this is the court's working surface and every pixel above
                        the scorer pushes the live bout down the page. */}
                    <div className="page-head__title-row">
                        {courts.length > 1 && courtKnown ? (
                            // The page title doubles as the court switcher: clicking it
                            // opens a native court picker (transparent <select> overlay), so
                            // there's no separate, duplicate "Shiaijo A" control in the actions.
                            <div className="shiaijo-title-select">
                                <h1 className="page-head__title">
                                    Shiaijo {court}
                                    <span className="shiaijo-title-select__chevron" aria-hidden="true">▾</span>
                                </h1>
                                <select
                                    className="shiaijo-title-select__native"
                                    value={court}
                                    onChange={(e) => {
                                        // The switch asks first over unsaved entries (R1):
                                        // the picker shows this court until it goes.
                                        const to = e.target.value;
                                        e.target.value = court;
                                        leaving(onSwitchCourt)(to);
                                    }}
                                    aria-label="Switch court"
                                >
                                    {courts.map((c) => <option key={c} value={c}>Shiaijo {c}</option>)}
                                </select>
                            </div>
                        ) : (
                            <h1 className="page-head__title">Shiaijo {court}</h1>
                        )}
                        {courtKnown && typeof (window.API || {}).fetchCourtMatches === "function" && (
                            // Manual re-sync: recovers a court whose tablet fell behind
                            // (dropped SSE / flaky venue WiFi) so a stale queue: e.g. a
                            // final still showing placeholder feeders that already
                            // resolved server-side: is re-pulled on demand.
                            <button
                                type="button"
                                className="btn btn--sm btn--ghost shiaijo-refresh"
                                onClick={manualRefresh}
                                disabled={refreshing}
                                title="Re-pull this court's matches from the server (use if the queue looks out of date)"
                            >
                                {Icon && <Icon name="refresh" />}{" "}
                                {refreshing ? "Refreshing…" : "Refresh"}
                            </button>
                        )}
                    </div>
                    {courtsComps.length > 0 && (() => {
                        // Mirror of the Shiaijo title on the right: the competition being
                        // officiated gets the same display-title treatment (big name + navy
                        // chevron chip) so "which court" and "which competition" read as a
                        // matched pair. Falls back to a plain title when only one comp is
                        // on this court (no switch affordance needed).
                        const officiating = courtsComps.find(c => c.id === effectiveCompId) || courtsComps[0];
                        return (
                            <div className="page-head__actions">
                                <div className="shiaijo-officiating">
                                    {courtsComps.length === 1 ? (
                                        <h2 className="page-head__title shiaijo-officiating__name">{officiating.name}</h2>
                                    ) : (
                                        <div className="shiaijo-title-select shiaijo-title-select--right">
                                            <h2 className="page-head__title shiaijo-officiating__name">
                                                {officiating.name}
                                                <span className="shiaijo-title-select__chevron" aria-hidden="true">▾</span>
                                            </h2>
                                            <select
                                                className="shiaijo-title-select__native"
                                                value={effectiveCompId || ""}
                                                onChange={(e) => setSelectedCompId(e.target.value)}
                                                aria-label="Select competition to officiate"
                                            >
                                                {courtsComps.map(c => <option key={c.id} value={c.id}>{c.name}</option>)}
                                            </select>
                                        </div>
                                    )}
                                </div>
                            </div>
                        );
                    })()}
                </div>

                {!courtKnown && (
                    <div className="empty">
                        <h3>Unknown shiaijo "{court}"</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            This court isn't part of the tournament: it may have been renamed or removed.{" "}
                            <button type="button" onClick={leaving(onBack)} className="linklike">Back to dashboard</button>.
                        </p>
                        {/* The title-overlay court switcher is gated on courtKnown, so an
                            unknown court would otherwise strand the operator. Offer a plain
                            picker here to jump to a valid court without leaving the page. */}
                        {courts.length > 0 && (
                            <label className="empty__action">
                                Go to a court:{" "}
                                <select className="input" value="" onChange={(e) => { if (e.target.value) leaving(onSwitchCourt)(e.target.value); }} aria-label="Switch to a valid court">
                                    <option value="" disabled>Choose…</option>
                                    {courts.map((c) => <option key={c} value={c}>Shiaijo {c}</option>)}
                                </select>
                            </label>
                        )}
                    </div>
                )}

                {courtKnown && allMatches.length === 0 && pendingPlaceholder.length === 0 && (
                    <div className="empty">
                        <h3>No matches on this court</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            Matches appear here once assigned to Shiaijo {court}. Assign courts in the competition settings or the Schedule page.
                        </p>
                    </div>
                )}

                {courtKnown && (allMatches.length > 0 || pendingPlaceholder.length > 0) && (
                    <div className={`shiaijo${queueOpen ? "" : " shiaijo--queue-collapsed"}`}>
                        {/* ── Queue (left) ───────────────────────────── */}
                        {/* Accordion: folded, the column becomes a narrow rail in the
                            same place that reopens it, so the queue is never out of
                            reach while the scorer takes the width. This rail button
                            mounts while the header "Hide" button below stays mounted
                            but hidden (its .shiaijo__queue parent goes display: none
                            via .shiaijo--queue-collapsed); the toggleQueue/queueOpen
                            effect above moves focus here after an operator-driven
                            collapse. */}
                        {!queueOpen && (
                            <button
                                type="button"
                                ref={queueShowBtnRef}
                                className="shiaijo-queue-rail"
                                onClick={toggleQueue}
                                aria-expanded={false}
                                aria-controls="shiaijo-queue"
                                data-testid="shiaijo-queue-show"
                            >
                                <span className="shiaijo-queue-rail__label">Show queue</span>
                                <span aria-hidden="true">▸</span>
                            </button>
                        )}
                        <div className="shiaijo__queue" id="shiaijo-queue">
                            {/* This header button stays mounted throughout; only hidden
                                while collapsed (the queue's display: none). Expanding
                                unmounts the rail button above; the toggleQueue/queueOpen
                                effect moves focus here after an operator-driven expand. */}
                            <button
                                type="button"
                                ref={queueHideBtnRef}
                                className="section-title shiaijo-queue__head"
                                onClick={toggleQueue}
                                aria-expanded={true}
                                aria-controls="shiaijo-queue"
                                data-testid="shiaijo-queue-hide"
                            >
                                <span>Queue</span>
                                <span className="shiaijo-queue__head-action"><span aria-hidden="true">◂</span> Hide</span>
                            </button>
                            {filteredScheduled.length === 0 && filteredPending.length === 0 && filteredCompleted.length === 0 && (
                                <p className="shiaijo-queue__empty">Nothing else queued on this court.</p>
                            )}
                            {upNext && (
                                <div className="shiaijo-upnext">
                                    <div className="section-title">Up next</div>
                                    <div className={`shiaijo-upnext__card ${calledKey === matchKey(upNext) ? "is-called" : ""}`}>
                                        <div className="shiaijo-upnext__time">
                                            {upNext.scheduledAt || "-"} · {upNext.compName}
                                            {shiaijoMatchNo(upNext)}
                                            {/* DH-only label: a tiebreaker ("-TB-") is also a rep bout
                                                (isSupplementaryBout), but it is NOT a daihyosen, so the "DH"
                                                tag gates on isPoolDaihyosenBout. Routing still uses
                                                isSupplementaryBout (see editorMatch above). */}
                                            {window.isPoolDaihyosenBout && window.isPoolDaihyosenBout(upNext.id) && (
                                                <span className="tag-badge" style={{ marginLeft: 6 }}>
                                                    {window.Term ? React.createElement(window.Term, { name: "daihyosen" }, "DH") : "DH"}
                                                </span>
                                            )}
                                        </div>
                                        <MatchSides m={upNext} large />
                                        <div className="shiaijo-upnext__actions">
                                            {/* Route through pickMatch (not startMatch) so starting the
                                                Up Next bout honours the same rules as the Upcoming
                                                "Start match" buttons: refused while a correction is open,
                                                and a running bout is sent back to the queue first,
                                                keeping its score (bc-sbq). With neither, it simply
                                                starts this one. */}
                                            <button type="button" className="btn btn--primary" disabled={startingKey === matchKey(upNext)} onClick={(ev) => tapStart(upNext, ev)}>
                                                {startingKey === matchKey(upNext) ? "Starting…" : "Start match"}
                                            </button>
                                            {isTeamMatch(upNext) && (
                                                <button type="button" className="btn btn--sm" onClick={() => setLineupKey(matchKey(upNext))}
                                                    title="Set the team lineup before starting">
                                                    Enter lineup
                                                </button>
                                            )}
                                            {/* Optional: announce the call to spectators/competitors.
                                                Never required: Start match works on its own. */}
                                            {window.API && typeof window.API.sendAnnouncement === "function" && (
                                                <button type="button"
                                                    className="btn btn--sm"
                                                    disabled={callingKey === matchKey(upNext)}
                                                    onClick={(ev) => callToCourt(upNext, ev)}
                                                    title="Announce this match to spectators and competitors"
                                                >
                                                    {Icon && <Icon name="megaphone" />}{" "}
                                                    {callingKey === matchKey(upNext) ? "Calling…" : (calledKey === matchKey(upNext) ? "Call again" : "Call to court")}
                                                </button>
                                            )}
                                            {window.API && typeof window.API.updateMatchTime === "function" && filteredScheduled.length > 1 && (
                                                <button type="button" className="btn btn--sm btn--ghost" aria-label="Move down" onClick={() => moveMatch(upNext, "down")} title="Move this match later in the queue">↓</button>
                                            )}
                                        </div>
                                        {upNextNotice && <div className="shiaijo-upnext__error" role="alert">{upNextNotice}</div>}
                                        <div className="shiaijo-upnext__hint">
                                            {calledKey === matchKey(upNext)
                                                ? "Announced to spectators. Start the match when both are at the line."
                                                : "Start when both competitors are at the line. Call to court announces the match to spectators (optional)."}
                                        </div>
                                    </div>
                                </div>
                            )}

                            {/* No "Now" group: the running match is officiated in the
                                scoring panel on the right, so repeating it in the queue is
                                redundant. */}

                            {upcomingQueueMatches.length > 0 && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" subGroup matches={upcomingQueueMatches}
                                    courts={courts} onMoveCourt={requestMoveCourt}
                                    onMove={moveMatch} onEnterLineup={(m) => setLineupKey(matchKey(m))}
                                    onPick={tapStart} noticeFor={(m) => startNoticeFor(matchKey(m))}
                                    onCall={callToCourt} callingKey={callingKey} calledKey={calledKey} startingKey={startingKey}
                                    scheduled={filteredScheduled}
                                    password={password}
                                />
                            )}

                            {/* Later: scheduled knockout bouts still waiting on a
                                "Winner of rX-mY" feeder (mp-y3nk). Shown so a court
                                whose only remaining bout is a downstream final does
                                not read as an empty queue. Non-actionable: the sides
                                are placeholders, so no Start/Call/move affordances are
                                passed. Phase 3 adds an opt-in "resolve to run now". */}
                            {filteredPending.length > 0 && (
                                <div className="shiaijo-group shiaijo-pending">
                                    <div className="section-title">
                                        Later
                                        <span className="shiaijo-count" aria-label={`${filteredPending.length} waiting on earlier matches`}>{filteredPending.length}</span>
                                    </div>
                                    <div className="shiaijo-pending__hint" style={{ fontSize: 12, color: "var(--ink-3)", marginBottom: 6 }}>
                                        {filteredPending.length === 1 ? "This bout starts" : "These bouts start"} once the earlier matches that feed {filteredPending.length === 1 ? "it" : "them"} are scored.
                                    </div>
                                    <div className="score-editor__list">
                                        {filteredPending.map((m) => {
                                            const bronze = m.id === BRONZE_MATCH_ID;
                                            return (
                                                <ShiaijoQueueRow key={matchKey(m)} m={m} pending
                                                    slotLabel={slotLabelFor(m.compId)}
                                                    emptyNames={bronze ? bronzeLabelsFor(m.compId) : undefined} />
                                            );
                                        })}
                                    </div>
                                </div>
                            )}

                            {filteredCompleted.length > 0 && (
                                <div className="shiaijo-completed">
                                    {/* Completed matches stay expanded: this is the operator's
                                        running record of what's been played on this court/pool today,
                                        so it must not be hidden behind a collapse toggle. To keep the
                                        live queue above the fold on a full day, only the most recent
                                        COMPLETED_PREVIEW show by default; "Show all N" reveals the rest.
                                        The tail IS the most recent: partitionShiaijoMatches orders
                                        completed by play time, so this stays a contiguous window and
                                        "Show all" extends the list rather than inserting rows into the
                                        middle of it (operator ruling 2026-09-19). */}
                                    <div className="section-title">
                                        Completed <span className="shiaijo-count" aria-label={`${filteredCompleted.length} matches`}>{filteredCompleted.length}</span>
                                    </div>
                                    <ShiaijoQueueGroup
                                        matches={completedShown}
                                        courts={courts} onMoveCourt={requestMoveCourt} onCorrect={correctMatch}
                                        noticeFor={(m) => refusalNotice(matchKey(m))}
                                    />
                                    {filteredCompleted.length > COMPLETED_PREVIEW && (
                                        <button
                                            type="button"
                                            className="linklike shiaijo-completed__more"
                                            aria-expanded={showAllCompleted}
                                            onClick={() => setShowAllCompleted((v) => !v)}
                                        >
                                            {showAllCompleted
                                                ? "Show fewer"
                                                : `Show all ${filteredCompleted.length}`}
                                        </button>
                                    )}
                                </div>
                            )}
                        </div>

                        {/* ── Scoring / lineup + context (right) ──────── */}
                        <div className="shiaijo__main">
                            {nudgeBanner && nudgeBanner.entries.map((e) => {
                                const matches = `${e.count} ${e.count === 1 ? "match" : "matches"}`;
                                const text = nudgeBanner.alsoWaiting
                                    ? `${e.count} ${e.name} ${e.count === 1 ? "match" : "matches"} also waiting on this court.`
                                    : `Switch to ${e.name}: ${matches} waiting on this court.`;
                                return (
                                    <button
                                        type="button"
                                        key={e.id}
                                        className={`alert shiaijo-nudge ${nudgeBanner.alsoWaiting ? "shiaijo-nudge--also" : "alert--warn"}`}
                                        data-testid="shiaijo-nudge"
                                        onClick={() => setSelectedCompId(e.id)}
                                        // The name carries the visible text (WCAG 2.5.3) and,
                                        // on the quiet line, what the tap does.
                                        aria-label={nudgeBanner.alsoWaiting ? `${text} Switch to ${e.name}` : text}
                                    >
                                        {!nudgeBanner.alsoWaiting && (
                                            <span className="shiaijo-nudge__icon" aria-hidden="true">{Icon ? <Icon name="alert-circle" size={15} /> : "⚠"}</span>
                                        )}
                                        <span className="shiaijo-nudge__text">{text}</span>
                                        <span className="shiaijo-nudge__cta" aria-hidden="true">Switch →</span>
                                    </button>
                                );
                            })}
                            {allDone && !correctingMatch && (
                                <div className="empty">
                                    <h3>{selectedCompName ? `${selectedCompName} is complete on Shiaijo ${court}` : `All matches complete on Shiaijo ${court}`}</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        {filteredCompleted.length} match{filteredCompleted.length === 1 ? "" : "es"} scored.{" "}
                                        {nudgeBanner ? "Another competition still has matches on this court: switch above." : "Nothing left to run on this court."}
                                    </p>
                                </div>
                            )}

                            {/* While a correction holds the panel the running bout has no
                                other home, so running[0] is listed too (a match started
                                from another screen mid-correction, bc-crpn). */}
                            {!allDone && alsoRunning.length > 0 && (
                                <div className="shiaijo-also-running" role="alert">
                                    <div className="shiaijo-also-running__title">Another bout is running on Shiaijo {court}</div>
                                    <ul className="shiaijo-also-running__list">
                                        {alsoRunning.map((m) => (
                                            <li key={matchKey(m)}>
                                                {m.sideB?.name || "?"} vs {m.sideA?.name || "?"}
                                                {m.compName ? <span className="shiaijo-also-running__comp"> · {m.compName}</span> : null}
                                            </li>
                                        ))}
                                    </ul>
                                    <div className="shiaijo-also-running__hint">
                                        {openCorrection
                                            ? "Save or cancel the correction (Back to court) to score it here."
                                            : "A court runs one bout at a time. Score or correct these from their competition's admin view."}
                                    </div>
                                </div>
                            )}

                            {/* correctingMatch too: correcting the court's last
                                bout makes allDone true, and the pin must still
                                hold the editor open. */}
                            {(!allDone || correctingMatch) && selectedMatch && (
                                <ScoreEditorModal
                                    hostUnsavedRef={editorUnsavedRef}
                                    // No subResults.length: see the same key in
                                    // admin_competition_bracket.jsx. Remounting
                                    // on every bout-log change threw away the
                                    // operator's unsaved scores, and the stale-
                                    // board problem it worked around is gone.
                                    key={matchKey(selectedMatch)}
                                    variant="inline"
                                    match={editorMatch}
                                    // The inline editor never closes itself. Its
                                    // callers are a landed decision on a correction
                                    // (which ends it) and the daihyosen add/remove
                                    // on a running match, where nothing is open: a
                                    // no-op.
                                    // Both close only THIS match's correction: the answer can
                                    // arrive after the operator opened a correction on another
                                    // match, so the key is captured before the write and
                                    // compared against the current one (endCorrectionOf).
                                    onClose={() => endCorrectionOf(matchKey(selectedMatch))}
                                    started={isStartedSnapshot(selectedMatch)}
                                    canClose={false}
                                    onSubmit={async (patch, opts) => {
                                        const key = matchKey(selectedMatch);
                                        try {
                                            // opts: the editor's own claim on a refusal (onRefusal).
                                            const res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch, opts);
                                            // A landed Save correction ends the correction (a
                                            // write that did not land keeps it open, with the
                                            // editor's not-saved banner).
                                            if (res && patch && patch.status === "completed" && !writeKeepsEditorOpen(patch, res) && mountedRef.current) {
                                                endCorrectionOf(key);
                                            }
                                            // Optimistically advance the local bracket so an offline court
                                            // sees the next match resolve (reconciled by refetch online).
                                            // NOT on a supersede: that winner was discarded in favour of a
                                            // newer stored one, so advancing would show the operator their
                                            // own dropped winner on the very queue the banner below tells
                                            // them to go and check. A queued write still advances (it
                                            // reconciles on reconnect); see writeWasSuperseded.
                                            if (!writeWasSuperseded(res)) maybeAdvanceLocal(selectedMatch, patch, res);
                                            // A finish the outbox holds shows at once, read from the
                                            // outbox (withOwnStatusMove, X3, M4).
                                            // What the write came back with, so the editor shows the
                                            // not-saved banner for one that did not land (queued, F5; or
                                            // superseded by a newer stored result, bc-lww1) and tells a
                                            // landed start from a refused one, which threw and returns
                                            // nothing.
                                            return res;
                                        }
                                        catch (_e) { /* surfaced via toast */ }
                                    }}
                                    onSubmitAndNext={async (patch) => {
                                        const next = nextActiveAfter(selectedMatch);
                                        let res;
                                        try {
                                            res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            // See the onSubmit handler above: a superseded write must not
                                            // advance the local bracket on a winner the server discarded.
                                            if (!writeWasSuperseded(res)) maybeAdvanceLocal(selectedMatch, patch, res);
                                            if (!mountedRef.current) return res;
                                            // A write that did not land must NOT advance: the match is
                                            // still running and still holds the court, so starting the
                                            // next one here just earns a court_busy on top of the
                                            // editor's not-saved banner (F5 queued; bc-lww1 superseded).
                                            if (writeDidNotLand(res)) return res;
                                            // Finish + start the next scheduled match, which then
                                            // becomes the running match the panel shows.
                                            if (next && next.status === "scheduled") {
                                                // Queued after the finish (X1): the server never
                                                // sees this start while the finish is still out.
                                                await startMatch(next, { after: writeQueueKey(res) });
                                            }
                                        } catch (_e) { /* keep panel */ }
                                        // The finish write's answer, as the editor needs it (a partial
                                        // apply's note, a refused write's banner); undefined when the
                                        // write threw.
                                        return res;
                                    }}
                                    onAfterDecision={async (result) => {
                                        // A fusenpai/hantei decision already persisted the bout via
                                        // the /decision POST: no score PUT here. It still resolves a
                                        // winner, so offline we must advance the LOCAL bracket the same
                                        // way the score path does (mp-y3nk), else a decision-completed
                                        // bout leaves the next match on placeholders until Refresh/SSE.
                                        // A decision the server refuses or the operator discards later
                                        // takes the advance back (advanceLocalUntilRefused).
                                        advanceLocalUntilRefused(selectedMatch, _bracketSideName(result && result.winner), result);
                                        // A decision the outbox holds shows at once, read from the
                                        // outbox (withOwnStatusMove, X3, M4).
                                        // Then start the next scheduled match so the panel advances
                                        // (mirrors onSubmitAndNext), passing over the matches of a
                                        // competitor a withdrawal or no-show has just barred.
                                        const next = nextActiveAfter(selectedMatch, sideBarredByDecision(result, selectedMatch));
                                        if (next && next.status === "scheduled") {
                                            // Queued after the decision (X1).
                                            await startMatch(next, { after: writeQueueKey(result) });
                                        }
                                    }}
                                    password={password}
                                />
                            )}

                            {correctingMatch && correctingMatch.status === "completed" && (
                                <div className="shiaijo-revert">
                                    <button
                                        type="button"
                                        className="btn btn--sm btn--ghost"
                                        onClick={stopCorrecting}
                                        title="Stop correcting this match without saving and return to the court's queue"
                                    >
                                        ← Back to court
                                    </button>
                                </div>
                            )}

                            {/* Exit for the LIVE running bout AND for a reopened
                                correction (completed -> Reopen -> running). NO
                                `!correctingMatch` guard: a reopened correction IS
                                the running bout on this court (it becomes the live
                                pick as soon as the refetch shows it running), so
                                "Send back to queue" is its exit - the twin of
                                "Back to court" above, which stopCorrecting
                                deliberately withholds while running (a stray Back
                                to court could strand a result-less bout behind
                                another running match). `status === "running"`
                                alone keeps this off a still-completed correction,
                                which shows "Back to court" instead. Offered
                                whatever has been scored: the match keeps its
                                score in the queue (bc-sbq). */}
                            {!allDone && selectedMatch && selectedMatch.status === "running" && window.API && typeof window.API.revertMatchToQueue === "function" && (
                                <div className="shiaijo-revert">
                                    <button
                                        type="button"
                                        className="btn btn--sm btn--ghost"
                                        onClick={() => requestRevert(selectedMatch)}
                                        title="Return this match to the queue without recording a result"
                                    >
                                        Send back to queue
                                    </button>
                                </div>
                            )}

                            {!allDone && !selectedMatch && (
                                <div className="empty shiaijo__placeholder">
                                    <h3>Ready when you are</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        {/* bc-cse: when every scheduled match here is barred there
                                            is no Up Next card at all (upNext skips a barred match on
                                            purpose), so telling the operator to use it points at
                                            something not on screen. Name the actual remedy instead:
                                            each barred row in the queue carries its own one-tap
                                            resolution (BarredMatchNotice). */}
                                        {!upNext && filteredScheduled.length > 0
                                            ? "Every scheduled match on this court is barred. Resolve a withdrawal in the queue to bring one back."
                                            : "Start the next match from the Up Next card to begin scoring on this court."}
                                    </p>
                                </div>
                            )}

                            {contextMatch && (
                                <ShiaijoContext
                                    match={contextMatch} competitions={courtCompetitions}
                                    court={court} nextPoolName={nextPoolName} tweaks={tweaks}
                                    lead={contextLead}
                                    open={contextOpen} onToggle={() => setContextOpen((v) => !v)}
                                />
                            )}
                        </div>
                    </div>
                )}
            </div>

            {pendingMove && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => !movingCourt && setPendingMove(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-move-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-move-title" className="shiaijo-move-confirm__title">
                            Move to Shiaijo {pendingMove.to}?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            {/* Only a waiting row offers a move here (ShiaijoQueueRow's
                                showActions), and a waiting match goes last there. */}
                            <strong>{pendingMove.label}</strong> leaves Shiaijo {pendingMove.from} and goes
                            last in the queue on Shiaijo {pendingMove.to}.
                            {pendingMove.unsaved ? ` ${UNSAVED_ENTRIES_DISCARDED}` : ""}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => setPendingMove(null)} disabled={movingCourt}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn--primary" onClick={confirmMoveCourt} disabled={movingCourt}>
                                {movingCourt ? "Moving…" : `Move to Shiaijo ${pendingMove.to}`}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {pendingRevert && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => !reverting && setPendingRevert(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-revert-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-revert-title" className="shiaijo-move-confirm__title">
                            Send back to queue?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            <strong>{pendingRevert.label}</strong> will be returned to the upcoming queue.
                            {pendingRevert.unsaved ? ` ${UNSAVED_ENTRIES_DISCARDED}` : ""} {SCORE_KEPT_SENTENCE}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => setPendingRevert(null)} disabled={reverting}>
                                Cancel
                            </button>
                            <button type="button" className="btn btn--primary" onClick={confirmRevert} disabled={reverting}>
                                {reverting ? "Sending…" : "Send back to queue"}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {pendingStartAsk && (
                <div className="modal-backdrop" ref={confirmOpenedRef} onClickCapture={confirmOnClickCapture}
                    onClick={() => answerStartAsk(false)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true" ref={confirmBoxRef}
                        aria-labelledby="shiaijo-start-ask-title" data-testid="start-ask" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-start-ask-title" className="shiaijo-move-confirm__title">
                            Start {pendingStartAsk.pickLabel}?
                        </h3>
                        <p className="shiaijo-move-confirm__body" aria-live="polite">
                            The app could not check with the server that <strong>{pendingStartAsk.pickLabel}</strong> can
                            start. <strong>{pendingStartAsk.curLabel}</strong> is still running: send it back to the
                            queue and start {pendingStartAsk.pickLabel}, or keep it running. {SCORE_KEPT_SENTENCE}
                        </p>
                        <div className="shiaijo-move-confirm__actions">
                            <button type="button" className="btn" onClick={() => answerStartAsk(false)}>
                                Keep it running
                            </button>
                            <button type="button" className="btn btn--primary"
                                onClick={(ev) => { if (acceptTap(confirmTapRef, ev)) answerStartAsk(true); }}>
                                Send back and start
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Pre-start lineup entry for a team match. Opens the dedicated
                per-match lineup panel: the per-position name pickers persist
                via putMatchLineup independent of scoring, so the operator can
                set the lineup and close without starting. */}
            {lineupMatch && window.MatchLineupPanel && (
                <window.MatchLineupPanel
                    key={`lineup:${matchKey(lineupMatch)}`}
                    match={lineupMatch}
                    tournament={{ competitions: courtCompetitions }}
                    password={password}
                    showToast={typeof showToast === "function" ? showToast : undefined}
                    onClose={() => setLineupKey(null)}
                />
            )}
        </div>
    );
}

// A queued match row: plain info (the court reassign and reorder controls
// work) but the row itself can't be opened in the scoring panel: you start a
// match from the Up Next card, not by clicking it. The console never shows a
// running match in these groups (the running bout is officiated in the scoring
// panel on the right), so only scheduled/completed states render here.
//
// `scheduled` is the filtered (selected-competition) scheduled list: the same
// list `moveMatch` reorders against: used to derive first/last position for
// disabling the ↑/↓ buttons. `onMove(m, direction)` swaps scheduledAt with the
// adjacent same-competition row via two updateMatchTime calls.
// Group an Upcoming slice for display so the operator sees what they'll be
// scoring: pool matches by pool ("Pool A", "Pool B"), knockout matches by round
// ("Final", "Round 16"). League is a single round-robin table and needs no
// grouping → returns null, and the caller renders a flat list. Groups keep
// first-appearance (i.e. scheduled-time) order; pools are seeded contiguously
// per court, so each group stays a contiguous time block.
export function groupQueueMatches(matches) {
    if (!matches.length || matches.every((m) => m.compFormat === "league")) return null;
    const order = [];
    const byKey = new Map();
    for (const m of matches) {
        let key, label;
        if (m.phase === "bracket") {
            // Key on the LABEL, not roundIndex: m.round is the EFFECTIVE round
            // (bracketRoundLabel), and one backend round can hold two of them
            // when a bye collapses a round, while two backend rounds can share
            // one. Keying on roundIndex therefore both put a quarterfinal under
            // a "Semifinals" heading and split one round name across two
            // identically-titled groups. Keying on the displayed string keeps
            // the invariant the operator relies on: the heading describes every
            // match under it. Same cross-competition behaviour as before, since
            // a shared roundIndex already merged those.
            key = "round:" + (m.round || "");
            label = m.round || "Knockout";
        } else if (m.phase === "pool") {
            key = "pool:" + (m.poolName || "");
            // Swiss rounds piggyback on the pool pipeline with a synthetic
            // "Swiss-RN" pool name (mp-pglr); show the operator "Round N".
            label = m.compFormat === "swiss"
                ? swissRoundLabel(m.poolName)
                : (m.poolName || "Pool");
        } else {
            key = "other";
            label = null;
        }
        if (!byKey.has(key)) { byKey.set(key, { key, label, matches: [] }); order.push(key); }
        byKey.get(key).matches.push(m);
    }
    return order.map((k) => byKey.get(k));
}

function ShiaijoQueueGroup({ label, matches, subGroup, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, noticeFor, onCall, callingKey, calledKey, startingKey, password }) {
    const renderRow = (m) => (
        <ShiaijoQueueRow
            key={matchKey(m)} m={m}
            notice={noticeFor ? noticeFor(m) : null}
            scheduled={scheduled}
            courts={courts} onMoveCourt={onMoveCourt} onMove={onMove} onEnterLineup={onEnterLineup} onPick={onPick} onCorrect={onCorrect}
            onCall={onCall} callingKey={callingKey} calledKey={calledKey} startingKey={startingKey}
            password={password}
        />
    );
    const groups = subGroup ? groupQueueMatches(matches) : null;
    return (
        <div className="shiaijo-group">
            {label && <div className="section-title">{label}</div>}
            {groups
                ? groups.map((g) => (
                    <div className="shiaijo-subgroup" key={g.key}>
                        {g.label && <div className="shiaijo-subgroup__title">{g.label}</div>}
                        <div className="score-editor__list">
                            {g.matches.map(renderRow)}
                        </div>
                    </div>
                ))
                : (
                    <div className="score-editor__list">
                        {matches.map(renderRow)}
                    </div>
                )}
        </div>
    );
}

export function ShiaijoQueueRow({ m, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, onCall, callingKey, calledKey, startingKey, pending, slotLabel, emptyNames, password, notice }) {
    const isComplete = m.status === "completed";
    // bc-cse: a scheduled match a competitor is barred from. `pending`
    // placeholder finals are excluded on purpose: their sides are still
    // feeder placeholders, not a resolved competitor the stamp could name.
    const barred = !pending && isBarredMatch(m);
    // Slot text through the one shared rule (bracket.jsx). Actionable rows never
    // hold a placeholder (hasBothSides filtered them out), so this only ever
    // changes the `pending` "Later" rows; the fallbacks keep any other caller
    // from printing a raw "Winner of rX-mY" if it ever does.
    const slotName = slotLabel || window.slotDisplayName || ((n) => n);
    // `emptyNames` ({a, b}) names an EMPTY side: only the 3rd-place match's
    // "Later" row passes it (bronzeSlotLabels), since its sides have no slot value.
    const aName = slotName(m.sideA?.name || "", (m.feeders || [])[0]) || (emptyNames ? emptyNames.a : "");
    const bName = slotName(m.sideB?.name || "", (m.feeders || [])[1]) || (emptyNames ? emptyNames.b : "");
    const scoreCell = shiaijoScoreCell(m);
    // bc-tmwn: for completed rows, determine which side won and get team marks.
    const winnerSide = isComplete ? winnerSideLR(m) : null;
    // teamMatchMarks itself returns no marks for a non-team row or one not completed.
    const { shiro: teamShiroMark, aka: teamAkaMark } = teamMatchMarks(m, isTeamMatch(m));
    // Derive position in the full scheduled list to know when to disable ↑/↓.
    // `scheduled` is the court's complete scheduled array (including Up Next);
    // the row may be in the Upcoming slice but we disable based on absolute pos.
    const scheduledIdx = onMove ? (scheduled || []).findIndex((x) => matchKey(x) === matchKey(m)) : -1;
    const isFirst = scheduledIdx === 0;
    const isLast = scheduledIdx >= 0 && scheduledIdx === (scheduled || []).length - 1;
    // Stacked card layout (not the wide score-editor grid): the matchup gets a
    // full-width line so names stay READABLE in the narrow queue column; the
    // controls live on their own line below so they never crowd the names.
    // A `pending` placeholder final is NEVER actionable (its sides are still
    // "Winner of rX-mY" feeders): suppress every control regardless of which
    // handlers were passed, so the row can only ever be informational.
    const showActions = !pending && m.status === "scheduled" && (
        (onMoveCourt && courts.length > 1) ||
        (onEnterLineup && isTeamMatch(m)) || onPick || onMove
    );
    return (
        <div className={`shiaijo-qrow ${isComplete ? "shiaijo-qrow--complete" : ""}`}>
            <div className="shiaijo-qrow__top">
                <span className="shiaijo-qrow__time">
                    {m.scheduledAt || "-"} · {m.compName}
                    {shiaijoMatchNo(m)}
                    {/* DH-only label (not "-TB-"): see the Up Next card note above. */}
                    {window.isPoolDaihyosenBout && window.isPoolDaihyosenBout(m.id) && (
                        <span className="tag-badge" style={{ marginLeft: 4 }}>
                            {window.Term ? React.createElement(window.Term, { name: "daihyosen" }, "DH") : "DH"}
                        </span>
                    )}
                </span>
                {/* No "Final" for a completed row (operator ruling 2026-09-20,
                    bc-sccl): "Final" names the LAST MATCH OF A KNOCKOUT, never a
                    finished bout. The row already says it two other ways -- the
                    score on its own line below and the "Completed" group heading
                    above -- plus a "Correct" button when the console wires
                    onCorrect (there is no "Score" button on this row; that
                    contrast belongs to the scores list). "Waiting" stays: a
                    blocked row has no other marker. */}
                <span className="shiaijo-qrow__state">
                    {pending && <span className="shiaijo-qrow__waiting">Waiting</span>}
                </span>
            </div>
            <div className="shiaijo-qrow__match">
                {/* Side by CELL TINT, not a SHIRO/AKA badge (operator decision
                    2026-09-20, bc-sccl). The text channel is an sr-only span, the
                    same one every other converted surface took.
                    It is NOT an aria-label on this div, which is what these cells
                    carried until 2026-09-20: a bare div is role=generic, and ARIA
                    prohibits author naming there, so that label was announced by
                    nothing. Removing the badge on top of it would have left the
                    side carried by colour alone on the one surface where reading
                    it wrong mis-scores a bout. */}
                <SideCell side="shiro" className={`shiaijo-qrow__side shiaijo-qrow__side--shiro ${winnerSide === "left" ? "shiaijo-qrow__side--win" : ""}`}>
                    {winnerSide === "left" && <WinnerTick />}
                    <span className={`shiaijo-qrow__name${teamShiroMark ? " msb-name--labelled" : ""}`}>
                        {teamNameMark("shiro", teamShiroMark, <NumberedName side="shiro" name={bName} number={m.sideB?.number} clip />)}
                    </span>
                </SideCell>
                <span className="shiaijo-qrow__vs">vs</span>
                <SideCell side="aka" className={`shiaijo-qrow__side shiaijo-qrow__side--aka ${winnerSide === "right" ? "shiaijo-qrow__side--win" : ""}`}>
                    {winnerSide === "right" && <WinnerTick />}
                    <span className={`shiaijo-qrow__name${teamAkaMark ? " msb-name--labelled" : ""}`}>
                        {teamNameMark("aka", teamAkaMark, <NumberedName side="aka" name={aName} number={m.sideA?.number} clip />)}
                    </span>
                </SideCell>
            </div>
            {/* bc-cse: a barred match cannot be started (the server refuses it),
                so the row shows why and the one-tap resolution here instead of a
                dead Start button. BarredMatchNotice (admin_scoring_shared.jsx) is
                the one component: same note/action/reinstate on every surface. */}
            {barred && <BarredMatchNotice match={m} password={password} />}
            {/* Completed result on its own centred line BELOW the names: the
                canonical "marks in the centre" position, but stacked so the
                (often long) names keep the full-width line and never crowd. The
                ippon string is shiro: aka, matching the Shiro-left/Aka-right
                names above. */}
            {isComplete && (scoreCell.kind === "ippon" || scoreCell.kind === "team" || scoreCell.kind === "engi") && (
                <div className="shiaijo-qrow__result">
                    {/* scoreCell.iv already self-labels ("IV s-a · PW s-a"), so no separate IV abbr prefix. */}
                    {scoreCell.kind === "team" && <span className="shiaijo-row__teamscore">{scoreCell.iv}</span>}
                    {scoreCell.kind === "engi" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Total flags received">Flags</abbr>{scoreCell.flags}</span>}
                    {scoreCell.kind === "ippon" && scoreCell.ippon}
                </div>
            )}
            {/* Correct: reopen/fix a completed match in place (parity with the
                Scores page). A pending placeholder final has no real result to
                correct, so it never gets the button. */}
            {!pending && isComplete && onCorrect && (
                <div className="shiaijo-qrow__actions" onClick={(e) => e.stopPropagation()}>
                    <button type="button" className="btn btn--ghost btn--sm shiaijo-row__correct" onClick={() => onCorrect(m)} title="Reopen or fix this completed match">Correct</button>
                </div>
            )}
            {notice && isComplete && <div className="shiaijo-upnext__error" role="alert">{notice}</div>}
            {showActions && (
                <div className="shiaijo-qrow__actions" onClick={(e) => e.stopPropagation()}>
                    {onMoveCourt && courts.length > 1 && (
                        <CourtPicker
                            value={m.court} courts={courts}
                            onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
                            btnClassName="score-edit-row__court score-edit-row__court--btn"
                        />
                    )}
                    {/* bc-cse: never offered on a barred row -- the server
                        would just refuse the Start this exists to prepare
                        for, and BarredMatchNotice above already owns the
                        one-tap resolution instead. */}
                    {onEnterLineup && isTeamMatch(m) && !barred && (
                        <button type="button" className="btn btn--ghost btn--sm" onClick={() => onEnterLineup(m)} title="Set the team lineup before starting">Lineup</button>
                    )}
                    {onMove && (
                        <>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move up" disabled={isFirst} onClick={() => onMove(m, "up")} title="Move earlier in the queue">↑</button>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move down" disabled={isLast} onClick={() => onMove(m, "down")} title="Move later in the queue">↓</button>
                        </>
                    )}
                    {/* Optional announce: mirrors the Up Next card so any queued match is a
                        complete view: call it to the floor, or start it directly.
                        bc-cse: never offered on a barred row -- there is nobody to
                        call to the court for a match the server would refuse to
                        start. */}
                    {onCall && window.API && typeof window.API.sendAnnouncement === "function" && !barred && (
                        <button type="button" className="btn btn--ghost btn--sm" disabled={callingKey === matchKey(m)} onClick={(ev) => onCall(m, ev)} title="Announce this match to spectators and competitors">
                            {callingKey === matchKey(m) ? "Calling…" : (calledKey === matchKey(m) ? "Call again" : "Call to court")}
                        </button>
                    )}
                    {/* Start match is the primary per-row action: pushed to the end (the "go" slot).
                        Same pickMatch path as the Up Next card: refused while a correction is
                        open (the notice below the actions), a running bout goes back to the
                        queue keeping its score (bc-sbq), then this match starts for scoring.
                        bc-cse: never offered on a barred row -- BarredMatchNotice above owns its
                        one-tap resolution instead, since the server would just refuse a Start. */}
                    {onPick && !barred && <button type="button" className="btn btn--primary btn--sm shiaijo-row__pick" disabled={startingKey === matchKey(m)} onClick={(ev) => onPick(m, ev)} title="Start this match now and begin scoring">{startingKey === matchKey(m) ? "Starting…" : "Start match"}</button>}
                </div>
            )}
            {notice && !isComplete && <div className="shiaijo-upnext__error" role="alert">{notice}</div>}
        </div>
    );
}

function MatchSides({ m, large }) {
    return (
        <div className={`shiaijo-sides ${large ? "shiaijo-sides--lg" : ""}`}>
            {/* Side by CELL TINT, not a SHIRO/AKA badge (bc-sccl); an sr-only
                span carries the side in text, as on the queue row above. */}
            <SideCell side="shiro" className="shiaijo-sides__side shiaijo-sides__side--shiro">
                <div className="name">
                    <NumberedName side="shiro" name={m.sideB?.name} number={m.sideB?.number} clip />
                </div>
                <div className="dojo">{m.sideB?.dojo}</div>
            </SideCell>
            <div className="shiaijo-sides__vs">vs</div>
            <SideCell side="aka" className="shiaijo-sides__side shiaijo-sides__side--aka" style={{ textAlign: "right" }}>
                <div className="name">
                    <NumberedName side="aka" name={m.sideA?.name} number={m.sideA?.number} clip />
                </div>
                <div className="dojo">{m.sideA?.dojo}</div>
            </SideCell>
        </div>
    );
}

// Standings ordering is decided by competition FORMAT, not by surface
// (mp-ahu6/mp-pglr): swiss renders cumulative standings from the dedicated
// /swiss/standings endpoint (window.SwissStandingsViewer), leagues are always
// rank-ordered (window.LeagueStandingsViewer), pools are always draw-ordered
// with rank as a badge (window.PoolsViewer) - the same three-way split as the
// public viewer (viewer_competition.jsx) and admin Pools tab. Swiss must NOT
// fall through to the pool path: Swiss piggybacks on pool-matches.csv with a
// synthetic pool name ("Swiss-R1") but never writes pools.csv, so the pool
// path's detail.pools lookup finds nothing and the panel sticks on "Loading
// standings…" forever. Exported so the format->viewer decision is
// independently testable and this invariant cannot silently regress again.
export function shiaijoStandingsKind(match) {
    if (!match) return "pool";
    if (match.compFormat === "swiss") return "swiss";
    return match.compFormat === "league" ? "league" : "pool";
}

// Collapsible context for the match being scored:
//   • pool phase  → live standings + results for the current pool, routed by
//     shiaijoStandingsKind. Pools also show which pool is next on this
//     court; leagues have no "next pool" concept.
//   • bracket phase → a bracket fragment with the anchored match highlighted.
// `lead` replaces the heading's leading word when the anchored match is not the
// live bout: "Up next" for a bout not yet fought, "Just played" for a finished
// one the panel fell back to, "Correcting" for one the operator opened to fix.
// Without it the highlight reads as the bout now being fought (mp-jnvl). With
// no lead the word names the panel's CONTENT - standings for a pool, the
// bracket fragment for a knockout; "Context" said nothing and was dropped
// (operator ruling 2026-09-19).
function ShiaijoContext({ match, competitions, court, nextPoolName, tweaks, lead, open, onToggle }) {
    const comp = (competitions || []).find((c) => c.id === match.compId);
    const bracket = comp && (comp.bracket || (Array.isArray(comp.rounds) ? { rounds: comp.rounds } : null));
    const isPool = match.phase === "pool";
    const standingsKind = shiaijoStandingsKind(match);
    const isLeagueComp = standingsKind === "league";
    const isSwissComp = standingsKind === "swiss";
    // leagueAwareLabel (viewer_utils.jsx) already folds the swiss case in via
    // swissRoundLabel, so the isSwissComp ternary that used to sit here is
    // redundant (mp-dej2).
    const phaseLabel = isPool
        ? window.leagueAwareLabel(match.compFormat, match.poolName, "Pool")
        : (match.round || "Elimination");
    const PoolsViewer = window.PoolsViewer;
    const LeagueStandingsViewer = window.LeagueStandingsViewer;
    const SwissStandingsViewer = window.SwissStandingsViewer;

    // Pools/standings aren't on the console's competition list payload: fetch
    // the competition detail on demand. Refetch whenever this comp's pool
    // matches change (a scored bout), so standings stay current. poolSig is the
    // change key; it's cheap and keyed only to this comp. Leagues skip this
    // fetch entirely (Copilot, PR #333): LeagueStandingsViewer fetches its own
    // standings via window.API.leagueStandings(c.id) and only needs
    // `comp`/`comp.poolMatches`, both already on the court feed - waiting on
    // fetchCompetitionDetails here would be an unneeded dependency that could
    // stall or blank the league panel if that endpoint (unlike the dedicated
    // league-standings one) has a transient failure.
    const poolSig = useMemoSh(() => {
        const pms = (comp && comp.poolMatches) || [];
        return pms.map((m) => `${m.id}:${m.status}:${(m.ipponsA || []).join("")}:${(m.ipponsB || []).join("")}`).join("|");
    }, [comp]);
    const [detail, setDetail] = useStateSh(null);
    const [detailErr, setDetailErr] = useStateSh(false);
    useEffectSh(() => {
        // Swiss and league both render self-fetching viewers off `comp` alone
        // (dedicated standings endpoints), so the competition-detail fetch is
        // only needed for the pool path.
        if (!isPool || isLeagueComp || isSwissComp || !match.compId || !window.API || typeof window.API.fetchCompetitionDetails !== "function") {
            setDetail(null);
            return;
        }
        let cancelled = false;
        setDetailErr(false);
        window.API.fetchCompetitionDetails(match.compId)
            .then((d) => { if (!cancelled) setDetail(d); })
            .catch(() => { if (!cancelled) { setDetail(null); setDetailErr(true); } });
        return () => { cancelled = true; };
    }, [match.compId, isPool, isLeagueComp, isSwissComp, poolSig]);

    const currentPool = detail && Array.isArray(detail.pools)
        ? detail.pools.find((p) => p.poolName === match.poolName)
        : null;

    // Shared loading placeholder for the self-fetching standings viewers
    // (swiss + league branches below).
    const standingsLoader = (
        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
            Loading standings…
        </p>
    );

    return (
        <div className="shiaijo-context">
            <button type="button" className="section-title shiaijo-context__toggle" aria-expanded={open} onClick={onToggle}>
                {open ? "−" : "+"} {lead || (isPool ? "Standings" : "Bracket")} · {match.compName} · {phaseLabel}
            </button>
            {open && (
                <div className="shiaijo-context__body">
                    {/* mp-gmcg: the scorer's live IV/PW tally and this standings
                        table disagreed on screen (critique P1) — standings only
                        count RECORDED encounters, so a running match reads 0.
                        Name the gap instead of leaving two contradictory numbers. */}
                    {isPool && match.status === "running" && (
                        <p className="shiaijo-context__live-note" style={{ fontSize: 12, color: "var(--ink-2)", margin: "0 0 8px" }}>
                            This match is still in progress, so its result isn’t in the standings below yet — they update when it’s recorded.
                        </p>
                    )}
                    {isPool ? (
                        isSwissComp ? (
                            // Swiss standings come from the dedicated
                            // /swiss/standings endpoint; SwissStandingsViewer
                            // fetches its own data (like LeagueStandingsViewer)
                            // and needs only `comp` from the court feed. The
                            // poolSig key remounts it after any scored bout so
                            // the cumulative table stays current mid-round
                            // (its own refetch deps only cover round changes).
                            // No "next pool" banner: Swiss rounds are not
                            // pools, the next round doesn't exist until the
                            // operator generates it.
                            SwissStandingsViewer && comp ? (
                                <div className="shiaijo-context__pools">
                                    <SwissStandingsViewer
                                        key={poolSig}
                                        competition={comp}
                                        poolMatches={comp.poolMatches}
                                        tweaks={tweaks || { showDojo: true }}
                                    />
                                </div>
                            ) : standingsLoader
                        ) : isLeagueComp ? (
                            // Leagues are always RANK-ordered (mp-ahu6): never fall
                            // through to the draw-order PoolsViewer here. Renders off
                            // `comp` alone (see the poolSig effect above for why) -
                            // LeagueStandingsViewer fetches its own standings.
                            LeagueStandingsViewer && comp ? (
                                <div className="shiaijo-context__pools">
                                    <LeagueStandingsViewer
                                        competition={comp}
                                        poolMatches={comp.poolMatches}
                                        tweaks={tweaks || { showDojo: true }}
                                        onMatchClick={null}
                                        highlightPlayers={[]}
                                        showDataIssues
                                    />
                                </div>
                            ) : standingsLoader
                        ) : (
                            <>
                                <div className="shiaijo-context__next">
                                    {nextPoolName
                                        ? <><span className="shiaijo-context__next-label">Next pool on Shiaijo {court}:</span> <strong>{nextPoolName}</strong></>
                                        : <span className="shiaijo-context__next-label">Last pool on Shiaijo {court}.</span>}
                                </div>
                                {PoolsViewer && currentPool ? (
                                    <div className="shiaijo-context__pools">
                                        <PoolsViewer
                                            pools={[currentPool]}
                                            standings={detail.standings}
                                            poolMatches={detail.poolMatches}
                                            competition={comp || detail}
                                            tweaks={tweaks || { showDojo: true }}
                                            onMatchClick={null}
                                            highlightPlayers={[]}
                                            showDataIssues
                                        />
                                    </div>
                                ) : (
                                    <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                                        {detailErr
                                            ? "Couldn't load standings: they'll appear once the connection recovers."
                                            : "Loading standings…"}
                                    </p>
                                )}
                            </>
                        )
                    ) : (match.phase === "bracket" && BracketTree && bracket && bracket.rounds) ? (
                        <div className="shiaijo-context__bracket">
                            <BracketTree rounds={bracket.rounds} highlightedMatchId={match.id} isEngi={!!match.compEngi} />
                        </div>
                    ) : (
                        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                            {match.compName}: {phaseLabel}. Bracket context appears here for elimination matches.
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}

window.AdminShiaijoPage = AdminShiaijoPage;
