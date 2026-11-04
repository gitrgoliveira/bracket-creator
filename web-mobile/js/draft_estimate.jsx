// draft_estimate.jsx: Schedule estimate hook and panel for unsaved competition shapes.
// Consumed by the Add competition form (admin_setup.jsx).

import { API } from './api_client.jsx';
import { finishClock, formatEstimateMinutes, estimateRangeParts } from './admin_schedule_utils.jsx';

// isShapeIdle reports true when required fields (participants count and start time)
// are missing or empty, in which case no request is sent.
function isShapeIdle(shape) {
  if (!shape) return true;
  const rawP = shape.participants;
  if (rawP === "" || rawP === null || rawP === undefined) return true;
  const p = Number(rawP);
  if (!Number.isFinite(p) || p <= 0) return true;
  const rawStart = shape.startTime;
  if (typeof rawStart !== "string" || rawStart.trim().length === 0) return true;
  return false;
}

// useDraftEstimate debounces shape changes by 250 ms, manages an AbortController
// per request, and queries the draft estimate endpoint. Returns { estimate, loading, error }.
export function useDraftEstimate(shape, password) {
  const [estimate, setEstimate] = React.useState(null);
  const [loading, setLoading] = React.useState(false);
  const [error, setError] = React.useState(null);

  const timerRef = React.useRef(null);
  const controllerRef = React.useRef(null);
  const shapeRef = React.useRef(shape);
  shapeRef.current = shape;
  const passwordRef = React.useRef(password);
  passwordRef.current = password;

  // Track serialization of shape to detect changes.
  const shapeKey = JSON.stringify(shape);

  React.useEffect(() => {
    // If shape lacks participants or start time, report idle state.
    if (isShapeIdle(shape)) {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      if (controllerRef.current) {
        controllerRef.current.abort();
        controllerRef.current = null;
      }
      setEstimate(null);
      setLoading(false);
      setError(null);
      return;
    }

    if (timerRef.current) {
      clearTimeout(timerRef.current);
    }

    timerRef.current = setTimeout(async () => {
      timerRef.current = null;
      if (controllerRef.current) {
        controllerRef.current.abort();
      }
      const controller = new AbortController();
      controllerRef.current = controller;

      setLoading(true);

      const api = (typeof window !== "undefined" && window.API) || API;
      const curShape = shapeRef.current;
      const curPw = passwordRef.current;

      try {
        const res = await api.estimateDraft(curShape, curPw, controller.signal);
        if (!controller.signal.aborted) {
          setEstimate(res);
          setError(null);
          setLoading(false);
        }
      } catch (err) {
        if (!controller.signal.aborted && err.name !== "AbortError") {
          setError(err.message || "Failed to estimate schedule");
          setEstimate(null);
          setLoading(false);
        }
      }
    }, 250);

    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };
  }, [shapeKey, password]);

  // Unmount cleanup: cancel any pending timer and abort in-flight controller.
  React.useEffect(() => {
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      if (controllerRef.current) {
        controllerRef.current.abort();
        controllerRef.current = null;
      }
    };
  }, []);

  return { estimate, loading, error };
}

