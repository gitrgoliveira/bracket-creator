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
    const isTeam = m.compKind === "team" || (m.teamSize || 0) > 0;
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
    useEffectSh(() => () => { mountedRef.current = false; }, []);

    // Selected match for the inline scoring panel. `calledKey` marks the match
    // the operator has announced this session (local cue only); `callingKey`
    // guards the in-flight announce request.
    const [selectedKey, setSelectedKey] = useStateSh(null);
    const [calledKey, setCalledKey] = useStateSh(null);
    const [callingKey, setCallingKey] = useStateSh(null);
    const [completedOpen, setCompletedOpen] = useStateSh(false);
    const [contextOpen, setContextOpen] = useStateSh(true);
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

    // The match shown in the scoring panel: explicit selection, else the
    // running match, else nothing.
    const selectedMatch = useMemoSh(() => {
        if (selectedKey) {
            const found = sorted.find((m) => matchKey(m) === selectedKey);
            if (found) return found;
        }
        return running[0] || null;
    }, [selectedKey, sorted, running]);

    // Up Next = the first non-completed, non-running match (the one to call).
    const upNext = scheduled[0] || null;

    // "Which pool is next" for the context panel: the first upcoming pool on
    // this court whose pool differs from the one currently being scored.
    const nextPoolName = useMemoSh(() => {
        const cur = (selectedMatch && selectedMatch.phase === "pool") ? (selectedMatch.poolName || "") : "";
        for (const m of scheduled) {
            const pn = m.poolName || "";
            if (m.phase === "pool" && pn && pn !== cur) return pn;
        }
        return null;
    }, [selectedMatch, scheduled]);

    // Auto-advance target after a submit: next non-completed match.
    const nextActiveAfter = (m) => {
        const idx = sorted.findIndex((x) => matchKey(x) === matchKey(m));
        if (idx < 0) return null;
        return sorted.slice(idx + 1).find((x) => x.status !== "completed") || null;
    };

    const startPatch = () => (window.startPatch ? window.startPatch() : {
        status: "running", winner: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
        score: { type: "ippon", winnerPts: 0, loserPts: 0, ippons: [], fouls: { a: 0, b: 0 }, live: true, corrected: false },
    });

    const startMatch = async (m) => {
        try {
            await onEditScore(m.compId, m.id, startPatch(), m);
            if (!mountedRef.current) return;
            setSelectedKey(matchKey(m));
        } catch (_e) { /* eligibility gate etc.; surfaced via toast */ }
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

    // Skip — defer a not-yet-started match to the end of this court's queue.
    // Clearing scheduledAt sinks it past the timed matches (backend
    // updateMatchTime, persisted + broadcast). The operator goes back to it by
    // selecting it from Upcoming later. No-op on running/completed matches.
    const skipMatch = async (m) => {
        if (!window.API || typeof window.API.updateMatchTime !== "function") return;
        if (m.status === "running" || m.status === "completed") return;
        const label = (m.sideB && m.sideB.name) || (m.sideA && m.sideA.name) || "Match";
        try {
            await window.API.updateMatchTime(m.compId, m.id, "", password);
            if (mountedRef.current && selectedKey === matchKey(m)) setSelectedKey(null);
            if (showToast) showToast(`Skipped ${label} — moved to the end of the queue`);
        } catch (e) {
            if (showToast) showToast((e && e.message) || "Could not skip the match", "error");
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
                        <div className="page-head__sub">Table operator view — run every match on this court without leaving the page.</div>
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
                                            <button className="btn btn--primary" onClick={() => startMatch(upNext)}>Start match</button>
                                            {/* Optional: announce the call to spectators/competitors.
                                                Never required — Start match works on its own. */}
                                            {window.API && typeof window.API.sendAnnouncement === "function" && (
                                                <button
                                                    className="btn btn--sm"
                                                    disabled={callingKey === matchKey(upNext)}
                                                    onClick={() => callToCourt(upNext)}
                                                    title="Announce this match to spectators and competitors"
                                                >
                                                    {Icon && <Icon name="megaphone" />}{" "}
                                                    {callingKey === matchKey(upNext) ? "Calling…" : (calledKey === matchKey(upNext) ? "Call again" : "Call to court")}
                                                </button>
                                            )}
                                            {window.API && typeof window.API.updateMatchTime === "function" && (
                                                <button className="btn btn--sm btn--ghost" onClick={() => skipMatch(upNext)} title="Move this match to the end of the queue">Skip</button>
                                            )}
                                        </div>
                                        <div className="shiaijo-upnext__hint">
                                            {calledKey === matchKey(upNext)
                                                ? "Announced to spectators. Start the match when both are at the line."
                                                : "Start when both competitors are at the line. Call to court announces the match to spectators (optional)."}
                                        </div>
                                    </div>
                                </div>
                            )}

                            {/* Running matches are scored in the panel on the right, so the
                                one being scored is not repeated here. A second running match
                                on this court (not currently selected) still surfaces so the
                                operator can switch to it. */}
                            {(() => {
                                const k = selectedMatch && matchKey(selectedMatch);
                                const others = running.filter((m) => matchKey(m) !== k);
                                return others.length > 0 ? (
                                    <ShiaijoQueueGroup
                                        label="Now" matches={others} selectedKey={k}
                                        onSelect={(m) => setSelectedKey(matchKey(m))} courts={courts} onMoveCourt={onMoveCourt}
                                    />
                                ) : null;
                            })()}

                            {scheduled.length > (upNext ? 1 : 0) && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" matches={upNext ? scheduled.slice(1) : scheduled}
                                    selectedKey={selectedMatch && matchKey(selectedMatch)}
                                    onSelect={(m) => setSelectedKey(matchKey(m))} courts={courts} onMoveCourt={onMoveCourt}
                                    onSkip={skipMatch}
                                />
                            )}

                            {completed.length > 0 && (
                                <div className="shiaijo-completed">
                                    <button className="section-title shiaijo-completed__toggle" onClick={() => setCompletedOpen((v) => !v)}>
                                        {completedOpen ? "▾" : "▸"} Completed <span className="shiaijo-count">{completed.length}</span>
                                    </button>
                                    {completedOpen && (
                                        <ShiaijoQueueGroup
                                            matches={completed} selectedKey={selectedMatch && matchKey(selectedMatch)}
                                            onSelect={(m) => setSelectedKey(matchKey(m))} courts={courts} onMoveCourt={onMoveCourt}
                                        />
                                    )}
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

                            {!allDone && selectedMatch && (
                                <ScoreEditorModal
                                    key={matchKey(selectedMatch)}
                                    variant="inline"
                                    match={selectedMatch}
                                    onClose={() => setSelectedKey(null)}
                                    canClose={!!selectedKey}
                                    onSubmit={async (patch) => {
                                        try {
                                            await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            if (!mountedRef.current) return;
                                            if (!(patch.status === "running" && !patch.winner)) setSelectedKey(null);
                                        } catch (_e) { /* surfaced via toast */ }
                                    }}
                                    onSubmitAndNext={async (patch) => {
                                        const next = nextActiveAfter(selectedMatch);
                                        try {
                                            await onEditScore(selectedMatch.compId, selectedMatch.id, patch, selectedMatch);
                                            if (!mountedRef.current) return;
                                            if (next) {
                                                setSelectedKey(matchKey(next));
                                                if (next.status === "scheduled") {
                                                    try { await onEditScore(next.compId, next.id, startPatch(), next); } catch (_s) { /* gate */ }
                                                }
                                            } else {
                                                setSelectedKey(null);
                                            }
                                        } catch (_e) { /* keep panel */ }
                                    }}
                                    password={password}
                                />
                            )}

                            {!allDone && !selectedMatch && (
                                <div className="empty shiaijo__placeholder">
                                    <h3>Ready when you are</h3>
                                    <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                                        Pick a match from the queue to start scoring. Completed matches stay in the list — select one to correct its result.
                                    </p>
                                </div>
                            )}

                            {selectedMatch && (
                                <ShiaijoContext
                                    match={selectedMatch} tournament={tournament}
                                    court={court} nextPoolName={nextPoolName} tweaks={tweaks}
                                    open={contextOpen} onToggle={() => setContextOpen((v) => !v)}
                                />
                            )}
                        </div>
                    </div>
                )}
            </div>
        </div>
    );
}

