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
import { applyPatch } from './patch.jsx';
import { SideCell } from './side_cell.jsx';
// Imported DIRECTLY from the leaf rather than read off `window`. Two of the
// call sites below sit inside a `try { } catch (_e) { }` that swallows, so a
// missing global there would degrade into exactly the silent not-saved failure
// these predicates exist to remove. write_result.jsx is import-only (never
// script-tagged), so the double-module-eval trap that keeps this file off
// api_client.jsx does not apply to it — the same move admin_scoring_shared.jsx
// already makes.
import {
    writeDidNotLand, writeWasSuperseded, writeWasRefusedForClock, CLOCK_SKEW_REASON_TEXT,
    attemptScoreWrite, DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED,
} from './write_result.jsx';
// swissRoundLabel: single owner is pool_ids.jsx (mp-dej2); this file used to
// carry its own copy.
import { swissRoundLabel } from './pool_ids.jsx';
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
import { isBarredMatch, sideBarredByDecision, involvesCompetitor } from './ineligible_match.jsx';
// Straight from its own leaf, NOT admin_scoring_shared.jsx: that module also
// imports bracket.jsx (for sideMarks), and admin_shiaijo.jsx's render suite
// stubs window.BracketTree before importing this file -- routing through
// admin_scoring_shared.jsx pulled bracket.jsx's module body in ahead of that
// stub taking effect and silently overwrote it. See barred_match_notice.jsx's
// header.
import { BarredMatchNotice } from './barred_match_notice.jsx';

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

// _bracketSideName reads a bracket side that may be a normalized {id,name}
// object or a bare string. Mirrors admin_helpers.sideName without importing it
// (admin_shiaijo reads its helpers off window; this stays pure for testing).
function _bracketSideName(side) {
    if (!side) return "";
    return typeof side === "object" ? (side.name || "") : side;
}

const _WINNER_OF_RE = /^Winner of r(\d+)-m(\d+)$/;

// pendingFeederSlots maps a pending placeholder final to the feeder matches whose
// winners an operator can assert (via override-winner) to make it runnable during
// an update outage (mp-y3nk Phase 3). For each side still reading "Winner of
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
export function makeReconnectRefetcher(onReconnect) {
    let sawError = false;
    return (status) => {
        if (status === "error") { sawError = true; return; }
        if (status === "open" && sawError) { sawError = false; onReconnect(); }
    };
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
            window.winnerSideLR ? window.winnerSideLR(m) : null)
        : "";
    return s ? { kind: "ippon", ippon: s } : { kind: "none" };
}

// recordOverrideWinner adapts attemptScoreWrite's generic (compId, matchId,
// result, password) call shape into overrideBracketWinner's own positional
// signature. Only sends forceDownstreamReopen when true, so the FIRST attempt
// for each feeder is byte-identical to the call before this override gained
// the confirm+retry loop (bc-kcdg).
function recordOverrideWinner(compId, matchId, result, pw) {
    return result.forceDownstreamReopen
        ? window.API.overrideBracketWinner(compId, matchId, result.winnerName, pw, true)
        : window.API.overrideBracketWinner(compId, matchId, result.winnerName, pw);
}

