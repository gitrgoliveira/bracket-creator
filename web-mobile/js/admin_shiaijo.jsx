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
import { Icon } from './icon.jsx';
import { courtQueueEntriesOf } from './court_queue.jsx';
import { applyPatch, createPushLedger, matchInComp } from './patch.jsx';
import { createRefetchGate } from './refetch_gate.jsx';
import { readSeqOf } from './read_seq.jsx';
import { useCourtPaceLookup } from './court_pace.jsx';
// Imported DIRECTLY from the leaf rather than read off `window`. Two of the
// call sites below sit inside a `try { } catch (_e) { }` that swallows, so a
// missing global there would degrade into exactly the silent not-saved failure
// these predicates exist to remove. write_result.jsx is import-only (never
// script-tagged), so the double-module-eval trap that keeps this file off
// api_client.jsx does not apply to it — the same move admin_scoring_shared.jsx
// already makes.
import {
    writeDidNotLand, writeKeepsEditorOpen, writeWasSuperseded, BRONZE_MATCH_ID,
    writePending, writeWasRefused, writeQueueKey, writeCourtNotHeld,
} from './write_result.jsx';
// bc-cse: a scheduled match a competitor is barred from (withdrew earlier)
// cannot be fought, so no auto-pick may offer it and the queue row shows the
// default-win action instead of Start. One leaf owns the question
// (ineligible_match.jsx); BarredMatchNotice (admin_scoring_shared.jsx) is the
// one component that renders the note plus that action across every surface.
import { isBarredMatch, sideBarredByDecision, involvesCompetitor } from './ineligible_match.jsx';
import { confirmLeave, leaveThen } from './unsaved_entries.jsx';
import { matchKey, readOwnMove, eventMatchCourt, bracketSideName,ownStatusMoveEnded, withOwnStatusMove, finishWinnerOf, propagateBracketWinnerLocal, applyBronzeLoserLocal, makeReconnectRefetcher } from './admin_shiaijo_feed.jsx';
import { sortShiaijoMatches, partitionShiaijoMatches, COMPLETED_PREVIEW, ShiaijoQueueGroup, forgetTieBreakerStandings, ShiaijoQueueRow } from './admin_shiaijo_queue.jsx';
import { ShiaijoContext } from './admin_shiaijo_context.jsx';
// The start, pick and correct flow, the court calls and the match moves are
// hooks the page calls on every render, in that order, so functions flow one
// way: the moves hook reads the start hook's pick in flight and takes a call
// down through the calls hook.
import { useCourtStart, ShiaijoUpNextCard } from './admin_shiaijo_start.jsx';
import { useCourtCalls } from './admin_shiaijo_calls.jsx';
import { useCourtMoves, ShiaijoConfirms } from './admin_shiaijo_moves.jsx';
// One tab runs each shiaijo (bc-sseq FR9): the mark under the top bar, Take
// control, and the check every console handler below makes before it writes.
import { useShiaijoControl, ShiaijoControlBar, refusedNotHeld } from './admin_shiaijo_control.jsx';

const { useState, useMemo, useEffect, useRef, useCallback } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const ScoreEditorModal = window.ScoreEditorModal;

const QUEUE_OPEN_KEY = "bc_shiaijo_queue_open";
const hasBothSides = window.hasBothSides;
const NO_COMPETITIONS = [];

// The match with id in one competition of the court feed, pool or knockout.

