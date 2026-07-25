// duration.jsx: shared minutes+seconds match-duration input (mp-m5kf).
// Operators enter match durations with sub-minute granularity (e.g. 2m30s);
// the control is bound to a single integer-seconds value so callers persist
// one canonical field (poolMatchDurationSeconds / playoffMatchDurationSeconds
// on the Go side). Two number sub-fields (min + sec) avoid mm:ss parse
// ambiguity and step cleanly on mobile.

// DURATION_MAX_MINUTES caps the minutes sub-field at 24 hours, matching
// maxMatchDurationMinutes in internal/mobileapp/handlers_competition.go so the
// client never emits a value the server would reject with a 400.
const DURATION_MAX_MINUTES = 24 * 60; // 1440

// DurationInput renders a minutes field and a seconds field bound to one
// integer-seconds value.
//   props.seconds        current value in seconds (number; NaN/undefined = unset)
//   props.onChange(sec)  called with the new integer seconds, or NaN when both
//                        sub-fields are cleared (caller treats NaN as "default")
//   props.disabled       disables both inputs
//   props.placeholderMin placeholder text for the minutes field (e.g. "3")
//   props.style          extra style merged onto the wrapping row
export function DurationInput({ seconds, onChange, disabled, placeholderMin, style }) {
  const has = Number.isFinite(seconds) && seconds > 0;
  const total = has ? Math.round(seconds) : null;
  const mmVal = total != null ? Math.floor(total / 60) : "";
  const ssVal = total != null ? total % 60 : "";

  // Recompute the combined seconds from the two sub-fields. Both blank emits
  // NaN so the caller falls back to the scheduler default; otherwise a blank
  // sub-field counts as 0, the minutes are clamped to [0, DURATION_MAX_MINUTES]
  // and the seconds component to [0, 59].
  const emit = (mRaw, sRaw) => {
    const mStr = String(mRaw).trim();
    const sStr = String(sRaw).trim();
    if (mStr === "" && sStr === "") { onChange(NaN); return; }
    let m = mStr === "" ? 0 : Math.max(0, Math.floor(Number(mStr) || 0));
    if (m > DURATION_MAX_MINUTES) m = DURATION_MAX_MINUTES;
    // Math.floor(Number(...) || 0) is always finite; only the range needs clamping.
    let s = sStr === "" ? 0 : Math.floor(Number(sStr) || 0);
    if (s < 0) s = 0;
    if (s > 59) s = 59;
    onChange(m * 60 + s);
  };

  return (
    <div className="row" style={{ gap: 6, alignItems: "center", ...(style || {}) }}>
      <input
        className="input"
        type="number"
        min="0"
        max={DURATION_MAX_MINUTES}
        step="1"
        style={{ width: 68 }}
        value={mmVal}
        placeholder={placeholderMin}
        disabled={disabled}
        onChange={(e) => emit(e.target.value, ssVal)}
        aria-label="minutes"
      />
      <span className="field__hint" style={{ margin: 0 }}>min</span>
      <input
        className="input"
        type="number"
        min="0"
        max="59"
        step="1"
        style={{ width: 68 }}
        value={ssVal}
        disabled={disabled}
        onChange={(e) => emit(mmVal, e.target.value)}
        aria-label="seconds"
      />
      <span className="field__hint" style={{ margin: 0 }}>sec</span>
    </div>
  );
}
