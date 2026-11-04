// pause_list.jsx: Schedule pauses editor and duration parser for tournament setup.
// Pinned to internal/engine/testdata/block_duration_cases.json and scheduler_slots.go.

export const MAX_PAUSES = 20;

/**
 * Converts a Go-style duration string ("30m", "1h", "1h30m", etc.) to whole minutes.
 * Returns 0 for empty or unparseable duration strings.
 * Matches Go's parseDurationMinutes in internal/engine/scheduler_slots.go.
 */
export function blockMinutes(text) {
  if (typeof text !== 'string' || text === '') return 0;

  // Match Go time.ParseDuration: optional sign, then sequences of number+unit.
  // Leading/trailing whitespace or invalid unit makes it invalid.
  const fullMatch = /^([+-])?(?:(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|ms|s|m|h))+$/.test(text);
  if (!fullMatch) return 0;

  const sign = text.startsWith('-') ? -1 : 1;
  const unitSeconds = {
    h: 3600,
    m: 60,
    s: 1,
    ms: 0.001,
    us: 0.000001,
    'µs': 0.000001,
    ns: 0.000000001,
  };

  const partRegex = /(\d+(?:\.\d*)?|\.\d+)(ns|us|µs|ms|s|m|h)/g;
  let match;
  let totalSeconds = 0;
  while ((match = partRegex.exec(text)) !== null) {
    const val = parseFloat(match[1]);
    const unit = match[2];
    totalSeconds += val * unitSeconds[unit];
  }
  totalSeconds *= sign;
  if (totalSeconds <= 0) return 0;
  return Math.round(totalSeconds / 60);
}

/**
 * Parses "HH:MM" (or "H:MM") clock into minutes since midnight (0-1439).
 * Returns null if invalid or empty.
 */
export function parseClock(str) {
  if (typeof str !== 'string') return null;
  const m = str.trim().match(/^(\d{1,2}):(\d{2})$/);
  if (!m) return null;
  const h = parseInt(m[1], 10);
  const min = parseInt(m[2], 10);
  if (h < 0 || h > 23 || min < 0 || min > 59) return null;
  return h * 60 + min;
}

/**
 * Formats minutes since midnight into "HH:MM".
 */
export function formatClock(minutes) {
  const norm = ((minutes % 1440) + 1440) % 1440;
  const h = Math.floor(norm / 60);
  const m = norm % 60;
  return `${String(h).padStart(2, '0')}:${String(m).padStart(2, '0')}`;
}

/**
 * Computes status explanation for a pause row.
 */
