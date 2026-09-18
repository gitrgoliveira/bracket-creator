import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText } from './helpers/vdom.js';

// HISTORY: the OBS lower-third used to read "0 - 0" through a scored
// KNOCKOUT bout while the board beside it read "K D vs M". Cause: a bracket
// match used to persist its running score as a formatted scoreA/scoreB
// STRING, not the ippon arrays a pool match carries, and the overlay read
// only the arrays. That split is now closed at the WIRE level: pool and
// bracket matches share one shape (ipponsA/ipponsB arrays; scoreA/scoreB
// strings never appear in any response), so the overlay reads the arrays
// unconditionally with no per-kind fallback (see ovlIppons in
// streaming_overlay.jsx).
//
// It also showed a DIGIT for an empty side, which the score-cell contract
// forbids outright — a cell with no points reads "-", never "0", so a kendo
// score never reads "M - 0".
describe('StreamingOverlay individual-match score', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term'];

  // A bracket match as the court endpoint serves it: ipponsA/ipponsB arrays,
  // the same shape a pool match carries.
  const bracketMatch = (ipponsA, ipponsB) => ({
    id: 'm-r1-0', court: 'A', status: 'running',
    sideA: { name: 'Alice' }, sideB: { name: 'Bob' },
    ipponsA, ipponsB, subResults: [],
  });
  const compWith = (match) => [{
    id: 'c1', name: 'Cup', kind: 'individual', teamSize: 0, withZekkenName: false,
    poolMatches: [], bracket: { rounds: [[match]] },
  }];

  const scoreText = (tree) => {
    // The score line renders as `{ipponsB} - {ipponsA}`.
    const all = collectText(tree);
    return all;
  };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
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

  it('reads a bracket match score from ipponsA/ipponsB', () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(bracketMatch(['M'], ['K', 'D'])),
    });
    const text = scoreText(tree);
    expect(text).toContain('KD');
    expect(text).toContain('M');
    // The historical bug's signature: a scored bout reading as zeroes.
    expect(text).not.toContain('0 - 0');
  });

  it('shows a dash, never a digit, for a side with no points', () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(bracketMatch([], ['M'])),
    });
    const text = scoreText(tree);
    expect(text).toContain('M');
    // "M - 0" is the reading the cell contract forbids.
    expect(text).not.toMatch(/\b0\b/);
  });

  it('drops the unfilled-slot placeholder rather than printing it', () => {
    const m = bracketMatch(['M', '•'], ['•']);
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(m),
    });
    const text = scoreText(tree);
    expect(text).not.toContain('•');
    expect(text).toContain('M');
  });
});

// bc-rvfx / PR #428 audit: three behaviours landed in streaming_overlay.jsx
// in one diff (git range 6743a602..05371c04) with no test file touched.
// Each block below pins one, off a fresh team-mode fixture: none of the
// existing describes in this file exercise a team match. A fourth, closely
// related behaviour (the individual-match name lines gaining an outer-side
// number) is pinned in its own block at the end since it is independently
// observable in the same diff and was otherwise left uncovered.

// findParent: like the vdom helpers' findAll/findInTree, but returns the
// PARENT of the first node matching `pred` rather than the node itself.
// Needed here because the bout-row name+label span carries no data-testid
// of its own; its position relative to a labelled sibling is exactly what
// the outer-side rulings below are about.
function findParent(node, pred, parent = null) {
  if (node == null || typeof node !== 'object') return null;
  if (Array.isArray(node)) {
    for (const n of node) { const r = findParent(n, pred, parent); if (r) return r; }
    return null;
  }
  if (pred(node)) return parent;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) { const r = findParent(k, pred, node); if (r) return r; }
  return null;
}

// The team-mode tests below need useTeamLineups' fetch effect to actually
// resolve (it populates squadA/squadB asynchronously, gated on window.API
// existing at all -- see match_scoreboard.jsx's useTeamLineups). flushAsync
// drains that promise chain (fetchCompetitionDetails, then Promise.all of
// two resolveMatchLineup calls, each with its own two-deep try/catch)
// before the test reads runtime.currentTree().
async function flushAsync() {
  for (let i = 0; i < 20; i++) await Promise.resolve();
}

describe('StreamingOverlay team bout name: a stored team name is never shown as a fighter (bc-dnst)', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term'];

  // A fixed-order row written before bc-dnst stores the TEAM's own name in
  // sub.sideA/sub.sideB. No lineup is set, so nothing else can supply a
  // fighter name: resolveBoutSideName's teamNameA/teamNameB filter must
  // reject the stored value and fall through to the FIK position label.
  const teamMatch = () => ({
    id: 'Pool A-1', court: 'A', status: 'running',
    sideA: { id: 'aka-team', name: 'Crimson Aka', number: 'T2' },
    sideB: { id: 'shiro-team', name: 'Ivory Shiro', number: 'T1' },
    ipponsA: [], ipponsB: [],
    subResults: [
      { position: 1, sideA: 'Crimson Aka', sideB: 'Ivory Shiro', ipponsA: [], ipponsB: [] },
    ],
  });
  const teamComp = (match) => [{
    id: 'c1', name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
    poolMatches: [match], bracket: { rounds: [] },
  }];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
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

  it('falls back to the FIK position label rather than the stored team name, on both sides', () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: teamComp(teamMatch()),
    });
    // Shiro's row is [nameLabelSpan, ippons]; Aka's is [ippons, nameLabelSpan]
    // (Aka's own row is deliberately reversed -- see the next describe block).
    const shiroRow = findParent(tree, n => n?.props?.['data-testid'] === 'overlay-shiro-bout');
    const akaRow = findParent(tree, n => n?.props?.['data-testid'] === 'overlay-aka-bout');
    const shiroNameText = collectText(shiroRow.props.children[0]);
    const akaNameText = collectText(akaRow.props.children[1]);
    expect(shiroNameText).toBe('Senpo');
    expect(akaNameText).toBe('Senpo');
    expect(shiroNameText).not.toContain('Ivory Shiro');
    expect(akaNameText).not.toContain('Crimson Aka');
  });
});

