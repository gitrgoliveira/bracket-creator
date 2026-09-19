// Regression (bc-cse): TWMatch (viewer_schedule.jsx, the tournament-wide
// per-court schedule row) gated its score cell on `m.status === "completed"`,
// so a team match's live subResults-derived score never showed while the
// match was RUNNING -- the operator/viewer only saw it once the match ended.
// Widened to admit "running" too, mirroring VSchedItem's existing
// completed-or-running gate (viewer_match.jsx).
//
// These tests mount the real TWMatch against the REAL window.matchScoreStr
// (bracket.jsx is imported for its side-effect window.* publication), so the
// team score string is produced by production code rather than asserted
// against a stub.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree, collectText } from './helpers/vdom.js';

const realReact = global.React;

describe('bc-cse: TWMatch score cell is live while running, not just at completion', () => {
  let runtime, TWMatch;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.queueLabelCompact = null;
    vi.resetModules();
    // Side-effect import: publishes the real window.matchScoreStr that
    // TWMatch calls.
    await import('../bracket.jsx');
    ({ TWMatch } = await import('../viewer_schedule.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.queueLabelCompact;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // phase: 'bracket' (not 'pool') so the row's phase label takes the plain
  // m.round string rather than routing through poolLabel, which needs a
  // fuller pool shape this fixture doesn't carry.
  const teamMatch = (status) => ({
    id: 'm-1', compId: 'c1', compName: 'Team Kumite', court: 'A',
    phase: 'bracket', round: 'Final',
    sideA: { id: 'a', name: 'Aka Dojo' }, sideB: { id: 'b', name: 'Shiro Dojo' },
    status,
    // A team match's score lives in teamResult, never the match-level ippon
    // arrays (see CLAUDE.md); this is the shape matchScoreStr's
    // teamIVPWScore reads.
    teamResult: { shiroIV: 1, akaIV: 1, shiroPW: 3, akaPW: 2 },
  });

  // The score container is the plain <div style={{ fontFamily:
  // "var(--font-mono)", ... }}> at the right of the row; it carries no
  // className, so it's located by its distinguishing inline style.
  const scoreContainer = (m) => {
    const tree = runtime.mount(TWMatch, { m });
    return findInTree(tree, (n) => n?.props?.style?.fontFamily === 'var(--font-mono)');
  };

  it('a RUNNING team match shows its live IV/PW score', () => {
    const node = scoreContainer(teamMatch('running'));
    expect(node).not.toBeNull();
    expect(collectText(node)).toBe('IV 1–1\nPW 3–2');
  });

  it('a COMPLETED team match still shows its final score (no regression)', () => {
    const node = scoreContainer(teamMatch('completed'));
    expect(collectText(node)).toBe('IV 1–1\nPW 3–2');
  });

  it('a SCHEDULED team match shows nothing in the score cell (not yet played)', () => {
    const node = scoreContainer(teamMatch('scheduled'));
    expect(collectText(node)).toBe('');
  });

  // The score string is two lines ("IV a-b" then "PW c-d"); the container
  // must preserve that newline as a real line break (whiteSpace: pre-line),
  // or it collapses to a single space in an ordinary `normal` box, which
  // widens the (content-sized, `auto`-tracked) score column and starves the
  // neighbouring `1fr` team-names column into ellipsis (measured live: the
  // names column recovers ~55px once the newline is allowed to stack).
  it('the score container preserves the IV/PW line break (whiteSpace: pre-line)', () => {
    const node = scoreContainer(teamMatch('running'));
    expect(node.props.style.whiteSpace).toBe('pre-line');
  });

  // Deliberate, not an oversight: TWMatch admits the FULL score string for a
  // running INDIVIDUAL match too, not just team. Unlike matchStateCell's one
  // caller (PoolNumberedMatchRow, which sits this value literally BETWEEN two
  // competitor name cells and is scoped to team-only for that reason -- see
  // bracket.jsx), this score block is its own standalone cell separate from
  // the names column, the same shape VSchedItem already renders a running
  // individual match's ippon letters in.
  it('a RUNNING individual match shows its live ippon letters too (not team-only)', () => {
    const individualMatch = {
      id: 'm-2', compId: 'c1', compName: 'Individual Kumite', court: 'A',
      phase: 'bracket', round: 'Semifinal',
      sideA: { id: 'a', name: 'Ryu' }, sideB: { id: 'b', name: 'Kaze' }, winner: null,
      status: 'running', ipponsB: ['M'], ipponsA: [],
    };
    const node = scoreContainer(individualMatch);
    expect(collectText(node)).toBe('M vs –');
  });
});
