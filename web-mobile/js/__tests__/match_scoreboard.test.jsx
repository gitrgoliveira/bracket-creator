// mp-13y: the ONE shared FIK scoreboard (match_scoreboard.jsx) used by the
// viewer card, the self-run modal and the TV display. Covers the rendering the
// delegation tests in viewer.test.jsx / display_white_board.test.jsx don't see.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}
function findInTree(node, pred) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) { for (const k of node) { const f = findInTree(k, pred); if (f) return f; } return null; }
  if (pred(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) { const f = findInTree(k, pred); if (f) return f; }
  return null;
}
// BoutSubRow children of TeamScoreboard are component vnodes (not expanded) —
// identify them by their `sub` prop.
function boutRows(node, out = []) {
  if (!node || typeof node !== 'object') return out;
  if (Array.isArray(node)) { node.forEach(n => boutRows(n, out)); return out; }
  const p = node.props || {};
  if (p.sub && typeof p.index === 'number') out.push(p);
  boutRows(node.children || p.children, out);
  return out;
}

describe('match_scoreboard: teamIVPW', () => {
  let teamIVPW;
  beforeEach(async () => { vi.resetModules(); ({ teamIVPW } = await import('../match_scoreboard.jsx')); });
  afterEach(() => { vi.resetModules(); });

  it('counts individual victories + points won per side (shiro=B, aka=A)', () => {
    const subs = [
      { position: 1, ipponsB: ['M', 'K'], ipponsA: [] },   // shiro wins, 2 pts
      { position: 2, ipponsB: [], ipponsA: ['D'] },          // aka wins, 1 pt
      { position: 3, ipponsB: ['M'], ipponsA: ['M'] },       // tie 1-1, no IV
      { position: -1, ipponsB: ['M'], ipponsA: [] },         // DH excluded
    ];
    expect(teamIVPW(subs)).toEqual({ ivShiro: 1, ivAka: 1, pwShiro: 3, pwAka: 2 });
  });
});

describe('match_scoreboard components', () => {
  const realReact = global.React;
  let runtime, BoutSubRow, IndividualScore, TeamScoreboard;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.isHikiwake = vi.fn((t) => t === 'hikiwake');
    global.window.ipponsFromScore = vi.fn(() => []);
    vi.resetModules();
    ({ BoutSubRow, IndividualScore, TeamScoreboard } = await import('../match_scoreboard.jsx'));
  });
  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.isHikiwake; delete global.window.ipponsFromScore;
    vi.restoreAllMocks(); vi.resetModules();
  });

  it('BoutSubRow renders ippon letters in slots + bout-number fallback', () => {
    const sub = { position: 1, ipponsB: ['M', 'K'], ipponsA: ['D'] };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    const text = collectText(tree);
    expect(text).toContain('M'); expect(text).toContain('K'); expect(text).toContain('D');
    // No lineup → both names fall back to the bout number "1".
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-0')).toBeTruthy();
  });

  it('BoutSubRow marks a hikiwake with X', () => {
    const sub = { position: 1, ipponsB: [], ipponsA: [], score: { type: 'hikiwake' } };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-draw')).toBeTruthy();
  });

  it('BoutSubRow shows the red ▲ hansoku on the offending side only', () => {
    const sub = { position: 1, ipponsB: [], ipponsA: ['M'], hansokuB: 1, hansokuA: 0 };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 5 });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-b')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-a')).toBeNull();
  });

  it('IndividualScore renders the ippon-letter slots (§263)', () => {
    const tree = runtime.mount(IndividualScore, { match: { ipponsB: ['M'], ipponsA: ['K', 'M'] } });
    const text = collectText(tree);
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'individual-score')).toBeTruthy();
    expect(text).toContain('M'); expect(text).toContain('K');
  });

  it('TeamScoreboard renders the IV/PW summary + one row per regular bout', () => {
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: 2, ipponsB: [], ipponsA: ['D'] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: false });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'team-summary')).toBeTruthy();
    const text = collectText(tree);
    expect(text).toContain('IV'); expect(text).toContain('PW');
    expect(boutRows(tree).length).toBe(2);
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'dh-banner')).toBeNull();
  });

  it('TeamScoreboard renders the DAIHYOSEN banner + rep-bout row when showDH', () => {
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: ['M'] },
      { position: -1, ipponsB: ['M'], ipponsA: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, { subResults, lineupA: null, lineupB: null, teamSize: 5, showDH: true });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'dh-banner')).toBeTruthy();
    expect(collectText(tree)).toContain('DAIHYOSEN');
    // regular bouts + the DH rep row.
    expect(boutRows(tree).some(r => r.isDH)).toBe(true);
  });
});
