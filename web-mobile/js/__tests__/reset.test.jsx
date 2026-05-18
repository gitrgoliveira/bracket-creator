import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// reset.jsx ships:
//   - ResetPasswordForm: the /reset SPA page. Renders either the
//     password-set form (file mode) or the operator-disabled message
//     (locked mode). On submit it POSTs to /api/tournament/reset,
//     persists bc_password/bc_authed to localStorage, and calls
//     onSuccess so app.jsx can pivot the user into admin mode.
//
// The global React stub in vitest.setup.js is non-reactive (useState
// returns [val, vi.fn()]) which is fine for asserting the static
// render tree but useless for exercising the submit handler — that's
// where the new password is sent, errors mapped, localStorage
// updated, and onSuccess invoked. To cover submit-time behavior we
// install a small per-test reactive React shim before importing
// reset.jsx and restore the original stub afterwards.
//
// The shim is deliberately minimal (single-component, no diffing,
// no batching) — just enough to make useState/useEffect/useRef
// reactive so the onSubmit closure sees current input values.

const realReact = global.React;

// reactiveReact: a single-component stand-in that mimics the parts of
// React the component actually uses. Each render call creates a fresh
// per-component "instance" record holding hook slots; subsequent calls
// reuse the slots so setState mutations are observable across renders.
function makeReactive() {
  let hookSlots = [];
  let hookIndex = 0;
  let scheduledRender = null;
  let rootProps = null;
  let rootFactory = null;

  function rerender() {
    hookIndex = 0;
    scheduledRender = rootFactory(rootProps);
    return scheduledRender;
  }

  const reactive = {
    createElement: (type, props, ...children) => ({ type, props, children }),
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
    useEffect: () => {}, // ignore effects in tests — submit handler doesn't depend on them
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
      return rerender();
    },
    currentTree: () => scheduledRender,
  };
}