export function computePauseRowStatus(pause, index, allPauses, durationDays = 1) {
  const day = pause.day ?? 0;
  const daysCount = durationDays > 0 ? durationDays : 1;

  if (day > daysCount) {
    return {
      type: 'invalid_day',
      text: `Not used: the tournament no longer has Day ${day}.`,
      warn: true,
    };
  }

  const startMin = parseClock(pause.start);
  if (startMin === null) {
    return {
      type: 'missing_start',
      text: 'Not used yet: give it a start time.',
      warn: true,
    };
  }

  const durMin = blockMinutes(pause.length);
  if (durMin <= 0) {
    return {
      type: 'missing_length',
      text: 'Not used yet: give it a length like 30m.',
      warn: true,
    };
  }

  const endMin = startMin + durMin;

  // Check for overlaps with other usable pauses on the same day.
  // Day 0 overlaps every day.
  const overlaps = [];
  allPauses.forEach((other, otherIdx) => {
    if (otherIdx === index) return;
    const oDay = other.day ?? 0;
    if (oDay > daysCount) return;

    // Check if days overlap
    let daysOverlap = false;
    let conflictDay = 0; // 0 = every day / 1-day tournament
    if (daysCount <= 1 || (day === 0 && oDay === 0)) {
      daysOverlap = true;
      conflictDay = 0;
    } else if (day === 0 && oDay > 0) {
      daysOverlap = true;
      conflictDay = oDay;
    } else if (day > 0 && oDay === 0) {
      daysOverlap = true;
      conflictDay = day;
    } else if (day > 0 && oDay > 0 && day === oDay) {
      daysOverlap = true;
      conflictDay = day;
    }

    if (!daysOverlap) return;

    const oStart = parseClock(other.start);
    if (oStart === null) return;
    const oDur = blockMinutes(other.length);
    if (oDur <= 0) return;
    const oEnd = oStart + oDur;

    // Time window overlap (non-empty intersection)
    if (Math.max(startMin, oStart) < Math.min(endMin, oEnd)) {
      overlaps.push({
        pause: other,
        start: oStart,
        end: oEnd,
        conflictDay,
      });
    }
  });

  if (overlaps.length > 0) {
    const otherNames = overlaps
      .map((o) => (o.pause.name || '').trim() || 'another pause')
      .join(', ');

    const groupStarts = [startMin, ...overlaps.map((o) => o.start)];
    const groupEnds = [endMin, ...overlaps.map((o) => o.end)];
    const mergedStart = Math.min(...groupStarts);
    const mergedEnd = Math.max(...groupEnds);

    const mergedStartClock = formatClock(mergedStart);
    const mergedEndClock = formatClock(mergedEnd);
    const pastMidnight = mergedEnd >= 1440 ? ' (after midnight)' : '';

    const firstConflictDay = overlaps[0].conflictDay;
    const dayClause = (daysCount > 1 && firstConflictDay > 0) ? ` on Day ${firstConflictDay}` : '';

    return {
      type: 'overlap',
      text: `Together with ${otherNames}: matches stop ${mergedStartClock} to ${mergedEndClock}${pastMidnight}${dayClause}.`,
      warn: false,
    };
  }

  // Usable standalone row
  const startClock = formatClock(startMin);
  const endClock = formatClock(endMin);
  const pastMidnight = endMin >= 1440 ? ' (after midnight)' : '';

  return {
    type: 'usable',
    text: `Matches stop ${startClock} to ${endClock}${pastMidnight}.`,
    warn: false,
  };
}

const NO_PAUSES = Object.freeze([]);
const NO_DAYS = Object.freeze([]);
const NO_DRAWN = Object.freeze([]);

let pauseKeySeq = 0;
const allocPauseKey = () => `pause-k-${++pauseKeySeq}`;

/**
 * PauseList component: The Schedule blocks pause editor.
 */
