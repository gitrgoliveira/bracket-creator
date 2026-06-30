import { describe, it, expect, beforeEach } from 'vitest';
import { compMatches } from '../viewer_utils.jsx';

// The bronze (3rd-place) playoff is a sibling of bracket.rounds, so it must be
// explicitly surfaced by compMatches to appear in the shiaijo court queue /
// find-my-matches / schedule alongside the final.
describe('compMatches: bronze (3rd-place) playoff', () => {
  beforeEach(() => {
    global.window = global.window || {};
    global.window.roundLabel = (i, n) => (i === n - 1 ? 'Final' : `Round ${i + 1}`);
  });

  const comp = () => ({
    id: 'c1', name: 'Naginata Cup', status: 'playoffs', format: 'playoffs',
    engi: false, kind: 'individual', teamSize: 0,
    bracket: {
      rounds: [
        [
          { id: 'm-sf-0', sideA: { id: '1', name: 'A' }, sideB: { id: '2', name: 'B' }, court: 'A', status: 'completed', winner: { id: '1', name: 'A' } },
          { id: 'm-sf-1', sideA: { id: '3', name: 'C' }, sideB: { id: '4', name: 'D' }, court: 'A', status: 'completed', winner: { id: '3', name: 'C' } },
        ],
        [{ id: 'm-final', sideA: { id: '1', name: 'A' }, sideB: { id: '3', name: 'C' }, court: 'A', status: 'scheduled' }],
      ],
      thirdPlaceMatch: { id: 'm-bronze', sideA: { id: '2', name: 'B' }, sideB: { id: '4', name: 'D' }, court: 'A', status: 'scheduled', displayRound: -1 },
    },
  });

  it('surfaces the bronze as a bracket match on the final court', () => {
    const bronze = compMatches(comp()).find((m) => m.id === 'm-bronze');
    expect(bronze).toBeTruthy();
    expect(bronze.phase).toBe('bracket');
    expect(bronze.round).toBe('3rd Place');
    expect(bronze.phaseName).toBe('3rd Place');
    expect(bronze.court).toBe('A'); // same shiaijo as the final
    expect(bronze.compId).toBe('c1');
    // Both sides present once the SF losers are seeded → eligible for the queue.
    expect(bronze.sideA.name).toBe('B');
    expect(bronze.sideB.name).toBe('D');
  });

  it('groups under its own roundIndex (one past the final) so it is not folded into the final', () => {
    const ms = compMatches(comp());
    const finalIdx = ms.find((m) => m.id === 'm-final').roundIndex;
    const bronzeIdx = ms.find((m) => m.id === 'm-bronze').roundIndex;
    expect(bronzeIdx).not.toBe(finalIdx);
  });

  it('stamps compEngi on the bronze for engi competitions', () => {
    const c = comp();
    c.engi = true;
    expect(compMatches(c).find((m) => m.id === 'm-bronze').compEngi).toBe(true);
  });

  it('omits the bronze for a preview bracket', () => {
    const c = comp();
    c.bracket.preview = true;
    expect(compMatches(c).find((m) => m.id === 'm-bronze')).toBeUndefined();
  });
});
