// Score editor components extracted from admin_schedule.jsx (mp-d7tl).
// ScoreEditCourtBtn (local), AdminScoreEditor, AdminScoreEditorPage.

import { writeKeepsEditorOpen } from './write_result.jsx';
import { matchMentions } from './competitor_search.jsx';
import { SideCell } from './side_cell.jsx';
import { allMatchesCompleted } from './admin_schedule_utils.jsx';
import { MatchLineupPanel } from './admin_schedule_lineup.jsx';
import { boutHansokuMark, teamNameMark } from './match_scoreboard.jsx';
import { sameCompetitor } from './competitor_identity.jsx';
// The withdrawn team's mark, imported from its leaf rather than read off
// `window.teamMatchMarks` (a missing global painted no mark and no error).
import { teamMatchMarks } from './side_marks.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
// NumberedName: single owner of the number-chip-on-the-outer-side rule
// (bc-dnst); see that file's header for why this stays an ES import.
import { NumberedName } from './numbered_name.jsx';
// A scheduled match a competitor is barred from cannot be fought, so
// its row shows the fusensho action instead.
import { isBarredMatch } from './ineligible_match.jsx';
// Straight from its own leaf, not admin_scoring_shared.jsx (which also
// imports bracket.jsx): see barred_match_notice.jsx's header for why that
// matters for this file's own render suite.
import { BarredMatchNotice } from './barred_match_notice.jsx';
import { matchShowsScore } from './match_shows_score.jsx';

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

// scoreRowMatchLabel: relocated to pool_ids.jsx (bc-cse), which can serve
// admin_scoring_shared.jsx too without the cycle importing this file would
// create (this file -> admin_schedule_lineup.jsx -> admin_scoring_shared.jsx
// already exists). Re-exported here (of the LOCAL binding imported above, so
// the module is fetched once) so this file's own use below, and the
// existing `from './admin_schedule_score_editor.jsx'` test import, need no
// change; see pool_ids.jsx for the full rule.
export { scoreRowMatchLabel };

// ---------- Score editor ----------
export function AdminScoreEditorPage({ tournament, onBack, onEditScore, onMoveCourt, onLogout, onViewerMode, password, showToast }) {
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
        <AdminScoreEditor t={tournament} onEditScore={onEditScore} onMoveCourt={onMoveCourt} password={password} showToast={showToast} />
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
      onChange={(cc) => onMoveCourt(m.compId, m.id, cc, { shown: m })}
      btnClassName="score-edit-row__court score-edit-row__court--btn"
    />
  );
}

