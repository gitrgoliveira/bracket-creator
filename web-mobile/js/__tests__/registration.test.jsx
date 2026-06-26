import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

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

describe('RegistrationForm', () => {
  let RegistrationForm;
  let runtime;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ RegistrationForm } = await import('../registration.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
    delete global.fetch;
  });

  describe('loading state', () => {
    it('renders loading indicator before metadata resolves', () => {
      global.fetch = vi.fn(() => new Promise(() => {}));
      const initial = runtime.mount(RegistrationForm, { compId: 'test-comp' });
      expect(collectText(initial)).toContain('Loading');
    });
  });

  describe('metadata error (404 — not available)', () => {
    it('shows unavailable message when GET returns 404', async () => {
      global.fetch = vi.fn(() =>
        Promise.resolve({ status: 404, ok: false, json: () => Promise.resolve({}) })
      );
      runtime.mount(RegistrationForm, { compId: 'test-comp' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Registration unavailable');
        expect(text).toContain('not available');
      });
    });
  });

  describe('registration closed (competition past setup)', () => {
    it('shows closed message when status is not setup', async () => {
      global.fetch = vi.fn(() =>
        Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'c1', name: 'Open', status: 'pools', withZekkenName: false }),
        })
      );
      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('closed');
      });
    });
  });

  describe('form renders for open competition', () => {
    it('shows form fields when metadata loads successfully', async () => {
      global.fetch = vi.fn(() =>
        Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'c1', name: 'Open Men', status: 'setup', withZekkenName: false }),
        })
      );
      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Register');
        expect(text).toContain('Full name');
        expect(text).toContain('Dojo');
        expect(text).toContain('Open Men');
      });
    });

    it('shows zekken field when withZekkenName is true', async () => {
      global.fetch = vi.fn(() =>
        Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'c1', name: 'Naginata', status: 'setup', withZekkenName: true }),
        })
      );
      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Display name');
        expect(text).toContain('zekken');
      });
    });
  });

  describe('successful registration', () => {
    it('shows success message after POST 200', async () => {
      global.fetch = vi.fn((url, opts) => {
        if (!opts || opts.method !== 'POST') {
          return Promise.resolve({
            status: 200,
            ok: true,
            json: () => Promise.resolve({ id: 'c1', name: 'Open', status: 'setup', withZekkenName: false }),
          });
        }
        return Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'p1', name: 'Alice', dojo: 'Dojo', source: 'registered' }),
        });
      });

      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Full name');
      });

      const findInputByPlaceholder = (ph) => findInTree(runtime.currentTree(),
        n => n.type === 'input' && n.props?.placeholder === ph);
      findInputByPlaceholder('e.g. Alice Tanaka').props.onChange({ target: { value: 'Alice' } });
      findInputByPlaceholder('e.g. Gyokusen').props.onChange({ target: { value: 'Dojo' } });

      const form = findInTree(runtime.currentTree(), n => n.type === 'form');
      expect(form).toBeTruthy();
      await form.props.onSubmit({ preventDefault: vi.fn() });

      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain("You're registered");
      });
    });
  });

  describe('duplicate name error', () => {
    it('shows friendly duplicate message on 409', async () => {
      global.fetch = vi.fn((url, opts) => {
        if (!opts || opts.method !== 'POST') {
          return Promise.resolve({
            status: 200,
            ok: true,
            json: () => Promise.resolve({ id: 'c1', name: 'Open', status: 'setup', withZekkenName: false }),
          });
        }
        return Promise.resolve({
          status: 409,
          ok: false,
          json: () => Promise.resolve({ error: 'A participant with this name is already registered. If this is you, no action needed.' }),
        });
      });

      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Full name');
      });

      const findInputByPlaceholder = (ph) => findInTree(runtime.currentTree(),
        n => n.type === 'input' && n.props?.placeholder === ph);
      findInputByPlaceholder('e.g. Alice Tanaka').props.onChange({ target: { value: 'Alice' } });
      findInputByPlaceholder('e.g. Gyokusen').props.onChange({ target: { value: 'Dojo' } });

      const form = findInTree(runtime.currentTree(), n => n.type === 'form');
      await form.props.onSubmit({ preventDefault: vi.fn() });

      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('already registered');
      });
    });
  });

  describe('register another', () => {
    it('resets form when "Register another" is clicked after success', async () => {
      global.fetch = vi.fn((url, opts) => {
        if (!opts || opts.method !== 'POST') {
          return Promise.resolve({
            status: 200,
            ok: true,
            json: () => Promise.resolve({ id: 'c1', name: 'Open', status: 'setup', withZekkenName: true }),
          });
        }
        return Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'p1', name: 'Alice', dojo: 'Dojo', source: 'registered' }),
        });
      });

      runtime.mount(RegistrationForm, { compId: 'c1' });
      await vi.waitFor(() => {
        expect(collectText(runtime.currentTree())).toContain('Full name');
      });

      const findInputByPlaceholder = (ph) => findInTree(runtime.currentTree(),
        n => n.type === 'input' && n.props?.placeholder === ph);
      findInputByPlaceholder('e.g. Alice Tanaka').props.onChange({ target: { value: 'Alice' } });
      findInputByPlaceholder('e.g. Gyokusen').props.onChange({ target: { value: 'Dojo' } });
      findInputByPlaceholder('e.g. 3 Dan').props.onChange({ target: { value: '3 Dan' } });
      findInputByPlaceholder('e.g. TANAKA').props.onChange({ target: { value: 'ALICE' } });

      const form = findInTree(runtime.currentTree(), n => n.type === 'form');
      await form.props.onSubmit({ preventDefault: vi.fn() });

      await vi.waitFor(() => {
        expect(collectText(runtime.currentTree())).toContain("You're registered");
      });

      const registerAnotherBtn = findInTree(runtime.currentTree(), n =>
        n.type === 'button' && collectText(n).includes('Register another')
      );
      expect(registerAnotherBtn).toBeTruthy();
      registerAnotherBtn.props.onClick();

      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Full name');
        expect(text).toContain('Dojo');
        expect(text).not.toContain("You're registered");
      });

      const nameInput = findInputByPlaceholder('e.g. Alice Tanaka');
      expect(nameInput.props.value).toBe('');
      const dojoInput = findInputByPlaceholder('e.g. Gyokusen');
      expect(dojoInput.props.value).toBe('');
      const danGradeInput = findInputByPlaceholder('e.g. 3 Dan');
      expect(danGradeInput.props.value).toBe('');
      const displayNameInput = findInputByPlaceholder('e.g. TANAKA');
      expect(displayNameInput.props.value).toBe('');

      // Competition metadata preserved — comp name still visible, no re-fetch
      const text = collectText(runtime.currentTree());
      expect(text).toContain('Open');
      expect(global.fetch).toHaveBeenCalledTimes(2); // 1 GET meta + 1 POST register — no extra fetch
    });
  });

  describe('back button', () => {
    it('calls onBack when back button is clicked', async () => {
      global.fetch = vi.fn(() =>
        Promise.resolve({
          status: 200,
          ok: true,
          json: () => Promise.resolve({ id: 'c1', name: 'Open', status: 'setup', withZekkenName: false }),
        })
      );
      const onBack = vi.fn();
      runtime.mount(RegistrationForm, { compId: 'c1', onBack });
      await vi.waitFor(() => {
        const text = collectText(runtime.currentTree());
        expect(text).toContain('Back');
      });

      const backBtn = findInTree(runtime.currentTree(), n =>
        n.type === 'button' && n.props?.type === 'button' && collectText(n).includes('Back')
      );
      expect(backBtn).toBeTruthy();
      backBtn.props.onClick();
      expect(onBack).toHaveBeenCalledOnce();
    });
  });
});
