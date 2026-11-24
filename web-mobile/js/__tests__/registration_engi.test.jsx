import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText } from './helpers/vdom.js';
import { ENGI_PAIR_PROBLEM } from '../engi_entry.jsx';

// The public self-registration form asks the one engi entry rule
// (engi_entry.jsx) before it sends, from the engi and engiSolo the GET
// answers: its name field says the pair format for a pair competition, and a
// name that does not fit the competition's shape is refused in words for a
// participant, with nothing posted. The server keeps no such rule.

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

const SOLO_SENTENCE = 'This competition takes single competitors. Enter one name.';

describe('RegistrationForm on an engi competition', () => {
  let RegistrationForm;
  let registrationNameProblem;
  let runtime;
  let posts;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ RegistrationForm, registrationNameProblem } = await import('../registration.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
    delete global.fetch;
  });

  function serve(meta) {
    posts = [];
    global.fetch = vi.fn((url, opts) => {
      if (!opts || opts.method !== 'POST') {
        return Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve(meta) });
      }
      posts.push(JSON.parse(opts.body));
      return Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve({ id: 'p1' }) });
    });
  }

  async function mountForm() {
    runtime.mount(RegistrationForm, { compId: 'kata' });
    await vi.waitFor(() => {
      expect(findInTree(runtime.currentTree(), (n) => n.type === 'form')).toBeTruthy();
    });
  }

  function labelText() {
    const label = findInTree(runtime.currentTree(), (n) => n.type === 'label' && n.props?.htmlFor === 'register-name');
    return collectText(label);
  }

  async function submit(name) {
    const byId = (id) => findInTree(runtime.currentTree(), (n) => n.type === 'input' && n.props?.id === id);
    byId('register-name').props.onChange({ target: { value: name } });
    byId('register-dojo').props.onChange({ target: { value: 'Gyokusen' } });
    const form = findInTree(runtime.currentTree(), (n) => n.type === 'form');
    await form.props.onSubmit({ preventDefault: vi.fn() });
  }

  const meta = (extra) => ({ id: 'kata', name: 'Kata', status: 'setup', withZekkenName: false, ...extra });

  it('labels the name field with the pair format and refuses one name in a pair competition', async () => {
    serve(meta({ engi: true, engiSolo: false }));
    await mountForm();
    expect(labelText()).toContain('Pair (Name 1 - Name 2)');
    expect(labelText()).not.toContain('Full name');
    await submit('Akira Tanaka');
    await vi.waitFor(() => expect(collectText(runtime.currentTree())).toContain(ENGI_PAIR_PROBLEM));
    expect(posts).toHaveLength(0);
  });

  it('registers a full pair in a pair competition', async () => {
    serve(meta({ engi: true, engiSolo: false }));
    await mountForm();
    await submit('Akira Tanaka - Ren Fujita');
    await vi.waitFor(() => expect(collectText(runtime.currentTree())).toContain("You're registered"));
    expect(posts).toEqual([{ name: 'Akira Tanaka - Ren Fujita', dojo: 'Gyokusen' }]);
  });

  it('asks for a full name and refuses a pair with single competitors, worded for a participant', async () => {
    serve(meta({ engi: true, engiSolo: true }));
    await mountForm();
    expect(labelText()).toContain('Full name');
    await submit('Akira Tanaka - Ren Fujita');
    await vi.waitFor(() => expect(collectText(runtime.currentTree())).toContain(SOLO_SENTENCE));
    // The operator's sentence points at Settings, which a participant cannot reach.
    expect(collectText(runtime.currentTree())).not.toContain('Settings');
    expect(posts).toHaveLength(0);
  });

  it('registers one name, a hyphenated one included, with single competitors', async () => {
    serve(meta({ engi: true, engiSolo: true }));
    await mountForm();
    await submit('Jean-Paul Martin');
    await vi.waitFor(() => expect(collectText(runtime.currentTree())).toContain("You're registered"));
    expect(posts).toEqual([{ name: 'Jean-Paul Martin', dojo: 'Gyokusen' }]);
  });

  it('asks no engi rule of a competition that is not engi, a stray engiSolo included', async () => {
    serve(meta({ engi: false, engiSolo: true }));
    await mountForm();
    expect(labelText()).toContain('Full name');
    await submit('Akira Tanaka - Ren Fujita');
    await vi.waitFor(() => expect(collectText(runtime.currentTree())).toContain("You're registered"));
    expect(posts).toHaveLength(1);
  });

  it('registrationNameProblem words each shape for a participant', () => {
    expect(registrationNameProblem(null, 'A - B')).toBeNull();
    expect(registrationNameProblem('pair', 'A - B')).toBeNull();
    expect(registrationNameProblem('pair', 'Alice -')).toBe(ENGI_PAIR_PROBLEM);
    expect(registrationNameProblem('pair', '- Bob')).toBe(ENGI_PAIR_PROBLEM);
    expect(registrationNameProblem('solo', 'Akira Tanaka')).toBeNull();
    expect(registrationNameProblem('solo', 'A - B')).toBe(SOLO_SENTENCE);
    expect(registrationNameProblem('solo', 'Alice -')).toBe(SOLO_SENTENCE);
  });
});
