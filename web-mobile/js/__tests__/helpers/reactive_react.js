// Reactive React stub for vitest.
//
// The global React stub in vitest.setup.js is non-reactive (useState
// returns [val, vi.fn()]), which is fine for asserting a single static
// render but useless for tests that need to exercise interactions
// (typing into an input, ticking a timer, then re-reading the tree).
//
// makeReactive() returns a single-component reactive shim with:
//   - useState that re-renders on setter
//   - useEffect that runs once, with cleanup capture (fired on unmount)
//   - useRef, useMemo (eager), useLayoutEffect (no-op), memo
//   - render counter and unmount() for tests that care about either
//
// Install per test:
//   const runtime = makeReactive();
//   global.React = runtime.React;
//   vi.resetModules();
//   const { MyComponent } = await import('../my_component.jsx');
//   runtime.mount(MyComponent, { ...props });
//
// In afterEach, call runtime.unmount() to fire captured effect cleanups
// (intervals, listeners, etc.) before restoring the original React. If
// the component under test uses no effects with cleanups, the unmount
// call is a no-op but still safe to include.

export function makeReactive() {
  let hookSlots = [];
  let hookIndex = 0;
  let scheduledRender = null;
  let rootProps = null;
  let rootFactory = null;
  let effectCleanups = [];
  let renderCount = 0;

  function rerender() {
    hookIndex = 0;
    renderCount++;
    scheduledRender = rootFactory(rootProps);
    return scheduledRender;
  }

  const reactive = {
    // Mirror React's element shape: children are exposed on props.children
    // (single child vs array, matching how JSX-compiled components read
    // them) and also kept as a top-level alias so simple tree-traversal
    // helpers can walk node.children directly.
    createElement: (type, props, ...children) => {
      const merged = { ...(props || {}) };
      if (children.length > 0) {
        merged.children = children.length === 1 ? children[0] : children;
      }
      return { type, props: merged, children };
    },
    useState: (initial) => {
      const i = hookIndex++;
      if (hookSlots.length <= i) {
        hookSlots[i] = typeof initial === 'function' ? initial() : initial;
      }
      const setter = (v) => {
        hookSlots[i] = typeof v === 'function' ? v(hookSlots[i]) : v;
        rerender();
      };
      return [hookSlots[i], setter];
    },
    useEffect: (effect, deps) => {
      const i = hookIndex++;
      if (hookSlots.length <= i) {
        hookSlots[i] = deps;
        const cleanup = effect();
        if (typeof cleanup === 'function') {
          effectCleanups.push(cleanup);
        }
      }
    },
    // useMemo runs eagerly without dependency tracking — the test runtime
    // intentionally simplifies hooks for render isolation. This means tests
    // can't catch "tick missing from deps" regressions; that contract is
    // enforced by code review instead.
    useMemo: (fn) => fn(),
    useRef: (initial) => {
      const i = hookIndex++;
      if (hookSlots.length <= i) {
        hookSlots[i] = { current: initial };
      }
      return hookSlots[i];
    },
    useLayoutEffect: () => {},
    memo: (c) => c,
  };

  return {
    React: reactive,
    mount: (factory, props) => {
      hookSlots = [];
      hookIndex = 0;
      rootFactory = factory;
      rootProps = props;
      effectCleanups = [];
      renderCount = 0;
      return rerender();
    },
    unmount: () => {
      effectCleanups.forEach((c) => c());
      effectCleanups = [];
    },
    currentTree: () => scheduledRender,
    renderCount: () => renderCount,
  };
}