// DraftEstimatePanel renders a draft competition schedule estimate.
export function DraftEstimatePanel({
  estimate,
  loading,
  error,
  shape,
  tournamentCourts,
  onChangeShiaijo,
  onEditTournament,
}) {
  if (error) {
    return (
      <div className="alert alert--error" role="alert">
        {error}
      </div>
    );
  }

  const isTeam = shape?.kind === "team" || Number(shape?.teamSize) > 0;

  if (!estimate) {
    if (loading) {
      return (
        <div className="draft-estimate draft-estimate--loading">
          <div className="draft-estimate__note" data-testid="draft-estimate-note">
            Working out...
          </div>
        </div>
      );
    }
    return (
      <div className="draft-estimate">
        <div className="draft-estimate__note" data-testid="draft-estimate-note">
          Enter the expected number of {isTeam ? "teams" : "participants"} and a start time to see an estimate.
        </div>
      </div>
    );
  }

  // 1. Finish line
  const { clock: finishTime, nextDay } = finishClock(shape?.startTime, estimate.totalDurationMinutes);
  let finishElement = null;
  if (nextDay) {
    finishElement = (
      <div className="draft-estimate__finish draft-estimate__finish--warn alert alert--warn" data-testid="draft-estimate-finish">
        Finishes about {finishTime}, after midnight. Start it earlier or give it more shiaijo.
      </div>
    );
  } else if (finishTime) {
    finishElement = (
      <div className="draft-estimate__finish" data-testid="draft-estimate-finish">
        Finishes about {finishTime}
      </div>
    );
  }

  // 2. Duration line
  const durationElement = (
    <div className="draft-estimate__duration" data-testid="draft-estimate-duration">
      Est. duration: about {formatEstimateMinutes(estimate.totalDurationMinutes)}
    </div>
  );

  // 3. Derived matches line
  const matchWord = isTeam ? "team match" : "match";
  const matchesWord = isTeam ? "team matches" : "matches";
  const matchParts = [];
  if (estimate.poolMatches > 0) {
    matchParts.push(`${estimate.poolMatches} pool ${estimate.poolMatches === 1 ? matchWord : matchesWord}`);
  }
  if (estimate.knockoutMatches > 0) {
    matchParts.push(`${estimate.knockoutMatches} knockout ${estimate.knockoutMatches === 1 ? matchWord : matchesWord}`);
  }
  const derivedMatchesElement = matchParts.length > 0 ? (
    <div className="draft-estimate__matches">
      {matchParts.join(" · ")}
    </div>
  ) : null;

  // 4. Per shiaijo line
  const courtLabels = (Array.isArray(shape?.courtLabels) && shape.courtLabels.length > 0)
    ? shape.courtLabels
    : ((Array.isArray(tournamentCourts) && tournamentCourts.length > 0)
        ? tournamentCourts
        : null);

  const perCourt = Array.isArray(estimate.perCourtMinutes) ? estimate.perCourtMinutes : [];
  const perCourtParts = perCourt.map((m, idx) => {
    const label = (courtLabels && courtLabels[idx]) || String.fromCharCode(65 + idx);
    if (m === 0) {
      return `${label} not used`;
    }
    return `${label} about ${formatEstimateMinutes(m)}`;
  });
  const perShiaijoElement = perCourtParts.length > 0 ? (
    <div className="draft-estimate__per-shiaijo" data-testid="draft-estimate-per-shiaijo">
      Per shiaijo: {perCourtParts.join(" · ")}
    </div>
  ) : null;

  // 5. Timing line
  const timing = estimate.timing || {};
  const multiplier = timing.multiplier !== undefined && timing.multiplier !== null ? timing.multiplier : 1.5;
  const allowancePct = timing.allowancePct !== undefined && timing.allowancePct !== null ? timing.allowancePct : 10;
  const breaks = Array.isArray(timing.breaks) ? timing.breaks : [];
  let pausesText = "No pauses that day.";
  if (breaks.length > 0) {
    const breakItems = breaks.map(b => `${b.name} ${b.start} to ${b.end}`);
    pausesText = `Pauses that day: ${breakItems.join(", ")}.`;
  }
  const timingElement = (
    <div className="draft-estimate__timing" data-testid="draft-estimate-timing">
      <span>
        Uses this tournament's timing: each match counts as {multiplier} times its clock time, plus {allowancePct}% for the slowest shiaijo. {pausesText}
      </span>
      {onEditTournament && (
        <button
          type="button"
          className="btn btn--sm btn--ghost"
          onClick={onEditTournament}
        >
          Edit tournament
        </button>
      )}
    </div>
  );

  // 6. Kachinuki range
  let kachinukiElement = null;
  if (shape?.teamMatchType === "kachinuki") {
    const range = estimateRangeParts(estimate);
    if (range) {
      const bestFinish = finishClock(shape?.startTime, range.best);
      const worstFinish = finishClock(shape?.startTime, range.worst);
      if (bestFinish.clock && worstFinish.clock) {
        kachinukiElement = (
          <div className="draft-estimate__kachinuki" data-testid="draft-estimate-kachinuki">
            Could end {bestFinish.clock} to {worstFinish.clock} (average)
          </div>
        );
      }
    }
  }

  // 7. Warnings
  const warnings = Array.isArray(estimate.warnings) ? estimate.warnings : [];
  const warningItems = warnings.map((w, idx) => {
    if (w.code === "too_many_shiaijo") {
      const count = w.participants ?? shape?.participants ?? 0;
      const pLabel = isTeam ? (count === 1 ? "team" : "teams") : (count === 1 ? "participant" : "participants");
      const usable = w.usable ?? 1;
      const mLabel = usable === 1 ? "match" : "matches";
      return (
        <div key={idx} className="draft-estimate__warning alert alert--warn" role="alert" data-testid="draft-estimate-warning">
          <span>
            With {count} {pLabel} only {usable} {mLabel} can run at a time, so a second shiaijo would stand empty. Use {usable} shiaijo.
          </span>
          {onChangeShiaijo && (
            <button
              type="button"
              className="btn btn--sm btn--ghost"
              onClick={onChangeShiaijo}
            >
              Change shiaijo
            </button>
          )}
        </div>
      );
    }
    return null;
  }).filter(Boolean);

  return (
    <div className={`draft-estimate ${loading ? "draft-estimate--loading" : ""}`}>
      {finishElement}
      {durationElement}
      {derivedMatchesElement}
      {perShiaijoElement}
      {kachinukiElement}
      {timingElement}
      {warningItems}
      {loading && (
        <div className="draft-estimate__note" data-testid="draft-estimate-note">
          Working out...
        </div>
      )}
    </div>
  );
}

if (typeof window !== "undefined") {
  window.useDraftEstimate = useDraftEstimate;
  window.DraftEstimatePanel = DraftEstimatePanel;
}