export function AdminScoreEditor({ t, c, onEditScore, onMoveCourt, restrictToCompId, password, showToast }) {
  const [filter, setFilter] = useStateA("");
  const [compFilter, setCompFilter] = useStateA(restrictToCompId || "all");
  const [statusFilter, setStatusFilter] = useStateA("all");
  // The open match is held as a KEY and re-resolved from live data on every
  // render, never captured as an object. A match has ONE result and every
  // surface asking for it shows the same one (operator ruling) — an editor
  // holding a snapshot is asking once and then showing that answer forever.
  //
  // It used to be `useState(match)`, set from the row that was clicked, and the
  // source said so: "SSE only refreshes the list, never this snapshot". The
  // effect was visible on one screen: record a result on a second device and
  // the list BEHIND the modal updated while the modal itself kept the state it
  // was opened with. Several `setOpenMatch(prev => ({...prev, ...}))` patches
  // existed to hand-forward the fields that mattered most, which is the same
  // information arriving twice by two routes, one of them partial.
  //
  // This is the pattern the other two mount sites already use — the bracket
  // panel resolves `selected.matchId` out of `bracket.rounds`, the shiaijo
  // panel resolves `pickedKey`/`correctingKey` out of `sorted` — so it is a
  // third site joining them rather than a new idea.
  const [openKey, setOpenKey] = useStateA(null);
  // mp-bkg: per-match lineup panel state. lineupKey is the match open in the
  // lineup panel (null = panel closed). Held as a key and resolved below, like
  // openKey: a snapshot would keep the panel's editors on the teams the match
  // had when it was opened, when a feeding match decided on another device has
  // since seated another team on a side.
  const [lineupKey, setLineupKey] = useStateA(null);
  // ScoreEditorModal's onSubmit / onSubmitAndNext callbacks await
  // onEditScore (which routes through AdminApp.editMatchScore: a
  // server PUT). If AdminScoreEditor unmounts during the in-flight
  // save (parent navigates away), the post-await setOpenKey fires
  // on a torn-down component. Gate via mountedRef.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  const tournament = t || (c ? { competitions: [c] } : { competitions: [] });
  const allMatches = useMemoA(
    () => (tournament.competitions || []).flatMap((cc) => window.compMatches(cc)).filter(hasBothSides),
    [tournament]
  );
  // Resolved against allMatches, NOT the filtered list: a match that completes
  // while the operator has the "Scheduled" filter on drops out of `filtered`,
  // and closing their editor underneath them because of a list filter would be
  // its own bug. Nav (prev/next) still scopes to the filtered same-court slice.
  const scoreKeyOf = (m) => `${m.compId}:${m.id}`;
  const openMatch = openKey ? allMatches.find((m) => scoreKeyOf(m) === openKey) || null : null;
  const lineupMatch = lineupKey ? allMatches.find((m) => scoreKeyOf(m) === lineupKey) || null : null;

  const f = filter.trim().toLowerCase();
  const filtered = allMatches.filter((m) => {
    if (restrictToCompId && m.compId !== restrictToCompId) return false;
    if (compFilter !== "all" && m.compId !== compFilter) return false;
    if (statusFilter === "running" && m.status !== "running") return false;
    if (statusFilter === "scheduled" && m.status !== "scheduled") return false;
    if (statusFilter === "complete" && m.status !== "completed") return false;
    if (!f) return true;
    // The shared rule (bc-nsrc), not a fourth hand-rolled copy. This page
    // shows a number chip on every row, so the operator could read the column
    // and not search it -- the same contradiction the bead was filed for.
    // Operator ruling 2026-09-22.
    return matchMentions(m, f);
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
          placeholder="Search player, team, dojo or number…"
          aria-label="Filter matches by player, team, dojo or number"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
        {!restrictToCompId && (
          <select className="input" style={{ width: "auto", minWidth: 160 }} value={compFilter} onChange={(e) => setCompFilter(e.target.value)}>
            <option value="all">All competitions</option>
            {(tournament.competitions || []).map((cc) => <option key={cc.id} value={cc.id}>{cc.name}</option>)}
          </select>
        )}
        <div className="seg" role="group" aria-label="Match status">
          <button type="button" aria-pressed={statusFilter === "all"} className={statusFilter === "all" ? "is-active" : ""} onClick={() => setStatusFilter("all")}>All</button>
          <button type="button" aria-pressed={statusFilter === "running"} className={statusFilter === "running" ? "is-active" : ""} onClick={() => setStatusFilter("running")}>Now</button>
          <button type="button" aria-pressed={statusFilter === "scheduled"} className={statusFilter === "scheduled" ? "is-active" : ""} onClick={() => setStatusFilter("scheduled")}>Scheduled</button>
          <button type="button" aria-pressed={statusFilter === "complete"} className={statusFilter === "complete" ? "is-active" : ""} onClick={() => setStatusFilter("complete")}>Completed</button>
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
            (pools/mixed/knockout/league/swiss), so we don't claim "Pool play is
            complete" or point at a specific next tab. */}
        {statusFilter !== "complete" && allMatchesCompleted(filtered) && (
          <div className="alert alert--success" style={{ marginBottom: 12 }}>
            <div style={{ fontWeight: 600, marginBottom: 4 }}>All matches scored</div>
            <div style={{ fontSize: 13, color: "var(--ink-2)" }}>Every visible match is complete. Open the competition to review standings, generate the knockout bracket, or start the next phase.</div>
          </div>
        )}
        {filtered.map((m) => {
          // bc-pnum: sameCompetitor, never a bare `winner.id === side.id`
          // (see bracket.jsx's MatchCard for why the naked equality lights
          // both sides once both are id-less).
          const aWin = !!m.winner && !!m.sideA && sameCompetitor(m.winner, m.sideA);
          const bWin = !!m.winner && !!m.sideB && sameCompetitor(m.winner, m.sideB);
          const isCorrection = m.status === "completed" && m.score?.corrected;
          // bc-tmfn: a TEAM row's score cell (window.matchScoreStr →
          // teamIVPWScore) is deliberately free of marks, so the match-level
          // Kiken/Fus. a default win closed a team match with rides beside
          // the withdrawn team's NAME instead -- the ONE shared computation
          // (teamMatchMarks, side_marks.jsx), which derives its own
          // team-row signal from m.subResults (bc-cse) so an individual
          // match's own mark, already inline in its score string below, is
          // never doubled here.
          const { shiro: teamShiroMark, aka: teamAkaMark } = teamMatchMarks(m);
          // Show the live ippon score for a running bout too (not just completed)
          // so the list reflects scoring in progress; "vs" only before it starts.
          // matchShowsScore is the one gate: a match sent back to the queue
          // keeps its score, but this row shows it as not started, penalty
          // marks included, until it restarts (bc-sbq).
          const showScore = matchShowsScore(m);
          // Outstanding single hansoku → red ▲ next to the offending side (same
          // mark as the scoresheet). hansoku may live on the match or under
          // score.fouls depending on the source; fall back across both.
          const foulB = showScore ? boutHansokuMark(m.hansokuB ?? m.score?.fouls?.b ?? 0) : "";
          const foulA = showScore ? boutHansokuMark(m.hansokuA ?? m.score?.fouls?.a ?? 0) : "";
          // A just-started running bout is 0–0, where formatIpponsScore returns "".
          // Fall back so the cell is never blank: an empty score renders the
          // boutMiddle placeholder (normally "vs"). Live techniques show once present.
          const seScore = showScore ? window.matchScoreStr(m) : "";
          const matchNo = scoreRowMatchLabel(m);
          return (
            <div key={`${m.compId}:${m.id}`} className={`score-edit-row ${m.status === "running" ? "score-edit-row--running is-running" : ""} ${m.status === "completed" ? "score-edit-row--complete" : ""}`}>
              <div>
                <div className="score-edit-row__time">{m.scheduledAt || "-"}</div>
                {matchNo && <div className="score-edit-row__matchno">{matchNo}</div>}
                <div style={{ fontSize: 10, color: "var(--ink-3)", marginTop: 2 }}>{m.compName}</div>
              </div>
              <ScoreEditCourtBtn m={m} courts={tournament.courts || []} onMoveCourt={onMoveCourt} />
              <div className="score-edit-row__sides">
                  {/* The side is carried by the CELL TINT (.side-fill--*), not by a
                      SHIRO/AKA text badge (operator decision 2026-09-20, bc-sccl):
                      DESIGN.md §4 offers a tinted cell, a coloured header or a filled
                      badge for a dense row, and the tint gives the side real area on
                      the densest list in the app. The badge was also the side's only
                      TEXT, so an sr-only label replaces it -- §4 requires colour never
                      be the only signal, and the Shiro hatch covers the sighted case.
                      Mirrors PoolNumberedMatchRow (viewer_standings.jsx), which
                      dropped its badge for the same tint. */}
                  <SideCell side="shiro" className={`score-edit-row__side score-edit-row__side--shiro ${bWin ? "score-edit-row__side--win" : ""}`} style={{ textAlign: "right" }}>
                    {/* bc-cse: msb-name--labelled -- see viewer_match.jsx's
                        VSchedItem comment for why a mark riding beside a
                        clip-mode name needs its own flex row: without it a
                        long name clips the mark off the end. justifyContent
                        here (not the msb-name--aka class, which is Aka's own)
                        because THIS row reads toward the centre score from
                        both sides -- Shiro right-aligned, Aka left-aligned --
                        the opposite of most name cells; the parent's own
                        textAlign:"right" no longer reaches a flex child once
                        .name becomes the flex container. */}
                    <div className="name msb-name--labelled" style={{ justifyContent: "flex-end" }}>{teamNameMark("shiro", teamShiroMark, <NumberedName side="shiro" name={m.sideB?.name} number={m.sideB?.number} clip />)}</div>
                    <div className="dojo">{m.sideB?.dojo}</div>
                  </SideCell>
                  {/* Foul ▲ flanks the SCORE (Shiro left, Aka right): a hansoku is part of
                      the scoreline, so it reads at the score's level. The slots are reserved
                      symmetrically so the centred score never shifts when a foul appears. */}
                  <div className="score-edit-row__score">
                    <span className="score-edit-row__foul">{foulB && <span className="msb-hansoku" data-testid="foul-mark-b">{foulB}</span>}</span>
                    <span className="score-edit-row__scoreval">
                      {seScore || <span style={{ fontSize: 11, color: "var(--ink-3)" }}>{showScore && window.boutMiddle ? window.boutMiddle(m.decision, m.encho, m.score) : "vs"}</span>}
                    </span>
                    <span className="score-edit-row__foul">{foulA && <span className="msb-hansoku" data-testid="foul-mark-a">{foulA}</span>}</span>
                  </div>
                  <SideCell side="aka" className={`score-edit-row__side score-edit-row__side--aka ${aWin ? "score-edit-row__side--win" : ""}`}>
                    {/* No msb-name--aka here: unlike most name cells this row
                        reads Aka LEFT-aligned (toward the centre score from
                        the right side), the opposite of that class's own
                        justify-content:flex-end/color:red base -- this host
                        already owns its own aka styling. */}
                    <div className="name msb-name--labelled">{teamNameMark("aka", teamAkaMark, <NumberedName side="aka" name={m.sideA?.name} number={m.sideA?.number} clip />)}</div>
                    <div className="dojo">{m.sideA?.dojo}</div>
                  </SideCell>
              </div>
              <div>
                {/* Running: no "● NOW" label: the row highlight is the signal (removed as
                    redundant). A completed row says nothing here either (operator ruling
                    2026-09-20, bc-sccl): "Final" restated what the row already showed
                    twice over, in the recorded score and in the button reading "Correct"
                    rather than "Score". "Corrected" stays, because a re-entered result is
                    NOT derivable from anything else on the row. */}
                {isCorrection && <span style={{ fontSize: 10, color: "var(--ink-3)" }}>Corrected</span>}
              </div>
              <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
                <button type="button" className={getScoreBtnClass(m.status)} onClick={() => setOpenKey(scoreKeyOf(m))}>
                  {m.status === "completed" ? "Correct" : "Score"}
                </button>
                {/* mp-bkg: show Lineup button only for team competitions */}
                {(m.compKind === "team" || m.teamSize > 0) && (
                  <button type="button"
                    className="btn btn--ghost btn--sm"
                    style={{ fontSize: 11 }}
                    onClick={() => setLineupKey(scoreKeyOf(m))}
                  >
                    Lineup
                  </button>
                )}
              </div>
              {/* bc-cse: a scheduled match a competitor is barred from cannot
                  be fought as scheduled; show why and the one-tap resolution
                  (BarredMatchNotice, admin_scoring_shared.jsx) on its own full-
                  width row rather than squeezed into the 5-column grid. */}
              {isBarredMatch(m) && (
                <div style={{ gridColumn: "1 / -1" }}>
                  <BarredMatchNotice match={m} password={password} />
                </div>
              )}
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
          onClose={() => setLineupKey(null)}
        />
      )}

      {openMatch && (() => {
        // For pool daihyosen/tiebreaker bouts, enrich the match with rep-player
        // roster data (repIsTeam / repRosterA / repRosterB) so ScoreEditorModal
        // renders the representative-player pickers. Regular matches pass through.
        // window.enrichPoolMatchWithComp is assigned by admin_pools.jsx at module
        // evaluation time, before this component renders.
        const enrichedOpenMatch = (window.isSupplementaryBout && window.isSupplementaryBout(openMatch.id) && window.enrichPoolMatchWithComp)
          ? window.enrichPoolMatchWithComp(openMatch, (tournament.competitions || []).find(cc => cc.id === openMatch.compId))
          : openMatch;
        // Chained nav (Prev/Next/←/→) must stay on the same
        // shiaijo. Operators run matches per-court; jumping courts mid-flow
        // skips the wrong matches. Unassigned matches scope to other
        // unassigned matches so the behaviour is consistent.
        const openCourt = openMatch.court || "";
        const sameCourt = filtered.filter(m => (m.court || "") === openCourt);
        const openIdx = sameCourt.findIndex(m => scoreKeyOf(m) === openKey);
        const prevMatch = openIdx > 0 ? sameCourt[openIdx - 1] : null;
        const nextMatch = openIdx >= 0 && openIdx < sameCourt.length - 1 ? sameCourt[openIdx + 1] : null;
        return (
          <ScoreEditorModal
            key={openKey}
            match={enrichedOpenMatch}
            prevMatch={prevMatch}
            nextMatch={nextMatch}
            onPrev={() => setOpenKey(scoreKeyOf(prevMatch))}
            onNext={() => setOpenKey(scoreKeyOf(nextMatch))}
            onClose={() => setOpenKey(null)}
            allowCourtForce
            onSubmit={async (patch, opts) => {
              try {
                // opts: the editor's own claim on a refusal (onRefusal).
                const res = await onEditScore(openMatch.compId, openMatch.id, patch, openMatch, { ...opts, allowCourtForce: true });
                if (!mountedRef.current) return res;
                // Whether the modal stays open is writeKeepsEditorOpen's
                // (write_result.jsx), the rule every host asks. A write that did
                // not land must not CLOSE the modal: doing so is a false
                // success. Queued (F5) or superseded (bc-lww1) alike, the editor
                // keeps the operator's entry on screen with its not-saved banner.
                //
                // ▶ Start Match: keep the operator IN the scoring surface rather
                // than dumping them back to the list (which forced a re-find +
                // reopen per match). A "start" patch is status:running with no
                // winner: flip the open match's status optimistically so the
                // modal re-renders as the live scoring board (the background
                // refresh/SSE then reconciles the canonical state). Any other
                // submit (finish/correction/draw) closes as before.
                //
                // mp-gmcg: a kachinuki Record-bout write (kachinukiBoutFinal)
                // comes back with the POST-advance bout log: the server
                // appended the next pairing (winner stays / stays-on slot).
                //
                // Nothing is hand-forwarded onto the open match any more: it is
                // resolved from live data every render, and editMatchScore
                // AWAITS its own refresh before resolving, so by this line the
                // started/advanced match is already what openMatch reads. The
                // two `setOpenMatch(prev => ({...prev, status:"running"}))`
                // patches that used to sit here (and the freshSubs they carried)
                // were compensating for the snapshot, partially.
                if (writeKeepsEditorOpen(patch, res)) {
                  // mp-gmcg review C1: also hand `res` back to the modal itself. A
                  // prior [Remove this bout] can leave the modal's local
                  // matchOverride shadowing THIS prop, and a Record-bout append can
                  // return openMatch.subResults.length to EXACTLY its pre-removal
                  // value (remove: L→L-1 here only; this append: L-1→L again) — the
                  // modal's length-keyed effect never observes a change and the
                  // override freezes forever. Returning res lets the modal adopt
                  // the fresh subResults into its own override directly, with no
                  // dependency on this prop ever visibly changing.
                  return res;
                } else {
                  setOpenKey(null);
                }
              } catch (_err) {
                // Error handled by onEditScore/toast, but we catch here to keep modal open
              }
            }}
            onSubmitAndNext={null}
            password={password}
          />
        );
      })()}
    </div>
  );
}
