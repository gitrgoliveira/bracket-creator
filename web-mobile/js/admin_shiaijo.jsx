// Dedicated table-operator console for a single shiaijo (court) — Direction C.
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

// A team encounter (vs an individual bout) — team matches carry a lineup the
// operator can set before the bout starts. Exported for unit tests (it gates
// the "Enter lineup" affordance).
export const isTeamMatch = (m) => !!m && (m.compKind === "team" || (m.teamSize || 0) > 0);

// (addMinuteHHMM and deferTimeFor removed — queue reordering now works by
// swapping scheduledAt between adjacent rows via moveMatch, which calls
// updateMatchTime for both the moved match and its neighbour.)

// shiaijoScoreCell — decide what the queue row's middle score column shows.
// Exported so the team-vs-individual routing is unit-testable. A team
// encounter's headline number is Individual Victories (IV); it must always
// carry the IV label and never appear as a bare figure (which could be read
// as wins or points). Individual bouts show the self-explanatory ippon score.
// Returns one of: {kind:"team",iv} | {kind:"ippon",ippon} | {kind:"vs"} | {kind:"none"}.
export function shiaijoScoreCell(m) {
    if (!m) return { kind: "none" };
    if (m.status === "scheduled") return { kind: "vs" };
    if (m.status !== "completed" && m.status !== "running") return { kind: "none" };
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

    // Selected match for the inline scoring panel. `calledKey` marks the match
    // the operator has announced this session (local cue only); `callingKey`
    // guards the in-flight announce request.
    const [calledKey, setCalledKey] = useStateSh(null);
    const [callingKey, setCallingKey] = useStateSh(null);
    const [startingKey, setStartingKey] = useStateSh(null);
    const [startError, setStartError] = useStateSh("");
    const [contextOpen, setContextOpen] = useStateSh(true);
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
    // Short-circuit to [] for a blank/unknown court so an accidental landing
    // (e.g. /admin/shiaijo/%20) never sorts/partitions the whole tournament.
    // Same condition as courtKnown below.
    const allMatches = useMemoSh(
        () => (tournament.courts || []).includes(court)
            ? window.filterMatchesByCourt(window.tournamentMatches(tournament).filter(hasBothSides), court)
            : [],
        [tournament, court]
    );
    const { sorted, running, scheduled, completed } = useMemoSh(
        () => partitionShiaijoMatches(allMatches),
        [allMatches]
    );

    const courts = tournament.courts || [];
    const courtKnown = courts.includes(court);

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

    // The standings/context panel follows the court's current focus — not
    // strictly the running match — so it stays visible (and updates) after a
    // bout is finished, instead of collapsing to the empty state. Priority:
    // the running match; else the bout just played (last completed), so the
    // operator sees their result land in the standings; else the next
    // scheduled bout (before the court's first match).
    const contextMatch = useMemoSh(
        () => selectedMatch || completed[completed.length - 1] || scheduled[0] || null,
        [selectedMatch, completed, scheduled]
    );

    // Up Next = the first non-completed, non-running match (the one to call).
    const upNext = scheduled[0] || null;

    // "Which pool is next" for the context panel: the first upcoming pool on
    // this court whose pool differs from the one currently in focus.
    const nextPoolName = useMemoSh(() => {
        const cur = (contextMatch && contextMatch.phase === "pool") ? (contextMatch.poolName || "") : "";
        for (const m of scheduled) {
            const pn = m.poolName || "";
            if (m.phase === "pool" && pn && pn !== cur) return pn;
        }
        return null;
    }, [contextMatch, scheduled]);

    // Auto-advance target after a submit: next non-completed match.
    const nextActiveAfter = (m) => {
        const idx = sorted.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return null;
        return sorted.slice(idx + 1).find((x) => x.status !== "completed") || null;
    };

    // Delegate to the canonical start-patch factory (admin_schedule.jsx) rather
    // than re-declaring its shape — a second copy could silently drift. Both
    // modules ship in the same admin bundle, so the global is always present;
    // fail loudly if that ever stops being true instead of forking behaviour.
    const startPatch = () => {
        if (typeof window.startPatch !== "function") {
            throw new Error("startPatch factory unavailable — admin_schedule.jsx not loaded");
        }
        return window.startPatch();
    };

    // Returns true when the start write succeeded, false otherwise — pickMatch
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
            if (mountedRef.current) setStartError((e && e.message) || "Could not start the match — check eligibility and try again.");
            if (showToast) showToast((e && e.message) || "Could not start the match", "error");
            return false;
        } finally {
            if (mountedRef.current) setStartingKey(null);
        }
    };

    // scoringStarted — has the current running bout had any score entered? Used
    // by pickMatch to decide whether switching away is safe. A bout with ippons
    // or points already recorded must be finished or corrected before the
    // operator can move to another match (you can't abandon a half-scored bout).
    const scoringStarted = (mm) => !!mm && mm.status === "running" && (
        (mm.ipponsA?.length || 0) > 0 ||
        (mm.ipponsB?.length || 0) > 0 ||
        (mm.score?.winnerPts || 0) > 0 ||
        (mm.score?.loserPts || 0) > 0
    );

    // pickMatch — run an upcoming match out of order. Rules:
    //   • Completed matches are never picked (correct them in the comp admin).
    //   • If a DIFFERENT bout is running with scoring already started, block —
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
            // real defer here means clearing the running status — otherwise the
            // pick below would leave the court with two running bouts. The shape
            // mirrors buildPatch("scheduled") in the scoring modal. If the
            // defer write fails (surfaced via toast) we must NOT start the pick.
            try {
                await onEditScore(cur.compId, cur.id, { status: "scheduled", winner: null, score: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0 }, cur);
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

    // Call to court — optional. Broadcasts a tournament announcement so the
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

    // moveMatch — reorder by swapping scheduledAt with the adjacent row.
    // direction: "up" (earlier) or "down" (later). Only scheduled matches
    // can be reordered; running/completed are stable. Two updateMatchTime
    // calls keep both rows' times consistent (persisted + broadcast).
    const moveMatch = async (m, direction) => {
        if (!window.API || typeof window.API.updateMatchTime !== "function") return;
        if (m.status === "running" || m.status === "completed") return;
        const idx = scheduled.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return;
        const neighbour = direction === "up" ? scheduled[idx - 1] : scheduled[idx + 1];
        if (!neighbour) return; // already first/last
        const label = (m.sideB && m.sideB.name) || (m.sideA && m.sideA.name) || "Match";
        const myTime = m.scheduledAt || null;
        const neighbourTime = neighbour.scheduledAt || null;
        try {
            // Swap the two times atomically (sequential; second call only runs
            // if first succeeds so state is never corrupted on partial failure).
            await window.API.updateMatchTime(m.compId, m.id, neighbourTime, password);
            await window.API.updateMatchTime(neighbour.compId, neighbour.id, myTime, password);
            if (showToast) showToast(`Moved ${label} ${direction === "up" ? "earlier" : "later"} in the queue`);
        } catch (e) {
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

    const allDone = courtKnown && allMatches.length > 0 && running.length === 0 && scheduled.length === 0;

    return (
        <div className="app">
            <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} hideRunningStrip />
            <div className="page page--wide">
                <Breadcrumbs items={[{ label: "Dashboard", onClick: onBack }, { label: `Shiaijo ${court}` }]} />
                <div className="page-head">
                    <div>
                        <h1 className="page-head__title">Shiaijo {court}</h1>
                        <div className="page-head__sub">{`Call, start, and score every match on Shiaijo ${court} from here.`}</div>
                    </div>
                    {courts.length > 1 && (
                        <div className="page-head__actions">
                            <select
                                className="input" style={{ width: "auto" }}
                                value={courtKnown ? court : ""}
                                onChange={(e) => onSwitchCourt(e.target.value)}
                                aria-label="Switch court"
                            >
                                {!courtKnown && <option value="" disabled>Shiaijo {court} (unknown)</option>}
                                {courts.map((c) => <option key={c} value={c}>Shiaijo {c}</option>)}
                            </select>
                        </div>
                    )}
                </div>

                {!courtKnown && (
                    <div className="empty">
                        <h3>Unknown shiaijo "{court}"</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            This court isn't part of the tournament — it may have been renamed or removed.{" "}
                            <button type="button" onClick={onBack} className="linklike">Back to dashboard</button>.
                        </p>
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
                                        <div className="shiaijo-upnext__time">{upNext.scheduledAt || "—"} · {upNext.compName}</div>
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
                                                Never required — Start match works on its own. */}
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
                                            {window.API && typeof window.API.updateMatchTime === "function" && scheduled.length > 1 && (
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

                            {scheduled.length > (upNext ? 1 : 0) && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" matches={upNext ? scheduled.slice(1) : scheduled}
                                    courts={courts} onMoveCourt={requestMoveCourt}
                                    onMove={moveMatch} onEnterLineup={setLineupMatch}
                                    onPick={pickMatch}
                                    scheduled={scheduled}
                                />
                            )}

                            {completed.length > 0 && (
                                <div className="shiaijo-completed">
                                    {/* Completed matches stay expanded — this is the operator's
                                        running record of what's been played on this court/pool today,
                                        so it must not be hidden behind a collapse toggle. */}
                                    <div className="section-title">
                                        Completed <span className="shiaijo-count" aria-label={`${completed.length} matches`}>{completed.length}</span>
                                    </div>
                                    <ShiaijoQueueGroup
                                        matches={completed}
                                        courts={courts} onMoveCourt={requestMoveCourt}
                                    />
                                </div>
                            )}
                        </div>

                        {/* ── Scoring / lineup + context (right) ──────── */}
                        <div className="shiaijo__main">
                            {allDone && (
                                <div className="empty">
                                    <h3>All matches complete on Shiaijo {court}</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        {completed.length} match{completed.length === 1 ? "" : "es"} scored. Nothing left to run on this court.
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
                                    match={selectedMatch}
                                    onClose={() => {}}
                                    canClose={false}
                                    onSubmit={async (patch) => {
                                        try { await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch); }
                                        catch (_e) { /* surfaced via toast */ }
                                    }}
                                    onSubmitAndNext={async (patch) => {
                                        const next = nextActiveAfter(selectedMatch);
                                        try {
                                            await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            if (!mountedRef.current) return;
                                            // Finish + start the next scheduled match, which then
                                            // becomes the running match the panel shows.
                                            if (next && next.status === "scheduled") {
                                                try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
                                            }
                                        } catch (_e) { /* keep panel */ }
                                    }}
                                    onAfterDecision={async () => {
                                        // A kiken/fusenpai decision already persisted the bout via
                                        // the /decision POST — no score PUT here. Just start the next
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
                                    match={contextMatch} tournament={tournament}
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

            {/* Pre-start lineup entry for a team match. Opens the team scoresheet
                as a modal: the per-position name pickers persist via putMatchLineup
                independent of scoring, so the operator can set the lineup and close
                without starting — or hit "Start match" from inside. */}
            {lineupMatch && (
                <ScoreEditorModal
                    key={`lineup:${matchKey(lineupMatch)}`}
                    variant="modal"
                    match={lineupMatch}
                    canClose={true}
                    onClose={() => setLineupMatch(null)}
                    onSubmit={async (patch) => {
                        try { await onEditScore(lineupMatch.compId, lineupMatch.id, patch, lineupMatch); }
                        catch (_e) { /* surfaced via toast */ }
                        if (mountedRef.current) setLineupMatch(null);
                    }}
                    password={password}
                />
            )}
        </div>
    );
}

// A queued match row: plain info (the court reassign and reorder controls
// work) but the row itself can't be opened in the scoring panel — you start a
// match from the Up Next card, not by clicking it. The console never shows a
// running match in these groups (the running bout is officiated in the scoring
// panel on the right), so only scheduled/completed states render here.
//
// `scheduled` is the full court scheduled list (used to derive first/last
// position for disabling the ↑/↓ buttons). `onMove(m, direction)` swaps
// scheduledAt with the adjacent row via two updateMatchTime calls.
function ShiaijoQueueGroup({ label, matches, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick }) {
    return (
        <div className="shiaijo-group">
            {label && <div className="section-title">{label}</div>}
            <div className="score-editor__list">
                {matches.map((m) => (
                    <ShiaijoQueueRow
                        key={matchKey(m)} m={m}
                        scheduled={scheduled}
                        courts={courts} onMoveCourt={onMoveCourt} onMove={onMove} onEnterLineup={onEnterLineup} onPick={onPick}
                    />
                ))}
            </div>
        </div>
    );
}

function ShiaijoQueueRow({ m, scheduled, courts, onMoveCourt, onMove, onEnterLineup, onPick }) {
    const isComplete = m.status === "completed";
    const scoreCell = shiaijoScoreCell(m);
    // Derive position in the full scheduled list to know when to disable ↑/↓.
    // `scheduled` is the court's complete scheduled array (including Up Next);
    // the row may be in the Upcoming slice but we disable based on absolute pos.
    const scheduledIdx = onMove ? (scheduled || []).findIndex((x) => matchKey(x) === matchKey(m)) : -1;
    const isFirst = scheduledIdx === 0;
    const isLast = scheduledIdx >= 0 && scheduledIdx === (scheduled || []).length - 1;
    return (
        <div className={`score-edit-row shiaijo-row shiaijo-row--static ${isComplete ? "score-edit-row--complete" : ""}`}>
            <div>
                <div className="score-edit-row__time">{m.scheduledAt || "—"}</div>
                <div className="shiaijo-row__comp">{m.compName}</div>
            </div>
            <div className="score-edit-row__sides">
                <div className="score-edit-row__side" style={{ textAlign: "right" }} aria-label={`Shiro: ${m.sideB?.name || ""}`}>
                    <div className="name">
                        {m.sideB?.number ? <span className="num-prefix">{m.sideB.number}</span> : null}
                        {m.sideB?.name}
                    </div>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                </div>
                <div className="score-edit-row__score">
                    {scoreCell.kind === "team" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Individual Victories">IV</abbr>{scoreCell.iv}</span>}
                    {scoreCell.kind === "ippon" && scoreCell.ippon}
                    {scoreCell.kind === "vs" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                </div>
                <div className="score-edit-row__side" aria-label={`Aka: ${m.sideA?.name || ""}`}>
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <div className="name">
                        {m.sideA?.number ? <span className="num-prefix">{m.sideA.number}</span> : null}
                        {m.sideA?.name}
                    </div>
                </div>
            </div>
            <div className="shiaijo-row__status">
                {isComplete && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>Final</span>}
            </div>
            <div className="shiaijo-row__actions" onClick={(e) => e.stopPropagation()}>
                {onMoveCourt && courts.length > 1 && !isComplete && (
                    <CourtPicker
                        value={m.court} courts={courts}
                        onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
                        btnClassName="score-edit-row__court score-edit-row__court--btn"
                    />
                )}
                {onEnterLineup && isTeamMatch(m) && m.status === "scheduled" && (
                    <button type="button" className="btn btn--ghost btn--sm" onClick={() => onEnterLineup(m)} title="Set the team lineup before starting">Lineup</button>
                )}
                {onPick && m.status === "scheduled" && (
                    <button type="button" className="btn btn--sm shiaijo-row__pick" onClick={() => onPick(m)} title="Run this match now and score it">Score</button>
                )}
                {onMove && m.status === "scheduled" && (
                    <>
                        <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move up" disabled={isFirst} onClick={() => onMove(m, "up")} title="Move earlier in the queue">↑</button>
                        <button type="button" className="btn btn--ghost btn--sm shiaijo-row__move" aria-label="Move down" disabled={isLast} onClick={() => onMove(m, "down")} title="Move later in the queue">↓</button>
                    </>
                )}
            </div>
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
function ShiaijoContext({ match, tournament, court, nextPoolName, tweaks, open, onToggle }) {
    const comp = (tournament.competitions || []).find((c) => c.id === match.compId);
    const bracket = comp && (comp.bracket || (Array.isArray(comp.rounds) ? { rounds: comp.rounds } : null));
    const isPool = match.phase === "pool";
    const phaseLabel = isPool ? (match.poolName || "Pool") : (match.round || "Elimination");
    const PoolsViewer = window.PoolsViewer;

    // Pools/standings aren't on the console's competition list payload — fetch
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
                                    />
                                </div>
                            ) : (
                                <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                                    {detailErr
                                        ? "Couldn't load pool standings — they'll appear once the connection recovers."
                                        : "Loading pool standings…"}
                                </p>
                            )}
                        </>
                    ) : (match.phase === "bracket" && BracketTree && bracket && bracket.rounds) ? (
                        <div className="shiaijo-context__bracket">
                            <BracketTree rounds={bracket.rounds} highlightId={match.id} />
                        </div>
                    ) : (
                        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                            {match.compName} — {phaseLabel}. Bracket context appears here for elimination matches.
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}

window.AdminShiaijoPage = AdminShiaijoPage;
