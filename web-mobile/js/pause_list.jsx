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
 * Whether the schedule works around this pause: its "Matches stop for this
 * pause" box is ticked. A pause saved before the box existed carries no
 * matchesGoOn and stops matches. The browser's one owner of the question,
 * mirroring Go's state.Pause.StopsMatches.
 */
export function pauseStopsMatches(pause) {
  return pause?.matchesGoOn !== true;
}

const WILL_NOT_STOP = 'Matches will not stop for this pause';

/**
 * Computes status explanation for a pause row.
 */
export function computePauseRowStatus(pause, index, allPauses, durationDays = 1) {
  const day = pause.day ?? 0;
  const daysCount = durationDays > 0 ? durationDays : 1;

  if (day > daysCount) {
    return {
      type: 'invalid_day',
      text: `${WILL_NOT_STOP}: the tournament no longer has Day ${day}.`,
      warn: true,
    };
  }

  const startMin = parseClock(pause.start);
  if (startMin === null) {
    return {
      type: 'missing_start',
      text: `${WILL_NOT_STOP}: give it a start time.`,
      warn: true,
    };
  }

  const durMin = blockMinutes(pause.length);
  if (durMin <= 0) {
    return {
      type: 'missing_length',
      text: `${WILL_NOT_STOP}: give it a length like 30m.`,
      warn: true,
    };
  }

  const endMin = startMin + durMin;

  if (!pauseStopsMatches(pause)) {
    const pastMidnight = endMin > 1440 ? ' (after midnight)' : '';
    return {
      type: 'matches_go_on',
      text: `Matches go on through it, ${formatClock(startMin)} to ${formatClock(endMin)}${pastMidnight}.`,
      warn: false,
    };
  }

  // Check for overlaps with other usable pauses on the same day.
  // Day 0 overlaps every day.
  const overlaps = [];
  allPauses.forEach((other, otherIdx) => {
    if (otherIdx === index) return;
    if (!pauseStopsMatches(other)) return;
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
    const pastMidnight = mergedEnd > 1440 ? ' (after midnight)' : '';

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
  const pastMidnight = endMin > 1440 ? ' (after midnight)' : '';

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
}) {
  // One stable key per row, so editing a row never remounts its inputs. Add and
  // Remove keep the keys in step; a list replaced from outside (a reload) is
  // brought to the same length here, during render, as React allows for state
  // derived from props. The rows of this render read the brought-up keys, so
  // a Remove whose parent has not yet passed the shorter list never leaves a
  // row without one.
  const [heldKeys, setRowKeys] = React.useState(() => pauses.map(allocPauseKey));
  let rowKeys = heldKeys;
  if (rowKeys.length !== pauses.length) {
    rowKeys = rowKeys.slice(0, pauses.length);
    while (rowKeys.length < pauses.length) rowKeys.push(allocPauseKey());
    setRowKeys(rowKeys);
  }

  const daysCount = Number.isInteger(durationDays) && durationDays > 0 ? durationDays : 1;
  const isMultiDay = daysCount > 1;
  const isMaxReached = pauses.length >= MAX_PAUSES;

  const handleUpdate = (index, field, value) => {
    const next = pauses.map((p, i) => {
      if (i !== index) return p;
      return { ...p, [field]: value };
    });
    onChange?.(next);
  };

  const handleRemove = (index) => {
    setRowKeys(keys => keys.filter((_, i) => i !== index));
    const next = pauses.filter((_, i) => i !== index);
    onChange?.(next);
  };

  const handleAdd = () => {
    if (isMaxReached) return;
    setRowKeys(keys => [...keys, allocPauseKey()]);
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

      {changed && drawnCompetitions && drawnCompetitions.length > 0 && (
        <div className="pause-warning alert alert--warn">
          Already drawn, keeps its times: {drawnCompetitions.join(', ')}.
        </div>
      )}

      {pauses.length > 0 && (
        <div className="pause-rows">
          {pauses.map((pause, idx) => {
            const status = computePauseRowStatus(pause, idx, pauses, daysCount);
            const pauseDay = pause.day ?? 0;
            const rowKey = rowKeys[idx];

            return (
              <div key={rowKey} className="pause-row-card">
                <div className={`pause-fields-grid ${(isMultiDay || pauseDay > daysCount) ? 'pause-fields-grid--multiday' : ''}`}>
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

                  {/* A one-day tournament still shows the day of a pause left on
                      a day it no longer has, so the organiser can move it: a
                      changed pause past the end is refused on save. */}
                  {(isMultiDay || pauseDay > daysCount) && (
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

                <label className="checkbox pause-stops-matches">
                  <input
                    type="checkbox"
                    checked={pauseStopsMatches(pause)}
                    onChange={(e) => handleUpdate(idx, 'matchesGoOn', !e.target.checked)}
                  />
                  Matches stop for this pause
                </label>

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

      <div className="pause-actions">
        <button
          type="button"
          className="btn"
          onClick={handleAdd}
          disabled={isMaxReached}
        >
          + Add a pause
        </button>
        {isMaxReached && (
          <span className="pause-max-hint">
            {`Maximum of ${MAX_PAUSES} pauses reached.`}
          </span>
        )}
      </div>

      <div className="pause-impact-notice">
        <div className="pause-impact-title">
          What a pause changes
        </div>
        <ul className="pause-impact-list">
          <li>Planned match times skip it on every shiaijo.</li>
          <li>Untick its box and matches go on through it. The calendar still shows it.</li>
          <li>Every estimate follows it: Add competition, each Overview and the competition calendar.</li>
          <li>A draw made after you save also follows it. A competition already drawn keeps the times its draw set.</li>
        </ul>
      </div>
    </div>
  );
}
