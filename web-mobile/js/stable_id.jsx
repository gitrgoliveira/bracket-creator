// stable_id.jsx: the ONE owner of a per-component id for wiring a control to
// its label or description (htmlFor, aria-labelledby, aria-describedby).
//
// An import-only leaf: ES-imported by glossary.jsx and competition_fields.jsx
// (and any form that renders more than once on a page), never script-tagged in
// index.html, so the browser keeps one module instance.
//
// useId is React 18+; preact/compat aliases it to a deterministic-per-mount
// counter, so the wiring stays valid across re-renders. React is read at CALL
// time rather than destructured at module load, so a test that installs its
// React stub after the import still gets the right branch. The fallback (a
// random suffix fixed on the first render) covers the unit suite's React stub,
// which has no useId.
export function useStableId(prefix) {
  if (typeof React.useId === 'function') {
    const id = React.useId();
    return `${prefix}-${id}`;
  }
  const ref = React.useRef(null);
  if (!ref.current) {
    ref.current = `${prefix}-${Math.random().toString(36).slice(2, 9)}`;
  }
  return ref.current;
}