export function PauseList({
  pauses = NO_PAUSES,
  onChange,
  durationDays = 1,
  tournamentDays = NO_DAYS,
  drawnCompetitions = NO_DRAWN,
  changed = false,
  dirty = false,
}) {
  const keysRef = React.useRef([]);
  while (keysRef.current.length < pauses.length) {
    keysRef.current.push(allocPauseKey());
  }
  if (keysRef.current.length > pauses.length) {
    keysRef.current.length = pauses.length;
  }

  const daysCount = Number.isInteger(durationDays) && durationDays > 0 ? durationDays : 1;
  const isMultiDay = daysCount > 1;
  const isDirty = changed || dirty;
  const isMaxReached = pauses.length >= MAX_PAUSES;

  const handleUpdate = (index, field, value) => {
    const next = pauses.map((p, i) => {
      if (i !== index) return p;
      return { ...p, [field]: value };
    });
    onChange?.(next);
  };

  const handleRemove = (index) => {
    keysRef.current.splice(index, 1);
    const next = pauses.filter((_, i) => i !== index);
    onChange?.(next);
  };

  const handleAdd = () => {
    if (isMaxReached) return;
    keysRef.current.push(allocPauseKey());
    const newPause = {
      name: '',
      start: '',
      length: '',
      day: 0,
    };
    onChange?.([...pauses, newPause]);
  };

  return (
    <div className="pause-list">
      <div className="pause-intro">
        Add a pause for lunch, an opening or closing ceremony, or anything else. A pause has a start time, so for a closing ceremony give the time you expect the last match to end.
      </div>

      {isDirty && drawnCompetitions && drawnCompetitions.length > 0 && (
        <div className="pause-warning alert alert--warn" style={{ marginBottom: 16 }}>
          Already drawn, keeps its times: {drawnCompetitions.join(', ')}.
        </div>
      )}

      {pauses.length > 0 && (
        <div className="pause-rows">
          {pauses.map((pause, idx) => {
            const status = computePauseRowStatus(pause, idx, pauses, daysCount);
            const pauseDay = pause.day ?? 0;
            const rowKey = keysRef.current[idx];

            return (
              <div key={rowKey} className="pause-row-card">
                <div className={`pause-fields-grid ${isMultiDay ? 'pause-fields-grid--multiday' : ''}`}>
                  <div className="field pause-field">
                    <label className="field__label" htmlFor={`pause-${idx}-name`}>Pause name</label>
                    <input
                      id={`pause-${idx}-name`}
                      className="input"
                      type="text"
                      value={pause.name || ''}
                      placeholder="e.g. Lunch"
                      onChange={(e) => handleUpdate(idx, 'name', e.target.value)}
                    />
                  </div>

                  {isMultiDay && (
                    <div className="field pause-field">
                      <label className="field__label" htmlFor={`pause-${idx}-day`}>On</label>
                      <select
                        id={`pause-${idx}-day`}
                        className="select"
                        value={pauseDay}
                        onChange={(e) => handleUpdate(idx, 'day', parseInt(e.target.value, 10) || 0)}
                      >
                        <option value={0}>Every day</option>
                        {Array.from({ length: daysCount }, (_, i) => {
                          const dayNum = i + 1;
                          const dateStr = tournamentDays[i] ? ` · ${tournamentDays[i]}` : '';
                          return (
                            <option key={dayNum} value={dayNum}>
                              Day {dayNum}{dateStr}
                            </option>
                          );
                        })}
                        {pauseDay > daysCount && (
                          <option value={pauseDay}>
                            Day {pauseDay} (outside tournament)
                          </option>
                        )}
                      </select>
                    </div>
                  )}

                  <div className="field pause-field">
                    <label className="field__label" htmlFor={`pause-${idx}-start`}>Starts at</label>
                    <input
                      id={`pause-${idx}-start`}
                      className="input"
                      type="time"
                      value={pause.start || ''}
                      placeholder="e.g. 12:30"
                      onChange={(e) => handleUpdate(idx, 'start', e.target.value)}
                    />
                  </div>

                  <div className="field pause-field">
                    <label className="field__label" htmlFor={`pause-${idx}-length`}>Length</label>
                    <input
                      id={`pause-${idx}-length`}
                      className="input"
                      type="text"
                      value={pause.length || ''}
                      placeholder="e.g. 1h or 45m"
                      onChange={(e) => handleUpdate(idx, 'length', e.target.value)}
                    />
                    <div className="field__hint">e.g. 1h or 45m</div>
                  </div>
                </div>

                <div className="pause-row-footer">
                  <div className={`pause-row-status ${status.warn ? 'pause-row-status--warn' : ''}`}>
                    {status.text}
                  </div>
                  <button
                    type="button"
                    className="btn btn--sm pause-remove-btn"
                    aria-label={`Remove pause ${idx + 1}`}
                    onClick={() => handleRemove(idx)}
                  >
                    Remove
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}

      <div className="pause-actions" style={{ marginTop: pauses.length > 0 ? 14 : 0, marginBottom: 16 }}>
        <button
          type="button"
          className="btn"
          onClick={handleAdd}
          disabled={isMaxReached}
        >
          + Add a pause
        </button>
        {isMaxReached && (
          <span className="pause-max-hint" style={{ marginLeft: 12, fontSize: 13, color: 'var(--ink-3)' }}>
            Maximum of 20 pauses reached.
          </span>
        )}
      </div>

      <div className="pause-impact-notice">
        <div className="pause-impact-title" style={{ fontWeight: 600, fontSize: 13, marginBottom: 6 }}>
          What a pause changes
        </div>
        <ul className="pause-impact-list" style={{ margin: 0, paddingLeft: 18, fontSize: 13, color: 'var(--ink-2)', lineHeight: 1.5 }}>
          <li>Planned match times skip it on every shiaijo.</li>
          <li>Every estimate follows it: Add competition, each Overview and the competition calendar.</li>
          <li>A draw made after you save also follows it. A competition already drawn keeps the times its draw set.</li>
        </ul>
      </div>
    </div>
  );
}
