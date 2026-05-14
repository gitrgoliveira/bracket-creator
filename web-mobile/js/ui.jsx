// Shared UI primitives used by both admin and viewer modules.

function StatusBadge({ status, showLiveDot }) {
  const map = {
    setup: ["badge--setup", "Pending"],
    pools: ["badge--pools", "Pools"],
    playoffs: ["badge--playoffs", "Playoffs"],
    completed: ["badge--completed", "Completed"],
  };
  const [cls, label] = map[status] || ["badge--setup", status];
  const showLive = showLiveDot && (status === "pools" || status === "playoffs");
  return (
    <span className={`badge ${cls}`}>
      {showLive && <span className="dot dot--live" style={{ marginRight: 4 }}></span>}
      {label}
    </span>
  );
}

function formatDate(d) {
  if (!d) return "Date TBA";
  let iso = d;
  // If it's DD-MM-YYYY or DD/MM/YYYY, convert to DD-MM-YYYY for the Date constructor
  const match = d.match(/^(\d{1,2})[-/](\d{1,2})[-/](\d{4})$/);
  if (match) {
    iso = `${match[3]}-${match[2].padStart(2, '0')}-${match[1].padStart(2, '0')}`;
  }
  const date = new Date(iso + "T00:00");
  if (isNaN(date.getTime())) return "Date TBA";
  return date.toLocaleDateString("en-GB", { day: "numeric", month: "short", year: "numeric" });
}

function Toast({ message, type, onClose }) {
  const [visible, setVisible] = React.useState(true);
  React.useEffect(() => {
    const t1 = setTimeout(() => setVisible(false), 2700);
    const t2 = setTimeout(onClose, 3000);
    return () => { clearTimeout(t1); clearTimeout(t2); };
  }, [onClose]);

  return (
    <div className={`toast toast--${type || 'info'} ${visible ? 'is-visible' : ''}`}>
      <div className="toast__icon">{type === 'error' ? '⚠️' : '✅'}</div>
      <div className="toast__msg">{message}</div>
    </div>
  );
}

// StableInput solves the character duplication issue by using a local state
// that only syncs with the parent onBlur or after a debounce, while still
// being "controlled" by receiving props.
//
// For type="number", a cleared input lands as NaN in local state (NOT 0
// as `+""` would produce) so the parent's onChange receives NaN
// explicitly. The render layer maps NaN-or-non-finite values back to ""
// at the value prop so React doesn't warn about "Received NaN for the
// value attribute" and the cleared input stays visually empty. Mirrors
// the decideNumericUpdate pattern used by the AdminSettings team/pool
// inputs at admin_competition.jsx.
function StableInput({ value, onChange, type, autoSelect = true, ...props }) {
  const [local, setLocal] = React.useState(value);
  const timer = React.useRef(null);
  const composing = React.useRef(false);

  // Sync local state when prop changes from outside (e.g. SSE)
  // Only sync if the user is NOT currently focused/composing.
  React.useEffect(() => {
    if (!composing.current && value !== local) setLocal(value);
  }, [value]);

  // Cancel the 200ms debounce on unmount so the timer can't fire
  // onChange(val) (which is the parent's setState) after teardown.
  // Pre-existing in this component before the PR but fits the same
  // teardown-race theme as the admin-side mountedRef sweep — fixing
  // here while the file is open for the NaN-display changes.
  React.useEffect(() => () => clearTimeout(timer.current), []);

  const handleChange = (e) => {
    const raw = e.target.value;
    // For number inputs: empty string → NaN, so a cleared input doesn't
    // collapse to 0 via `+""`. Non-empty strings still parse via unary +
    // (so "2.5" stays 2.5, "abc" becomes NaN — same as before for those).
    const val = type === 'number' ? (raw === "" ? NaN : +raw) : raw;
    setLocal(val);

    // Immediate local update, debounced parent update to avoid race conditions
    // during typing if the parent re-renders the whole tree.
    clearTimeout(timer.current);
    timer.current = setTimeout(() => onChange(val), 200);
  };

  const handleBlur = (e) => {
    composing.current = false;
    clearTimeout(timer.current);
    onChange(local);
    if (props.onBlur) props.onBlur(e);
  };

  const handleFocus = (e) => {
    composing.current = true;
    if (autoSelect) e.target.select();
    if (props.onFocus) props.onFocus(e);
  };

  // Render NaN / non-finite numeric local state as "" so React doesn't
  // warn ("Received NaN for the value attribute") and the input stays
  // visually empty after the user clears it. Non-number types pass
  // through unchanged.
  const displayValue = type === 'number' && !Number.isFinite(local) ? "" : local;

  return (
    <input
      {...props}
      type={type}
      value={displayValue}
      onChange={handleChange}
      onBlur={handleBlur}
      onFocus={handleFocus}
    />
  );
}

function pluralize(count, singular, plural) {
  return count === 1 ? `${count} ${singular}` : `${count} ${plural || singular + 's'}`;
}

// Hook: register an Escape key listener that always calls the latest onClose
// without re-registering on every render (listener registered once, ref kept fresh).
function useEscapeToClose(onClose) {
  const { useRef, useEffect } = React;
  const cbRef = useRef(onClose);
  useEffect(() => { cbRef.current = onClose; }, [onClose]);
  useEffect(() => {
    const onKey = (e) => {
      if (e.key === "Escape" && typeof cbRef.current === "function") {
        e.preventDefault();
        cbRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

// Returns true when el is a text-entry element (blocks navigation shortcuts
// to avoid clobbering cursor movement in inputs).
function isTextEntry(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || !!el.isContentEditable;
}

// Returns true when el is any interactive element (blocks scoring shortcuts
// so native keyboard activation of buttons/links still works).
function isInteractiveTarget(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || tag === "button" || tag === "a" || !!el.isContentEditable;
}

export { StatusBadge, formatDate, Toast, StableInput, pluralize, useEscapeToClose, isTextEntry, isInteractiveTarget };

if (typeof window !== "undefined") {
  window.StatusBadge = StatusBadge;
  window.formatDate = formatDate;
  window.Toast = Toast;
  window.StableInput = StableInput;
  window.pluralize = pluralize;
  window.useEscapeToClose = useEscapeToClose;
  window.isTextEntry = isTextEntry;
  window.isInteractiveTarget = isInteractiveTarget;
}