// ResolveFeedersModal (mp-y3nk Phase 3): last-resort recovery when a court must
// run a knockout final whose feeder results have not synced from other shiaijos
// (dropped/delayed SSE). The operator asserts each unresolved feeder's winner;
// each assertion is written via the existing override-winner endpoint, which
// propagates and resolves the final's sides so it becomes startable. Deliberately
// NOT a way to fabricate the final directly: the winner must be one of the
// feeder's two real competitors, and the write is audited (IsOverridden). If the
// real feeder result arrives later it re-propagates over the assertion.
function ResolveFeedersModal({ match, comp, password, onClose, onResolved, onOptimisticResolve, showToast }) {
    const rounds = (comp && comp.bracket && comp.bracket.rounds) || [];
    const slots = useMemoSh(() => pendingFeederSlots(match, rounds), [match, rounds]);
    // Slot text comes from the ONE rule in bracket.jsx, so this modal names a
    // feeder exactly as the bracket card does ("Winner of M3") instead of
    // stripping the prefix off the wire value and printing "r2-m0".
    // (Guarded like the window.boutMiddle reads elsewhere: a mount without
    // bracket.js degrades instead of throwing. index.html loads it first.)
    const slotLabel = useMemoSh(
        () => (window.bracketSlotLabeller ? window.bracketSlotLabeller(rounds) : (n) => n),
        [rounds]
    );
    const resolvable = slots.filter(s => s.resolvable);
    const blocked = slots.filter(s => !s.resolvable);
    // feeder id → asserted winner name.
    const [picks, setPicks] = useStateSh({});
    const [busy, setBusy] = useStateSh(false);
    const [error, setError] = useStateSh("");
    const allPicked = resolvable.length > 0 && resolvable.every(s => picks[s.feeder.id]);

    const submit = async () => {
        if (!allPicked || busy) return;
        setBusy(true); setError("");
        try {
            // Sequential: each override is an independent one-side resolution, but
            // serialising keeps a mid-way failure's partial state easy to reason
            // about (some feeders resolved, the operator retries the rest).
            let anyQueued = false;
            let anyDropped = false;
            let anyClockRefused = false;
            for (const s of resolvable) {
                const winner = picks[s.feeder.id];
                // bc-kcdg: routed through the shared attemptScoreWrite loop
                // (write_result.jsx) so a 409 downstream_knockout_played
                // refusal -- this feeder's assertion would repaint a later
                // match that already played on the current winner -- gets the
                // same confirm+retry experience as a score correction, rather
                // than surfacing the raw refusal as an opaque error string.
                // recordOverrideWinner adapts the (compId, matchId, result,
                // password) shape attemptScoreWrite calls into
                // overrideBracketWinner's own positional signature.
                const r = await attemptScoreWrite({
                    recordScore: recordOverrideWinner,
                    confirmDialog: window.confirmDialog,
                    compId: comp.id,
                    matchId: s.feeder.id,
                    result: { winnerName: winner },
                    password,
                    match: null,
                });
                if (writeWasSuperseded(r)) {
                    // The server dropped this assertion, so our pick is NOT the
                    // authoritative winner: skip the optimistic advance and let
                    // onResolved() (refreshCourt) pull the real server tree. That
                    // part is the same for both drop reasons; what the operator is
                    // TOLD is not (bc-cse), so split them here.
                    if (writeWasRefusedForClock(r)) {
                        // Refused for clock skew: nothing newer exists, nothing was
                        // recorded anywhere, and the client has just resynced. The
                        // "already recorded elsewhere" wording would be a plain
                        // falsehood and would stop the operator retrying the one
                        // action that now works.
                        anyClockRefused = true;
                    } else {
                        anyDropped = true;
                    }
                    continue;
                }
                // Optimistically advance the LOCAL bracket so the final becomes
                // startable immediately: even offline, where { queued: true } means
                // the server has not confirmed yet. The queued write reconciles the
                // server on reconnect; a refetch then replaces this optimistic tree.
                if (onOptimisticResolve) onOptimisticResolve(comp.id, s.feeder.id, winner);
                if (r && r.queued) anyQueued = true;
            }
            if (showToast) {
                // anyDropped is checked BEFORE anyClockRefused on purpose: when one
                // loop produces both, the supersede wording wins because it is the
                // one that is destructive to ignore (a real result exists elsewhere
                // and the operator must look at it), while a clock refusal costs
                // only a retry. The refetch below happens either way.
                showToast(anyQueued
                    ? "Recorded offline. This match is ready to run now and will sync when the court reconnects."
                    : anyDropped
                        ? "Some results were already recorded elsewhere. Refreshing this court to show the current state."
                        : anyClockRefused
                            ? "This device's clock was out of step with the server and has been resynced. Try resolving again."
                            : "Feeders resolved. The match is ready to start.");
            }
            if (onResolved) onResolved();
            onClose();
        } catch (e) {
            // bc-kcdg: a declined downstream-reopen override leaves everything
            // unchanged (neither this feeder assertion nor the later match it
            // would have reopened was written), so it gets its own copy rather
            // than the generic connection-failure message below.
            if (e && e.downstreamKnockoutPlayedCancelled) {
                setError(DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED);
            } else {
                setError((e && e.message) || "Could not resolve the feeders. Check your connection and try again.");
            }
        } finally {
            setBusy(false);
        }
    };

    return (
        <Modal title="Run this match now" onClose={busy ? undefined : onClose} size="md" dismissable={!busy}>
            <p className="hint" style={{ marginTop: 0 }}>
                This match is waiting on earlier bouts whose results have not reached this court yet.
                If you know their outcomes, record each winner below to make this match startable.
                Recording a winner here is provisional: if the real result arrives later it takes over.
            </p>
            {resolvable.map((s) => (
                <div key={s.feeder.id} className="resolve-feeder">
                    {/* s.feeder is the match whose winner is being asserted, so
                        name the slot after IT rather than re-deriving from the
                        slot string. */}
                    <div className="resolve-feeder__label">{slotLabel(s.placeholder, s.feeder.id)}</div>
                    <div className="resolve-feeder__opts">
                        {s.options.map((opt) => {
                            const name = _bracketSideName(opt);
                            const picked = picks[s.feeder.id] === name;
                            return (
                                <button
                                    key={name}
                                    type="button"
                                    className={`btn btn--sm ${picked ? "btn--primary" : "btn--ghost"}`}
                                    aria-pressed={picked}
                                    disabled={busy}
                                    onClick={() => setPicks((p) => ({ ...p, [s.feeder.id]: name }))}
                                >
                                    {name}
                                </button>
                            );
                        })}
                    </div>
                </div>
            ))}
            {blocked.length > 0 && (
                <p className="hint hint--sm" style={{ color: "var(--ink-3)" }}>
                    {blocked.length === 1 ? "One feeder" : `${blocked.length} feeders`} cannot be resolved yet
                    because {blocked.length === 1 ? "its own" : "their own"} earlier matches are still pending.
                </p>
            )}
            {resolvable.length === 0 && (
                <p className="hint hint--sm" style={{ color: "var(--ink-3)" }}>
                    None of this match's feeders can be resolved yet. Score the earlier matches first.
                </p>
            )}
            {error && <div className="alert alert--warn" role="alert">{error}</div>}
            <div className="modal__actions" style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 12 }}>
                <button type="button" className="btn btn--ghost" onClick={onClose} disabled={busy}>Cancel</button>
                <button type="button" className="btn btn--primary" onClick={submit} disabled={!allPicked || busy}>
                    {busy ? "Recording…" : "Record & make startable"}
                </button>
            </div>
        </Modal>
    );
}

