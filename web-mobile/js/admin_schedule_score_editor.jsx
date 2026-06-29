// Score editor components extracted from admin_schedule.jsx (mp-d7tl).
// startPatch, ScoreEditCourtBtn (local), AdminScoreEditor, AdminScoreEditorPage.

import { allMatchesCompleted } from './admin_schedule_utils.jsx';
import { MatchLineupPanel } from './admin_schedule_lineup.jsx';
import { boutHansokuMark } from './match_scoreboard.jsx';

const { useState: useStateA, useMemo: useMemoA, useEffect: useEffectA, useRef: useRefA } = React;

const pluralize = window.pluralize;
const EmptyState = window.EmptyState;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const ScoreEditorModal = window.ScoreEditorModal;
// `hasBothSides` rejects matches with bye/TBD placeholder sides: see
// admin_helpers.jsx. Required because normalizeMatch substitutes
// {id:"",name:""} for missing sides, making the naive `m.sideA && m.sideB`
// check always pass.
const hasBothSides = window.hasBothSides;
const getScoreBtnClass = window.getScoreBtnClass;

// ---------- Score editor ----------
export function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password }) {
  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide" style={{ maxWidth: 1200 }}>
        <Breadcrumbs items={[
          { label: "Dashboard", onClick: onBack },
          { label: "Scores" }
        ]} />
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Score editor</h1>
            <div className="page-head__sub">Update scores or correct past matches across the tournament. Changes propagate through the bracket.</div>
          </div>
        </div>
        <AdminScoreEditor t={tournament} onEditScore={onEditScore} onMoveCourt={onMoveCourt} password={password} />
      </div>
    </div>
  );
}

function ScoreEditCourtBtn({ m, courts, onMoveCourt }) {
  if (!onMoveCourt || !courts.length) {
    return <div className="score-edit-row__court">{m.court}</div>;
  }
  return (
    <CourtPicker
      value={m.court}
      courts={courts}
      onChange={(cc) => onMoveCourt(m.compId, m.id, cc)}
      btnClassName="score-edit-row__court score-edit-row__court--btn"
    />
  );
}

// Module-level factory so admin_shiaijo.jsx can consume it via window.startPatch.
export function startPatch() {
  return {
    status: "running", winner: null, ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
    score: { type: "ippon", winnerPts: 0, loserPts: 0, ippons: [], fouls: { a: 0, b: 0 }, live: true, corrected: false },
  };
}

