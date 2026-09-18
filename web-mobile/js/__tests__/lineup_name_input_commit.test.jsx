// T2: LineupNameInput click-outside and blur commit behaviour.
// Guards against typed-but-uncommitted names being silently dropped when the
// operator clicks another field or tabs away mid-entry.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree, findAll, hasClass, collectText } from './helpers/vdom.js';

const realReact = global.React;

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
    const inputNode = findInTree(tree, n => n?.type === 'input');
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
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onChange({ target: { value: '   ' } });
    clickOutsideCb();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('option onMouseDown commits the option name (single call, no double-commit from subsequent click-outside)', () => {
    const onSelect = vi.fn();
    const roster = ['Tanaka', 'Suzuki'];
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    // Type to open the dropdown.
    inputNode.props.onChange({ target: { value: 'Tan' } });
    tree = runtime.currentTree();

    // Find the first dropdown option button (inside the dropdown).
    const optionBtn = findInTree(tree, n => n?.type === 'button' && !!n?.props?.onMouseDown);
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

// bc-dnst: roster entries are now either plain name strings (unchanged
// rendering/commit) or squad-member objects `{ id, index, name, label }`
// (admin_scoring_team.jsx's rosterForSide). An object entry renders its
// number chip and a "no name yet" placeholder for a blank name, and
// picking one hands the WHOLE entry back as onSelect's second argument.
describe('LineupNameInput object-entry roster (bc-dnst)', () => {
  let runtime, LineupNameInput;

  beforeEach(async () => {
    global.window.useClickOutside = vi.fn();
    runtime = makeReactive();
    global.React = runtime.React;
    vi.resetModules();
    ({ LineupNameInput } = await import('../admin_scoring_shared.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
  });

  const roster = [
    { id: 'm1', index: 1, name: 'Sato', label: 'T2.1' },
    { id: 'm2', index: 2, name: '', label: 'T2.2' },
  ];

  it('renders a label + name for a named entry, and a label + "no name yet" for a blank one', () => {
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect: vi.fn(), ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onFocus();
    tree = runtime.currentTree();

    const labelNodes = findAll(tree, n => hasClass(n, 'pmf__opt-label'));
    expect(labelNodes.map(collectText)).toEqual(['T2.1', 'T2.2']);

    const nameNodes = findAll(tree, n => hasClass(n, 'pmf__opt-name'));
    expect(nameNodes.map(collectText)).toEqual(['Sato', 'no name yet']);
    expect(hasClass(nameNodes[1], 'pmf__opt-name--blank')).toBe(true);
    expect(hasClass(nameNodes[0], 'pmf__opt-name--blank')).toBe(false);
  });

  it('typing the label text (not just the name) matches the entry in the filtered list', () => {
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect: vi.fn(), ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    // "T2.2" matches ONLY the blank entry's label, not its (empty) name; no
    // entry's NAME is exactly "T2.2" either, so the query also offers an
    // "+ Add" row (the exact-match check compares names only).
    inputNode.props.onChange({ target: { value: 'T2.2' } });
    tree = runtime.currentTree();

    const nameNodes = findAll(tree, n => hasClass(n, 'pmf__opt-name'));
    expect(nameNodes.map(collectText)).toEqual(['no name yet', '+ Add “T2.2”']);
  });

  it('clicking an object entry calls onSelect with the name AND the whole entry', () => {
    const onSelect = vi.fn();
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onFocus();
    tree = runtime.currentTree();

    const optionButtons = findAll(tree, n => n?.type === 'button' && hasClass(n, 'pmf__option'));
    expect(optionButtons.length).toBe(2);
    // First option is the named entry (Sato).
    optionButtons[0].props.onMouseDown({ preventDefault: vi.fn() });

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('Sato', roster[0]);
  });

  it('clicking a blank object entry calls onSelect with an empty name and the entry', () => {
    const onSelect = vi.fn();
    let tree = runtime.mount(LineupNameInput, { value: '', roster, onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onFocus();
    tree = runtime.currentTree();

    const optionButtons = findAll(tree, n => n?.type === 'button' && hasClass(n, 'pmf__option'));
    optionButtons[1].props.onMouseDown({ preventDefault: vi.fn() });

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('', roster[1]);
  });

  it('a plain string entry still renders name-only, with no label chip', () => {
    let tree = runtime.mount(LineupNameInput, { value: '', roster: ['Tanaka'], onSelect: vi.fn(), ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onFocus();
    tree = runtime.currentTree();

    expect(findAll(tree, n => hasClass(n, 'pmf__opt-label')).length).toBe(0);
    const nameNodes = findAll(tree, n => hasClass(n, 'pmf__opt-name'));
    expect(nameNodes.map(collectText)).toEqual(['Tanaka']);
  });

  it('a plain string entry still commits with a single argument (onSelect(name) only)', () => {
    const onSelect = vi.fn();
    let tree = runtime.mount(LineupNameInput, { value: '', roster: ['Tanaka'], onSelect, ariaLabel: 'pos', color: 'shiro' });
    const inputNode = findInTree(tree, n => n?.type === 'input');
    inputNode.props.onFocus();
    tree = runtime.currentTree();

    const optionButtons = findAll(tree, n => n?.type === 'button' && hasClass(n, 'pmf__option'));
    optionButtons[0].props.onMouseDown({ preventDefault: vi.fn() });

    expect(onSelect).toHaveBeenCalledTimes(1);
    // A string-origin entry still commits like a plain string always did:
    // ONE argument, not a second `undefined` (which would fail
    // toHaveBeenCalledWith('Tanaka') under Vitest's exact-arity matching).
    expect(onSelect).toHaveBeenCalledWith('Tanaka');
    expect(onSelect.mock.calls[0].length).toBe(1);
  });
});
