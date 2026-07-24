// duration.jsx: shared minutes+seconds match-duration input (mp-m5kf).
// Operators enter match durations with sub-minute granularity (e.g. 2m30s);
// the control is bound to a single integer-seconds value so callers persist
// one canonical field (poolMatchDurationSeconds / playoffMatchDurationSeconds
// on the Go side). Two number sub-fields (min + sec) avoid mm:ss parse
// ambiguity and step cleanly on mobile.

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
  // sub-field counts as 0 and the seconds component is clamped to [0, 59].
  const emit = (mRaw, sRaw) => {
    const mStr = String(mRaw).trim();
    const sStr = String(sRaw).trim();
    if (mStr === "" && sStr === "") { onChange(NaN); return; }
    const m = mStr === "" ? 0 : Math.max(0, Math.floor(Number(mStr) || 0));
    let s = sStr === "" ? 0 : Math.floor(Number(sStr) || 0);
    if (!Number.isFinite(s) || s < 0) s = 0;
    if (s > 59) s = 59;
    onChange(m * 60 + s);
  };

  return (
    <div className="row" style={{ gap: 6, alignItems: "center", ...(style || {}) }}>
      <input
        className="input"
        type="number"
        min="0"
        step="1"
        style={{ width: 68 }}
        value={mmVal === "" ? "" : mmVal}
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
        value={ssVal === "" ? "" : ssVal}
        disabled={disabled}
        onChange={(e) => emit(mmVal, e.target.value)}
        aria-label="seconds"
      />
      <span className="field__hint" style={{ margin: 0 }}>sec</span>
    </div>
  );
}
