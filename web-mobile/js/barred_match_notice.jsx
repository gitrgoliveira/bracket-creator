// barred_match_notice.jsx: the ONE component that renders "this scheduled
// match cannot be fought because a competitor is barred" -- the note, the
// one-tap default-win action, and (where reinstateable) the Reinstate
// action -- across the court console, the Scores tab, and both score
// editors. Reads ineligible_match.jsx's stamp; writes through the ordinary
// /decision and /reinstate calls, exactly like every other operator action.
//
// A DELIBERATE LEAF: imports only ineligible_match.jsx and write_result.jsx,
// both leaves themselves with no imports of their own (see their headers for
// why). admin_scoring_shared.jsx re-exports this component so the editors'
// existing `from './admin_scoring_shared.jsx'` import lists need no change,
// but admin_shiaijo.jsx and admin_schedule_score_editor.jsx import it
// directly from here. Importing it via admin_scoring_shared.jsx from either
// of those two pulls in that module's own import of bracket.jsx (for
// sideMarks), which assigns `window.BracketTree` etc. at module-eval time
// (bracket.jsx's bottom) and silently overwrote those two render suites'
// `BracketTree: () => null` stub, installed before the dynamic import --
// found via the two shiaijo render suites failing on the REAL bracket tree
// mounting (ResizeObserver) and relabelling ("Winner of r2-m0" no longer
// matched `getByText`) the moment this component's import was added there.
// Route straight to this leaf from any surface that does not already need
// admin_scoring_shared.jsx's much larger graph.

const { useState, useEffect, useRef } = React;

import {
  barredNote, awaitedDefaultWin, defaultWinDecisionBody, defaultWinActionLabel, bothBarredDrawAction,
} from './ineligible_match.jsx';
import { notLandedBanner, writeDidNotLand } from './write_result.jsx';

// No onDone/refresh plumbing here on purpose: recording the default win, or
// reinstating the competitor, is an ordinary /decision or /reinstate write,
// and every host already re-renders from live data (SSE match_updated /
// competitor_status_updated) once it lands. `onDone`, when a caller passes
// one, still fires so a host that wants to react sooner (e.g. close a modal)
// can.
export function BarredMatchNotice({ match, password, onDone }) {
  const [busy, setBusy] = useState(false);
  // bc-cse: without this, a successful write's button re-enables the instant
  // busy clears and stays tappable until the SSE refetch repaints this
  // component away (or replaces `match` with one no longer barred) -- a
  // second tap in that window sends a second /decision (or /reinstate).
  // Mirrors useMatchReopen's landed state: lock the button on ANY response
  // that is not refused, queued included (a queued write still lands later,
  // so a second tap would double-enqueue it).
  const [landed, setLanded] = useState(false);
  const [err, setErr] = useState("");
  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);

  const note = barredNote(match);
  if (!note) return null;
  const w = awaitedDefaultWin(match);
  const drawAction = bothBarredDrawAction(match);
  const pw = password || "";

  // bc-cse: shared by the default-win decision AND the both-barred draw
  // decision -- both are ordinary /decision writes and must land/queue/fail
  // identically.
  const submitDecision = async (body) => {
    if (!body || !window.API || typeof window.API.recordDecision !== "function") return;
    setErr("");
    setBusy(true);
    try {
      const res = await window.API.recordDecision(match.compId, match.id, body, pw);
      if (!mountedRef.current) return;
      // bc-cse: notLandedBanner, not writeWasSuperseded alone -- a clock_skew
      // refusal used to read "a newer result for this match is already
      // recorded", which sends the operator to check a result that was never
      // written and tells them not to do the one thing (re-enter it) that
      // would actually save it. See write_result.jsx for why the two must
      // never share a banner.
      const banner = notLandedBanner(res);
      if (banner) {
        setErr(`Not saved: ${banner.reason}. ${banner.advice}`);
        return;
      }
      if (writeDidNotLand(res)) {
        setErr("Not saved yet: queued, and will be recorded once the connection returns.");
      }
      setLanded(true);
      if (typeof onDone === "function") onDone(res);
    } catch (e) {
      if (!mountedRef.current) return;
      setErr(e?.message || "Failed to record the decision");
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  const recordDefaultWin = () => submitDecision(defaultWinDecisionBody(match));
  const recordDrawn = () => submitDecision(drawAction?.body);

  const reinstate = async () => {
    const id = w?.barred?.id;
    if (!w || !w.reinstateable || !id || !window.API || typeof window.API.reinstateCompetitor !== "function") return;
    setErr("");
    setBusy(true);
    try {
      const res = await window.API.reinstateCompetitor(match.compId, id, pw);
      if (!mountedRef.current) return;
      setLanded(true);
      if (typeof onDone === "function") onDone(res);
    } catch (e) {
      if (!mountedRef.current) return;
      setErr(e?.message || "Failed to reinstate the competitor");
    } finally {
      if (mountedRef.current) setBusy(false);
    }
  };

  return (
    <div className="alert alert--warn" role="status" data-testid="barred-match-notice">
      <div>{note}</div>
      {err && <div style={{ marginTop: 4 }}>{err}</div>}
      <div style={{ display: "flex", gap: 8, marginTop: 6, flexWrap: "wrap" }}>
        {w && (
          <button type="button" className="btn btn--sm" data-testid="barred-match-default-win"
            onClick={recordDefaultWin} disabled={busy || landed}>
            {busy ? "Recording…" : landed ? "Recorded" : defaultWinActionLabel(match)}
          </button>
        )}
        {w && w.reinstateable && (
          <button type="button" className="btn btn--sm btn--ghost" data-testid="barred-match-reinstate"
            onClick={reinstate} disabled={busy || landed}>
            {busy ? "Reinstating…" : landed ? "Recorded" : `Reinstate ${w.barred?.name || "competitor"}`}
          </button>
        )}
        {drawAction && (
          <button type="button" className="btn btn--sm" data-testid="barred-match-record-drawn"
            onClick={recordDrawn} disabled={busy || landed}>
            {busy ? "Recording…" : landed ? "Recorded" : drawAction.label}
          </button>
        )}
      </div>
    </div>
  );
}
