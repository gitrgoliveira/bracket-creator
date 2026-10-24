// Stage 6 review 12, m8: the number prefix can be changed after the draw, and
// the PUT's answer carries no numbers (they are composed when a competition is
// read). The page lays the stored answer on what it shows at once; the numbers
// it shows then must be the ones the refetch will show, which the server
// composes as prefix + the draw position (helper.CompetitorNumber,
// engine.RenumberCompetitors).
import { describe, it, expect } from 'vitest';
import { storedCompetitionInDetail } from '../admin.jsx';

const composeLikeTheServer = (prefix, n) => `${prefix}${n}`;

const detail = (prefix) => ({
  config: {
    id: 'c1', numberPrefix: prefix, courts: [],
    players: [
      { id: 'p-1', name: 'Alice', dojo: 'D', number: composeLikeTheServer(prefix, 1) },
      { id: 'p-2', name: 'Bob', dojo: 'D', number: composeLikeTheServer(prefix, 12) },
      { id: 'p-3', name: 'Carol', dojo: 'D' },
    ],
  },
});

describe('storedCompetitionInDetail numbers the competitors as the refetch will', () => {
  it('a prefix changed after the draw renumbers every competitor as the server composes it', () => {
    const stored = { numberPrefix: ' M ', players: detail('K').config.players.map(({ number: _number, ...p }) => p) };
    const next = storedCompetitionInDetail(detail('K'), 'c1', stored);
    expect(next.config.players.map((p) => p.number)).toEqual([
      composeLikeTheServer('M', 1), composeLikeTheServer('M', 12), undefined,
    ]);
  });

  it('an unchanged prefix keeps the numbers', () => {
    const stored = { numberPrefix: 'K', players: detail('K').config.players.map(({ number: _number, ...p }) => p) };
    const next = storedCompetitionInDetail(detail('K'), 'c1', stored);
    expect(next.config.players.map((p) => p.number)).toEqual(['K1', 'K12', undefined]);
  });

  it('a prefix whose stored numbers do not start with it numbers nobody rather than guess', () => {
    const prev = detail('K');
    prev.config.players[0].number = 'X1';
    const stored = { numberPrefix: 'M', players: prev.config.players.map(({ number: _number, ...p }) => p) };
    const next = storedCompetitionInDetail(prev, 'c1', stored);
    expect(next.config.players[0].number).toBeUndefined();
    expect(next.config.players[1].number).toBe('M12');
  });
});
