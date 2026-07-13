// T5 regression: VSchedItem winner-cue on completed team matches.
//
// Spec: Q8 from mp-8b1b. The winning side's div must carry
// vsched-item__side--w. The check runs through normalizeMatch first so the
// test exercises the full client-side normalisation path:
//
//   Backend payload (string sides) -> normalizeMatch -> VSchedItem render
//
// The test also covers the daihyosen-decided case: 5 hikiwake sub-bouts + one
// position=-1 sub-bout carrying the match winner. If normalizeMatch + VSchedItem
// correctly propagate the match-level winner, no fix is needed and this file
// serves as a regression pin.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, hasClass } from './helpers/vdom.js';

const realReact = global.React;

describe('T5: VSchedItem winner cue - completed team match', () => {
  let runtime, VSchedItem, normalizeMatch;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.ipponsFromScore = vi.fn(() => []);
    global.window.matchScoreStr = vi.fn(() => '');
    global.window.queueLabelCompact = null;
    vi.resetModules();
    // Import serializers first (attaches window.normalizeMatch) then viewer_match.
    ({ normalizeMatch } = await import('../api_serializers.jsx'));
    ({ VSchedItem } = await import('../viewer_match.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.ipponsFromScore;
    delete global.window.matchScoreStr;
    delete global.window.queueLabelCompact;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  const mountSides = (m) => {
    const tree = runtime.mount(VSchedItem, { m, tweaks: {} });
    const sides = findAll(tree, n => hasClass(n, 'vsched-item__side'));
    return {
      tree, sides,
      shiroDivs: sides.filter(n => hasClass(n, 'vsched-item__side--shiro')),
      akaDivs: sides.filter(n => hasClass(n, 'vsched-item__side--aka')),
    };
  };

  it('winner string "Ryu" normalises to sideA.id and vsched-item__side--w lands on the aka side', () => {
    // Backend payload: team names as plain strings, winner as string.
    // No playerMap (team names are not in the individual-player map).
    const raw = {
      id: 'm-team-1',
      status: 'completed',
      court: 'A',
      phase: 'bracket',
      round: 'Final',
      sideA: 'Ryu',
      sideB: 'Phoenix',
      winner: 'Ryu',
    };
    const m = normalizeMatch(raw, {});

    // After normalization both sides and winner should be {id, name} objects.
    // Because neither has a UUID in the playerMap, id falls back to the name.
    expect(m.sideA).toEqual(expect.objectContaining({ name: 'Ryu' }));
    expect(m.sideB).toEqual(expect.objectContaining({ name: 'Phoenix' }));
    expect(m.winner).toEqual(expect.objectContaining({ name: 'Ryu' }));
    // The critical assertion: winner.id must equal sideA.id so aWin is truthy.
    expect(m.winner.id).toBe(m.sideA.id);
    expect(m.winner.id).not.toBe(m.sideB.id);

    // Mount VSchedItem and verify the winner CSS class.
    const { sides, shiroDivs, akaDivs } = mountSides(m);
    // 2 side divs: shiro (sideB/left) and aka (sideA/right).
    expect(sides).toHaveLength(2);
    expect(shiroDivs).toHaveLength(1);
    expect(akaDivs).toHaveLength(1);
    // Aka (sideA = "Ryu") wins: must carry vsched-item__side--w.
    expect(hasClass(akaDivs[0], 'vsched-item__side--w')).toBe(true);
    // Shiro (sideB = "Phoenix") did not win: must NOT carry the class.
    expect(hasClass(shiroDivs[0], 'vsched-item__side--w')).toBe(false);
  });

  it('winner "Phoenix" (sideB) applies vsched-item__side--w to the shiro side', () => {
    const raw = {
      id: 'm-team-2',
      status: 'completed',
      court: 'B',
      phase: 'bracket',
      round: 'SF',
      sideA: 'Ryu',
      sideB: 'Phoenix',
      winner: 'Phoenix',
    };
    const m = normalizeMatch(raw, {});
    expect(m.winner.id).toBe(m.sideB.id);
    expect(m.winner.id).not.toBe(m.sideA.id);

    const { shiroDivs, akaDivs } = mountSides(m);
    // Shiro (sideB = "Phoenix") wins.
    expect(hasClass(shiroDivs[0], 'vsched-item__side--w')).toBe(true);
    expect(hasClass(akaDivs[0], 'vsched-item__side--w')).toBe(false);
  });

  it('daihyosen-decided match: 5 hikiwake + position -1 with winner; match-level winner cue is correct', () => {
    // 5 regular sub-bouts all drawn (hikiwake), then a daihyosen (position -1)
    // won by "Ryu". The match-level winner = "Ryu".
    const hikiwakeSub = (pos) => ({ position: pos, ipponsA: [], ipponsB: [] });
    const raw = {
      id: 'm-dh-1',
      status: 'completed',
      court: 'A',
      phase: 'bracket',
      round: 'Final',
      sideA: 'Ryu',
      sideB: 'Phoenix',
      winner: 'Ryu',
      subResults: [
        hikiwakeSub(1), hikiwakeSub(2), hikiwakeSub(3), hikiwakeSub(4), hikiwakeSub(5),
        // Daihyosen: position -1, Ryu won as sideA.
        { position: -1, sideA: 'RyuRep', sideB: 'PhoenixRep', winner: 'RyuRep', ipponsA: ['M'], ipponsB: [] },
      ],
    };
    const m = normalizeMatch(raw, {});
    // IV is 0-0 for all regular sub-bouts (all draws): not our concern here,
    // but the match-level winner must still resolve correctly.
    expect(m.winner.id).toBe(m.sideA.id);

    const { shiroDivs, akaDivs } = mountSides(m);
    // Daihyosen winner is "Ryu" (sideA = Aka): must carry vsched-item__side--w.
    expect(hasClass(akaDivs[0], 'vsched-item__side--w')).toBe(true);
    expect(hasClass(shiroDivs[0], 'vsched-item__side--w')).toBe(false);
  });

  it('no winner set: neither side carries vsched-item__side--w', () => {
    const raw = {
      id: 'm-team-3',
      status: 'completed',
      court: 'C',
      phase: 'bracket',
      round: 'QF',
      sideA: 'Ryu',
      sideB: 'Phoenix',
      // No winner field.
    };
    const m = normalizeMatch(raw, {});
    const tree = runtime.mount(VSchedItem, { m, tweaks: {} });
    const sides = findAll(tree, n => hasClass(n, 'vsched-item__side'));
    const winners = sides.filter(n => hasClass(n, 'vsched-item__side--w'));
    expect(winners).toHaveLength(0);
  });
});
