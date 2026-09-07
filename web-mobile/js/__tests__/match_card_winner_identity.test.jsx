// bc-pnum (HIGH regression from dfe6ea24): MatchCard's
// aWin/bWin used a bare `match.winner.id === match.side*.id` with no name
// fallback at all. Once buildPlayerMap started keeping id "" for an
// id-less participant (instead of inventing one from the name), a bracket
// match whose sides resolve to id-less entries got sideA.id === sideB.id
// === winner.id === "", and the naked equality trivially matched BOTH
// sides. Routed through competitor_identity.jsx's sameCompetitor so a
// mixed/empty pair can never light both (or neither, wrongly).
//
// The reactive test harness does not expand child function components
// (PlayerLine stays an unexecuted {type: PlayerLine, props} vnode), so this
// reads the isWinner PROP MatchCard passes to each PlayerLine rather than a
// rendered CSS class.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

function findAll(node, pred, acc = []) {
  if (node == null || typeof node !== 'object') return acc;
  if (Array.isArray(node)) { node.forEach((k) => findAll(k, pred, acc)); return acc; }
  if (pred(node)) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach((k) => findAll(k, pred, acc));
  return acc;
}

describe('MatchCard winner highlight: id-less/mixed pairs never light the wrong side (bc-pnum)', () => {
  const realReact = global.React;
  let runtime;
  let MatchCard;
  let PlayerLine;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    vi.resetModules();
    ({ MatchCard, PlayerLine } = await import('../bracket.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // playerLines: the two PlayerLine vnodes MatchCard renders, in sideA/sideB
  // order, exposing each one's isWinner prop.
  function playerLines(tree) {
    return findAll(tree, (n) => n?.type === PlayerLine);
  }

  it('a fully id-less (legacy) bracket match lights exactly the winner, never both', () => {
    // buildPlayerMap keeps id "" for an id-less participant; a match whose
    // sides resolve through the name-keyed map to id-less entries carries
    // this exact shape: sideA.id === sideB.id === winner.id === "".
    const match = {
      id: 'm1', status: 'completed',
      sideA: { id: '', name: 'Alice' },
      sideB: { id: '', name: 'Bob' },
      winner: { id: '', name: 'Bob' },
      ipponsA: [], ipponsB: ['M'],
    };
    const tree = runtime.mount(MatchCard, { match, variant: 1, showDojo: false });
    const lines = playerLines(tree);
    expect(lines).toHaveLength(2);
    const winners = lines.filter((l) => l.props.isWinner);
    expect(winners).toHaveLength(1);
    expect(winners[0].props.player.name).toBe('Bob');
  });

  it('a same-name/different-dojo pair with distinct real ids lights the right side', () => {
    const match = {
      id: 'm2', status: 'completed',
      sideA: { id: 'S1', name: 'Sato', dojo: 'Tokyo' },
      sideB: { id: 'S2', name: 'Sato', dojo: 'Osaka' },
      winner: { id: 'S2', name: 'Sato' },
      ipponsA: [], ipponsB: ['M'],
    };
    const tree = runtime.mount(MatchCard, { match, variant: 1, showDojo: true });
    const winners = playerLines(tree).filter((l) => l.props.isWinner);
    expect(winners).toHaveLength(1);
    expect(winners[0].props.player.dojo).toBe('Osaka');
  });

  it('a mixed pair (winner and one side share an id) still resolves the id-carrying side correctly', () => {
    // The winner record HAS a real id, and sideB's does too (and matches);
    // sideA's resolved record has no id at all (a bracket row
    // api_serializers.jsx could not resolve). sideB must still win by id.
    const match = {
      id: 'm3', status: 'completed',
      sideA: { id: '', name: 'Sato' },
      sideB: { id: 'S2', name: 'Tanaka' },
      winner: { id: 'S2', name: 'Tanaka' },
      ipponsA: [], ipponsB: ['M'],
    };
    const tree = runtime.mount(MatchCard, { match, variant: 1, showDojo: false });
    const winners = playerLines(tree).filter((l) => l.props.isWinner);
    expect(winners).toHaveLength(1);
    expect(winners[0].props.player.name).toBe('Tanaka');
  });

  it('a mixed pair where NEITHER side matches the id-carrying winner lights nothing', () => {
    const match = {
      id: 'm4', status: 'completed',
      sideA: { id: '', name: 'Sato' },
      sideB: { id: '', name: 'Tanaka' },
      winner: { id: 'S9', name: 'Sato' }, // winner carries an id; neither side does.
      ipponsA: [], ipponsB: ['M'],
    };
    const tree = runtime.mount(MatchCard, { match, variant: 1, showDojo: false });
    const winners = playerLines(tree).filter((l) => l.props.isWinner);
    expect(winners).toHaveLength(0);
  });
});
