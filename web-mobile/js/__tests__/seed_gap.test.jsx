import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
  seedGapDiagnosis,
  seededRanks,
  competitionDrawBlocker,
  partitionStartableCompetitions,
} from '../admin_helpers.jsx';

// JS half of the shared Go/JS golden table for the incomplete-seeding message:
// see the `_comment` in seed_gap_messages.json for why the table is shared and
// what "" means in it. Go half: TestSeedGapDiagnosis_GoldenTable in
// internal/helper/seed_warnings_test.go.
describe('seedGapDiagnosis Go/JS mirror', () => {
  const table = JSON.parse(
    readFileSync(
      resolve(__dirname, '..', '..', '..', 'internal', 'helper', 'testdata', 'seed_gap_messages.json'),
      'utf8'
    )
  );

  // Load-bearing: it.each over an empty array silently produces zero tests
  // (no red), so a degraded table needs its own failure.
  it('the shared golden table is present and non-empty', () => {
    expect(
      table.cases?.length,
      'internal/helper/testdata/seed_gap_messages.json parsed to zero cases: the mirror would assert nothing'
    ).toBeGreaterThan(0);
  });

  it.each(table.cases)('$why', ({ ranks, diagnosis }) => {
    expect(seedGapDiagnosis(ranks)).toBe(diagnosis);
  });
});

describe('seededRanks', () => {
  // The panel's own updateSeed maps a cleared or non-positive input to null, so
  // "unseeded" arrives in several shapes and none of them is a rank.
  it('takes only the ranks an operator actually typed', () => {
    expect(seededRanks([
      { name: 'A', seed: 2 },
      { name: 'B', seed: null },
      { name: 'C' },
      { name: 'D', seed: 1 },
    ])).toEqual([2, 1]);
  });

  it('survives a missing or empty roster', () => {
    expect(seededRanks(undefined)).toEqual([]);
    expect(seededRanks([])).toEqual([]);
    expect(seededRanks([null])).toEqual([]);
  });
});

// The console must refuse a draw the server would refuse, BEFORE the request
// is fired. Pre-fix the operator clicked "Generate draw", the request came back
// 400, and the only trace was a toast that expires.
describe('competitionDrawBlocker: seeding', () => {
  const comp = (players, over = {}) => ({
    id: 'c1', name: 'C1', format: 'mixed', status: 'setup',
    courts: ['A', 'B'], players, ...over,
  });
  const seeded = (...ranks) => ranks.map((seed, i) => ({ name: `P${i + 1}`, seed }));
  const venue = ['A', 'B'];

  it('lets a complete seeding through', () => {
    expect(competitionDrawBlocker(comp(seeded(1, 2, 3)), venue)).toBeNull();
  });

  it('lets an unseeded competition through: no seeds is a valid seeding', () => {
    expect(competitionDrawBlocker(comp(seeded(null, null)), venue)).toBeNull();
  });

  it('blocks the half-typed seeding the panel produces, naming the ranks', () => {
    const blocker = competitionDrawBlocker(comp(seeded(null, null, null, 4)), venue);
    expect(blocker.reason).toContain('seed ranks 1, 2 and 3 have not been set');
    expect(blocker.reason).toContain('rank 4 has');
  });

  it('sends the operator to the seeding panel, not to Settings', () => {
    const blocker = competitionDrawBlocker(comp(seeded(null, 3)), venue);
    expect(blocker.section).toBe('participants');
    expect(blocker.fix).toContain('Participants & seeds');
    // The regression this whole shape exists to prevent: a hard-coded
    // "Reassign shiaijo" tail sending them to the wrong screen.
    expect(blocker.fix).not.toContain('shiaijo');
  });

  it('blocks a duplicated rank too, in its own words rather than as a gap', () => {
    const blocker = competitionDrawBlocker(comp(seeded(1, 2, 2)), venue);
    expect(blocker.reason).toContain('seed rank 2');
    expect(blocker.reason).toContain('used more than once');
    expect(blocker.reason).not.toContain('have not been set');
    expect(blocker.section).toBe('participants');
  });

  // A competition can break both rules at once. The shiaijo allocation is a
  // structural property; the seeding may still be halfway through being typed,
  // so the structural one is named first.
  it('names the shiaijo rule first when both are broken', () => {
    const blocker = competitionDrawBlocker(
      comp(seeded(null, 4), { courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(blocker.reason).toContain('shiaijo cannot be paired');
    expect(blocker.section).toBe('settings');
  });

  // League and Swiss draw no bracket, but their seeds are still read, so a
  // half-typed seeding must block them the same way.
  it.each(['league', 'swiss', 'playoffs', 'mixed'])('applies to %s', (format) => {
    expect(competitionDrawBlocker(comp(seeded(null, 4), { format }), venue).reason)
      .toContain('not been set');
  });

  it('answers null for no competition at all', () => {
    expect(competitionDrawBlocker(null, venue)).toBeNull();
  });
});

// "Start all" must not offer a competition the server would refuse, and must
// name why rather than dropping it: the count in the button and the cards on
// the dashboard have to keep adding up.
describe('partitionStartableCompetitions with an incomplete seeding', () => {
  it('blocks the competition and carries its remedy in the reason', () => {
    const comps = [
      { id: 'ok', name: 'OK', format: 'mixed', status: 'setup', courts: ['A', 'B'],
        players: [{ name: 'A', seed: 1 }, { name: 'B', seed: 2 }] },
      { id: 'gap', name: 'Gap', format: 'mixed', status: 'setup', courts: ['A', 'B'],
        players: [{ name: 'C' }, { name: 'D', seed: 3 }] },
    ];
    const { startable, blocked } = partitionStartableCompetitions(comps, ['A', 'B']);
    expect(startable.map(c => c.id)).toEqual(['ok']);
    expect(blocked).toHaveLength(1);
    expect(blocked[0].comp.id).toBe('gap');
    // reason + fix in one string: the modal lists several competitions blocked
    // for possibly different reasons and cannot write a single tail itself.
    expect(blocked[0].reason).toContain('have not been set');
    expect(blocked[0].reason).toContain('Participants & seeds');
  });
});
