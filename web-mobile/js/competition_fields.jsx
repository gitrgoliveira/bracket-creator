// competition_fields.jsx: the RENDERED controls the two competition-config
// surfaces share -- admin_setup.jsx's create form and
// admin_competition_settings.jsx's settings tab.
//
// competition_shape.jsx (this module's only project import) owns the RULES:
// option lists, copy, visibility predicates, normalizers. It is a pure leaf
// with no React, deliberately, so it can be imported anywhere including
// tests. This module is the other half of the same job: the MARKUP those
// rules render into.
//
// Why that half needed a home too. Sharing the constants stopped the two
// screens disagreeing about what a control is CALLED, and it did nothing
// about what the control DOES, because each screen still hand-wrote its own
// `<div className="field"> <label> <div className="radio-group"> {OPTIONS.map(...)}`
// around them. Four divergences were found in that hand-written half, all of
// them invisible to a guard that compares imports:
//
//   - The team-match-format pills used DIFFERENT active-pill predicates. The
//     settings screen deliberately treats any value that is not exactly
//     "kachinuki" as Regular (a stored legacy "" reads as fixed, mirroring
//     ValidateTeamMatchType); the create form used plain equality and
//     carried a comment explaining that plain equality was "safe here". It
//     was, for its own local state -- but two spellings of one rule is how
//     the next reader picks the wrong one. The asymmetric reading is correct
//     on both and is what PillGroup now does for every caller that asks for
//     it.
//   - Ticking "Naginata competition" on the create form also cleared
//     "Award two joint 3rd places" (naginata awards a single 3rd). The
//     settings screen had no such coupling, so the same tick left a kendo
//     league config on a naginata competition. The rule now lives in
//     competition_shape.jsx as twoThirdPlacesForNaginata and both screens
//     call it.
//   - Checkbox hints were spaced by `style={{ marginTop: 4 }}` on create and
//     by a flex wrapper with `gap: 4` on settings, and the checkboxes
//     themselves sat 14px apart on create (`.field`) against 8px on settings
//     (a flex wrapper with `gap: 8`). Same controls, two rhythms.
//   - A hint was rendered unconditionally on one screen and conditionally on
//     the other, so an empty hint left a stray empty div behind on one of
//     them.
//
// None of those is a bug a reviewer would spot in a diff, because each
// screen is internally consistent. They are only visible side by side, which
// is exactly the comparison nobody makes.
//
// Loaded the same way duration.jsx and competition_shape.jsx are: ES-imported
// by both screens, never script-tagged in index.html, so the browser keeps
// ONE module instance (see the double-module-eval trap in
// web-mobile/js/README-style notes on the viewer split). `React` is the
// global the surrounding admin bundle already relies on.

// PillGroup: label + radio-pill row + optional hint, the shape every
// option-list control on both screens uses.
//
// `isActive` is a predicate over the option VALUE rather than a bare
// comparison against `value`, because three of the callers need something
// other than equality and each of those readings is a rule with a single
// owner in competition_shape.jsx: resolvePoolFormat (a stored "" means
// "full"), resolvePoolSizeMode (a stored "" means minimum, which is what the
// engine does with it), leagueTiebreakActive (a stored 0 means Top 3) and
// teamMatchTypeActive (anything that is not exactly "kachinuki" is Regular).
// Defaulting it to equality keeps the simple callers simple without letting
// the awkward ones re-spell their rule inline.
//
// `hint` renders only when non-empty: an empty hint used to leave a stray
// `<div className="field__hint">` in the tree, which carries the class's own
// top margin and pushed the next control down by a few pixels on whichever
// screen rendered it unconditionally.
export function PillGroup({ label, options, value, isActive, onChange, disabled, hint }) {
  const active = isActive || ((v) => v === value);
  return (
    <div className="field">
      <label className="field__label">{label}</label>
      <div className="radio-group">
        {options.map((o) => (
          <button
            key={o.value}
            className={`radio-pill ${active(o.value) ? "is-active" : ""}`}
            type="button"
            onClick={() => onChange(o.value)}
            disabled={!!disabled}
          >{o.label}</button>
        ))}
      </div>
      {hint ? <div className="field__hint">{hint}</div> : null}
    </div>
  );
}

