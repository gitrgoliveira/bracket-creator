// latest_ref.jsx: the ONE owner of "a ref that holds the latest value", for a
// handler, an async continuation or an effect that must read what the component
// last rendered rather than what its closure captured.
//
// The ref is written in a layout effect, never during render (the React
// Compiler's refs rule): every commit brings it up to the value that render
// had, before any passive effect runs and before the browser takes the next
// event, so every reader outside render sees it. A layout effect declared
// AFTER the call in the same component also sees it, since layout effects run
// in declaration order. A render reads the value itself, never the ref.
//
// For handlers and async continuations: oxlint's exhaustive-deps takes only a
// `useRef` call's result for a stable value, so an effect with a dependency list
// that reads this ref is told to list it (or its `.current`), and the compiler
// rules lose sight of the ref inside that effect. Such a reader keeps its own
// `useRef` plus layout effect (lineup_draft.jsx's `latest`, `live`, `squadRef`).
//
// A leaf with no imports: every consumer ES-imports it directly. React is read
// at CALL time rather than destructured at module load, so a test that installs
// its React stub after the import still gets it.
export function useLatestRef(value) {
  const r = React.useRef(value);
  React.useLayoutEffect(() => { r.current = value; });
  return r;
}
