// retime_warning.jsx: what moving a drawn (draw-ready) competition does to its
// matches, told the same way wherever it is asked: before a Settings save and
// in the calendar's move dialog. The server's dry run (GET
// /api/schedule/calendar/preview, `retime`; RetimeOutcome in specs/openapi.yaml)
// is read by readRetimePreview and drawn by RetimeWarning.

import { finishClock } from './admin_schedule_utils.jsx';

// answer.retime = {
//   anyChange: boolean,
//   matchCount: number,
//   notFreshCount: number,
//   courts: [ { court: "A", oldStart: 855, oldEnd: 962, newStart: 900, newEnd: 1007 } ]
// }
// where the four times are minutes from midnight (an end can be 1440 or more for
// a finish past midnight; formatted with finishClock which wraps at 24h).
export function readRetimePreview(answer) {
  if (!answer || !answer.retime) return null;
  const r = answer.retime;
  const formatClock = (mins) => {
    if (typeof mins === "string") return mins;
    if (Number.isFinite(mins)) return finishClock("00:00", mins).clock;
    return "";
  };
  return {
    anyChange: !!r.anyChange,
    matchCount: Number(r.matchCount) || 0,
    notFreshCount: Number(r.notFreshCount) || 0,
    courts: Array.isArray(r.courts)
      ? r.courts.map((c) => ({
          court: c.court,
          oldStart: formatClock(c.oldStart),
          oldEnd: formatClock(c.oldEnd),
          newStart: formatClock(c.newStart),
          newEnd: formatClock(c.newEnd),
        }))
      : [],
  };
}

// RetimeWarning draws a preview read by readRetimePreview that changes a time.
// `lead` names the act ("Saving moves" in Settings, "Moving it moves" in the
// calendar).
export function RetimeWarning({ preview, lead = "Saving moves" }) {
  if (!preview || !preview.anyChange) return null;
  return (
    <div role="status" className="alert alert--warn retime-warning" data-testid="retime-warning">
      <div className="retime-warning__lead">
        {preview.matchCount === 1
          ? `${lead} its 1 match, in the same order:`
          : `${lead} its ${preview.matchCount} matches, in the same order:`}
      </div>
      <div className="retime-warning__courts">
        {preview.courts.map((courtInfo) => (
          <div key={courtInfo.court} className="retime-warning__court">
            <span>{`Shiaijo ${courtInfo.court}: ${courtInfo.newStart} to ${courtInfo.newEnd} `}</span>
            <span>{`(was ${courtInfo.oldStart} to ${courtInfo.oldEnd})`}</span>
          </div>
        ))}
      </div>
      {preview.notFreshCount > 0 && (
        <div className="retime-warning__stale">
          {preview.notFreshCount === 1
            ? "1 of its matches no longer has the time a fresh draw would give it now: a time set by hand, a pause added or changed, or another competition moved since the draw. It gets a new time too."
            : `${preview.notFreshCount} of its matches no longer have the times a fresh draw would give them now: a time set by hand, a pause added or changed, or another competition moved since the draw. They get new times too.`}
        </div>
      )}
      <div className="retime-warning__note">
        The pools, the bracket and the competitor numbers stay as they are, and so do its shiaijo. Anyone who printed or noted the old times will need the new ones.
      </div>
    </div>
  );
}
