// viewer_match.jsx: match-display components extracted from viewer.jsx
// (mp-pxxc step 4). Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module: do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// Cycle note: viewer.jsx imports from this file and re-exports every symbol
// here (plus window.* assignments) so the public surface of viewer.jsx is
// unchanged.

import { useTeamLineups, TeamScoreboard, IndividualScore, withNumber } from './match_scoreboard.jsx';
import { TermV, poolLabel } from './viewer_utils.jsx';
import { DAIHYOSEN_POSITION } from './pool_ids.jsx';

const { useState, useRef: useRefV, useCallback } = React;

// ---------------------------------------------------------------------------
// mymatchQueueLabel: FR-025 label for the "Your next match" Queue chip.
//
// Contract:
//   - status==="scheduled" + queuePosition===1 → "Next up"
//   - status==="scheduled" + queuePosition>1   → "<qp-1> before yours"
//   - status==="running"                       → null (WatchHeroCard signals running
//                                                        via .my-match--running ring + label change;
//                                                        no Queue chip needed)
//   - anything else (completed/forfeit/cancelled, or no qp)  → null (hide chip)
//
// Wording mirrors the VSchedItem helper below and display.jsx::queueLabel
// so all three viewer surfaces agree. Running matches return null because
// WatchHeroCard already signals the running state via the .my-match--running
// CSS ring and label change ("Your match"): the Queue chip must not add a
// redundant label. We intentionally do NOT
// fall back to "Scheduled HH:MM" the way display.jsx does: the
// MyMatchPanel already has a dedicated Time chip.
// Exported for unit-testing.
export function mymatchQueueLabel(m) {
  if (!m) return null;
  if (m.status === "running") return null;
  if (m.status !== "scheduled") return null;
  const qp = Number(m.queuePosition);
  if (!Number.isFinite(qp) || qp <= 0) return null;
  if (qp === 1) return "Next up";
  return `${qp - 1} before yours`;
}

// subBoutLabel: center label for a team sub-bout row. The daihyosen
// (representative bout) is stored with the sentinel DAIHYOSEN_POSITION (see
// admin_scoring_modal.jsx buildPatch); render it as "Daihyosen" (matching
// admin_pools.jsx wording) rather than the literal "Match -1" the
// `position || index+1` fallback would otherwise produce. Shared by both
// viewer sub-row sites (MatchDetailCard, MatchViewerModal). Exported for
// unit-testing.
export function subBoutLabel(sub, index) {
  if (sub && sub.position === DAIHYOSEN_POSITION) return "Daihyosen";
  return `Match ${(sub && sub.position) || index + 1}`;
}

// Local mirror of display.jsx::queueLabelCompact ("Next up" / "#N").
// display.js loads before viewer.js (see index.html), so
// window.queueLabelCompact is normally available on first render; this
// serves as defense-in-depth if that ever changes. (The "N before yours"
// wording lives in mymatchQueueLabel: followed-player context only.)
// Exported so viewer_schedule.jsx's TWMatch shares this single definition.
export function localQueueLabelCompact(m) {
  if (!m || m.status !== "scheduled") return null;
  const qp = Number(m.queuePosition);
  if (!Number.isFinite(qp) || qp <= 0) return null;
  return qp === 1 ? "Next up" : `#${qp}`;
}

// ---------------------------------------------------------------------------
// MatchDetailCard
// ---------------------------------------------------------------------------

