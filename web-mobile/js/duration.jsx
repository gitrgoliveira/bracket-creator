// duration.jsx: shared masked m:ss match-duration input (mp-m5kf).
// Operators enter match durations with sub-minute granularity (e.g. 2:30);
// the control is bound to a single integer-seconds value so callers persist
// one canonical field (poolMatchDurationSeconds / playoffMatchDurationSeconds
// on the Go side).
//
// This is ONE masked text field, not a minutes box plus a seconds box. The
// two-field version shipped first and was wrong twice over: it rendered as a
// 2x2 grid (it reused `.row`, which is a two-column CSS grid, so each unit
// caption stranded ~160px from its input and collapsed to four stacked strips
// under 720px), and its `(min:sec)` label taught a colon the control silently
// swallowed, so "2:30" saved as 230 MINUTES with no warning. One field that
// actually accepts the advertised format removes both failure modes and halves
// the number of touch targets that have to clear 44px on a tablet.

// Accepted band: 1:00 to 60:00. Match duration drives auto-scheduling for the
// whole event, so values outside it are rejected outright rather than silently
// clamped: a fat-fingered 0:03 would otherwise persist to config.md and drive
// the day's timetable. Mirrored server-side by MinMatchDurationSeconds /
// MaxMatchDurationSeconds in internal/state/models.go, which both validates
// writes and clamps migrated legacy durations. A client-only block is not a
// block; keep these two in step.
export const MIN_DURATION_SECONDS = 60;
export const MAX_DURATION_SECONDS = 60 * 60; // 3600

// Scheduler fallback when no duration is set. Mirrors defaultPerMatchClockSeconds
// in internal/engine/scheduler_slots.go. The blank-state note below states this
// resolved value outright, so callers do NOT need to repeat "leave blank for the
// default" in their field hint.
export const DEFAULT_DURATION_SECONDS = 180;

// formatDuration renders canonical "m:ss". Returns "" for unset/zero so the
// field shows its placeholder and the caller falls back to the default.
export function formatDuration(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "";
  const total = Math.round(seconds);
  return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, "0")}`;
}

const BARE_MINUTES = /^\d{1,4}$/;
const MINUTES_SECONDS = /^(\d{0,4}):(\d{1,2})$/;

// parseDuration turns operator input into { seconds, error }.
//   ""      -> { NaN,  null }   blank means "use the default"
//   "3"     -> { 180,  null }   a bare number is WHOLE MINUTES, preserving the
//                               habit from the minutes-only field this replaced
//   "2:30"  -> { 150,  null }
//   "2:3"   -> { 123,  null }   1-digit seconds parse so typing toward "2:30"
//                               never flashes an error mid-keystroke; the field
//                               normalizes the display to "2:03" on blur
//   ":45"   -> { 45,   null }
//   "2:60"  -> { NaN,  "Seconds must be 00-59." }
//   "0:03"  -> { NaN,  "Minimum is 0:30." }
// error is non-null exactly when the value must not be committed.
export function parseDuration(raw) {
  const text = String(raw == null ? "" : raw).trim();
  if (text === "") return { seconds: NaN, error: null };

  let total;
  if (BARE_MINUTES.test(text)) {
    total = Number(text) * 60;
  } else {
    const parts = MINUTES_SECONDS.exec(text);
    if (!parts) return { seconds: NaN, error: "Use m:ss, for example 2:30." };
    const secs = Number(parts[2]);
    if (secs > 59) return { seconds: NaN, error: "Seconds must be 00-59." };
    total = Number(parts[1] || 0) * 60 + secs;
  }

  if (total < MIN_DURATION_SECONDS) {
    return { seconds: NaN, error: `Minimum is ${formatDuration(MIN_DURATION_SECONDS)}.` };
  }
  if (total > MAX_DURATION_SECONDS) {
    return { seconds: NaN, error: `Maximum is ${formatDuration(MAX_DURATION_SECONDS)}.` };
  }
  return { seconds: total, error: null };
}

// DurationInput renders one masked m:ss field bound to an integer-seconds value.
//   props.seconds       current value in seconds (number; NaN/undefined = unset)
//   props.onChange(sec) called with the new integer seconds, or NaN when the
//                       field is cleared (caller treats NaN as "use default").
//                       NOT called while the field is invalid, so the staged
//                       value never holds an out-of-band duration.
//   props.onValidity(e) called with the error string (or null) on every edit so
//                       the caller can gate its Save / Auto-schedule button
//   props.id            id for the field, so the caller's <label htmlFor> binds
//   props.label         accessible name when there is no visible <label>
//   props.describedBy   id of the caller's hint element
//   props.disabled      disables the field
export function DurationInput({ seconds, onChange, onValidity, id, label, describedBy, disabled }) {
  const [draft, setDraft] = React.useState(() => formatDuration(seconds));
  const [error, setError] = React.useState(null);
  // Last value this field emitted. The effect below resyncs the draft only when
  // the prop moves independently (an SSE push, a discarded edit), so a resync
  // never fights the operator mid-keystroke.
  const emitted = React.useRef(seconds);

  React.useEffect(() => {
    if (Object.is(seconds, emitted.current)) return;
    emitted.current = seconds;
    setDraft(formatDuration(seconds));
    setError(null);
    // Report the cleared validity too, not just the cleared visual state. The
    // caller keeps its own per-field error map to gate Save, and an invalid
    // draft never reaches onChange, so the field is not in editedFieldsRef and
    // an SSE push CAN move this prop out from under it. Clearing only setError
    // left Save disabled on an error message that was no longer displayed
    // anywhere, with nothing on screen to fix.
    if (onValidity) onValidity(null);
  }, [seconds]);

  const apply = (raw) => {
    setDraft(raw);
    const parsed = parseDuration(raw);
    setError(parsed.error);
    if (onValidity) onValidity(parsed.error);
    if (!parsed.error) {
      emitted.current = parsed.seconds;
      onChange(parsed.seconds);
    }
  };

  // Snap a valid draft to canonical "m:ss" on blur, so "2:3" reads back as
  // "2:03" and the operator sees exactly what was committed.
  const normalize = () => {
    const parsed = parseDuration(draft);
    if (!parsed.error) setDraft(formatDuration(parsed.seconds));
  };

  const errorId = id ? `${id}-error` : undefined;
  const noteId = id ? `${id}-note` : undefined;
  const usingDefault = draft.trim() === "" && !error;
  const describers = [describedBy, error ? errorId : null, usingDefault ? noteId : null].filter(Boolean);

  return (
    <div className="duration-input">
      <input
        id={id}
        className={`input duration-input__field${error ? " duration-input__field--invalid" : ""}`}
        type="text"
        inputMode="numeric"
        autoComplete="off"
        placeholder={formatDuration(DEFAULT_DURATION_SECONDS)}
        value={draft}
        disabled={disabled}
        aria-label={label}
        aria-invalid={error ? "true" : undefined}
        aria-describedby={describers.length ? describers.join(" ") : undefined}
        onChange={(e) => apply(e.target.value)}
        onBlur={normalize}
      />
      {error && <div className="duration-input__error" id={errorId} role="alert">{error}</div>}
      {usingDefault && (
        <div className="duration-input__note" id={noteId}>
          Using the default, {formatDuration(DEFAULT_DURATION_SECONDS)}.
        </div>
      )}
    </div>
  );
}
