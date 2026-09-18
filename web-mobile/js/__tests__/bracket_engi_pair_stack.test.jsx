// Engi bracket cards stack the pair: member 2 (split from the combined
// "Name 1 - Name 2" participant name) renders on its own line under member 1
// instead of truncating on narrow cards. Non-engi cards render the name as-is.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  // NumberedName is a plain (hookless) function component: the mock React
  // runtime's createElement never invokes it, so expand it explicitly to
  // reach the name text it wraps. Matched by name, not identity: bracket.jsx
  // is re-imported per test via vi.resetModules(), so a statically imported
  // reference here would never === the freshly loaded one.
  if (typeof node.type === 'function' && node.type.name === 'NumberedName') return collectText(node.type(node.props));
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}

// Count bc-name spans in the rendered tree (member lines).
function countNameSpans(node, acc = { n: 0 }) {
  if (node == null || typeof node !== 'object') return acc.n;
  if (Array.isArray(node)) { node.forEach(k => countNameSpans(k, acc)); return acc.n; }
  if (node.props?.className && String(node.props.className).split(' ').includes('bc-name')) acc.n++;
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => countNameSpans(k, acc));
  return acc.n;
}

describe('bracket MatchCard engi pair stacking', () => {
  const realReact = global.React;
  let runtime;
  let PlayerLine;

  const player = { id: 'p1', name: 'Ren Suzuki - Emi Nakamura', dojo: 'Higashi Dojo' };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    window.engiPairParts = (name) => {
      const s = String(name || '');
      const i = s.indexOf(' - ');
      return i < 0 ? [s.trim(), ''] : [s.slice(0, i).trim(), s.slice(i + 3).trim()];
    };
    vi.resetModules();
    ({ PlayerLine } = await import('../bracket.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
  });

  it('renders both members on separate lines when isEngi=true', () => {
    const tree = runtime.mount(PlayerLine, { player, side: 'a', showDojo: true, score: '2', isEngi: true });
    const text = collectText(tree);
    expect(text).toContain('Ren Suzuki');
    expect(text).toContain('Emi Nakamura');
    expect(text).not.toContain('Ren Suzuki - Emi Nakamura');
    expect(countNameSpans(tree)).toBe(2);
  });

  it('renders the plain combined name on one line when isEngi is not set', () => {
    const tree = runtime.mount(PlayerLine, { player, side: 'a', showDojo: true, score: '2' });
    const text = collectText(tree);
    expect(text).toContain('Ren Suzuki - Emi Nakamura');
    expect(countNameSpans(tree)).toBe(1);
  });
});

// bc-rvfx: PlayerLine's own competitor-number placement and the removed
// colour badge landed with no test anywhere in the repo -- including in this
// file, whose collectText above already carries the NumberedName-expansion
// branch (added the same time the number rendering did) but whose two
// existing fixtures never set a `number`, so that branch was pinned by
// nothing.
describe('PlayerLine: outer number placement and no colour badge (bc-rvfx)', () => {
  const realReact = global.React;
  let runtime;
  let PlayerLine;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    vi.resetModules();
    ({ PlayerLine } = await import('../bracket.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.resetModules();
  });

  // True when any node in the tree carries `cls` as one of its className
  // tokens. A class check, rather than a text search, so the badge-absence
  // assertions below do not depend on the exact wording the removed badge
  // used to render.
  function hasClassToken(node, cls) {
    if (node == null || typeof node !== 'object') return false;
    if (Array.isArray(node)) return node.some(n => hasClassToken(n, cls));
    const classes = String(node.props?.className || '').split(' ');
    if (classes.includes(cls)) return true;
    const kids = node.children || node.props?.children || [];
    return [].concat(kids).some(n => hasClassToken(n, cls));
  }

  // The bracket card stacks Shiro over Aka instead of placing them
  // left/right, so there is no outer side: the number sits BEFORE the name
  // on BOTH sides (operator ruling 2026-09-14, bc-dnst). Checked for both
  // `side` values to prove PlayerLine does not thread its own side prop
  // into NumberedName's left/right outer-side rule.
  it('places the competitor number before the name on the Shiro (side b) card', () => {
    const shiroPlayer = { id: 'p1', name: 'Tanaka Kenji', dojo: 'Higashi Dojo', number: 'K5' };
    const tree = runtime.mount(PlayerLine, { player: shiroPlayer, side: 'b', showDojo: false });
    const text = collectText(tree);
    expect(text).toContain('K5');
    expect(text).toContain('Tanaka Kenji');
    expect(text.indexOf('K5')).toBeLessThan(text.indexOf('Tanaka Kenji'));
  });

  it('places the competitor number before the name on the Aka (side a) card too', () => {
    const akaPlayer = { id: 'p2', name: 'Yamada Hanako', dojo: 'Nishi Dojo', number: 'K8' };
    const tree = runtime.mount(PlayerLine, { player: akaPlayer, side: 'a', showDojo: false });
    const text = collectText(tree);
    expect(text).toContain('K8');
    expect(text).toContain('Yamada Hanako');
    expect(text.indexOf('K8')).toBeLessThan(text.indexOf('Yamada Hanako'));
  });

  // The card used to carry a visible AKA/SHIRO text badge on each side; it
  // was removed because the leading colour bar and tint already carry the
  // side, and the card now names no side in text at all (operator ruling
  // 2026-09-14, bc-dnst). A removal leaves no failing test behind on its
  // own, so this pins the absence.
  it('renders no side-name colour badge on a real player card', () => {
    const player2 = { id: 'p3', name: 'Suzuki Ichiro', dojo: 'Minami Dojo', number: 'K2' };
    const tree = runtime.mount(PlayerLine, { player: player2, side: 'a', showDojo: false });
    expect(hasClassToken(tree, 'bc-color-badge')).toBe(false);
    const text = collectText(tree);
    expect(text).not.toContain('AKA');
    expect(text).not.toContain('SHIRO');
  });

  it('renders no side-name colour badge on the TBD placeholder card', () => {
    const tree = runtime.mount(PlayerLine, { player: null, side: 'b', isTBD: true });
    expect(hasClassToken(tree, 'bc-color-badge')).toBe(false);
    const text = collectText(tree);
    expect(text).toContain('TBD');
    expect(text).not.toContain('AKA');
    expect(text).not.toContain('SHIRO');
  });
});
