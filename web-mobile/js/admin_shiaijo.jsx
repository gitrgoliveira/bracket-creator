// Dedicated table operator view for a single shiaijo (court).
// Route: /admin/shiaijo/:court  →  admin.kind = "shiaijo", court = "A"
//
// Shows all matches assigned to this court, grouped:
//   Running (now) → Scheduled → Completed
// Operator taps "Score" / "Correct" to open ScoreEditorModal as an overlay.
//
// Data flows entirely from window.tournamentMatches(tournament) — no new
// backend API is needed. The court filter mirrors filterMatchesByCourt in
// admin_schedule.jsx; inlined because each JSX file is compiled independently
// by esbuild (cross-file ES imports are not used in this module pattern).

const { useState: useStateSh, useMemo: useMemoSh, useEffect: useEffectSh, useRef: useRefSh } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const ScoreEditorModal = window.ScoreEditorModal;
const hasBothSides = window.hasBothSides;
const getScoreBtnClass = window.getScoreBtnClass;

function courtMatches(matches, court) {
    const c = (court || "").trim();
    if (!c || c === "all") return matches;
    return matches.filter((m) => m.court === c);
}

function AdminShiaijoPage({ tournament, court, onBack, onEditScore, onLogout, onViewerMode, password, onSwitchCourt }) {
    const [openMatch, setOpenMatch] = useStateSh(null);
    const mountedRef = useRefSh(true);
    useEffectSh(() => () => { mountedRef.current = false; }, []);

    const allMatches = useMemoSh(
        () => courtMatches(window.tournamentMatches(tournament).filter(hasBothSides), court),
        [tournament, court]
    );

    const sorted = useMemoSh(() => {
        const order = { running: 0, scheduled: 1, completed: 2 };
        return [...allMatches].sort((a, b) => {
            const ao = order[a.status] ?? 99;
            const bo = order[b.status] ?? 99;
            if (ao !== bo) return ao - bo;
            return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
        });
    }, [allMatches]);

    const running   = sorted.filter(m => m.status === "running");
    const scheduled = sorted.filter(m => m.status === "scheduled");
    const completed = sorted.filter(m => m.status === "completed");

    // Mirrors AdminScoreEditor's start-patch constant.
    const startPatch = () => ({
        status: "running", winner: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
        score: { type: "ippon", winnerPts: 0, loserPts: 0, ippons: [], fouls: { a: 0, b: 0 }, live: true, corrected: false },
    });

    const courts = tournament.courts || [];

    return (
        <div className="app">
            <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
            <div className="page page--wide" style={{ maxWidth: 900 }}>
                <Breadcrumbs items={[
                    { label: "Dashboard", onClick: onBack },
                    { label: `Shiaijo ${court}` }
                ]} />
                <div className="page-head">
                    <div>
                        <h1 className="page-head__title">Shiaijo {court}</h1>
                        <div className="page-head__sub">Table operator view — matches assigned to this court.</div>
                    </div>
                    {courts.length > 1 && onSwitchCourt && (
                        <div className="page-head__actions">
                            <select
                                className="input"
                                style={{ width: "auto" }}
                                value={court}
                                onChange={(e) => onSwitchCourt(e.target.value)}
                                aria-label="Switch court"
                            >
                                {courts.map(c => <option key={c} value={c}>Shiaijo {c}</option>)}
                            </select>
                        </div>
                    )}
                </div>

                {allMatches.length === 0 && (
                    <div className="empty">
                        <h3>No matches on this court</h3>
                        <p style={{ fontSize: 13, color: "var(--ink-3)" }}>
                            Matches appear here once assigned to Shiaijo {court}. Use the Schedule page to assign matches.
                        </p>
                    </div>
                )}

                {running.length > 0 && (
                    <ShiaijoMatchGroup label="Now" matches={running} onScore={setOpenMatch} />
                )}
                {scheduled.length > 0 && (
                    <ShiaijoMatchGroup label="Up next" matches={scheduled} onScore={setOpenMatch} />
                )}
                {completed.length > 0 && (
                    <ShiaijoMatchGroup label="Completed" matches={completed} onScore={setOpenMatch} />
                )}

                {openMatch && (() => {
                    const openIdx = sorted.findIndex(m => `${m.compId}:${m.id}` === `${openMatch.compId}:${openMatch.id}`);
                    const prevMatch = openIdx > 0 ? sorted[openIdx - 1] : null;
                    const nextMatch = openIdx >= 0 && openIdx < sorted.length - 1 ? sorted[openIdx + 1] : null;
                    const nextActiveMatch = openIdx >= 0
                        ? sorted.slice(openIdx + 1).find(m => m.status !== "completed") || null
                        : null;
                    return (
                        <ScoreEditorModal
                            key={openMatch.compId + '-' + openMatch.id}
                            match={openMatch}
                            prevMatch={prevMatch}
                            nextMatch={nextMatch}
                            onPrev={() => setOpenMatch(prevMatch)}
                            onNext={() => setOpenMatch(nextMatch)}
                            onClose={() => setOpenMatch(null)}
                            onSubmit={async (patch) => {
                                try {
                                    await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                                    if (!mountedRef.current) return;
                                    if (patch.status === "running" && !patch.winner) {
                                        setOpenMatch(prev => prev ? { ...prev, status: "running" } : prev);
                                    } else {
                                        setOpenMatch(null);
                                    }
                                } catch (_err) { /* error surfaced by onEditScore/toast */ }
                            }}
                            onSubmitAndNext={nextActiveMatch ? async (patch) => {
                                try {
                                    await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                                    if (!mountedRef.current) return;
                                    setOpenMatch(nextActiveMatch);
                                    if (nextActiveMatch.status === "scheduled") {
                                        try {
                                            await onEditScore(nextActiveMatch.compId, nextActiveMatch.id, startPatch(), nextActiveMatch);
                                            if (mountedRef.current) setOpenMatch(prev => prev ? { ...prev, status: "running" } : prev);
                                        } catch (_startErr) { /* eligibility gate; stay on next match */ }
                                    }
                                } catch (_err) { /* keep modal open */ }
                            } : null}
                            password={password}
                        />
                    );
                })()}
            </div>
        </div>
    );
}