// CheckboxField: checkbox + label + optional hint.
//
// `.field--checkbox` carries the label-to-hint gap that create spelled as an
// inline `marginTop: 4` and settings spelled as a flex wrapper, so the two
// screens can no longer drift on it, and it inherits `.field`'s own
// margin-bottom so a checkbox sits the same distance from its neighbour as
// every other control on the form.
//
// onChange receives the BOOLEAN, not the event: every caller wanted
// `e.target.checked` and one of them (naginata) needs to derive a second
// field from it, which reads better against a value than against an event.
export function CheckboxField({ label, checked, onChange, disabled, hint }) {
  return (
    <div className="field field--checkbox">
      <label className="checkbox">
        <input
          type="checkbox"
          checked={!!checked}
          disabled={!!disabled}
          onChange={(e) => onChange(e.target.checked)}
        />
        {" "}{label}
      </label>
      {hint ? <div className="field__hint field__hint--checkbox">{hint}</div> : null}
    </div>
  );
}

// FieldLabel: the `<label className="field__label">` both screens render,
// including the greyed "(optional)" suffix that was two identical inline
// `<span style={{ fontWeight: 400, color: "var(--ink-3)" }}>` copies -- one
// per screen, on the same field.
function FieldLabel({ children, optional }) {
  return (
    <label className="field__label">
      {children}
      {optional ? <span className="field__label-optional"> (optional)</span> : null}
    </label>
  );
}

// NumberField: label + number input + optional hint and inline error.
//
// `onChange` receives the RAW input string, not a parsed number, because the
// two screens parse it differently on purpose: the create form writes
// straight to component state, the settings screen also has to mark the field
// edited and the form dirty. Both funnel through decideNumericUpdate with the
// same MIN_* constant, so the RULE is already shared; what was not shared was
// this markup, and it had drifted three ways -- the create form set
// `step="1"` on the pool inputs where settings did not (so the spinner moved
// by different amounts for the same field), the create form gave "Team size"
// a hint naming the five kendo positions where settings gave it none, and the
// "Set to 1 by the knockout qualifiers setting below." coupling hint was a
// verbatim literal in both files.
//
// `width` is a number of pixels rather than a style object: both screens
// capped the Swiss-rounds and number-prefix inputs, each with its own inline
// `style={{ maxWidth: ... }}`.
export function NumberField({
  label, optional, value, onChange, min, max, step = "1",
  disabled, hint, error, width,
}) {
  return (
    <div className="field">
      <FieldLabel optional={optional}>{label}</FieldLabel>
      <input
        className="input"
        type="number"
        min={min}
        max={max}
        step={step}
        value={Number.isFinite(value) ? value : ""}
        onChange={(e) => onChange(e.target.value)}
        disabled={!!disabled}
        style={width ? { maxWidth: width } : undefined}
      />
      {error ? <window.FieldError>{error}</window.FieldError> : null}
      {hint ? <div className="field__hint">{hint}</div> : null}
    </div>
  );
}

// TextField: the same shape for a plain text input. One caller per screen
// today (the player-number prefix), and those two were byte-identical apart
// from their bindings.
export function TextField({
  label, optional, value, onChange, placeholder, maxLength, disabled, hint, width,
}) {
  return (
    <div className="field">
      <FieldLabel optional={optional}>{label}</FieldLabel>
      <input
        className="input"
        placeholder={placeholder}
        maxLength={maxLength}
        value={value || ""}
        onChange={(e) => onChange(e.target.value)}
        disabled={!!disabled}
        style={width ? { maxWidth: width } : undefined}
      />
      {hint ? <div className="field__hint">{hint}</div> : null}
    </div>
  );
}