export function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId, password, showToast }) {
  const [filter, setFilter] = useStateA("");
  const [compFilter, setCompFilter] = useStateA(restrictToCompId || "all");
  const [statusFilter, setStatusFilter] = useStateA("all");
  const [openMatch, setOpenMatch] = useStateA(null);
  // mp-bkg: per-match lineup panel state. lineupMatch holds the match
  // currently open in the lineup panel (null = panel closed).
  const [lineupMatch, setLineupMatch] = useStateA(null);
  // ScoreEditorModal's onSubmit / onSubmitAndNext callbacks await
  // onEditScore (which routes through AdminApp.editMatchScore: a
  // server PUT). If AdminScoreEditor unmounts during the in-flight
  // save (parent navigates away), the post-await setOpenMatch fires
  // on a torn-down component. Gate via mountedRef.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  const tournament = t || (c ? { competitions: [c] } : { competitions: [] });
  const allMatches = useMemoA(
    () => tournament.competitions.flatMap((cc) => window.compMatches(cc)).filter(hasBothSides),
    [tournament]
  );

  const f = filter.trim().toLowerCase();
  const filtered = allMatches.filter((m) => {
    if (restrictToCompId && m.compId !== restrictToCompId) return false;
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (statusFilter === "running" && m.status !== "running") return false;
    if (statusFilter === "scheduled" && m.status !== "scheduled") return false;
    if (statusFilter === "complete" && m.status !== "completed") return false;
    if (!f) return true;
    return [m.sideA?.name, m.sideB?.name, m.sideA?.dojo, m.sideB?.dojo].some((s) => (s || "").toLowerCase().includes(f));
  });

  // Status keys must match backend values; sort running first, then upcoming,
  // then completed. Anything unrecognized goes to the end.
  const order = { running: 0, scheduled: 1, completed: 2 };
  filtered.sort((a, b) => {
    const ao = order[a.status] ?? 99;
    const bo = order[b.status] ?? 99;
    if (ao !== bo) return ao - bo;
    return (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99");
  });

  return (
    <div className="score-editor">
      <div className="score-editor__bar">
        <span className="tw-sched__filter-label">Filter</span>
        <input
          className="input"
          style={{ flex: 1, minWidth: 180 }}
          placeholder="Search player, team, dojo…"
          aria-label="Filter matches by player, team, or dojo"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {!restrictToCompId && (
          <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
            <option value="all">All competitions</option>
            {tournament.competitions.map((cc) => <option key={cc.id} value={cc.id}>{cc.name}</option>)}
          </select>
        )}
        <div className="seg">
          <button type="button" className={statusFilter === "all" ? "is-active" : ""} onClick={() => setStatusFilter("all")}>All</button>
          <button type="button" className={statusFilter === "running" ? "is-active" : ""} onClick={() => setStatusFilter("running")}>Now</button>
          <button type="button" className={statusFilter === "scheduled" ? "is-active" : ""} onClick={() => setStatusFilter("scheduled")}>Scheduled</button>
          <button type="button" className={statusFilter === "complete" ? "is-active" : ""} onClick={() => setStatusFilter("complete")}>Completed</button>
        </div>
        <span style={{ marginLeft: "auto", fontSize: 12, color: "var(--ink-3)" }}>{pluralize(filtered.length, "match", "matches")}</span>
      </div>

      <div className="score-editor__list">
        {filtered.length === 0 && (
          <EmptyState icon="🔍" title="No matches" message="Adjust your filters or check that competitions have started." />
        )}
        {/* "All matches scored" banner. Guarded against statusFilter === "complete"
            because the filter trivially makes filtered all-completed, which would
            misleadingly fire the banner. The wording is intentionally generic: this
            view spans all match phases (pool + bracket) and all competition formats
            (pools/mixed/playoffs/league/swiss), so we don't claim "Pool play is
            complete" or point at a specific next tab. */}
        {statusFilter !== "complete" && allMatchesCompleted(filtered) && (
          <div className="alert alert--success" style={{ marginBottom: 12 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>All matches scored</div>
            <div style={{ fontSize: 13, color: "var(--ink-2)" }}>Every visible match is complete. Open the competition to review standings, generate playoffs, or start the next phase.</div>
          </div>
        )}
        {filtered.map((m) => {
          const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
          const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
          const isCorrection = m.status === "completed" && m.score?.corrected;
          // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B
          // arrays (see normalizeMatch). Apply the same fallback used in VSchedItem.
          // Use ipponsFromScore so the trailing "(HN)" hansoku suffix from
          // Go's formatScore doesn't get split into bogus ippon letters.
          const seIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
          const seIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
          // Outstanding single hansoku → red ▲ next to the offending side (same
          // mark as the scoresheet). hansoku may live on the match or under
          // score.fouls depending on the source; fall back across both.
          const foulB = boutHansokuMark(m.hansokuB ?? m.score?.fouls?.b ?? 0);
          const foulA = boutHansokuMark(m.hansokuA ?? m.score?.fouls?.a ?? 0);
          // Show the live ippon score for a running bout too (not just completed)
          // so the list reflects scoring in progress; "vs" only before it starts.
          const showScore = m.status === "completed" || m.status === "running";
          // A just-started running bout is 0–0, where formatIpponsScore returns "".
          // Fall back so the cell is never blank: running 0–0 → "vs", a completed
          // match with no recorded score → ":". Live techniques show once present.
          const seScore = showScore ? window.formatIpponsScore(seIpponsB, seIpponsA, m.score, m.decision, m.encho, m.decidedByHantei) : "";
          return (
            <div key={`${m.compId}:${m.id}`} className={`score-edit-row ${m.status === "running" ? "score-edit-row--running is-running" : ""} ${m.status === "completed" ? "score-edit-row--complete" : ""}`}>
              <div>
                <div className="score-edit-row__time">{m.scheduledAt || ":"}</div>
                <div style={{ fontSize: 10, color: "var(--ink-3)", marginTop: 2 }}>{m.compName}</div>
              </div>
              <ScoreEditCourtBtn m={m} courts={tournament.courts || []} onMoveCourt={onMoveCourt} />
              <div className="score-edit-row__sides">
                  <div className={`score-edit-row__side ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                    <div className="name">{m.sideB?.number ? <span className="num-prefix">{m.sideB.number}</span> : null}{m.sideB?.name}</div>
                    <div className="dojo">{m.sideB?.dojo}</div>
                    <span className="se-color-badge se-color-badge--shiro">SHIRO</span>
                  </div>
                  {/* Foul ▲ flanks the SCORE (Shiro left, Aka right): a hansoku is part of
                      the scoreline, so it reads at the score's level. The slots are reserved
                      symmetrically so the centred score never shifts when a foul appears. */}
                  <div className="score-edit-row__score">
                    <span className="score-edit-row__foul">{foulB && <span className="msb-hansoku" data-testid="foul-mark-b">{foulB}</span>}</span>
                    <span className="score-edit-row__scoreval">
                      {m.status === "scheduled"
                        ? <span style={{ fontSize: 11, color: "var(--ink-3)" }}>vs</span>
                        : (seScore || <span style={{ fontSize: 11, color: "var(--ink-3)" }}>{m.status === "running" ? "vs" : ":"}</span>)}
                    </span>
                    <span className="score-edit-row__foul">{foulA && <span className="msb-hansoku" data-testid="foul-mark-a">{foulA}</span>}</span>
                  </div>
                  <div className={`score-edit-row__side ${aWin ? "score-edit-row__side--win" : ""}`}>
                    <span className="se-color-badge se-color-badge--aka">AKA</span>
                    <div className="name">{m.sideA?.number ? <span className="num-prefix">{m.sideA.number}</span> : null}{m.sideA?.name}</div>
                    <div className="dojo">{m.sideA?.dojo}</div>
                  </div>
              </div>
              <div>
                {/* Running: no "● NOW" label: the row highlight is the signal (removed as
                    redundant). Completed keeps its Final / Corrected status. */}
                {m.status === "completed" && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>{isCorrection ? "Corrected" : "Final"}</span>}
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                <button type="button" className={getScoreBtnClass(m.status)} onClick={() => setOpenMatch(m)}>
                  {m.status === "completed" ? "Correct" : "Score"}
                </button>
                {/* mp-bkg: show Lineup button only for team competitions */}
                {(m.compKind === "team" || (m.teamSize || 0) > 0) && (
                  <button type="button"
                    className="btn btn--ghost btn--sm"
                    style={{ fontSize: 11 }}
                    onClick={() => setLineupMatch(m)}
                  >
                    Lineup
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>

      {/* mp-bkg: per-match lineup panel */}
      {lineupMatch && (
        <MatchLineupPanel
          key={lineupMatch.compId + '-' + lineupMatch.id}
          match={lineupMatch}
          tournament={tournament}
          password={password}
          showToast={showToast}
          onClose={() => setLineupMatch(null)}
        />
      )}

      {openMatch && (() => {
        // Chained nav (Prev/Next/Finish+Start Next/←/→) must stay on the same
        // shiaijo. Operators run matches per-court; jumping courts mid-flow
        // skips the wrong matches. Unassigned matches scope to other
        // unassigned matches so the behaviour is consistent.
        const openCourt = openMatch.court || "";
        const sameCourt = filtered.filter(m => (m.court || "") === openCourt);
        const openIdx = sameCourt.findIndex(m => `${m.compId}:${m.id}` === `${openMatch.compId}:${openMatch.id}`);
        const prevMatch = openIdx > 0 ? sameCourt[openIdx - 1] : null;
        const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
        // Finish+Start Next must only advance to a non-completed match. Without
        // this guard, the last scheduled match has nextMatch = the first completed
        // match (completed matches sort after scheduled in the list), causing the
        // modal to loop back to match 1 after the final save.
        // Guard on openIdx >= 0: when openMatch is not found in sameCourt (openIdx
        // === -1), slice(0) would scan the whole array and return a spurious match.
        const nextActiveMatch = openIdx >= 0
          ? sameCourt.slice(openIdx + 1).find(m => m.status !== 'completed') || null
          : null;
        // Minimal "start" patch (status → running, empty score). Mirrors the
        // modal's own buildPatch("running") for an unscored match and works for
        // both individual and team matches (subResults is omitted, which the
        // serializer treats as "no bouts scored yet"). The server routes this
        // through eng.StartMatchTx, so all start-gating (eligibility 409,
        // ≥players checks) still runs: a 409 throws and is caught below.
        // Defined at module level as window.startPatch for reuse across admin_*.jsx.
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
                const res = await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                if (!mountedRef.current) return res;
                // F5: when the terminal write was only queued (offline), return
                // the { queued: true } signal to the editor so it can show the
                // pending-save banner instead of closing the modal.
                if (res && res.queued) return res;
                // ▶ Start Match: keep the operator IN the scoring surface rather
                // than dumping them back to the list (which forced a re-find +
                // reopen per match). A "start" patch is status:running with no
                // winner: flip the open match's status optimistically so the
                // modal re-renders as the live scoring board (the background
                // refresh/SSE then reconciles the canonical state). Any other
                // submit (finish/correction/draw) closes as before.
                if (patch.status === "running" && !patch.winner) {
                  setOpenMatch(prev => prev ? { ...prev, status: "running" } : prev);
                } else {
                  setOpenMatch(null);
                }
              } catch (_err) {
                // Error handled by onEditScore/toast, but we catch here to keep modal open
              }
            }}
            onSubmitAndNext={nextActiveMatch ? async (patch) => {
              try {
                const res = await onEditScore(openMatch.compId, openMatch.id, patch, openMatch);
                if (!mountedRef.current) return res;
                // F5: queued write: do NOT advance to the next match. Return
                // the signal so the editor shows the pending-save banner.
                if (res && res.queued) return res;
                // "Finish + Start Next →": land on the next match on the SAME
                // shiaijo AND actually start it (honest to the label). If the
                // next match is already running/completed, just open it. Start
                // gating runs server-side (StartMatchTx); a 409 throws: we
                // catch it so the operator still lands on the next match (in
                // pre-match) to resolve the eligibility issue manually.
                setOpenMatch(nextActiveMatch);
                if (nextActiveMatch.status === "scheduled") {
                  try {
                    await onEditScore(nextActiveMatch.compId, nextActiveMatch.id, startPatch(), nextActiveMatch);
                    if (mountedRef.current) setOpenMatch(prev => prev ? { ...prev, status: "running" } : prev);
                  } catch (_startErr) { /* gate rejected the start; stay on the next match in pre-match */ }
                }
              } catch (_err) { /* keep modal open on error */ }
            } : null}
            onAfterDecision={nextActiveMatch ? async () => {
              // A kiken/fusenpai decision already persisted the bout via the
              // /decision POST: no score PUT here. Mirror onSubmitAndNext's
              // start-next so a fusenpai advances the operator to (and starts)
              // the next same-court match.
              if (nextActiveMatch.status === "scheduled") {
                try {
                  await onEditScore(nextActiveMatch.compId, nextActiveMatch.id, startPatch(), nextActiveMatch);
                  if (mountedRef.current) setOpenMatch(nextActiveMatch);
                } catch (_startErr) { /* gate rejected the start; leave the operator where they are */ }
              }
            } : null}
            password={password}
          />
        );
      })()}
    </div>
  );
}