describe('StreamingOverlay team bout name: a rename reaches the current bout (bc-dnst)', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term', 'API'];

  // The stored sub-bout text is frozen at whatever it was when the bout was
  // written; resolveBoutSideDisplayName resolves the fighter's CURRENT name
  // by squad member id instead, so a later rename shows up on a bout
  // already in progress.
  const teamMatch = () => ({
    id: 'Pool A-1', court: 'A', status: 'running',
    sideA: { id: 'aka-team', name: 'Crimson Aka', number: 'T2' },
    sideB: { id: 'shiro-team', name: 'Ivory Shiro', number: 'T1' },
    ipponsA: [], ipponsB: [],
    subResults: [
      { position: 1, sideA: 'Suzuki OLD NAME', sideAMemberId: 'mem-aka-1', sideB: 'Someone Else', ipponsA: [], ipponsB: [] },
    ],
  });
  const teamComp = (match) => [{
    id: 'c1', name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
    squads: { 'aka-team': [{ id: 'mem-aka-1', index: 3, name: 'Suzuki RENAMED' }] },
    poolMatches: [match], bracket: { rounds: [] },
  }];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    // Minimal API stub: useTeamLineups only proceeds past its window.API
    // guard when this exists at all. Squads come straight off
    // competition.squads (set above), never through these fetches, so the
    // stub only needs to resolve without throwing.
    global.window.API = {
      fetchCompetitionDetails: async () => ({}),
      fetchMatchLineup: async () => null,
      fetchTeamLineup: async () => null,
    };
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
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

  it("shows the squad member's current name, not the bout's frozen stored name", async () => {
    runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: teamComp(teamMatch()),
    });
    await flushAsync();
    const tree = runtime.currentTree();
    const akaRow = findParent(tree, n => n?.props?.['data-testid'] === 'overlay-aka-bout');
    const akaNameText = collectText(akaRow.props.children[1]);
    expect(akaNameText).toContain('Suzuki RENAMED');
    expect(akaNameText).not.toContain('Suzuki OLD NAME');
  });
});

describe('StreamingOverlay team bout: Aka squad label sits after the name, not before (bc-dnst)', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term', 'API'];

  const teamMatch = () => ({
    id: 'Pool A-1', court: 'A', status: 'running',
    sideA: { id: 'aka-team', name: 'Crimson Aka', number: 'T9' },
    sideB: { id: 'shiro-team', name: 'Ivory Shiro', number: 'T1' },
    ipponsA: [], ipponsB: [],
    subResults: [
      { position: 1, sideA: 'Yamada', sideAMemberId: 'mem-aka-1', sideB: 'Someone Else', ipponsA: [], ipponsB: [] },
    ],
  });
  const teamComp = (match) => [{
    id: 'c1', name: 'Team Cup', kind: 'team', teamSize: 5, withZekkenName: false,
    squads: { 'aka-team': [{ id: 'mem-aka-1', index: 4, name: 'Yamada' }] },
    poolMatches: [match], bracket: { rounds: [] },
  }];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.API = {
      fetchCompetitionDetails: async () => ({}),
      fetchMatchLineup: async () => null,
      fetchTeamLineup: async () => null,
    };
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
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

  it('renders [name, label] in that order, matching the outer-side number rule (operator ruling 2026-09-14)', async () => {
    runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: teamComp(teamMatch()),
    });
    await flushAsync();
    const tree = runtime.currentTree();
    const akaRow = findParent(tree, n => n?.props?.['data-testid'] === 'overlay-aka-bout');
    const nameLabelSpan = akaRow.props.children[1];
    const kids = [].concat(nameLabelSpan.props.children);
    expect(kids[0]).toBe('Yamada');
    expect(kids[1]).toBeTruthy();
    expect(kids[1].props['data-testid']).toBe('overlay-aka-member-label');
    expect(collectText(kids[1])).toBe('T9.4');
  });
});

// The overlay's INDIVIDUAL (non-team) name lines still call sideLabel
// directly as a plain string (the later NumberedName/clip conversion,
// bc-rvfx commit 51e9e670, only touched the TEAM name lines above the QR).
// This is the one PR #428 call-site change still reachable in its original
// diff form -- see this file's own header note on the disagreement.
describe('StreamingOverlay individual match: competitor number sits on the outer side (bc-dnst)', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term'];

  const numberedMatch = () => ({
    id: 'm-r1-0', court: 'A', status: 'running',
    sideA: { name: 'Yamada', number: 'K9' }, sideB: { name: 'Tanaka', number: 'K5' },
    ipponsA: [], ipponsB: [], subResults: [],
  });
  const compWith = (match) => [{
    id: 'c1', name: 'Cup', kind: 'individual', teamSize: 0, withZekkenName: false,
    poolMatches: [], bracket: { rounds: [[match]] },
  }];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
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

  it("puts Shiro's number before the name and Aka's after it", () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(numberedMatch()),
    });
    const text = collectText(tree);
    // sideB (Tanaka) is Shiro: number leads. sideA (Yamada) is Aka: number trails.
    expect(text).toContain('K5 Tanaka');
    expect(text).toContain('Yamada K9');
    expect(text).not.toContain('K9 Yamada');
    expect(text).not.toContain('Tanaka K5');
  });
});
