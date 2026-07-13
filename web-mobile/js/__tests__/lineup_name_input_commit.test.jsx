// T2: LineupNameInput click-outside and blur commit behaviour.
// Guards against typed-but-uncommitted names being silently dropped when the
// operator clicks another field or tabs away mid-entry.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

const realReact = global.React;

function walk(node, visit) {
  if (node == null || typeof node !== 'object') return;
  if (Array.isArray(node)) { node.forEach(n => walk(n, visit)); return; }
  visit(node);
  const kids = node.children ?? node.props?.children;
  if (kids != null) walk(kids, visit);
}
function findHost(tree, type, predicate) {
  let found = null;
  walk(tree, n => {
    if (!found && n?.type === type && (!predicate || predicate(n))) found = n;
  });
  return found;
}

describe('LineupNameInput click-outside / blur commit', () => {
  let runtime, LineupNameInput;
  let clickOutsideCb = null;

  beforeEach(async () => {
    // Stub useClickOutside: always capture the latest callback, mirroring how
    // the real implementation updates cbRef.current on every render regardless
    // of the enabled flag. The enabled flag only controls whether the browser
    // listener is attached; the callback itself is always the latest closure.
    clickOutsideCb = null;
    global.window.useClickOutside = vi.fn((ref, cb /*, enabled */) => {
      clickOutsideCb = cb;
    });

    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ LineupNameInput } = await import('../admin_scoring_shared.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
    clickOutsideCb = null;
  });

  it('typing then click-outside commits the typed value via onSelect', () => {
    const onSelect = vi.fn();
    const roster = ['Tanaka', 'Suzuki'];
    // Mount then get the input node from the initial tree.
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findHost(tree, 'input');
    expect(inputNode).toBeTruthy();

    // Simulate typing "New Player": onChange opens the list and sets query.
    inputNode.props.onChange({ target: { value: 'New Player' } });
    // makeReactive re-renders synchronously; get the latest callback.
    tree = runtime.currentTree();
    // clickOutsideCb is now the closure where q === "New Player".

    expect(clickOutsideCb).toBeTruthy();
    clickOutsideCb();
    // onSelect must have been called with the typed name.
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('New Player');
  });

  it('empty query on click-outside closes without calling onSelect', () => {
    const onSelect = vi.fn();
    runtime.mount(LineupNameInput, { value: '', roster: ['Tanaka'], onSelect, ariaLabel: 'pos', color: 'shiro' });
    // No typing: query is "". Click-outside should not commit.
    expect(clickOutsideCb).toBeTruthy();
    clickOutsideCb();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('whitespace-only query on click-outside does not commit', () => {
    const onSelect = vi.fn();
    let tree = runtime.mount(LineupNameInput, { value: '', roster: ['Tanaka'], onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findHost(tree, 'input');
    inputNode.props.onChange({ target: { value: '   ' } });
    clickOutsideCb();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('option onMouseDown commits the option name (single call, no double-commit from subsequent click-outside)', () => {
    const onSelect = vi.fn();
    const roster = ['Tanaka', 'Suzuki'];
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findHost(tree, 'input');
    // Type to open the dropdown.
    inputNode.props.onChange({ target: { value: 'Tan' } });
    tree = runtime.currentTree();

    // Find the first dropdown option button (inside the dropdown).
    const optionBtn = findHost(tree, 'button', n => !!n?.props?.onMouseDown);
    expect(optionBtn).toBeTruthy();

    // Fire option mousedown (preventDefault + commit option name).
    optionBtn.props.onMouseDown({ preventDefault: vi.fn() });
    // onSelect called with the option name.
    expect(onSelect).toHaveBeenCalledTimes(1);

    // After option commit, query is cleared. A subsequent click-outside
    // (which could fire if the event bubbled strangely) must NOT call onSelect again.
    if (clickOutsideCb) clickOutsideCb();
    expect(onSelect).toHaveBeenCalledTimes(1);
  });
});