export function MatchDetailCard({ match, onClose, escapeToClose = true }) {
  window.useEscapeToClose(escapeToClose ? onClose : undefined);
  if (!match) return null;
  const isTeam = match.compKind === "team" || match.teamSize > 0;
  const teamSize = match.teamSize || 0;
  // withNumber prepends the assigned competitor number (e.g. "K1") when the
  // competition has numberPrefix; team-level sides have no .number so this
  // degrades to the bare team name.
  const aName = withNumber(match.sideA);
  const bName = withNumber(match.sideB);
  const aWin = match.winner?.id === match.sideA?.id && match.winner?.id;
  const bWin = match.winner?.id === match.sideB?.id && match.winner?.id;
  const isRunning = match.status === "running";
  const isDone = match.status === "completed";

  // mp-13y: fetch per-match lineups for team matches so bout rows show
  // competitor names instead of bout numbers.
  const { lineupA, lineupB } = useTeamLineups(isTeam ? match : null, undefined, isTeam ? match.roundIndex : undefined);
  // Show the Daihyosen row when a rep-bout subResult exists (position DAIHYOSEN_POSITION);
  // TeamScoreboard additionally gates it on the match actually being tied.
  const showDH = isTeam && (match.subResults || []).some(s => s.position === DAIHYOSEN_POSITION);

  return (
    <div className="match-detail-card">
      <div className="match-detail-card__head">
        <div className="match-detail-card__meta">
          {match.compName && <><span className="match-detail-card__comp">{match.compName}</span><span>·</span></>}
          <span><TermV name="shiaijo">Shiaijo</TermV> {match.court}</span>
          <span>·</span>
          <span>{match.phase === "pool" ? poolLabel(match) : (match.round || "")}</span>
          {match.scheduledAt && <><span>·</span><span>{match.scheduledAt}</span></>}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          {isRunning && <span className="bc-running">● NOW</span>}
          {isDone && <span style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>FINAL</span>}
          {onClose && <button type="button" className="match-detail-card__close" onClick={onClose} aria-label="Close">×</button>}
        </div>
      </div>

      {/* Individual matches show the two player names colour-coded: Shiro
          dark (left), Aka red (right): matching the team scoreboard's
          summary-row name colours, instead of SHIRO/AKA text badges
          (mp-13y). Team matches carry the names in the summary row. */}
      {!isTeam && (
        <div className="match-detail-card__players">
          <div className={`match-detail-card__side ${bWin ? "match-detail-card__side--win" : ""}`}>
            <span className="match-detail-card__name match-detail-card__name--shiro">{bName}</span>
          </div>
          <div className="match-detail-card__score"><span className="match-detail-card__vs">vs</span></div>
          <div className={`match-detail-card__side match-detail-card__side--right ${aWin ? "match-detail-card__side--win" : ""}`}>
            <span className="match-detail-card__name match-detail-card__name--aka">{aName}</span>
          </div>
        </div>
      )}

      {/* mp-13y: the ONE shared FIK scoreboard (match_scoreboard.jsx): same
          component the TV display uses. Team → team-name + IV/PW summary row +
          per-bout rows (numbered when not yet started) + Daihyosen (tie only);
          individual → ippon-letter slots. */}
      {isTeam
        ? <TeamScoreboard subResults={match.subResults || []} lineupA={lineupA} lineupB={lineupB}
            teamSize={teamSize} showDH={showDH} variant="card" isRunning={isRunning} shiroName={bName} akaName={aName}
            matchSideA={match.sideA?.name || (typeof match.sideA === "string" ? match.sideA : "")}
            matchSideB={match.sideB?.name || (typeof match.sideB === "string" ? match.sideB : "")}
            kachinuki={match.teamMatchType === "kachinuki"} />
        : (isDone || isRunning) && <IndividualScore match={match} variant="card" />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// VSchedItem
// ---------------------------------------------------------------------------

export const VSchedItem = React.memo(({ m, tweaks, showCompetition, onClick, highlight }) => {
  const aWin = m.winner && m.sideA && m.winner.id === m.sideA.id;
  const bWin = m.winner && m.sideB && m.winner.id === m.sideB.id;
  // Bracket matches carry scoreA/scoreB strings rather than ipponsA/B arrays.
  // Fall back so the score string reflects per-side letters instead of the
  // orientation-agnostic winnerPts–loserPts that formatIpponsScore uses when
  // both ippon arrays are absent (which would invert left/right when AKA wins).
  const vIpponsA = m.ipponsA || window.ipponsFromScore(m.scoreA);
  const vIpponsB = m.ipponsB || window.ipponsFromScore(m.scoreB);
  // Score string for completed matches (final) and running matches (live, once
  // at least one ippon has landed). matchScoreStr returns "" before any score
  // exists, so a just-started running match falls through to the "vs" render.
  const isRunning = m.status === "running";
  const scoreStr = (m.status === "completed" || isRunning)
    ? (window.matchScoreStr(m, vIpponsB, vIpponsA) || null)
    : null;
  // FR-025: queue position is 1-indexed per court for scheduled matches;
  // running/completed are 0 (set server-side, omitempty in JSON → undefined
  // on older payloads). Treat null/undefined/0 as "don't render" so the UI
  // stays gracefully empty for non-queued matches and pre-T046 responses.
  // Wording is owned by display.jsx::queueLabel (bead mp-e3k) so every
  // viewer surface stays in sync; we still gate on scheduled+qp>0 here
  // because this row already renders ● NOW / Final on the right for
  // running/completed and we don't want the fallback "Scheduled hh:mm".
  const qp = Number(m.queuePosition);
  // Use the NEUTRAL court-queue label ("Next up" / "#N") here, not the
  // "N before yours" wording: VSchedItem renders on the general schedule and
  // home lists where no player is selected, so "yours" is meaningless. The
  // "…before yours" phrasing is reserved for the followed-player next-match
  // banner (mymatchQueueLabel), which has a real "you" context.
  const queueLabel = (m.status === "scheduled" && Number.isFinite(qp) && qp > 0)
    ? (window.queueLabelCompact ? window.queueLabelCompact(m) : localQueueLabelCompact(m))
    : null;
  return (
    <button type="button" className={`vsched-item ${m.status === "running" ? "vsched-item--running" : ""} ${highlight ? "vsched-item--me" : ""}`} onClick={onClick} data-clickable={onClick ? "" : undefined}>
      <div className="vsched-item__head">
        <span className="vsched-item__time">{m.scheduledAt || "-"}</span>
        <span className="vsched-item__court">SHIAIJO {m.court}</span>
        {queueLabel && (
          <span className={`vsched-item__queue${qp === 1 ? " vsched-item__queue--next" : ""}`}>
            {queueLabel}
          </span>
        )}
        {m.status === "completed" && <span className="vsched-item__status">Final</span>}
        {m.status === "completed" && m.decidedByHantei && (
          <span className="vsched-item__hantei" data-testid="vsched-hantei">HANTEI</span>
        )}
      </div>
      {(showCompetition || m.phase === "pool" || m.round) ? (
        <div className="vsched-item__ctx">
          {showCompetition && m.compName ? m.compName : null}
          {showCompetition && m.compName && (m.phase === "pool" || m.round) ? " · " : null}
          {m.phase === "pool" ? poolLabel(m) : (m.round || null)}
        </div>
      ) : null}
      <div className="vsched-item__players">
        <div className={`vsched-item__side vsched-item__side--shiro ${bWin ? "vsched-item__side--w" : ""}`}>
          <span className="sr-only">Shiro:</span>
          <span className="n">{withNumber(m.sideB)}</span>
          {tweaks.showDojo && m.sideB?.dojo ? <span className="d">{m.sideB.dojo}</span> : null}
        </div>
        {scoreStr ? (
          <span className={`vsched-item__score${isRunning ? " vsched-item__score--live" : ""}`}>{scoreStr}</span>
        ) : m.status === "completed" ? (
          <span className="vsched-item__vs">-</span>
        ) : (
          <span className="vsched-item__vs">vs</span>
        )}
        <div className={`vsched-item__side vsched-item__side--aka ${aWin ? "vsched-item__side--w" : ""}`}>
          <span className="sr-only">Aka:</span>
          <span className="n">{withNumber(m.sideA)}</span>
          {tweaks.showDojo && m.sideA?.dojo ? <span className="d">{m.sideA.dojo}</span> : null}
        </div>
      </div>
    </button>
  );
});
VSchedItem.displayName = "VSchedItem";

// ---------------------------------------------------------------------------
// MatchViewerModal
// ---------------------------------------------------------------------------

export function MatchViewerModal({ match, onClose, tournament, compId: defaultCompId }) {
  window.useEscapeToClose(onClose);
  const [scoringMatch, setScoringMatch] = useState(null);
  const triggerRef = useRefV(null);
  const trapRef = useRefV(null);
  const modalRefCb = useCallback((node) => {
    if (node) {
      triggerRef.current = document.activeElement;
      const prevOverflow = document.body.style.overflow;
      document.body.style.overflow = "hidden";
      const onKeyDown = (e) => {
        if (e.key !== "Tab") return;
        const f = [...node.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])')]
          .filter((el) => !el.disabled && el.offsetParent !== null);
        if (f.length === 0) { e.preventDefault(); return; }
        const first = f[0], last = f[f.length - 1], active = document.activeElement;
        if (e.shiftKey && (active === first || active === node)) { e.preventDefault(); last.focus(); }
        else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus(); }
      };
      document.addEventListener("keydown", onKeyDown, true);
      const focusTimer = setTimeout(() => {
        (node.querySelector("button") || node).focus();
      }, 0);
      trapRef.current = { onKeyDown, focusTimer, prevOverflow };
    } else {
      const t = trapRef.current;
      if (t) {
        clearTimeout(t.focusTimer);
        document.removeEventListener("keydown", t.onKeyDown, true);
        document.body.style.overflow = t.prevOverflow;
        trapRef.current = null;
      }
      const trig = triggerRef.current;
      if (trig && typeof trig.focus === "function" && document.contains(trig)) trig.focus();
    }
  }, []);
  if (!match) return null;
  const isSelfRun = tournament && tournament.mode === "self-run";
  const bothSidesReady = window.hasBothSides ? window.hasBothSides(match) : false;
  const isFinalized = match.status === "completed";
  const sideAName = match.sideA?.name || (typeof match.sideA === "string" ? match.sideA : "");
  const sideBName = match.sideB?.name || (typeof match.sideB === "string" ? match.sideB : "");
  const dialogLabel = sideAName && sideBName ? `Match: ${sideBName} vs ${sideAName}` : "Match details";

  if (scoringMatch && window.ScoreEditorModal) {
    return React.createElement(window.ScoreEditorModal, {
      match: scoringMatch,
      onClose: () => setScoringMatch(null),
      onSubmit: async (patch) => {
        try {
          const res = await window.API.recordScore(scoringMatch.compId || defaultCompId, scoringMatch.id, patch, "", scoringMatch);
          // A queued (offline/transient) write is NOT a confirmed save: keep the
          // editor open and return the signal so its pending-write banner shows:
          // closing here would be a false success on the public self-run surface.
          if (res && res.queued) return res;
          setScoringMatch(null);
          onClose();
          return res;
        } catch (err) {
          // A non-queued failure (direct 4xx validation/conflict, or an unexpected
          // exception). ScoreEditorModal doesn't render generic submit errors and the
          // public viewer has no toast, so surface it explicitly: log + alert (the
          // established fallback here, mirroring app.jsx): rather than silently
          // leaving the modal open. The modal stays open so the operator can retry.
          console.error("Self-run score submit failed:", err);
          window.alert(err && err.message ? err.message : "Couldn't save the result. Please try again.");
        }
      },
      password: "",
      selfReport: true,
    });
  }

  return (
    <div className="modal-backdrop" onClick={onClose} style={{ zIndex: 1000, display: 'flex', alignItems: 'center', justifyContent: 'center' }}>
      <div ref={modalRefCb} tabIndex={-1} role="dialog" aria-modal="true" aria-label={dialogLabel} onClick={e => e.stopPropagation()} style={{ width: "100%", maxWidth: 500, margin: 16 }}>
        {/* Reuse the canonical MatchDetailCard so the modal and the inline
            card render identically (DRY): same header, colour badges and
            BoutSubRow team grid. The modal adds only the self-run scoring. */}
        <MatchDetailCard match={match} onClose={onClose} escapeToClose={false} />
        {isSelfRun && bothSidesReady && (
          <div className="card" style={{ marginTop: 12, padding: 16 }}>
            {isFinalized ? (
              <div style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, color: "var(--ink-3)" }}>
                <span style={{ background: "var(--bg-2)", padding: "4px 10px", borderRadius: 4, fontSize: 12 }}>Result reported</span>
                <span>Contact the organizer to correct this result.</span>
              </div>
            ) : (
              <button type="button"
                className="btn btn--primary btn--sm"
                onClick={() => setScoringMatch({ ...match, id: match.id })}
              >
                Report result
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