function AdminShiaijoPage({ tournament, court: routeCourt, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password, showToast, tweaks, onSwitchCourt }) {
    // Normalize once: filterMatchesByCourt trims its param, so a bookmarked URL
    // with stray whitespace must use the trimmed value everywhere.
    const court = (routeCourt || "").trim();

    const mountedRef = useRef(true);
    // The mounted score editor's unsaved entries (R1, stage 5 review 8): every
    // console action that unmounts the editor or puts another match in it asks
    // first (leaveEditorConfirmed), and Send back to queue says so in its own
    // confirm.
    const editorUnsavedRef = useRef(null);
    // The newest court call this console made per match, keyed by matchKey, as
    // { id, msg, endsAt, answered }: the announcement id the device made and
    // the text sent, so the call can be withdrawn once its match leaves
    // scheduled (bc-cdbl), and when its time is over (endsAt, in the server's
    // clock frame: made-at plus CALL_MINUTES until the server answers, then the
    // expiry it answered). A reload forgets them; the 5-minute expiry is the
    // backstop. Declared ahead of the court feed's handler, which withdraws one;
    // the calls hook (useCourtCalls, admin_shiaijo_calls.jsx) keeps it.
    const callIdsRef = useRef(new Map());
    // Bound by the calls hook's layout effect to the render's withdrawCall,
    // for the court feed's handler below, which is set up before that hook is
    // called and runs on an event, after the commit has bound it.
    const withdrawCallRef = useRef(null);
    useEffect(() => {
        if (typeof window.scrollTo === "function") window.scrollTo(0, 0);
        return () => {
            mountedRef.current = false;
        };
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
    const [courtComps, setCourtComps] = useState(null);
    // The feed as of this render, for the event handler below, whose closure
    // outlives renders.
    const courtCompsRef = useRef(null);
    React.useLayoutEffect(() => { courtCompsRef.current = courtComps; });
    // The server pushes applied to the feed, for its refetches (push-wins,
    // createPushLedger, patch.jsx).
    const [pushes] = useState(() => createPushLedger());
    // R4 (bc-evts stage 3 review 2): a court move or time change this device
    // makes shows in the court feed at once, as a push would, and a refusal
    // puts the old value back (told by the outbox, subscribeLocalMatchPatch).
    useEffect(() => {
        if (!window.API || typeof window.API.subscribeLocalMatchPatch !== "function") return undefined;
        return window.API.subscribeLocalMatchPatch((event) => {
            const cid = event && event.data && event.data.competitionId;
            if (!cid) return;
            setCourtComps((prev) => (Array.isArray(prev) ? prev.map((c) => (c.id === cid ? applyPatch(c, event) : c)) : prev));
        });
    }, []);
    // True while a manual "Refresh" is in flight (button feedback only).
    const [refreshing, setRefreshing] = useState(false);
    // Refetch this court's live feed. Hoisted (not an effect-local closure) so
    // the SSE effect, the reconnect handler, AND the manual Refresh button all
    // drive the same fetch. Guarded by mountedRef so a late resolve after
    // unmount is dropped. Returns the promise so callers can await completion.
    // The feed effect's refetch gate (createRefetchGate, refetch_gate.jsx), as
    // { court, gate }: every read of the feed tells it the seq of the answer
    // it applied (bc-sseq FR4), so an event that answer holds fetches nothing.
    const feedGateRef = useRef(null);
    // fetchCourt reads the court's feed and answers it (whose seq readSeqOf
    // reads), or nothing when there is no court to read, and rejects when the
    // read failed. It shows nothing: takeCourt, called as the read is sent,
    // answers what does, so the feed gate can leave an answer older than a read
    // the page already shows unshown, and a late answer after the court
    // changed never shown at all.
    const fetchCourt = useCallback(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return Promise.resolve(null);
        return window.API.fetchCourtMatches(court);
    }, [court]);
    const takeCourt = useCallback(() => {
        // A refetch never puts a row back to an older state
        // (keepNewerCompetitions, patch.jsx; the rule showRunningPush applies
        // to a push), nor behind a push applied after it was sent (push-wins).
        const keep = pushes.competitions();
        return (comps) => {
            // Guarded by mountedRef: a late resolve after unmount is dropped.
            if (!mountedRef.current || !comps) return false;
            setCourtComps((prev) => keep(prev, comps));
            forgetTieBreakerStandings(comps);
            return true;
        };
    }, [pushes]);
    // An answer read outside the feed gate that is older than one the gate
    // already applied is dropped before it is shown (createRefetchGate.late,
    // refetch_gate.jsx), so it never puts older data over a newer feed while the
    // gate's applied seq stays above it.
    const lateCourtRead = useCallback((comps) => {
        const held = feedGateRef.current;
        return !!(held && held.court === court && held.gate.late(readSeqOf(comps)));
    }, [court]);
    // readCourt is fetchCourt and takeCourt in one, for a read made outside the
    // feed gate: it answers the feed it showed, or nothing when none was (or
    // when a newer feed was already applied through the gate).
    const readCourt = useCallback(() => {
        const apply = takeCourt();
        return fetchCourt().then((comps) => (!lateCourtRead(comps) && apply(comps) ? comps : null));
    }, [fetchCourt, takeCourt, lateCourtRead]);
    // refreshCourt is every read of the feed made outside the feed gate: a
    // failed one is logged and answers nothing, an applied one is told to the
    // gate so an event it holds fetches nothing.
    const refreshCourt = useCallback(() => readCourt().then((comps) => {
        const held = feedGateRef.current;
        if (comps && held && held.court === court) held.gate.applied(readSeqOf(comps));
        return comps;
    }, (err) => { console.error("Failed to fetch court matches", err); return null; }), [readCourt, court]);
    // Operator-triggered re-sync: the last-resort recovery when a court's tablet
    // has fallen behind (dropped SSE, flaky venue WiFi) and the queue looks
    // stale: e.g. a downstream final still shows placeholder feeders that have
    // in fact resolved on the server. One tap re-pulls the authoritative feed.
    const manualRefresh = useCallback(() => {
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
    const applyLocalBracketWin = useCallback((compId, feederId, winnerName, onApplied) => {
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
    const advanceLocalUntilRefused = useCallback((match, winnerName, res) => {
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
    const maybeAdvanceLocal = useCallback((match, patch, res) => {
        if (!match || match.phase !== "bracket" || !patch || patch.status !== "completed") return;
        const w = finishWinnerOf(match, patch);
        advanceLocalUntilRefused(match, bracketSideName(w), res);
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
    const [startError, setStartError] = useState(null);
    useEffect(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return;
        let cancelled = false;
        const timerPool = createTimerPool();
        // One refetch per burst of events, not one per event: a running match
        // broadcasts every saved point, and each used to schedule its own
        // full fetch of the court's feed. The gate (createRefetchGate,
        // refetch_gate.jsx; bc-sseq FR4) sends it 300 to 600 ms after the event, and
        // none for an event the feed applied already holds.
        const feedGate = createRefetchGate({
            schedule: timerPool.schedule,
            fetch: () => (cancelled ? null : fetchCourt()),
            take: () => (cancelled ? undefined : takeCourt()),
            maxDelayMs: 600,
            onError: (err) => console.error("Failed to fetch court matches", err),
            // Read at each schedule, so a test's Math.random stub reaches it.
            random: () => Math.random(),
        });
        feedGateRef.current = { court, gate: feedGate };
        const scheduleRefresh = (event) => feedGate.event(event && event.seq);
        const refreshNow = () => feedGate.now();
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
            setCourtComps((prev) => (Array.isArray(prev) ? prev.map((c) => (c.id === cid ? pushes.push(c, cid, event) : c)) : prev));
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
            // A RECONNECT (an 'open' following an 'error') restarts the gate:
            // the server may have restarted (its seqs begin again), so no feed
            // held counts as applied, and one refetch follows (still coalesced,
            // 300 to 600 ms) so a court that missed events while disconnected
            // self-heals.
            const onStatus = makeReconnectRefetcher(() => { if (!cancelled) feedGate.restart(); });
            const off = window.API.subscribeToEvents(
                (event) => {
                    if (cancelled || !event) return;
                    // Every held copy may be wrong: no push outranks the
                    // refetch that follows.
                    if (event.type === "resync_required" || event.type === "tournament_updated") pushes.clear();
                    if (!REFRESH_EVENTS.has(event.type)) return;
                    if (event.type === "resync_required") {
                        // A new stream: no feed held counts as applied, and
                        // the refetch is asked for whatever its seq says.
                        feedGate.restart();
                        return;
                    }
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
                    scheduleRefresh(event);
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
        const onVisible = () => { if (!document.hidden && !cancelled) refreshNow(); };
        document.addEventListener("visibilitychange", onVisible);
        return () => {
            cancelled = true;
            timerPool.clearAll();
            feedGate.dispose();
            if (feedGateRef.current && feedGateRef.current.gate === feedGate) feedGateRef.current = null;
            document.removeEventListener("visibilitychange", onVisible);
            unsub();
        };
    }, [court, refreshCourt, fetchCourt, takeCourt, pushes]);

    // Court-scoped competitions: the live feed once loaded, else the prop
    // aggregate as a transient fallback. All competition/match derivations below
    // read from this so the page operates only on THIS court's competitions.
    const courtCompetitions = courtComps || tournament.competitions || NO_COMPETITIONS;

    const paceLookup = useCourtPaceLookup(courtCompetitions);

    // The key of the start in flight (a pick still deferring the running bout
    // included), for the rendering only (Starting…): the start hook's guards
    // read its refs, never this, and it follows them (syncStartingKey).
    const [startingKey, setStartingKey] = useState(null);
    const [contextOpen, setContextOpen] = useState(true);
    // The whole queue column folds away so the scorer can take the full width;
    // the choice is per device, like the operator's other console preferences.
    const [queueOpen, setQueueOpen] = useState(() => {
        try { return localStorage.getItem(QUEUE_OPEN_KEY) !== "0"; } catch (_) { return true; }
    });
    // Set only by an operator click in toggleQueue below, never true on mount
    // or on a server-driven rerender, so the focus effect it gates fires
    // solely for an explicit toggle.
    const queueToggledByUserRef = useRef(false);
    // The two controls that swap places across the fold: the rail button only
    // exists while collapsed, the header button only exists while expanded.
    const queueShowBtnRef = useRef(null);
    const queueHideBtnRef = useRef(null);
    const toggleQueue = () => {
        queueToggledByUserRef.current = true;
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
    useEffect(() => {
        if (!queueToggledByUserRef.current) return;
        queueToggledByUserRef.current = false;
        const target = queueOpen ? queueHideBtnRef.current : queueShowBtnRef.current;
        // preventScroll: the counterpart sits where the pressed control was,
        // already in view, so the focus move must not scroll the page.
        if (target) target.focus({ preventScroll: true });
    }, [queueOpen]);
    // Completed list stays expanded (it's the operator's running record), but a
    // full-day court accumulates many bouts that would bury the live queue on
    // mobile. Show the most recent COMPLETED_PREVIEW by default; the rest fold
    // behind an inline "Show all N" toggle. The recent ones are never hidden.
    const [showAllCompleted, setShowAllCompleted] = useState(false);
    // The match the operator has explicitly picked to score from the queue.
    // null = follow the running bout (running[0]). A picked match drives the
    // scoring panel instead; the find() in pickedMatch guards staleness, so a
    // pick that completes or vanishes falls back to running[0] automatically.
    const [pickedKey, setPickedKey] = useState(null);
    // The match pickMatch started, and the scheduled snapshot it started
    // from: see ScoreEditorModal's `started` (bc-strt).
    const [startedFrom, setStartedFrom] = useState(null);
    // The status moves this device made (a send back, a finish, a decision)
    // show at once over the court's feed, offline included, until the feed
    // shows them (withOwnStatusMove): the panel moves on, a bout sent back
    // leaves the live slot and the editor. They are read from the outbox
    // through ONE owner (API.ownStatusMove, stage 6 review 8, M4), never kept
    // in this page's state, so a reload or the console mounted again shows
    // what the operator did; a refused or discarded move leaves the outbox,
    // and the match shows as the server holds it. Re-read whenever a move
    // changes: each change gives a new reader, so what reads it is worked out
    // again.
    const [ownMoveOf, setOwnMoveOf] = useState(() => readOwnMove);
    useEffect(() => {
        if (!window.API || typeof window.API.subscribeOwnStatusMoves !== "function") return undefined;
        return window.API.subscribeOwnStatusMoves(() => setOwnMoveOf(() => (m) => readOwnMove(m)));
    }, []);
    // The COMPLETED match the operator has opened to correct in place (parity
    // with the Scores page's "Correct" button). Kept separate from pickedKey so
    // it takes priority over the running bout WITHOUT disturbing it, and so it
    // never interferes with the finish-advance fallback that pickedKey drives.
    // Cleared by "Back to court", and handed over to pickedKey once the match
    // is reopened (see the effect beside correctingMatch below): a running
    // match is the live bout, not a correction. null = not correcting anything.
    const [correctingKey, setCorrectingKey] = useState(null);
    // A scheduled team match whose lineup the operator is entering before start.
    // Opens the team scoresheet as a modal; positions persist via putMatchLineup
    // independent of scoring, so the operator can set the lineup and close
    // without starting (or hit Start from inside the modal). Held as a key and
    // resolved from the court's matches below, like pickedKey: a snapshot would
    // keep the panel's editors on the teams the match had when it was opened,
    // when a feeding match decided on another device has since seated another
    // team on a side.
    const [lineupKey, setLineupKey] = useState(null);
    // Selected competition for filtering the queue. Default: running match's comp,
    // else first comp with scheduled matches here, else any comp with matches here.
    const [selectedCompId, setSelectedCompId] = useState(null);

    // Short-circuit to [] for a blank/unknown court so an accidental landing
    // (e.g. /admin/shiaijo/%20) never sorts/partitions the whole tournament.
    // Same condition as courtKnown below.
    // The court's full match set BEFORE the hasBothSides split. Actionable rows
    // (allMatches) and pending placeholder finals (pendingPlaceholder) are both
    // derived from this so the two are computed from one filtered pass.
    const courtMatchesRaw = useMemo(
        () => (tournament.courts || []).includes(court)
            ? window.filterMatchesByCourt(window.tournamentMatches({ competitions: courtCompetitions }), court)
            : [],
        [courtCompetitions, tournament.courts, court]
    );
    // A match this device finished or sent back reads so, until the feed says
    // so itself (withOwnStatusMove): every list below (running, Up next,
    // allDone, the advance target) agrees on it.
    const allMatches = useMemo(
        () => {
            const entryMeta = new Map();
            for (const comp of courtCompetitions) {
                const entries = courtQueueEntriesOf(comp);
                for (const e of entries) {
                    entryMeta.set(`${e.competitionId}/${e.matchId}`, {
                        competitionId: e.competitionId,
                        competitionStartTime: e.competitionStartTime,
                        date: e.date,
                        drawIndex: e.drawIndex,
                    });
                }
            }
            return courtMatchesRaw.filter(hasBothSides).map((m) => {
                const moved = withOwnStatusMove(m, ownMoveOf(m));
                const meta = entryMeta.get(`${m.compId}/${m.id}`) || {};
                return {
                    ...moved,
                    competitionId: meta.competitionId || m.compId || "",
                    competitionStartTime: meta.competitionStartTime || "",
                    date: meta.date || "",
                    drawIndex: meta.drawIndex ?? 0,
                };
            });
        },
        // ownMoveOf: a move queued, answered, refused or discarded.
        [courtMatchesRaw, courtCompetitions, ownMoveOf]
    );
    // A move the server answered ends once the feed shows it on a copy not
    // older than the move, or a later change made elsewhere: the owner
    // forgets it (ownStatusMoveEnded).
    useEffect(() => {
        if (!window.API || typeof window.API.forgetOwnStatusMove !== "function") return;
        for (const m of courtMatchesRaw) {
            if (ownStatusMoveEnded(ownMoveOf(m), m)) window.API.forgetOwnStatusMove(m.compId, m.id);
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
    }, [courtMatchesRaw, tournament.competitions, ownMoveOf]);
    // Scheduled knockout bouts still waiting on a "Winner of rX-mY" feeder
    // (mp-y3nk). Kept SEPARATE from allMatches / filteredScheduled so they never
    // become upNext, never enter moveMatch reordering, and never carry a Start
    // button: a placeholder side has no participant to call to the court. They
    // are rendered as non-actionable "later" rows so a court whose only remaining
    // bout is a downstream final does not show a falsely-empty Upcoming list.
    // Each competition's bracket rounds, by id: the 3rd-place match is pending
    // only while a semi-final that feeds an empty side is unplayed, so the
    // pending split and the slot labels below both read them.
    const roundsFor = useMemo(() => {
        const byComp = new Map();
        for (const c of courtCompetitions) byComp.set(c.id, (c.bracket && c.bracket.rounds) || []);
        return (compId) => byComp.get(compId) || [];
    }, [courtCompetitions]);
    const pendingPlaceholder = useMemo(
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
    const bracketLabelsFor = useMemo(() => {
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
    const slotLabelFor = useCallback((compId) => bracketLabelsFor(compId).slot, [bracketLabelsFor]);
    const bronzeLabelsFor = useCallback((compId) => bracketLabelsFor(compId).bronze, [bracketLabelsFor]);
    const { sorted, running, scheduled, completed } = useMemo(
        () => partitionShiaijoMatches(allMatches),
        [allMatches]
    );

    const courts = tournament.courts || [];
    const courtKnown = courts.includes(court);

    // All competitions that have at least one match on this court (AC3/AC4).
    // Pending placeholder finals count too, so a court whose only remaining bout
    // is a downstream final still surfaces its competition in the selector and
    // is not treated as empty.
    const courtsComps = useMemo(() => {
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
    const effectiveCompId = useMemo(() => {
        if (!courtsComps.length) return null;
        // If explicitly selected and still valid, keep it
        if (selectedCompId && courtsComps.some(c => c.id === selectedCompId)) return selectedCompId;
        // Default: running match's comp
        if (running.length > 0) return running[0].compId;
        // Else first waiting match in court-queue order
        if (scheduled.length > 0) return scheduled[0].compId;
        // Else any comp
        return courtsComps[0].id;
    }, [selectedCompId, courtsComps, running, scheduled]);
    // One operator per court (operator ruling 2026-09-26): the console never
    // switches competition by itself. The default above only CHOOSES the first
    // one shown; it is then held as if picked, so a finished bout or a match
    // moved onto this court cannot swap the competition under the operator.
    // From then on only the picker and the banner below change it (and a
    // competition that leaves this court falls back to the default again).
    useEffect(() => {
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
    const pickedMatch = useMemo(
        () => pickedKey ? sorted.find((x) => matchKey(x) === pickedKey && x.status !== "completed") || null : null,
        [pickedKey, sorted]
    );
    // The completed match being corrected. Takes priority over the running
    // match so the panel follows the deliberate correction the operator asked
    // for. "Back to court" clears correctingKey to fall back to the live bout.
    // No status filter here, on purpose: a filter would leave the key set, and
    // the key would re-pin the match the moment it completed again. The
    // effect below ends the correction instead.
    const correctingMatch = useMemo(
        () => correctingKey ? sorted.find((x) => matchKey(x) === correctingKey) || null : null,
        [correctingKey, sorted]
    );
    const lineupMatch = useMemo(
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
    useEffect(() => {
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
    const selectedMatch = useMemo(
        () => correctingMatch || pickedMatch || running[0] || null,
        [correctingMatch, pickedMatch, running]
    );

    // bc-strt: the feed's copy of the match this console started (startMatch
    // pins it, `startedFrom`), until the refetch shows it running. ONE
    // predicate for startedHere below, the editor's `started` prop and the
    // lists that hide the started match from Up next.
    const isStartedSnapshot = useCallback(
        (m) => !!m && !!startedFrom && startedFrom.key === matchKey(m) && startedFrom.at === m.modifiedAt,
        [startedFrom]
    );
    // That match on the court, anchored on the start pin itself and not on
    // the pick, so a pick cleared elsewhere cannot hide it; a finish laid on
    // it here (withOwnStatusMove) ends it.
    const startedHere = startedFrom
        ? sorted.find((x) => x.status !== "completed" && isStartedSnapshot(x)) || null
        : null;
    // The bout running on this court as this console knows it (bc-sssf): the
    // pick when the feed says it is running, else the start this console just
    // made (the feed still reads "scheduled" until the refetch; a queued
    // offline start until the connection is back), else running[0]
    // (court-wide, never filtered by the competition selector). ONE owner for
    // the bout a pick sends back (deferAndStart, and its re-judge through
    // latestRef), the live bout below and the Send back to queue button: read
    // from `running` alone, a pick made in the window between a start and its
    // refetch found nothing running and started over it, which the server
    // refused as court_busy. A start still out (startingBlocker) stays out of
    // it on purpose: pickMatch refuses a pick while one is out, and during a
    // pick's own send back it names the pick itself.
    const liveHere = (pickedMatch && pickedMatch.status === "running")
        ? pickedMatch
        : startedHere || running[0] || null;
    // The match whose start request is still in flight (startingKey), found on
    // the court, for the render (liveBout below). The guards read startingRef.
    const startingBlocker = startingKey ? sorted.find((x) => matchKey(x) === startingKey) || null : null;
    // The bout live on this court right now (bc-crpn): liveHere, else the
    // match whose start request is still in flight.
    const liveBout = liveHere || startingBlocker;
    // The correction the console holds open (completed), if any. A reopened
    // one is the live bout and is handed over by the effect above.
    const openCorrection = correctingMatch && correctingMatch.status === "completed" ? correctingMatch : null;
    // The "Another bout is running" list: every running bout but the one the
    // panel shows, by key (all of them while a correction holds the panel,
    // and while it shows this console's own start the feed does not yet hold
    // as running, so another device's bout is never left off).
    const shownKey = selectedMatch ? matchKey(selectedMatch) : null;
    const alsoRunning = openCorrection ? running : running.filter((x) => matchKey(x) !== shownKey);
    // leaveEditorConfirmed: true when the console may unmount the editor or put
    // another match in it: nothing unsaved, or the operator chose Discard
    // entries (R1). With nothing unsaved it answers `true` at once.
    const leaveEditorConfirmed = () => confirmLeave(editorUnsavedRef, () => mountedRef.current);
    // leaving: a page exit (the Dashboard crumb, Sign out, Public viewer, the
    // court switcher) behind the same question; with nothing unsaved it goes
    // in the same tick (stage 5 review 9, M1).
    const leaving = (go) => (...args) => leaveThen(editorUnsavedRef, () => go(...args), () => mountedRef.current);
    // markSentBackHere: the outbox holds a send back of the match keyed `key`
    // (its pending answer `res`): the console shows it queued from the outbox
    // itself (withOwnStatusMove), and a start pin of the match (startedFrom)
    // goes with it: the bout is queued.
    const markSentBackHere = (key, res) => {
        if (!key || !writePending(res)) return;
        setStartedFrom((prev) => (prev && prev.key === key ? null : prev));
    };
    // Who runs this shiaijo (bc-sseq FR9). While another tab or device runs it,
    // or nobody does and this tablet lost it, the console is view only: the
    // action area is a disabled fieldset, and every handler below asks
    // refusedNotHeld(controlRef) before it acts (the outbox's own gate,
    // court_control.jsx, refuses any write that still reaches it). The
    // automatic withdrawal of this console's court calls is not gated:
    // taking a call down is cleanup the new holder wants too.
    const control = useShiaijoControl(courtKnown ? court : "", password, showToast);
    const { controlRef, canWrite } = control;
    // The pre-start lineup panel saves a lineup: opened only while this tab runs
    // the shiaijo (or has had no answer yet).
    const openLineup = (m) => {
        if (refusedNotHeld(controlRef)) return;
        setLineupKey(matchKey(m));
    };
    // The start, pick and correct flow (useCourtStart, admin_shiaijo_start.jsx),
    // called once the court's live bout, the open correction and the selected
    // match are known. It binds latestRef in its own layout effect.
    const {
        pickingRef, refusalNotice, startNoticeFor, startMatch, tapStart, correctMatch, stopCorrecting,
        endCorrectionOf, pendingStartAsk, answerStartAsk,
    } = useCourtStart({
        court, password, showToast, onEditScore, mountedRef, editorUnsavedRef, leaveEditorConfirmed,
        allMatches, courtMatchesRaw, liveHere, liveBout, openCorrection, selectedMatch, isStartedSnapshot,
        startingKey, setStartingKey, startError, setStartError, setPickedKey, setCorrectingKey, setStartedFrom,
        markSentBackHere, controlRef,
    });
    // Call to court and its withdrawal (useCourtCalls, admin_shiaijo_calls.jsx),
    // which binds withdrawCallRef for the court feed's handler.
    const { callingKey, isCalled, callToCourt, withdrawCall } = useCourtCalls({
        court, password, showToast, mountedRef, callIdsRef, withdrawCallRef,
        courtComps, courtCompetitions, courtMatchesRaw, tournament, controlRef,
    });

    // For pool daihyosen/tiebreaker bouts, enrich the selected match with
    // rep-player roster data so ScoreEditorModal renders the rep-picker dropdowns
    // (repIsTeam / repRosterA / repRosterB). Regular matches pass through
    // unchanged. enrichPoolMatchWithComp is exposed by admin_pools.jsx via
    // window.enrichPoolMatchWithComp; isSupplementaryBout via window.isSupplementaryBout.
    // These globals are assigned at module evaluation time, so they are always
    // present when admin_shiaijo.jsx executes (admin_pools.js loads first per
    // the <script> order in index.html).
    const editorMatch = useMemo(() => {
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
    const filteredScheduled = useMemo(
        () => scheduled.filter((m) => (!effectiveCompId || m.compId === effectiveCompId) && !isStartedSnapshot(m)),
        [scheduled, effectiveCompId, isStartedSnapshot]
    );
    const filteredCompleted = useMemo(
        () => effectiveCompId ? completed.filter(m => m.compId === effectiveCompId) : completed,
        [completed, effectiveCompId]
    );
    // Pending placeholder finals for the selected competition, rendered as
    // non-actionable "later" rows below Upcoming. Sorted by scheduled time so a
    // multi-round pending tail reads in play order.
    const filteredPending = useMemo(
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
    const contextMatch = useMemo(
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
    const contextLead = useMemo(() => {
        if (contextMatch?.status === "scheduled") return "Up next";
        if (contextMatch?.status !== "completed") return "";
        return contextMatch === correctingMatch ? "Correcting" : (selectedMatch ? "" : "Just played");
    }, [contextMatch, correctingMatch, selectedMatch]);

    // The rows the Completed section shows: the whole list once the operator
    // expands it, else the most recently played preview. Memoised because this
    // console re-renders on every SSE broadcast and a full-day court can hold
    // a hundred completed bouts.
    const completedShown = useMemo(
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

    // "Which pool is next" for the context panel: the first upcoming pool on
    // this court (within the selected comp) whose pool differs from the one
    // currently in focus.
    const nextPoolName = useMemo(() => {
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

    // The match a finish starts, if any: the advance target, when it is still
    // scheduled. The editor's button reads "Finish + Start Next" only while this
    // is non-null, and plain "Finish" on the court's last bout.
    const nextToStartAfter = (m, withdrawn = null) => {
        const next = nextActiveAfter(m, withdrawn);
        return next && next.status === "scheduled" ? next : null;
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
    const nudgeBanner = useMemo(() => {
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
    }, [allMatches, effectiveCompId, running, liveBout, filteredScheduled, courtKnown, isStartedSnapshot]);

    // Send back to queue, a court move and a queue reorder, with the console's
    // confirms (useCourtMoves, admin_shiaijo_moves.jsx). Called last of the
    // three: it needs the filtered queue, the start hook's pick in flight and
    // the calls hook's withdrawCall.
    const { moveMatch, requestRevert, requestMoveCourt, confirms } = useCourtMoves({
        court, password, showToast, onMoveCourt, mountedRef, editorUnsavedRef, selectedMatch, sorted,
        filteredScheduled, pickingRef, markSentBackHere, setPickedKey, setCorrectingKey, withdrawCall,
        pendingStartAsk, answerStartAsk, controlRef,
    });

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
            {/* Who runs this shiaijo, stuck under the top bar so it is on
                screen however far the operator scrolls (bc-sseq FR9). */}
            {courtKnown && <ShiaijoControlBar court={court} control={control} showToast={showToast} />}
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
                                    <span className="shiaijo-title-select__chevron" aria-hidden="true"><Icon name="chevron-down" size={16} /></span>
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
                                                <span className="shiaijo-title-select__chevron" aria-hidden="true"><Icon name="chevron-down" size={16} /></span>
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
                        <h3>Unknown shiaijo &quot;{court}&quot;</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            This court isn&apos;t part of the tournament: it may have been renamed or removed.{" "}
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
                                <Icon name="chevron-right" size={14} />
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
                                <span className="shiaijo-queue__head-action"><Icon name="chevron-left" size={12} className="icon-lead" />Hide</span>
                            </button>
                            {filteredScheduled.length === 0 && filteredPending.length === 0 && filteredCompleted.length === 0 && (
                                <p className="shiaijo-queue__empty">Nothing else queued on this court.</p>
                            )}
                            {/* The queue's actions (Start, Call, Lineup, the moves, a
                                barred row's decision) are view only while this tab
                                does not run the shiaijo. The Hide toggle and Show all
                                stay outside: they only change the view. */}
                            <fieldset className="shiaijo-control-scope shiaijo-control-scope--queue" disabled={!canWrite}>
                            {upNext && (
                                <ShiaijoUpNextCard
                                    upNext={upNext} paceLookup={paceLookup} notice={upNextNotice}
                                    starting={startingKey === matchKey(upNext)}
                                    called={isCalled(matchKey(upNext))} calling={callingKey === matchKey(upNext)}
                                    onStart={(ev) => tapStart(upNext, ev)}
                                    onEnterLineup={() => openLineup(upNext)}
                                    onCall={window.API && typeof window.API.sendAnnouncement === "function" ? (ev) => callToCourt(upNext, ev) : null}
                                    onMoveDown={window.API && typeof window.API.updateMatchTime === "function" && filteredScheduled.length > 1 ? () => moveMatch(upNext, "down") : null}
                                />
                            )}

                            {/* No "Now" group: the running match is officiated in the
                                scoring panel on the right, so repeating it in the queue is
                                redundant. */}

                            {upcomingQueueMatches.length > 0 && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" subGroup matches={upcomingQueueMatches}
                                    courts={courts} onMoveCourt={requestMoveCourt}
                                    onMove={moveMatch} onEnterLineup={openLineup}
                                    onPick={tapStart} noticeFor={(m) => startNoticeFor(matchKey(m))}
                                    onCall={callToCourt} callingKey={callingKey} isCalled={isCalled} startingKey={startingKey}
                                    scheduled={filteredScheduled}
                                    password={password}
                                />
                            )}
                            </fieldset>

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
                                    <fieldset className="shiaijo-control-scope" disabled={!canWrite}>
                                        <ShiaijoQueueGroup
                                            matches={completedShown}
                                            courts={courts} onMoveCourt={requestMoveCourt} onCorrect={correctMatch}
                                            noticeFor={(m) => refusalNotice(matchKey(m))}
                                        />
                                    </fieldset>
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
                                            <span className="shiaijo-nudge__icon" aria-hidden="true"><Icon name="alert-circle" size={15} /></span>
                                        )}
                                        <span className="shiaijo-nudge__text">{text}</span>
                                        <span className="shiaijo-nudge__cta" aria-hidden="true">Switch</span>
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

                            {/* Every running bout the panel does not show (alsoRunning):
                                while a correction holds the panel that is all of them (a
                                match started from another screen mid-correction, bc-crpn). */}
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
                                // View only while this tab does not run the shiaijo:
                                // readOnly={!canWrite} disables write controls and
                                // scoring shortcuts while keeping read-only controls
                                // (History, Ippon key, Prev/Next, Discard held write)
                                // usable; the outbox gate still refuses any write.
                                <ScoreEditorModal
                                    readOnly={!canWrite}
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
                                            // Refused before it was queued: this tab does not run
                                            // the shiaijo (court_control.jsx). Nothing to end or
                                            // advance; the editor shows the refusal.
                                            if (writeCourtNotHeld(res)) return res;
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
                                    startsNext={!!nextToStartAfter(selectedMatch)}
                                    onSubmitAndNext={async (patch) => {
                                        const next = nextToStartAfter(selectedMatch);
                                        let res;
                                        try {
                                            res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            // Refused before it was queued (court_control.jsx): no
                                            // advance, no next start.
                                            if (writeCourtNotHeld(res)) return res;
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
                                            if (next) {
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
                                        // A decision refused before it was queued (this tab
                                        // does not run the shiaijo) advances nothing and starts
                                        // nothing; the editor already says why.
                                        if (writeCourtNotHeld(result) || refusedNotHeld(controlRef, { quiet: true })) return;
                                        // A fusenpai/hantei decision already persisted the bout via
                                        // the /decision POST: no score PUT here. It still resolves a
                                        // winner, so offline we must advance the LOCAL bracket the same
                                        // way the score path does (mp-y3nk), else a decision-completed
                                        // bout leaves the next match on placeholders until Refresh/SSE.
                                        // A decision the server refuses or the operator discards later
                                        // takes the advance back (advanceLocalUntilRefused).
                                        advanceLocalUntilRefused(selectedMatch, bracketSideName(result && result.winner), result);
                                        // A decision the outbox holds shows at once, read from the
                                        // outbox (withOwnStatusMove, X3, M4).
                                        // Then start the next scheduled match so the panel advances
                                        // (mirrors onSubmitAndNext), passing over the matches of a
                                        // competitor a withdrawal or no-show has just barred.
                                        const next = nextToStartAfter(selectedMatch, sideBarredByDecision(result, selectedMatch));
                                        if (next) {
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
                                        Back to court
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
                                another running match). Offered on a running match,
                                or on the court's live bout as this console knows
                                it (liveHere, by key): a start this console just
                                made reads "scheduled" in the feed until the
                                refetch (until the connection is back for one
                                queued offline), and its exit is needed at once
                                (bc-sssf); the outbox sends the send back after
                                the start it follows. Neither holds for a
                                still-completed correction, which shows "Back to
                                court" instead. Offered whatever has been scored:
                                the match keeps its score in the queue (bc-sbq). */}
                            {!allDone && selectedMatch && (selectedMatch.status === "running" || (liveHere && matchKey(selectedMatch) === matchKey(liveHere))) && window.API && typeof window.API.revertMatchToQueue === "function" && (
                                <div className="shiaijo-revert">
                                    <button
                                        type="button"
                                        className="btn btn--sm btn--ghost"
                                        onClick={() => requestRevert(selectedMatch)}
                                        disabled={!canWrite}
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

            <ShiaijoConfirms {...confirms} />

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

window.AdminShiaijoPage = AdminShiaijoPage;