function findInTree(node, predicate) {
  if (!node || typeof node !== 'object') return null;
  if (predicate(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) {
    const found = findInTree(k, predicate);
    if (found) return found;
  }
  return null;
}

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

describe('ResetPasswordForm', () => {
  let ResetPasswordForm;
  let runtime;
  let mockStorage;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    // Force a fresh import so reset.jsx binds to our reactive React.
    vi.resetModules();
    ({ ResetPasswordForm } = await import('../reset.jsx'));

    global.window = global.window || {};
    global.window.API = { resetPassword: vi.fn(async () => true) };
    global.window.location = { pathname: '/reset', origin: 'http://test' };

    mockStorage = (() => {
      const store = {};
      return {
        getItem: (k) => (k in store ? store[k] : null),
        setItem: vi.fn((k, v) => { store[k] = String(v); }),
        removeItem: vi.fn((k) => { delete store[k]; }),
        clear: () => { for (const k in store) delete store[k]; },
      };
    })();
    global.localStorage = mockStorage;
  });

  afterEach(() => {
    global.React = realReact;
    vi.resetModules();
  });

  describe('locked-mode branch (resetEnabled: false)', () => {
    it('renders the operator-disabled message and does NOT render the form', () => {
      const tree = runtime.mount(ResetPasswordForm, {
        authConfig: { mode: 'locked', resetEnabled: false },
        onBack: vi.fn(),
        onSuccess: vi.fn(),
      });
      const text = collectText(tree);
      expect(text).toContain('Password reset disabled');
      expect(text).toContain('environment variable');
      expect(text).not.toContain('Reset tournament password');
    });

    it('renders a Back button that fires onBack', () => {
      const onBack = vi.fn();
      const tree = runtime.mount(ResetPasswordForm, {
        authConfig: { mode: 'locked', resetEnabled: false },
        onBack,
        onSuccess: vi.fn(),
      });
      const backBtn = findInTree(tree, (n) => n.type === 'button' && collectText(n) === '← Back');
      expect(backBtn).toBeTruthy();
      backBtn.props.onClick();
      expect(onBack).toHaveBeenCalledOnce();
    });
  });

  describe('file-mode branch (resetEnabled: true)', () => {
    it('renders the form with two password inputs and a submit button', () => {
      const tree = runtime.mount(ResetPasswordForm, {
        authConfig: { mode: 'file', resetEnabled: true },
        onBack: vi.fn(),
        onSuccess: vi.fn(),
      });
      const text = collectText(tree);
      expect(text).toContain('Reset tournament password');
      const pwInputs = [];
      findInTree(tree, (n) => {
        if (n.type === 'input' && n.props?.type === 'password') pwInputs.push(n);
        return false;
      });
      expect(pwInputs).toHaveLength(2);
      const submit = findInTree(tree, (n) => n.type === 'button' && n.props?.type === 'submit');
      expect(submit).toBeTruthy();
    });

    it('renders even when authConfig is undefined (fail-open default)', () => {
      const tree = runtime.mount(ResetPasswordForm, { onBack: vi.fn(), onSuccess: vi.fn() });
      const text = collectText(tree);
      expect(text).toContain('Reset tournament password');
    });
  });

  // --- Submit-flow tests via the reactive shim ---
  //
  // The shim makes useState reactive, so input onChange handlers
  // observably update the hooks; re-rendering picks up the new
  // values. The form's onSubmit closure then reads the latest
  // state.

  describe('submit flow', () => {
    function setupForm({ onSuccess = vi.fn(), originatorId } = {}) {
      runtime.mount(ResetPasswordForm, {
        authConfig: { mode: 'file', resetEnabled: true },
        onBack: vi.fn(),
        onSuccess,
        originatorId,
      });
      // Re-query the tree on every interaction. Each setState rerenders
      // the component, producing fresh onChange/onSubmit closures bound
      // to the latest state slots — stale references would invoke
      // setters but read stale closure values, so the submit handler
      // would see pw="" even after typing.
      const inputs = (tree) => {
        const out = [];
        findInTree(tree, (n) => {
          if (n.type === 'input' && n.props?.type === 'password') out.push(n);
          return false;
        });
        return out;
      };
      const typeInto = (idx, value) => {
        const node = inputs(runtime.currentTree())[idx];
        node.props.onChange({ target: { value } });
      };
      const submit = async () => {
        const form = findInTree(runtime.currentTree(), (n) => n.type === 'form');
        await form.props.onSubmit({ preventDefault: () => {} });
      };
      return { typeInto, submit, onSuccess };
    }

    it('rejects empty password without calling the API', async () => {
      window.API.resetPassword = vi.fn();
      const { submit } = setupForm();
      await submit();
      expect(window.API.resetPassword).not.toHaveBeenCalled();
      const tree = runtime.currentTree();
      expect(collectText(tree)).toContain('Enter a new password');
    });

    it('rejects mismatched confirmation without calling the API', async () => {
      window.API.resetPassword = vi.fn();
      const { typeInto, submit } = setupForm();
      typeInto(0, 'abc');
      typeInto(1, 'xyz');
      await submit();
      expect(window.API.resetPassword).not.toHaveBeenCalled();
      expect(collectText(runtime.currentTree())).toContain('Passwords do not match');
    });

    it('POSTs the new password and originatorId, then auto-logs in via localStorage + onSuccess', async () => {
      window.API.resetPassword = vi.fn(async () => true);
      const { typeInto, submit, onSuccess } = setupForm({ originatorId: 'client-zzz' });
      typeInto(0, 'newpw');
      typeInto(1, 'newpw');
      await submit();

      expect(window.API.resetPassword).toHaveBeenCalledWith('newpw', 'client-zzz');
      expect(mockStorage.setItem).toHaveBeenCalledWith('bc_password', 'newpw');
      expect(mockStorage.setItem).toHaveBeenCalledWith('bc_authed', 'true');
      expect(onSuccess).toHaveBeenCalledWith('newpw');
    });

    it('maps a 404 response to the operator-disabled message', async () => {
      const err404 = new Error('reset disabled');
      err404.status = 404;
      window.API.resetPassword = vi.fn(async () => { throw err404; });
      const onSuccess = vi.fn();
      const { typeInto, submit } = setupForm({ onSuccess });
      typeInto(0, 'newpw');
      typeInto(1, 'newpw');
      await submit();

      expect(collectText(runtime.currentTree())).toContain('Password reset has been disabled by the operator.');
      expect(onSuccess).not.toHaveBeenCalled();
      // localStorage must NOT have been written — the reset failed.
      expect(mockStorage.setItem).not.toHaveBeenCalledWith('bc_password', expect.anything());
    });

    it('surfaces a generic error from the server on non-404 failures', async () => {
      window.API.resetPassword = vi.fn(async () => { throw new Error('store unavailable'); });
      const { typeInto, submit } = setupForm();
      typeInto(0, 'newpw');
      typeInto(1, 'newpw');
      await submit();
      expect(collectText(runtime.currentTree())).toContain('store unavailable');
    });
  });
});