// The match with id in one competition of the court feed, pool or knockout.
function matchInComp(comp, id) {
    const pool = (comp.poolMatches || []).find((m) => m.id === id);
    if (pool) return pool;
    const b = comp.bracket;
    if (!b) return null;
    for (const round of b.rounds || []) {
        const m = round.find((x) => x.id === id);
        if (m) return m;
    }
    return b.thirdPlaceMatch && b.thirdPlaceMatch.id === id ? b.thirdPlaceMatch : null;
}

function AdminShiaijoPage({ tournament, court: routeCourt, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password, showToast, tweaks, onSwitchCourt }) {
    // Normalize once: filterMatchesByCourt trims its param, so a bookmarked URL
    // with stray whitespace must use the trimmed value everywhere.
    const court = (routeCourt || "").trim();

    const mountedRef = useRefSh(true);
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
    // True while a manual "Refresh" is in flight (button feedback only).
    const [refreshing, setRefreshing] = useStateSh(false);
    // Refetch this court's live feed. Hoisted (not an effect-local closure) so
    // the SSE effect, the reconnect handler, AND the manual Refresh button all
    // drive the same fetch. Guarded by mountedRef so a late resolve after
    // unmount is dropped. Returns the promise so callers can await completion.
    const refreshCourt = useCallbackSh(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return Promise.resolve();
        return window.API.fetchCourtMatches(court)
            .then(comps => { if (mountedRef.current) setCourtComps(comps); })
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
    // a feeder's asserted winner to the LOCAL court feed so a downstream final
    // becomes runnable immediately: even with no server (the override write is
    // separately queued for sync). The server stays authoritative: a later
    // refetch/reconnect replaces this optimistic tree with the real one.
    const applyLocalBracketWin = useCallbackSh((compId, feederId, winnerName) => {
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
                return {
                    ...c,
                    bracket: {
                        ...c.bracket,
                        rounds,
                        ...(thirdPlaceMatch !== null ? { thirdPlaceMatch } : {}),
                    },
                };
            });
            return changed ? next : prev;
        });
    }, [tournament.competitions]);
    // Advance the local bracket when a bracket bout is scored to completion, so a
    // court running offline sees the NEXT match (incl. the final) become runnable
    // without the server round-trip. Belt-and-suspenders online (the refetch also
    // reflects the server's propagation); the ONLY path that resolves it offline.
    // Draws/no-winner (patch.winner null) never apply: knockout bouts always
    // resolve to a winner before completing.
    const maybeAdvanceLocal = useCallbackSh((match, patch) => {
        if (!match || match.phase !== "bracket" || !patch || patch.status !== "completed") return;
        const w = patch.winner;
        const wname = _bracketSideName(w);
        if (wname) applyLocalBracketWin(match.compId, match.id, wname);
    }, [applyLocalBracketWin]);
    // The last Start the server refused, as { key, compId, msg } for the match
    // it was refused for (null = none). It is shown under Up next only while
    // that match IS Up next, and it is dropped once it may no longer apply:
    // whenever Up next changes to a different match, whichever match the
    // refusal was for (the effect beside upNext below), and when any
    // competitor's eligibility changes in that competition (the
    // competitor_status_updated handler just below), which is what restoring
    // a withdrawn competitor broadcasts. A refusal that
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
                    if (event.type === "match_updated") showRunningPush(event);
                    scheduleRefresh();
                },
                onStatus
            );
            unsub = () => { if (typeof off === "function") off(); };
        }
        let unsubResync = () => {};
        if (typeof window.subscribeBracketResync === "function") {
            // A queued override the server LWW-dropped emits no SSE broadcast, so
            // refetch to replace any stale optimistic bracket state (mp-y3nk).
            unsubResync = window.subscribeBracketResync(() => { if (!cancelled) scheduleRefresh(); });
        }
        return () => { cancelled = true; timerPool.clearAll(); unsub(); unsubResync(); };
    }, [court, refreshCourt]);

    // Court-scoped competitions: the live feed once loaded, else the prop
    // aggregate as a transient fallback. All competition/match derivations below
    // read from this so the page operates only on THIS court's competitions.
    const courtCompetitions = courtComps || tournament.competitions || [];

    // Selected match for the inline scoring panel. `calledKey` marks the match
    // the operator has announced this session (local cue only); `callingKey`
    // guards the in-flight announce request.
    const [calledKey, setCalledKey] = useStateSh(null);
    const [callingKey, setCallingKey] = useStateSh(null);
    const [startingKey, setStartingKey] = useStateSh(null);
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
    // The COMPLETED match the operator has opened to correct in place (parity
    // with the Scores page's "Correct" button). Kept separate from pickedKey so
    // it takes priority over the running bout WITHOUT disturbing it, and so it
    // never interferes with the finish-advance fallback that pickedKey drives.
    // Cleared by "Back to court", and handed over to pickedKey once the match
    // is reopened (see the effect beside correctingMatch below): a running
    // match is the live bout, not a correction. null = not correcting anything.
    const [correctingKey, setCorrectingKey] = useStateSh(null);
    // Pending court reassignment, awaiting operator confirmation. Moving a match
    // off this shiaijo is disruptive (it leaves the court and joins another's
    // queue), so it's gated behind a confirm step. {compId, matchId, to, label, from}.
    const [pendingMove, setPendingMove] = useStateSh(null);
    const [movingCourt, setMovingCourt] = useStateSh(false);
    // A scheduled team match whose lineup the operator is entering before start.
    // Opens the team scoresheet as a modal; positions persist via putMatchLineup
    // independent of scoring, so the operator can set the lineup and close
    // without starting (or hit Start from inside the modal).
    const [lineupMatch, setLineupMatch] = useStateSh(null);
    // A pending placeholder final the operator is resolving to run during an
    // update outage (mp-y3nk Phase 3): opens ResolveFeedersModal.
    const [resolveMatch, setResolveMatch] = useStateSh(null);
    // Pending revert-to-queue confirmation. Set when the operator clicks
    // "Send back to queue" on a running bout. Cleared on confirm or cancel.
    // Shape: {compId, matchId, label}.
    const [pendingRevert, setPendingRevert] = useStateSh(null);
    const [reverting, setReverting] = useStateSh(false);
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
    const allMatches = useMemoSh(() => courtMatchesRaw.filter(hasBothSides), [courtMatchesRaw]);
    // Scheduled knockout bouts still waiting on a "Winner of rX-mY" feeder
    // (mp-y3nk). Kept SEPARATE from allMatches / filteredScheduled so they never
    // become upNext, never enter moveMatch reordering, and never carry a Start
    // button: a placeholder side has no participant to call to the court. They
    // are rendered as non-actionable "later" rows so a court whose only remaining
    // bout is a downstream final does not show a falsely-empty Upcoming list.
    const pendingPlaceholder = useMemoSh(
        () => courtMatchesRaw.filter(window.isPendingBracketMatch),
        [courtMatchesRaw]
    );
    // A "Later" row shows the placeholder sides of a bout whose feeders are not
    // in yet, so its names go through the shared slot rule (bracket.jsx) and read
    // "Winner of M3" — the number on the bracket card — instead of the internal
    // "r2-m0". One labeller per competition, memoised with the court feed it is
    // derived from. NOTE: the labeller is DISPLAY only; the queue is still split
    // by isPendingBracketMatch / hasBothSides on the RAW sides above, so a
    // relabelled row stays non-actionable exactly as before.
    const slotLabelFor = useMemoSh(() => {
        const cache = new Map();
        return (compId) => {
            if (!cache.has(compId)) {
                const c = courtCompetitions.find((x) => x.id === compId);
                const rounds = (c && c.bracket && c.bracket.rounds) || [];
                cache.set(compId, window.bracketSlotLabeller ? window.bracketSlotLabeller(rounds) : (n) => n);
            }
            return cache.get(compId);
        };
    }, [courtCompetitions]);
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
    const filteredScheduled = useMemoSh(
        () => effectiveCompId ? scheduled.filter(m => m.compId === effectiveCompId) : scheduled,
        [scheduled, effectiveCompId]
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
    // Everything else in Upcoming: the whole scheduled list minus whichever
    // match became Up Next (by key, not index: Up Next may not be [0] when a
    // barred match sits ahead of it). Any barred match stays here, rendered
    // as a normal row that shows its own resolution instead of Start.
    const upcomingQueueMatches = upNext
        ? filteredScheduled.filter((m) => matchKey(m) !== matchKey(upNext))
        : filteredScheduled;
    // A Start refusal describes ONE match at one moment. Whenever Up next
    // changes to a different match (that one started, was moved, the operator
    // switched competition, or the court moved on), any stored refusal is
    // dropped, whichever match it was for. That includes a refusal for a match
    // picked from further down the queue: when that match later reaches Up
    // next, its cause (e.g. a competitor then fighting on another court) is
    // usually gone, and nothing else would clear it (a finished match sends no
    // competitor_status_updated). A refusal that still applies comes straight
    // back on the next tap. Keyed on the key VALUE: a refetch that keeps the
    // same Up next leaves a refusal for it where it is.
    const upNextKey = upNext ? matchKey(upNext) : null;
    useEffectSh(() => {
        setStartError(null);
    }, [upNextKey]);

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
    // on the wrong competition, switch" case. It deliberately does NOT fire just
    // because another competition has an earlier match while this one is still
    // active: the operator runs their current competition to completion first.
    // Never red/navy: uses --warn-* tokens only.
    const nudgeBanner = useMemoSh(() => {
        if (!effectiveCompId || !courtKnown) return null;

        // Selected comp still has a running or scheduled match here → no nudge.
        const selHasActive = running.some(m => m.compId === effectiveCompId) || filteredScheduled.length > 0;
        if (selHasActive) return null;

        // Other competitions' scheduled matches still on this court.
        const otherScheduled = allMatches.filter(
            m => m.compId !== effectiveCompId && m.status === "scheduled"
        );
        if (otherScheduled.length === 0) return null;

        // Null-prototype: compId is user-controlled (a comp id of "__proto__"
        // must not pollute the map or collide with inherited keys).
        const byComp = Object.create(null);
        for (const m of otherScheduled) {
            if (!byComp[m.compId]) byComp[m.compId] = { id: m.compId, name: m.compName, count: 0 };
            byComp[m.compId].count++;
        }
        const entries = Object.values(byComp);
        entries.sort((a, b) => b.count - a.count);
        return { comp: entries[0].name, compId: entries[0].id, count: entries[0].count };
    }, [allMatches, effectiveCompId, running, filteredScheduled, courtKnown]);

    // Delegate to the canonical start-patch factory (admin_schedule.jsx) rather
    // than re-declaring its shape: a second copy could silently drift. Both
    // modules ship in the same admin bundle, so the global is always present;
    // fail loudly if that ever stops being true instead of forking behaviour.
    const startPatch = () => {
        if (typeof window.startPatch !== "function") {
            throw new Error("startPatch factory unavailable: admin_schedule.jsx not loaded");
        }
        return window.startPatch();
    };

    // Returns true when the start write succeeded, false otherwise: pickMatch
    // relies on this so it only pins pickedKey for a match that actually started
    // (a blocked-by-eligibility start must not steal the panel).
    const startMatch = async (m) => {
        if (startingKey) return false;
        setStartError(null);
        setStartingKey(matchKey(m));
        const refusalFor = (msg) => ({ key: matchKey(m), compId: m.compId, msg });
        try {
            // Starting makes the match running; the scoring panel shows
            // running[0], so it picks the match up on the next refetch.
            const res = await onEditScore(m.compId, m.id, startPatch(), m);
            // A clock_skew refusal means the server stored NOTHING and, unlike
            // a queued start, nothing will land later — so returning true here
            // would pin the panel on a match that never started while the tap
            // looked like it worked. Found in browser verification: this card
            // button is a start path none of the review sweeps enumerated (it
            // is not one of the editor call sites). The relearn the refusal
            // triggers means a SECOND tap normally succeeds; the toast tells
            // the operator that, instead of leaving a dead first tap.
            if (writeWasRefusedForClock(res)) {
                const msg = "Could not start: " + CLOCK_SKEW_REASON_TEXT + ". The clock has been resynced; try again.";
                if (mountedRef.current) setStartError(refusalFor(msg));
                if (showToast) showToast(msg, "error");
                return false;
            }
            return true;
        } catch (e) {
            if (mountedRef.current) setStartError(refusalFor((e && e.message) || "Could not start the match: check eligibility and try again."));
            if (showToast) showToast((e && e.message) || "Could not start the match", "error");
            return false;
        } finally {
            if (mountedRef.current) setStartingKey(null);
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
    const pickMatch = async (m) => {
        if (!m || m.status === "completed") return;
        const cur = running[0] || null;
        const isSame = cur && matchKey(cur) === matchKey(m);
        if (cur && !isSame) {
            // Send the current bout back to the queue, otherwise the pick below
            // would leave the court with two running bouts. Route through the
            // dedicated revert-to-queue endpoint (not a scheduled /score write):
            // it keeps the bout's score server-side AND skips the StartMatchTx
            // eligibility gate, so a stale cross-match ineligibility can't
            // silently 409 the defer. If the revert fails we surface a toast and
            // must NOT start the pick.
            if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
            try {
                await window.API.revertMatchToQueue(cur.compId, cur.id, password);
            } catch (e) {
                if (showToast) showToast((e && e.message) || "Could not defer the current bout", "error");
                return;
            }
            if (!mountedRef.current) return;
        }
        if (m.status === "scheduled") {
            const ok = await startMatch(m);
            if (!ok || !mountedRef.current) return;
            // bc-strt: the editor treats this snapshot as running, so a point
            // struck before the refetch shows it running saves at once.
            setStartedFrom({ key: matchKey(m), at: m.modifiedAt });
        }
        setPickedKey(matchKey(m));
    };

    // correctMatch: open a COMPLETED match to correct it in place, mirroring the
    // Scores page's "Correct" button. The court operator often spots a wrong
    // score after the match closed (frequently once the court is otherwise
    // done), so they must be able to fix it here rather than leaving for the
    // competition admin view. This deliberately does NOT touch the running bout:
    // a past-match correction must never defer or revert the live match. The
    // editor then offers Reopen (kachinuki: back to running, keep the bout log;
    // a withdrawal-decided match: Clear withdrawal and reopen) or a direct
    // score re-write (other formats), same as the Scores page. A reopen makes
    // the match the court's live bout and ends the correction (the effect
    // beside correctingMatch).
    const correctMatch = (m) => {
        if (!m || m.status !== "completed") return;
        setCorrectingKey(matchKey(m));
    };
    // Leave the correction and return to the live court (running bout or the
    // done state). Only offered on a COMPLETED correction: once it is
    // reopened it is the live bout, finished via End match / Finish or sent
    // back to the queue like any other.
    const stopCorrecting = () => { setCorrectingKey(null); };

    // Call to court: optional. Broadcasts a tournament announcement so the
    // competitors (and anyone watching the public app) are notified they're
    // being summoned to this shiaijo. It does NOT start the match; Start is
    // always available on its own.
    const callToCourt = async (m) => {
        if (!window.API || typeof window.API.sendAnnouncement !== "function") return;
        const a = (m.sideA && m.sideA.name) || "Aka";
        const b = (m.sideB && m.sideB.name) || "Shiro";
        const msg = `Now calling ${b} and ${a} to Shiaijo ${court}.`.slice(0, 200);
        setCallingKey(matchKey(m));
        try {
            await window.API.sendAnnouncement(msg, 5, password);
            if (!mountedRef.current) return;
            setCalledKey(matchKey(m));
            if (showToast) showToast(`Called ${b} and ${a} to Shiaijo ${court}`);
        } catch (e) {
            if (showToast) showToast((e && e.message) || "Could not send the call announcement", "error");
        } finally {
            if (mountedRef.current) setCallingKey(null);
        }
    };

    // moveMatch: reorder by swapping scheduledAt with the adjacent row.
    // direction: "up" (earlier) or "down" (later). Only scheduled matches
    // can be reordered; running/completed are stable.
    //
    // The swap is two separate updateMatchTime PUTs (no server-side swap API),
    // so it is NOT atomic. If the second PUT fails we best-effort roll the first
    // one back to its original time, so a partial failure doesn't leave both rows
    // with the same/incorrect time. Times are sent as strings ("" for an unset
    // time): never null, which the Go handler's `scheduledAt string` rejects (400).
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
        let firstDone = false;
        try {
            await window.API.updateMatchTime(m.compId, m.id, neighbourTime, password);
            firstDone = true;
            await window.API.updateMatchTime(neighbour.compId, neighbour.id, myTime, password);
            if (showToast) showToast(`Moved ${label} ${direction === "up" ? "earlier" : "later"} in the queue`);
        } catch (e) {
            // Roll back the first swap so the queue isn't left half-swapped.
            if (firstDone) {
                try { await window.API.updateMatchTime(m.compId, m.id, myTime, password); }
                catch (_rb) { /* rollback also failed; the error toast below still fires */ }
            }
            if (showToast) showToast((e && e.message) || "Could not reorder the match", "error");
        }
    };

    // Revert a running bout back to the queue. Gated behind a confirm dialog
    // (same pattern as court reassignment) because discarding the running state
    // is disruptive: viewers drop the match from "Now" and it reappears in the
    // upcoming list. Always offered on a running bout, and the match keeps its
    // score (operator ruling 2026-09-26, bc-sbq): starting it again carries on
    // from it, and the operator removes a wrong mark themselves.
    const requestRevert = (m) => {
        // Name BOTH competitors so the confirm identifies the match, not just
        // one side. Order mirrors the on-court display (Shiro/sideB vs Aka/sideA).
        const shiro = (m.sideB && m.sideB.name) || "";
        const aka = (m.sideA && m.sideA.name) || "";
        const label = (shiro && aka) ? `${shiro} vs ${aka}` : (shiro || aka || "this match");
        setPendingRevert({ compId: m.compId, matchId: m.id, label });
    };
    const confirmRevert = async () => {
        if (!pendingRevert || reverting) return;
        if (!window.API || typeof window.API.revertMatchToQueue !== "function") return;
        const { compId, matchId, label } = pendingRevert;
        setReverting(true);
        try {
            await window.API.revertMatchToQueue(compId, matchId, password);
            if (!mountedRef.current) return;
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
        const mm = sorted.find((x) => x.compId === compId && x.id === matchId);
        const label = mm ? ((mm.sideB && mm.sideB.name) || (mm.sideA && mm.sideA.name) || "this match") : "this match";
        setPendingMove({ compId, matchId, to: toCourt, label, from: (mm && mm.court) || court });
    };
    const confirmMoveCourt = async () => {
        if (!pendingMove || movingCourt) return;
        const { compId, matchId, to } = pendingMove;
        setMovingCourt(true);
        try {
            await onMoveCourt(compId, matchId, to);
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
    const allDone = courtKnown && allMatches.length > 0 && running.length === 0 && filteredScheduled.length === 0 && filteredPending.length === 0;
    const selectedCompName = (courtsComps.find((c) => c.id === effectiveCompId) || {}).name || "";

    return (
        <div className="app">
            <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} hideRunningStrip />
            <div className="page page--wide">
                <Breadcrumbs items={[{ label: "Dashboard", onClick: onBack }, { label: `Shiaijo ${court}` }]} />
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
                                    onChange={(e) => onSwitchCourt(e.target.value)}
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
                            <button type="button" onClick={onBack} className="linklike">Back to dashboard</button>.
                        </p>
                        {/* The title-overlay court switcher is gated on courtKnown, so an
                            unknown court would otherwise strand the operator. Offer a plain
                            picker here to jump to a valid court without leaving the page. */}
                        {courts.length > 0 && (
                            <label className="empty__action">
                                Go to a court:{" "}
                                <select className="input" value="" onChange={(e) => { if (e.target.value) onSwitchCourt(e.target.value); }} aria-label="Switch to a valid court">
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
                                            {upNext.phase === "pool" && upNext.poolPosition > 0 && upNext.poolCount > 0
                                                ? ` · Match ${upNext.poolPosition} of ${upNext.poolCount}`
                                                : upNext.phase === "bracket" && upNext.matchNumber > 0
                                                ? ` · Match ${upNext.matchNumber}`
                                                : ""}
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
                                                Up Next bout honours the same lock/defer rules as the
                                                Upcoming "Score" buttons: blocked while a different bout is
                                                being scored, and an unscored running bout is deferred first.
                                                With no running bout it simply starts this one. */}
                                            <button type="button" className="btn btn--primary" disabled={startingKey === matchKey(upNext)} onClick={() => pickMatch(upNext)}>
                                                {startingKey === matchKey(upNext) ? "Starting…" : "Start match"}
                                            </button>
                                            {isTeamMatch(upNext) && (
                                                <button type="button" className="btn btn--sm" onClick={() => setLineupMatch(upNext)}
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
                                                    onClick={() => callToCourt(upNext)}
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
                                        {startError && startError.key === matchKey(upNext) && <div className="shiaijo-upnext__error" role="alert">{startError.msg}</div>}
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
                                    onMove={moveMatch} onEnterLineup={setLineupMatch}
                                    onPick={pickMatch}
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
                                        {filteredPending.map((m) => (
                                            <ShiaijoQueueRow key={matchKey(m)} m={m} pending onResolve={setResolveMatch} slotLabel={slotLabelFor(m.compId)} />
                                        ))}
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
                            {nudgeBanner && (
                                <button
                                    type="button"
                                    className="alert alert--warn shiaijo-nudge"
                                    onClick={() => setSelectedCompId(nudgeBanner.compId)}
                                    aria-label={`Switch to ${nudgeBanner.comp}`}
                                >
                                    <span className="shiaijo-nudge__icon" aria-hidden="true">{Icon ? <Icon name="alert-circle" size={15} /> : "⚠"}</span>
                                    <span className="shiaijo-nudge__text">
                                        {`Switch to ${nudgeBanner.comp}: ${nudgeBanner.count} match${nudgeBanner.count === 1 ? "" : "es"} waiting on this court.`}
                                    </span>
                                    <span className="shiaijo-nudge__cta" aria-hidden="true">Switch →</span>
                                </button>
                            )}
                            {allDone && !correctingMatch && (
                                <div className="empty">
                                    <h3>{selectedCompName ? `${selectedCompName} is complete on Shiaijo ${court}` : `All matches complete on Shiaijo ${court}`}</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        {filteredCompleted.length} match{filteredCompleted.length === 1 ? "" : "es"} scored.{" "}
                                        {nudgeBanner ? "Another competition still has matches on this court: switch above." : "Nothing left to run on this court."}
                                    </p>
                                </div>
                            )}

                            {!allDone && running.length > 1 && (
                                <div className="shiaijo-also-running" role="alert">
                                    <div className="shiaijo-also-running__title">Another bout is running on Shiaijo {court}</div>
                                    <ul className="shiaijo-also-running__list">
                                        {running.slice(1).map((m) => (
                                            <li key={matchKey(m)}>
                                                {m.sideB?.name || "?"} vs {m.sideA?.name || "?"}
                                                {m.compName ? <span className="shiaijo-also-running__comp"> · {m.compName}</span> : null}
                                            </li>
                                        ))}
                                    </ul>
                                    <div className="shiaijo-also-running__hint">A court runs one bout at a time. Score or correct these from their competition's admin view.</div>
                                </div>
                            )}

                            {/* correctingMatch too: correcting the court's last
                                bout makes allDone true, and the pin must still
                                hold the editor open. */}
                            {(!allDone || correctingMatch) && selectedMatch && (
                                <ScoreEditorModal
                                    // No subResults.length: see the same key in
                                    // admin_competition_bracket.jsx. Remounting
                                    // on every bout-log change threw away the
                                    // operator's unsaved scores, and the stale-
                                    // board problem it worked around is gone.
                                    key={matchKey(selectedMatch)}
                                    variant="inline"
                                    match={editorMatch}
                                    // The inline editor never closes itself; every
                                    // caller of onClose is a no-op here.
                                    onClose={() => {}}
                                    started={!!startedFrom && startedFrom.key === matchKey(selectedMatch) && startedFrom.at === selectedMatch.modifiedAt}
                                    canClose={false}
                                    onSubmit={async (patch) => {
                                        try {
                                            const res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            // Optimistically advance the local bracket so an offline court
                                            // sees the next match resolve (reconciled by refetch online).
                                            // NOT on a supersede: that winner was discarded in favour of a
                                            // newer stored one, so advancing would show the operator their
                                            // own dropped winner on the very queue the banner below tells
                                            // them to go and check. A queued write still advances (it
                                            // reconciles on reconnect); see writeWasSuperseded.
                                            if (!writeWasSuperseded(res)) maybeAdvanceLocal(selectedMatch, patch);
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
                                        try {
                                            const res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            // See the onSubmit handler above: a superseded write must not
                                            // advance the local bracket on a winner the server discarded.
                                            if (!writeWasSuperseded(res)) maybeAdvanceLocal(selectedMatch, patch);
                                            if (!mountedRef.current) return res;
                                            // A write that did not land must NOT advance: the match is
                                            // still running and still holds the court, so starting the
                                            // next one here just earns a court_busy on top of the
                                            // editor's not-saved banner (F5 queued; bc-lww1 superseded).
                                            if (writeDidNotLand(res)) return res;
                                            // Finish + start the next scheduled match, which then
                                            // becomes the running match the panel shows.
                                            if (next && next.status === "scheduled") {
                                                try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
                                            }
                                        } catch (_e) { /* keep panel */ }
                                    }}
                                    onAfterDecision={async (result) => {
                                        // A fusenpai/hantei decision already persisted the bout via
                                        // the /decision POST: no score PUT here. It still resolves a
                                        // winner, so offline we must advance the LOCAL bracket the same
                                        // way the score path does (mp-y3nk), else a decision-completed
                                        // bout leaves the next match on placeholders until Refresh/SSE.
                                        const winnerName = _bracketSideName(result && result.winner);
                                        if (selectedMatch && selectedMatch.phase === "bracket" && winnerName) {
                                            applyLocalBracketWin(selectedMatch.compId, selectedMatch.id, winnerName);
                                        }
                                        // Then start the next scheduled match so the panel advances
                                        // (mirrors onSubmitAndNext), passing over the matches of a
                                        // competitor a withdrawal or no-show has just barred.
                                        const next = nextActiveAfter(selectedMatch, sideBarredByDecision(result, selectedMatch));
                                        if (next && next.status === "scheduled") {
                                            try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
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
                                        title="Stop correcting this completed match and return to the live court"
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
                <div className="modal-backdrop" onClick={() => !movingCourt && setPendingMove(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true"
                        aria-labelledby="shiaijo-move-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-move-title" className="shiaijo-move-confirm__title">
                            Move to Shiaijo {pendingMove.to}?
                        </h3>
                        <p className="shiaijo-move-confirm__body">
                            <strong>{pendingMove.label}</strong> leaves Shiaijo {pendingMove.from} and joins
                            the queue on Shiaijo {pendingMove.to}.
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
                <div className="modal-backdrop" onClick={() => !reverting && setPendingRevert(null)}>
                    <div className="shiaijo-move-confirm" role="dialog" aria-modal="true"
                        aria-labelledby="shiaijo-revert-title" onClick={(e) => e.stopPropagation()}>
                        <h3 id="shiaijo-revert-title" className="shiaijo-move-confirm__title">
                            Send back to queue?
                        </h3>
                        <p className="shiaijo-move-confirm__body">
                            <strong>{pendingRevert.label}</strong> will be returned to the upcoming queue.
                            {" Any score entered is kept: starting it again carries on from there."}
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
                    onClose={() => setLineupMatch(null)}
                />
            )}
            {resolveMatch && Modal && (
                <ResolveFeedersModal
                    key={`resolve:${matchKey(resolveMatch)}`}
                    match={resolveMatch}
                    comp={courtCompetitions.find(c => c.id === resolveMatch.compId)}
                    password={password}
                    showToast={typeof showToast === "function" ? showToast : undefined}
                    onOptimisticResolve={applyLocalBracketWin}
                    onResolved={refreshCourt}
                    onClose={() => setResolveMatch(null)}
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

function ShiaijoQueueGroup({ label, matches, subGroup, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, onCall, callingKey, calledKey, startingKey, password }) {
    const renderRow = (m) => (
        <ShiaijoQueueRow
            key={matchKey(m)} m={m}
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

export function ShiaijoQueueRow({ m, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCorrect, onCall, callingKey, calledKey, startingKey, pending, onResolve, slotLabel, password }) {
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
    const aName = slotName(m.sideA?.name || "", (m.feeders || [])[0]);
    const bName = slotName(m.sideB?.name || "", (m.feeders || [])[1]);
    const scoreCell = shiaijoScoreCell(m);
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
                    {m.phase === "pool" && m.poolPosition > 0 && m.poolCount > 0
                        ? ` · Match ${m.poolPosition} of ${m.poolCount}`
                        : m.phase === "bracket" && m.matchNumber > 0
                        ? ` · Match ${m.matchNumber}`
                        : ""}
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
                <SideCell side="shiro" className="shiaijo-qrow__side shiaijo-qrow__side--shiro">
                    <span className="shiaijo-qrow__name"><NumberedName side="shiro" name={bName} number={m.sideB?.number} clip /></span>
                </SideCell>
                <span className="shiaijo-qrow__vs">vs</span>
                <SideCell side="aka" className="shiaijo-qrow__side shiaijo-qrow__side--aka">
                    <span className="shiaijo-qrow__name"><NumberedName side="aka" name={aName} number={m.sideA?.number} clip /></span>
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
                        <button type="button" className="btn btn--ghost btn--sm" disabled={callingKey === matchKey(m)} onClick={() => onCall(m)} title="Announce this match to spectators and competitors">
                            {callingKey === matchKey(m) ? "Calling…" : (calledKey === matchKey(m) ? "Call again" : "Call to court")}
                        </button>
                    )}
                    {/* Start match is the primary per-row action: pushed to the end (the "go" slot).
                        Same pickMatch path as the Up Next card: defers an unscored running bout,
                        blocks while one is being scored, then starts this match for scoring.
                        bc-cse: never offered on a barred row -- BarredMatchNotice above owns its
                        one-tap resolution instead, since the server would just refuse a Start. */}
                    {onPick && !barred && <button type="button" className="btn btn--primary btn--sm shiaijo-row__pick" disabled={startingKey === matchKey(m)} onClick={() => onPick(m)} title="Start this match now and begin scoring">{startingKey === matchKey(m) ? "Starting…" : "Start match"}</button>}
                </div>
            )}
            {/* Pending placeholder final: the ONLY affordance is the opt-in
                "Run now" recovery, which opens ResolveFeedersModal so the operator
                can assert the unsynced feeder results. No Start/Call/move here. */}
            {pending && onResolve && (
                <div className="shiaijo-qrow__actions" onClick={(e) => e.stopPropagation()}>
                    <button type="button" className="btn btn--ghost btn--sm" onClick={() => onResolve(m)}
                        title="Feeder results not synced from other courts? Record them to run this match now.">
                        Run now
                    </button>
                </div>
            )}
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