function ShiaijoMatchGroup({ label, matches, onScore }) {
    return (
        <div style={{ marginBottom: 24 }}>
            <div className="section-title">{label}</div>
            <div className="score-editor__list">
                {matches.map(m => (
                    <ShiaijoMatchRow key={`${m.compId}:${m.id}`} m={m} onScore={onScore} />
                ))}
            </div>
        </div>
    );
}

function ShiaijoMatchRow({ m, onScore }) {
    const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
    const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
    const seIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
    const seIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
    return (
        <div className={`score-edit-row ${m.status === "running" ? "score-edit-row--live" : ""} ${m.status === "completed" ? "score-edit-row--complete" : ""}`}>
            <div>
                <div className="score-edit-row__time">{m.scheduledAt || "—"}</div>
                <div style={{ fontSize: 10, color: "var(--ink-3)", marginTop: 2 }}>{m.compName}</div>
            </div>
            <div className="score-edit-row__court">{m.court || "—"}</div>
            <div className="score-edit-row__sides">
                <div className={`score-edit-row__side ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                    <div className="name">{m.sideB?.name}</div>
                    <div className="dojo">{m.sideB?.dojo}</div>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                </div>
                <div className="score-edit-row__score">
                    {m.status === "completed" && window.formatIpponsScore(seIpponsB, seIpponsA, m.score, m.decision, m.encho, m.decidedByHantei)}
                    {m.status === "running" && <span className="bc-live">●</span>}
                    {m.status === "scheduled" && <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>}
                </div>
                <div className={`score-edit-row__side ${aWin ? "score-edit-row__side--win" : ""}`}>
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <div className="name">{m.sideA?.name}</div>
                    <div className="dojo">{m.sideA?.dojo}</div>
                </div>
            </div>
            <div>
                {m.status === "running" && <span className="bc-live">● NOW</span>}
                {m.status === "completed" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>Final</span>}
            </div>
            <div>
                <button className={getScoreBtnClass(m.status)} onClick={() => onScore(m)}>
                    {m.status === "completed" ? "Correct" : "Score"}
                </button>
            </div>
        </div>
    );
}

window.AdminShiaijoPage = AdminShiaijoPage;
