// lineup_rename.jsx: the shared "rename a team member" editing form (a text
// input plus Save/Cancel) used by both lineup-editing surfaces --
// admin_lineup.jsx's Lineups page and admin_schedule_lineup.jsx's per-match
// panel (bc-rvfx). The two had drifted into two copies of the same three
// elements and the same Enter/Escape wiring, keyed by whichever identifier
// each host's own "who is being renamed" state happens to use.
//
// Deliberately a PLAIN FUNCTION, called directly and spliced into the host's
// own JSX (`{renameMemberFields({...})}`), never a component reached via
// `<RenameMemberFields/>`. Both hosts' own unit tests walk their rendered
// tree WITHOUT expanding a nested custom component (see e.g.
// admin_lineup_form.test.jsx's own header comment: the test runtime "does
// NOT recurse into child component bodies"), so a JSX-invoked component
// would hide its input/buttons from those tests' host-element queries. A
// plain function call produces the identical element tree a fully-inlined
// copy would, so no test needed to change for this extraction.
//
// The two hosts key "which item is being renamed" DIFFERENTLY -- a squad
// member id (admin_lineup.jsx) vs. a lineup position key
// (admin_schedule_lineup.jsx) -- and each host's own commit/cancel functions
// already close over whichever key is currently active. So this function
// takes no key at all, only the value being typed and the callbacks to
// invoke; the key stays entirely inside the host, exactly as it always was.
//
// `stacked` selects between the two hosts' layouts, which differ for a
// documented reason: admin_schedule_lineup.jsx's per-position row is a fixed
// two-column layout that must not grow WIDTH, so its Save/Cancel row sits
// BELOW the input rather than beside it (see the comment on that branch
// below). `disabled` is the input's own disabled attribute, kept separate
// from `busy` (which always governs the two buttons and the Save label)
// because admin_schedule_lineup.jsx's input was never disabled while a
// rename was in flight -- a pre-existing difference between the two hosts,
// preserved here rather than smoothed away.
export function renameMemberFields({
  value,
  onChange,
  onCommit,
  onCancel,
  busy,
  ariaLabel,
  inputStyle,
  disabled = false,
  autoFocus = false,
  stacked = false,
}) {
  const input = (
    <input
      className="input"
      style={inputStyle}
      value={value}
      autoFocus={autoFocus}
      aria-label={ariaLabel}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter") { e.preventDefault(); onCommit(); }
        else if (e.key === "Escape") { e.preventDefault(); onCancel(); }
      }}
    />
  );
  const saveButton = (
    <button type="button" className="btn btn--sm" onClick={onCommit} disabled={busy || !value.trim()}>
      {busy ? "Saving…" : "Save"}
    </button>
  );
  const cancelButton = (
    <button type="button" className="btn btn--ghost btn--sm" onClick={onCancel} disabled={busy}>Cancel</button>
  );
  if (stacked) {
    // Input on top, Save and Cancel below: adds height, never width (a
    // two-column modal keeps its columns this way -- beside two buttons the
    // input pushed the other side out and brought in a scrollbar).
    return (
      <div style={{ display: "flex", flexDirection: "column", gap: 4, flex: 1, minWidth: 0 }}>
        {input}
        <div style={{ display: "flex", gap: 6 }}>{saveButton}{cancelButton}</div>
      </div>
    );
  }
  return (<>{input}{saveButton}{cancelButton}</>);
}
