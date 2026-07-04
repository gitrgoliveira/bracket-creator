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

const { useState: useStateSh, useMemo: useMemoSh, useEffect: useEffectSh, useRef: useRefSh } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const ScoreEditorModal = window.ScoreEditorModal;
const CourtPicker = window.CourtPicker;
const BracketTree = window.BracketTree;
const Icon = window.Icon;
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

export function partitionShiaijoMatches(matches) {
    const sorted = sortShiaijoMatches(matches);
    const running = [], scheduled = [], completed = [];
    for (const m of sorted) {
        if (m.status === "running") running.push(m);
        else if (m.status === "scheduled") scheduled.push(m);
        else if (m.status === "completed") completed.push(m);
    }
    return { sorted, running, scheduled, completed };
}

const matchKey = (m) => `${m.compId}:${m.id}`;

// How many of the most-recent completed bouts the Completed section shows
// before the "Show all N" toggle. Keeps the live queue + standings above the
// fold on a full-day court without hiding the recent record.
const COMPLETED_PREVIEW = 8;

// A team encounter (vs an individual bout): team matches carry a lineup the
// operator can set before the bout starts. Exported for unit tests (it gates
// the "Enter lineup" affordance).
export const isTeamMatch = (m) => !!m && (m.compKind === "team" || (m.teamSize || 0) > 0);

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
        const iv = window.teamIVScore ? window.teamIVScore(m) : null;
        return iv ? { kind: "team", iv } : { kind: "none" };
    }
    const ipponsA = m.ipponsA || (window.ipponsFromScore ? window.ipponsFromScore(m.scoreA) : []);
    const ipponsB = m.ipponsB || (window.ipponsFromScore ? window.ipponsFromScore(m.scoreB) : []);
    const s = window.formatIpponsScore
        ? window.formatIpponsScore(ipponsB, ipponsA, m.score, m.decision, m.encho, m.decidedByHantei)
        : "";
    return s ? { kind: "ippon", ippon: s } : { kind: "none" };
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
    useEffectSh(() => {
        if (!court || !window.API || typeof window.API.fetchCourtMatches !== "function") return;
        let cancelled = false;
        const timers = new Set();
        const refresh = () => {
            window.API.fetchCourtMatches(court)
                .then(comps => { if (!cancelled && mountedRef.current) setCourtComps(comps); })
                .catch(err => console.error("Failed to fetch court matches", err));
        };
        refresh();
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
            const off = window.API.subscribeToEvents((event) => {
                if (cancelled || !event || !REFRESH_EVENTS.has(event.type)) return;
                const id = setTimeout(() => { timers.delete(id); if (!cancelled) refresh(); }, 200 + Math.random() * 400);
                timers.add(id);
            });
            unsub = () => { if (typeof off === "function") off(); };
        }
        return () => { cancelled = true; timers.forEach(clearTimeout); unsub(); };
    }, [court]);

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
    const [startError, setStartError] = useStateSh("");
    const [contextOpen, setContextOpen] = useStateSh(true);
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
    // Selected competition for filtering the queue. Default: running match's comp,
    // else first comp with scheduled matches here, else any comp with matches here.
    const [selectedCompId, setSelectedCompId] = useStateSh(null);

    // Short-circuit to [] for a blank/unknown court so an accidental landing
    // (e.g. /admin/shiaijo/%20) never sorts/partitions the whole tournament.
    // Same condition as courtKnown below.
    const allMatches = useMemoSh(
        () => (tournament.courts || []).includes(court)
            ? window.filterMatchesByCourt(window.tournamentMatches({ competitions: courtCompetitions }).filter(hasBothSides), court)
            : [],
        [courtCompetitions, tournament.courts, court]
    );
    const { sorted, running, scheduled, completed } = useMemoSh(
        () => partitionShiaijoMatches(allMatches),
        [allMatches]
    );

    const courts = tournament.courts || [];
    const courtKnown = courts.includes(court);

    // All competitions that have at least one match on this court (AC3/AC4).
    const courtsComps = useMemoSh(() => {
        const seen = new Set();
        const out = [];
        for (const m of allMatches) {
            if (!seen.has(m.compId)) {
                seen.add(m.compId);
                const comp = courtCompetitions.find(c => c.id === m.compId);
                out.push({ id: m.compId, name: m.compName || (comp && comp.name) || m.compId });
            }
        }
        return out;
    }, [allMatches, courtCompetitions]);

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
    // match to run out of order via its "Score" button (pickMatch). Picking is
    // governed by two rules in pickMatch: it is BLOCKED while the current bout
    // has scoring in progress (you must finish or correct it first), and an
    // unscored current bout is DEFERRED one slot before the new pick starts.
    // A completed pick (or one that disappears) falls back to running[0]: the
    // find() filters out completed matches and the `pickedMatch || running[0]`
    // expression covers the null case. Fixing a finished score is still done
    // in the competition's own admin view, so completed matches aren't picked.
    const pickedMatch = useMemoSh(
        () => pickedKey ? sorted.find((x) => matchKey(x) === pickedKey && x.status !== "completed") || null : null,
        [pickedKey, sorted]
    );
    const selectedMatch = useMemoSh(() => pickedMatch || running[0] || null, [pickedMatch, running]);

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

    // The standings/context panel follows the court's current focus: not
    // strictly the running match: so it stays visible (and updates) after a
    // bout is finished, instead of collapsing to the empty state. Priority:
    // the running match; else the bout just played (last completed), so the
    // operator sees their result land in the standings; else the next
    // scheduled bout (before the court's first match).
    const contextMatch = useMemoSh(
        () => selectedMatch || filteredCompleted[filteredCompleted.length - 1] || filteredScheduled[0] || null,
        [selectedMatch, filteredCompleted, filteredScheduled]
    );

    // Up Next = the first scheduled match in the selected competition.
    const upNext = filteredScheduled[0] || null;

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
    const nextActiveAfter = (m) => {
        const pool = [...running, ...scheduled].filter((x) => x.compId === m.compId);
        const idx = pool.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return null;
        return pool.slice(idx + 1).find((x) => x.status !== "completed") || null;
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
        setStartError("");
        setStartingKey(matchKey(m));
        try {
            // Starting makes the match running; the scoring panel shows
            // running[0], so it picks the match up on the next refetch.
            await onEditScore(m.compId, m.id, startPatch(), m);
            return true;
        } catch (e) {
            if (mountedRef.current) setStartError((e && e.message) || "Could not start the match: check eligibility and try again.");
            if (showToast) showToast((e && e.message) || "Could not start the match", "error");
            return false;
        } finally {
            if (mountedRef.current) setStartingKey(null);
        }
    };

    // scoringStarted: has the current running bout had ANY score entered? Used
    // by pickMatch to decide whether switching away is safe: a bout with any
    // entered state must be finished or corrected first (you can't abandon a
    // half-scored bout, and the defer/un-start path would discard it).
    // Counts real ippons (ignoring "•" placeholder slots), points, FOULS
    // (hansoku: top-level or score.fouls), and team sub-bout results, so a
    // lone foul or a scored team sub-bout also locks the switch.
    const scoringStarted = (mm) => {
        if (!mm || mm.status !== "running") return false;
        const realIppons = (arr) => (arr || []).filter((x) => x && x !== "•").length;
        const fouls = (mm.hansokuA || 0) + (mm.hansokuB || 0) + (mm.score?.fouls?.a || 0) + (mm.score?.fouls?.b || 0);
        return realIppons(mm.ipponsA) > 0 ||
            realIppons(mm.ipponsB) > 0 ||
            (mm.score?.winnerPts || 0) > 0 ||
            (mm.score?.loserPts || 0) > 0 ||
            fouls > 0 ||
            (mm.encho?.periodCount || 0) > 0 ||
            (mm.subResults?.length || 0) > 0;
    };

    // pickMatch: run an upcoming match out of order. Rules:
    //   • Completed matches are never picked (correct them in the comp admin).
    //   • If a DIFFERENT bout is running with scoring already started, block: 
    //     finish or correct it first (can't abandon a half-scored bout).
    //   • If a DIFFERENT bout is running but unscored, defer it: un-start it
    //     (back to scheduled) so the court is never left with two running bouts.
    //     The deferred bout returns to the queue and runs after the picked one.
    //   • A scheduled pick is started (through the eligibility gate); a pick
    //     that's already running just takes the panel.
    const pickMatch = async (m) => {
        if (!m || m.status === "completed") return;
        const cur = running[0] || null;
        const isSame = cur && matchKey(cur) === matchKey(m);
        if (cur && !isSame) {
            if (scoringStarted(cur)) {
                if (showToast) showToast("Finish or correct the bout in progress first", "error");
                return;
            }
            // Unscored current bout: un-start it back to scheduled. moveMatch
            // only reorders scheduled rows (it no-ops on a running bout), so a
            // real defer here means clearing the running status: otherwise the
            // pick below would leave the court with two running bouts. The shape
            // mirrors buildPatch("scheduled") in the scoring modal. If the
            // defer write fails (surfaced via toast) we must NOT start the pick.
            // Reset to scheduled. scoringStarted (above) guarantees this bout has
            // no entered state, but clear every scoring field: ippons, fouls AND
            // team subResults: so no partially-entered data can be stranded on the
            // now-scheduled match.
            try {
                await onEditScore(cur.compId, cur.id, { status: "scheduled", winner: null, score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, subResults: [] }, cur);
            } catch (_e) {
                return;
            }
            if (!mountedRef.current) return;
        }
        if (m.status === "scheduled") {
            const ok = await startMatch(m);
            if (!ok || !mountedRef.current) return;
        }
        setPickedKey(matchKey(m));
    };

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
    const allDone = courtKnown && allMatches.length > 0 && running.length === 0 && filteredScheduled.length === 0;
    const selectedCompName = (courtsComps.find((c) => c.id === effectiveCompId) || {}).name || "";

    return (
        <div className="app">
            <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} hideRunningStrip />
            <div className="page page--wide">
                <Breadcrumbs items={[{ label: "Dashboard", onClick: onBack }, { label: `Shiaijo ${court}` }]} />
                <div className="page-head">
                    <div>
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
                        <div className="page-head__sub">{`Call, start, and score every match on Shiaijo ${court} from here.`}</div>
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
                                    <div className="page-head__sub shiaijo-officiating__sub">Officiating</div>
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

                {courtKnown && allMatches.length === 0 && (
                    <div className="empty">
                        <h3>No matches on this court</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            Matches appear here once assigned to Shiaijo {court}. Assign courts in the competition settings or the Schedule page.
                        </p>
                    </div>
                )}

                {courtKnown && allMatches.length > 0 && (
                    <div className="shiaijo">
                        {/* ── Queue (left) ───────────────────────────── */}
                        <div className="shiaijo__queue">
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
                                        {startError && <div className="shiaijo-upnext__error" role="alert">{startError}</div>}
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

                            {filteredScheduled.length > (upNext ? 1 : 0) && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" subGroup matches={upNext ? filteredScheduled.slice(1) : filteredScheduled}
                                    courts={courts} onMoveCourt={requestMoveCourt}
                                    onMove={moveMatch} onEnterLineup={setLineupMatch}
                                    onPick={pickMatch}
                                    onCall={callToCourt} callingKey={callingKey} calledKey={calledKey} startingKey={startingKey}
                                    scheduled={filteredScheduled}
                                />
                            )}

                            {filteredCompleted.length > 0 && (
                                <div className="shiaijo-completed">
                                    {/* Completed matches stay expanded: this is the operator's
                                        running record of what's been played on this court/pool today,
                                        so it must not be hidden behind a collapse toggle. To keep the
                                        live queue above the fold on a full day, only the most recent
                                        COMPLETED_PREVIEW show by default; "Show all N" reveals the rest.
                                        completed is sorted ascending, so the most recent are the tail. */}
                                    <div className="section-title">
                                        Completed <span className="shiaijo-count" aria-label={`${filteredCompleted.length} matches`}>{filteredCompleted.length}</span>
                                    </div>
                                    <ShiaijoQueueGroup
                                        matches={(showAllCompleted || filteredCompleted.length <= COMPLETED_PREVIEW)
                                            ? filteredCompleted
                                            : filteredCompleted.slice(-COMPLETED_PREVIEW)}
                                        courts={courts} onMoveCourt={requestMoveCourt}
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
                                    <span className="shiaijo-nudge__icon" aria-hidden="true">⚠</span>
                                    <span className="shiaijo-nudge__text">
                                        {`Switch to ${nudgeBanner.comp}: ${nudgeBanner.count} match${nudgeBanner.count === 1 ? "" : "es"} waiting on this court.`}
                                    </span>
                                    <span className="shiaijo-nudge__cta" aria-hidden="true">Switch →</span>
                                </button>
                            )}
                            {allDone && (
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

                            {!allDone && selectedMatch && (
                                <ScoreEditorModal
                                    key={`${matchKey(selectedMatch)}:${(selectedMatch.subResults || []).length}`}
                                    variant="inline"
                                    match={editorMatch}
                                    onClose={() => {}}
                                    canClose={false}
                                    onSubmit={async (patch) => {
                                        try {
                                            const res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            // F5: queued write: return signal so the editor shows the pending-save banner.
                                            if (res && res.queued) return res;
                                        }
                                        catch (_e) { /* surfaced via toast */ }
                                    }}
                                    onSubmitAndNext={async (patch) => {
                                        const next = nextActiveAfter(selectedMatch);
                                        try {
                                            const res = await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            if (!mountedRef.current) return res;
                                            // F5: queued write: do NOT advance. Return signal to editor.
                                            if (res && res.queued) return res;
                                            // Finish + start the next scheduled match, which then
                                            // becomes the running match the panel shows.
                                            if (next && next.status === "scheduled") {
                                                try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
                                            }
                                        } catch (_e) { /* keep panel */ }
                                    }}
                                    onAfterDecision={async () => {
                                        // A kiken/fusenpai decision already persisted the bout via
                                        // the /decision POST: no score PUT here. Just start the next
                                        // scheduled match so the panel advances (mirrors onSubmitAndNext).
                                        const next = nextActiveAfter(selectedMatch);
                                        if (next && next.status === "scheduled") {
                                            try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
                                        }
                                    }}
                                    password={password}
                                />
                            )}

                            {!allDone && !selectedMatch && (
                                <div className="empty shiaijo__placeholder">
                                    <h3>Ready when you are</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        Start the next match from the Up Next card to begin scoring on this court.
                                    </p>
                                </div>
                            )}

                            {contextMatch && (
                                <ShiaijoContext
                                    match={contextMatch} competitions={courtCompetitions}
                                    court={court} nextPoolName={nextPoolName} tweaks={tweaks}
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
// scoring: pool matches by pool ("Pool A", "Pool B"), playoff matches by round
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
            key = "round:" + (m.roundIndex != null ? m.roundIndex : (m.round || ""));
            label = m.round || "Playoffs";
        } else if (m.phase === "pool") {
            key = "pool:" + (m.poolName || "");
            label = m.poolName || "Pool";
        } else {
            key = "other";
            label = null;
        }
        if (!byKey.has(key)) { byKey.set(key, { key, label, matches: [] }); order.push(key); }
        byKey.get(key).matches.push(m);
    }
    return order.map((k) => byKey.get(k));
}

function ShiaijoQueueGroup({ label, matches, subGroup, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCall, callingKey, calledKey, startingKey }) {
    const renderRow = (m) => (
        <ShiaijoQueueRow
            key={matchKey(m)} m={m}
            scheduled={scheduled}
            courts={courts} onMoveCourt={onMoveCourt} onMove={onMove} onEnterLineup={onEnterLineup} onPick={onPick}
            onCall={onCall} callingKey={callingKey} calledKey={calledKey} startingKey={startingKey}
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

export function ShiaijoQueueRow({ m, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick, onCall, callingKey, calledKey, startingKey }) {
    const isComplete = m.status === "completed";
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
    const showActions = m.status === "scheduled" && (
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
                <span className="shiaijo-qrow__state">
                    {isComplete && <span className="shiaijo-qrow__final">Final</span>}
                </span>
            </div>
            <div className="shiaijo-qrow__match">
                <div className="shiaijo-qrow__side" aria-label={`Shiro: ${m.sideB?.name || ""}`}>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                    <span className="shiaijo-qrow__name">{m.sideB?.number ? <span className="num-prefix">{m.sideB.number}</span> : null}{m.sideB?.name}</span>
                </div>
                <span className="shiaijo-qrow__vs">vs</span>
                <div className="shiaijo-qrow__side shiaijo-qrow__side--aka" aria-label={`Aka: ${m.sideA?.name || ""}`}>
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <span className="shiaijo-qrow__name">{m.sideA?.number ? <span className="num-prefix">{m.sideA.number}</span> : null}{m.sideA?.name}</span>
                </div>
            </div>
            {/* Completed result on its own centred line BELOW the names: the
                canonical "marks in the centre" position, but stacked so the
                (often long) names keep the full-width line and never crowd. The
                ippon string is shiro: aka, matching the Shiro-left/Aka-right
                names above. */}
            {isComplete && (scoreCell.kind === "ippon" || scoreCell.kind === "team" || scoreCell.kind === "engi") && (
                <div className="shiaijo-qrow__result">
                    {scoreCell.kind === "team" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Individual Victories">IV</abbr>{scoreCell.iv}</span>}
                    {scoreCell.kind === "engi" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Total flags received">Flags</abbr>{scoreCell.flags}</span>}
                    {scoreCell.kind === "ippon" && scoreCell.ippon}
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
                    {onEnterLineup && isTeamMatch(m) && (
                        <button type="button" className="btn btn--ghost btn--sm" onClick={() => onEnterLineup(m)} title="Set the team lineup before starting">Lineup</button>
                    )}
                    {onMove && (
                        <>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move up" disabled={isFirst} onClick={() => onMove(m, "up")} title="Move earlier in the queue">↑</button>
                            <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move down" disabled={isLast} onClick={() => onMove(m, "down")} title="Move later in the queue">↓</button>
                        </>
                    )}
                    {/* Optional announce: mirrors the Up Next card so any queued match is a
                        complete view: call it to the floor, or start it directly. */}
                    {onCall && window.API && typeof window.API.sendAnnouncement === "function" && (
                        <button type="button" className="btn btn--ghost btn--sm" disabled={callingKey === matchKey(m)} onClick={() => onCall(m)} title="Announce this match to spectators and competitors">
                            {callingKey === matchKey(m) ? "Calling…" : (calledKey === matchKey(m) ? "Call again" : "Call to court")}
                        </button>
                    )}
                    {/* Start match is the primary per-row action: pushed to the end (the "go" slot).
                        Same pickMatch path as the Up Next card: defers an unscored running bout,
                        blocks while one is being scored, then starts this match for scoring. */}
                    {onPick && <button type="button" className="btn btn--primary btn--sm shiaijo-row__pick" disabled={startingKey === matchKey(m)} onClick={() => onPick(m)} title="Start this match now and begin scoring">{startingKey === matchKey(m) ? "Starting…" : "Start match"}</button>}
                </div>
            )}
        </div>
    );
}

function MatchSides({ m, large }) {
    return (
        <div className={`shiaijo-sides ${large ? "shiaijo-sides--lg" : ""}`}>
            <div className="shiaijo-sides__side" aria-label={`Shiro: ${m.sideB?.name || ""}`}>
                <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                <div className="name">
                    {m.sideB?.number ? <span className="num-prefix">{m.sideB.number}</span> : null}
                    {m.sideB?.name}
                </div>
                <div className="dojo">{m.sideB?.dojo}</div>
            </div>
            <div className="shiaijo-sides__vs">vs</div>
            <div className="shiaijo-sides__side" style={{ textAlign: "right" }} aria-label={`Aka: ${m.sideA?.name || ""}`}>
                <span className="se-color-badge se-color-badge--aka">AKA</span>
                <div className="name">
                    {m.sideA?.number ? <span className="num-prefix">{m.sideA.number}</span> : null}
                    {m.sideA?.name}
                </div>
                <div className="dojo">{m.sideA?.dojo}</div>
            </div>
        </div>
    );
}

// Collapsible context for the match being scored:
//   • pool phase  → live standings + results for the current pool (the shared
//     read-only window.PoolsViewer), plus which pool is next on this court.
//   • bracket phase → a bracket fragment with the current match highlighted.
function ShiaijoContext({ match, competitions, court, nextPoolName, tweaks, open, onToggle }) {
    const comp = (competitions || []).find((c) => c.id === match.compId);
    const bracket = comp && (comp.bracket || (Array.isArray(comp.rounds) ? { rounds: comp.rounds } : null));
    const isPool = match.phase === "pool";
    const isLeagueComp = match.compFormat === "league";
    const phaseLabel = isPool
        ? window.leagueAwareLabel(match.compFormat, match.poolName, "Pool")
        : (match.round || "Elimination");
    const PoolsViewer = window.PoolsViewer;

    // Pools/standings aren't on the console's competition list payload: fetch
    // the competition detail on demand. Refetch whenever this comp's pool
    // matches change (a scored bout), so standings stay current. poolSig is the
    // change key; it's cheap and keyed only to this comp.
    const poolSig = useMemoSh(() => {
        const pms = (comp && comp.poolMatches) || [];
        return pms.map((m) => `${m.id}:${m.status}:${m.scoreA || ""}:${m.scoreB || ""}`).join("|");
    }, [comp]);
    const [detail, setDetail] = useStateSh(null);
    const [detailErr, setDetailErr] = useStateSh(false);
    useEffectSh(() => {
        if (!isPool || !match.compId || !window.API || typeof window.API.fetchCompetitionDetails !== "function") {
            setDetail(null);
            return;
        }
        let cancelled = false;
        setDetailErr(false);
        window.API.fetchCompetitionDetails(match.compId)
            .then((d) => { if (!cancelled) setDetail(d); })
            .catch(() => { if (!cancelled) { setDetail(null); setDetailErr(true); } });
        return () => { cancelled = true; };
    }, [match.compId, isPool, poolSig]);

    const currentPool = detail && Array.isArray(detail.pools)
        ? detail.pools.find((p) => p.poolName === match.poolName)
        : null;

    return (
        <div className="shiaijo-context">
            <button type="button" className="section-title shiaijo-context__toggle" aria-expanded={open} onClick={onToggle}>
                {open ? "−" : "+"} {isPool ? "Standings" : "Context"} · {match.compName} · {phaseLabel}
            </button>
            {open && (
                <div className="shiaijo-context__body">
                    {isPool ? (
                        <>
                            {!isLeagueComp && (
                                <div className="shiaijo-context__next">
                                    {nextPoolName
                                        ? <><span className="shiaijo-context__next-label">Next pool on Shiaijo {court}:</span> <strong>{nextPoolName}</strong></>
                                        : <span className="shiaijo-context__next-label">Last pool on Shiaijo {court}.</span>}
                                </div>
                            )}
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
                    ) : (match.phase === "bracket" && BracketTree && bracket && bracket.rounds) ? (
                        <div className="shiaijo-context__bracket">
                            <BracketTree rounds={bracket.rounds} highlightId={match.id} />
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
