import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// Depth-first search for all nodes matching a predicate. Same shape as the
// helper in viewer_bronze_match.test.jsx, minus the function-type recursion:
// this file only ever asserts on the shell element ViewerCompetition returns
// itself, so executing child components would add noise, not coverage.
function findAll(node, predicate, acc = []) {
  if (!node || typeof node !== 'object') return acc;
  if (Array.isArray(node)) {
    node.forEach(k => findAll(k, predicate, acc));
    return acc;
  }
  if (predicate(node)) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, predicate, acc));
  return acc;
}

const shellClass = (tree) => {
  const hits = findAll(tree, n =>
    typeof n === 'object' && !Array.isArray(n) &&
    typeof n.props?.className === 'string' &&
    /(^|\s)viewer__shell(\s|$|--)/.test(n.props.className)
  );
  return hits.length ? hits[0].props.className : null;
};

// The public viewer's shell caps its width (480/768/1024px) so every text
// surface in it reads as a bounded column. The bracket tab is the one
// exception: it is a wide diagram, and inside the 1024px cap the FINAL card of
// an ordinary 7-pool / 2-qualifier draw hung ~80px past the canvas edge and
// rendered with its match number and "Winner of …" labels sliced in half, at
// every viewport (widening the window only grew the empty side margins).
//
// The fix is `.viewer__shell--bracket` (styles.css), which raises the cap for
// that tab alone. The CSS is only half of it: without the modifier CLASS on the
// shell the rule matches nothing, and nothing else in the app adds it. jsdom
// does no layout, so the widths themselves are verified in a real browser
// (measured bounding boxes); what this file pins is the wiring — the class is
// present exactly when the bracket tab is the one actually being displayed.
describe('ViewerCompetition bracket-tab shell width modifier', () => {
  const realReact = global.React;
  let runtime;
  let ViewerCompetition;
  const savedGlobals = {};

  // window globals captured at module-eval time in viewer_competition.jsx
  // (e.g. `const StatusBadge = window.StatusBadge`) MUST be set before import.
  const STUBBED = [
    'StatusBadge', 'formatDate', 'formatLabel', 'pluralize', 'Term',
    'BracketTree', 'MatchCard', 'buildBracket', 'roundLabel', 'bracketRoundLabel',
    'formatIpponsScore', 'isHikiwake', 'hasBothSides',
    'compareDmy', 'queueLabel', 'queueLabelCompact', 'teamIVScore',
    'matchScoreStr', 'EmptyState', 'bronzeUnderFinalStyle',
  ];

  const mkComp = (overrides = {}) => ({
    id: 'mixed-1',
    name: 'Mixed Test',
    kind: 'individual',
    teamSize: 0,
    format: 'mixed',
    status: 'running',
    startTime: '09:00',
    courts: ['A'],
    players: [],
    ...overrides,
  });

  const mkBracket = () => ({
    rounds: [
      [
        { id: 'r0-m0', sideA: { id: 'p1', name: 'Alice' }, sideB: { id: 'p2', name: 'Bob' }, status: 'scheduled' },
        { id: 'r0-m1', sideA: { id: 'p3', name: 'Carol' }, sideB: { id: 'p4', name: 'Dave' }, status: 'scheduled' },
      ],
      [
        { id: 'r1-m0', sideA: { id: '', name: 'Winner of M1' }, sideB: { id: '', name: 'Winner of M2' }, status: 'scheduled' },
      ],
    ],
  });

  const mount = (props) => runtime.mount(ViewerCompetition, {
    tournament: { competitions: [] },
    competition: mkComp(),
    pools: [],
    poolMatches: [],
    standings: [],
    bracket: mkBracket(),
    onBack: () => {},
    tweaks: {},
    onTabChange: () => {},
    ...props,
  });

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });
    global.window.StatusBadge = function StatusBadge() { return null; };
    global.window.BracketTree = function BracketTree() { return null; };
    global.window.MatchCard = function MatchCard() { return null; };
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.EmptyState = function EmptyState(props) {
      return { type: 'div', props: { className: 'empty', ...props }, children: [props.icon, props.title, props.message].filter(Boolean) };
    };
    global.window.formatDate = (d) => d || '';
    global.window.formatLabel = (s) => s || '';
    global.window.pluralize = (n, a, b) => `${n} ${n === 1 ? a : b}`;
    global.window.buildBracket = () => [];
    global.window.roundLabel = (i) => `Round ${i + 1}`;
    global.window.bracketRoundLabel = (_m, i, n) => global.window.roundLabel(i, n);
    global.window.formatIpponsScore = () => '';
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = () => '';
    global.window.isHikiwake = () => false;
    global.window.hasBothSides = (m) => !!(m && m.sideA && m.sideB);
    global.window.compareDmy = (a, b) => String(a).localeCompare(String(b));
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    global.window.bronzeUnderFinalStyle = () => ({});

    vi.resetModules();
    ({ ViewerCompetition } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('adds viewer__shell--bracket on the bracket tab', () => {
    const cls = shellClass(mount({ activeTab: 'bracket' }));
    expect(cls).toBeTruthy();
    expect(cls.split(/\s+/)).toContain('viewer__shell');
    expect(cls.split(/\s+/)).toContain('viewer__shell--bracket');
  });

  it('keeps the reading-width cap on every other tab', () => {
    for (const tab of [undefined, 'overview', 'pools', 'results']) {
      const cls = shellClass(mount({ activeTab: tab }));
      expect(cls).toBeTruthy();
      expect(cls.split(/\s+/)).toContain('viewer__shell');
      expect(cls, `tab=${String(tab)}`).not.toContain('viewer__shell--bracket');
    }
  });

  it('keeps the cap when a /bracket deep link falls back to Overview', () => {
    // No bracket in the payload, so the Bracket tab does not exist and
    // effectiveTab falls back to overview. The modifier must follow the tab
    // that is DISPLAYED, not the one that was requested: widening the shell
    // here would stretch the overview's text column for no reason.
    const cls = shellClass(mount({ activeTab: 'bracket', bracket: null }));
    expect(cls).toBeTruthy();
    expect(cls).not.toContain('viewer__shell--bracket');
  });
});
