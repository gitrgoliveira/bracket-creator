// Engi bracket cards stack the pair: member 2 (split from the combined
// "Name 1 - Name 2" participant name) renders on its own line under member 1
// instead of truncating on narrow cards. Non-engi cards render the name as-is.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
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
      return i < 0 ? [s, ''] : [s.slice(0, i), s.slice(i + 3)];
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
