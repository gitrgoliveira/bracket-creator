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
const hasBothSides = window.hasBothSides;

// Pure ordering/partition helpers (exported for unit tests). Matches sort
// running → scheduled → completed, then by scheduled time within a group.
export function sortShiaijoMatches(matches) {
    const order = { running: 0, scheduled: 1, completed: 2 };
    return [...matches].sort((a, b) => {
        const ao = order[a.status] ?? 99;
        const bo = order[b.status] ?? 99;
        if (ao !== bo) return ao - bo;
        return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
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

function AdminShiaijoPage({ tournament, court: routeCourt, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password, onSwitchCourt }) {
    // Normalize once: filterMatchesByCourt trims its param, so a bookmarked URL
    // with stray whitespace must use the trimmed value everywhere.
    const court = (routeCourt || "").trim();

    const mountedRef = useRefSh(true);
    useEffectSh(() => () => { mountedRef.current = false; }, []);

    // Selected match for the inline scoring panel; "called" is local-only UI.
    const [selectedKey, setSelectedKey] = useStateSh(null);
    const [calledKey, setCalledKey] = useStateSh(null);
    const [completedOpen, setCompletedOpen] = useStateSh(false);
    const [contextOpen, setContextOpen] = useStateSh(true);

    const allMatches = useMemoSh(
        () => window.filterMatchesByCourt(window.tournamentMatches(tournament).filter(hasBothSides), court),
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

    const allDone = courtKnown && allMatches.length > 0 && running.length === 0 && scheduled.length === 0;

    return (
        <div className="app">
            <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
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
                                            {calledKey === matchKey(upNext) ? (
                                                <>
                                                    <span className="shiaijo-called">● Called</span>
                                                    <button className="btn btn--primary" onClick={() => startMatch(upNext)}>▶ Start Match</button>
                                                </>
                                            ) : (
                                                <button className="btn btn--primary" onClick={() => setCalledKey(matchKey(upNext))}>📣 Call to Court</button>
                                            )}
                                        </div>
                                    </div>
                                </div>
                            )}

                            {running.length > 0 && (
                                <ShiaijoQueueGroup
                                    label="Now" matches={running} selectedKey={selectedMatch && matchKey(selectedMatch)}
                                    onSelect={(m) => setSelectedKey(matchKey(m))} courts={courts} onMoveCourt={onMoveCourt}
                                />
                            )}

                            {scheduled.length > (upNext ? 1 : 0) && (
                                <ShiaijoQueueGroup
                                    label="Upcoming" matches={upNext ? scheduled.slice(1) : scheduled}
                                    selectedKey={selectedMatch && matchKey(selectedMatch)}
                                    onSelect={(m) => setSelectedKey(matchKey(m))} courts={courts} onMoveCourt={onMoveCourt}
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

                        {/* ── Scoring + context (right) ───────────────── */}
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
                                        Call the next match to the court, then start it to begin scoring here.
                                    </p>
                                </div>
                            )}

                            {selectedMatch && (
                                <ShiaijoContext
                                    match={selectedMatch} tournament={tournament}
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

// A queued match row with select + a three-dot reassign menu.
function ShiaijoQueueGroup({ label, matches, selectedKey, onSelect, courts, onMoveCourt }) {
    return (
        <div className="shiaijo-group">
            {label && <div className="section-title">{label}</div>}
            <div className="score-editor__list">
                {matches.map((m) => (
                    <ShiaijoQueueRow
                        key={matchKey(m)} m={m} selected={selectedKey === matchKey(m)}
                        onSelect={onSelect} courts={courts} onMoveCourt={onMoveCourt}
                    />
                ))}
            </div>
        </div>
    );
}

function ShiaijoQueueRow({ m, selected, onSelect, courts, onMoveCourt }) {
    const isRunning = m.status === "running";
    const isComplete = m.status === "completed";
    const seIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
    const seIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
    return (
        <div
            className={`score-edit-row shiaijo-row ${isRunning ? "score-edit-row--running" : ""} ${isComplete ? "score-edit-row--complete" : ""} ${selected ? "is-selected" : ""}`}
            onClick={() => onSelect(m)} role="button" tabIndex={0}
            onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onSelect(m); } }}
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
                    {isComplete && window.formatIpponsScore(seIpponsB, seIpponsA, m.score, m.decision, m.encho, m.decidedByHantei)}
                    {isRunning && <span className="bc-running">●</span>}
                    {m.status === "scheduled" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
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
            <div onClick={(e) => e.stopPropagation()}>
                {onMoveCourt && courts.length > 1 && !isComplete && (
                    <CourtPicker
                        value={m.court} courts={courts}
                        onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
                        btnClassName="score-edit-row__court score-edit-row__court--btn"
                    />
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

// Collapsible context: the current match's competition + phase, plus a bracket
// fragment for elimination matches when bracket data is available on the comp.
function ShiaijoContext({ match, tournament, open, onToggle }) {
    const comp = (tournament.competitions || []).find((c) => c.id === match.compId);
    const bracket = comp && (comp.bracket || (Array.isArray(comp.rounds) ? { rounds: comp.rounds } : null));
    const phaseLabel = match.phase === "pool" ? (match.poolName || "Pool") : (match.round || "Elimination");
    return (
        <div className="shiaijo-context">
            <button className="section-title shiaijo-context__toggle" onClick={onToggle}>
                {open ? "▾" : "▸"} Context · {match.compName} · {phaseLabel}
            </button>
            {open && (
                <div className="shiaijo-context__body">
                    {match.phase === "bracket" && BracketTree && bracket && bracket.rounds ? (
                        <div className="shiaijo-context__bracket">
                            <BracketTree rounds={bracket.rounds} highlightId={match.id} />
                        </div>
                    ) : (
                        <p style={{ fontSize: 12, color: "var(--ink-3)", margin: 0 }}>
                            {match.compName} — {phaseLabel}. {match.phase === "pool"
                                ? "Pool standings update as bouts are scored."
                                : "Bracket context appears here for elimination matches."}
                        </p>
                    )}
                </div>
            )}
        </div>
    );
}

window.AdminShiaijoPage = AdminShiaijoPage;