// A queued match row with select + court reassign (+ optional skip).
function ShiaijoQueueGroup({ label, matches, selectedKey, onSelect, courts, onMoveCourt, onSkip }) {
    return (
        <div className="shiaijo-group">
            {label && <div className="section-title">{label}</div>}
            <div className="score-editor__list">
                {matches.map((m) => (
                    <ShiaijoQueueRow
                        key={matchKey(m)} m={m} selected={selectedKey === matchKey(m)}
                        onSelect={onSelect} courts={courts} onMoveCourt={onMoveCourt} onSkip={onSkip}
                    />
                ))}
            </div>
        </div>
    );
}

function ShiaijoQueueRow({ m, selected, onSelect, courts, onMoveCourt, onSkip }) {
    const isRunning = m.status === "running";
    const isComplete = m.status === "completed";
    const scoreCell = shiaijoScoreCell(m);
    return (
        <div
            className={`score-edit-row shiaijo-row ${isRunning ? "score-edit-row--running" : ""} ${isComplete ? "score-edit-row--complete" : ""} ${selected ? "is-selected" : ""}`}
            onClick={() => onSelect(m)} role="button" tabIndex={0}
            onKeyDown={(e) => { if (e.target !== e.currentTarget) return; if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(m); } }}
        >
            <div>
                <div className="score-edit-row__time">{m.scheduledAt || "—"}</div>
                <div className="shiaijo-row__comp">{m.compName}</div>
            </div>
            <div className="score-edit-row__sides">
                <div className="score-edit-row__side" style={{ textAlign: "right" }}>
                    <div className="name">{m.sideB?.name}</div>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                </div>
                <div className="score-edit-row__score">
                    {scoreCell.kind === "team" && <span className="shiaijo-row__teamscore"><abbr className="shiaijo-row__iv" title="Individual Victories">IV</abbr>{scoreCell.iv}</span>}
                    {scoreCell.kind === "ippon" && scoreCell.ippon}
                    {scoreCell.kind === "vs" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                </div>
                <div className="score-edit-row__side">
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <div className="name">{m.sideA?.name}</div>
                </div>
            </div>
            <div className="shiaijo-row__status">
                {isRunning && <span className="bc-running">● NOW</span>}
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
                {onSkip && m.status === "scheduled" && (
                    <button className="btn btn--ghost btn--sm shiaijo-row__skip" onClick={() => onSkip(m)} title="Move to the end of the queue">Skip</button>
                )}
            </div>
        </div>
    );
}

function MatchSides({ m, large }) {
    return (
        <div className={`shiaijo-sides ${large ? "shiaijo-sides--lg" : ""}`}>
            <div className="shiaijo-sides__side">
                <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                <div className="name">{m.sideB?.name}</div>
                <div className="dojo">{m.sideB?.dojo}</div>
            </div>
            <div className="shiaijo-sides__vs">vs</div>
            <div className="shiaijo-sides__side" style={{ textAlign: "right" }}>
                <span className="se-color-badge se-color-badge--aka">AKA</span>
                <div className="name">{m.sideA?.name}</div>
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
            <button className="section-title shiaijo-context__toggle" onClick={onToggle}>
                {open ? "▾" : "▸"} {isPool ? "Standings" : "Context"} · {match.compName} · {phaseLabel}
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
