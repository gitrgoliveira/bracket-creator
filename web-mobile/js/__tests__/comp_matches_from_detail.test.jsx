// mp-dej2: compMatches needs ONE object carrying both a competition's identity
// and its match data. GET /api/viewer/competitions/:id splits them, answering
// {config, pools, poolMatches, bracket, standings} with id/name/format/status
// nested under `config`.
//
// Handing that response straight to compMatches looks correct and silently
// returns []: `detail.status` is undefined, which trips the "setup" early
// return before a single match is read. admin_scoring_shared.jsx's withdrawal
// panel did exactly that, so after a kiken it offered no matches to award, on
// every format. The panel's list had no test, which is why it survived.
//
// These pin the recombination helper against the REAL payload shape, verified
// against a live server: top-level keys are bracket/config/courts/
// extraQualifiers/poolMatches/pools/standings, and `status` is NOT among them.
import { describe, it, expect } from 'vitest';
import { compMatches, compMatchesForCompetition } from '../viewer_utils.jsx';

// Exactly the shape the endpoint returns for a running Swiss round.
const detail = () => ({
  config: {
    id: 'swisscomp', name: 'SwissComp', format: 'swiss', status: 'pools',
    kind: 'individual', teamSize: 0,
  },
  pools: [],
  poolMatches: [
    { id: 'Swiss-R1-0', sideA: 'Alice', sideB: 'Bob', status: 'scheduled' },
    { id: 'Swiss-R1-1', sideA: 'Carol', sideB: 'Dave', status: 'scheduled' },
  ],
  bracket: null,
  standings: {},
  courts: [],
  extraQualifiers: '',
});

describe('compMatchesForCompetition; recombines the split viewer detail payload', () => {
  it('returns the competition\'s matches from a detail response', () => {
    const out = compMatchesForCompetition(detail().config, detail());
    expect(out.map((m) => m.id)).toEqual(['Swiss-R1-0', 'Swiss-R1-1']);
  });

  it('stamps the competition identity that lives under config', () => {
    const [first] = compMatchesForCompetition(detail().config, detail());
    expect(first.compId).toBe('swisscomp');
    expect(first.compFormat).toBe('swiss');
    // Derived from the id, so the row can be labelled without a pools array.
    expect(first.poolName).toBe('Swiss-R1');
  });

  it('DOCUMENTS the trap: the raw detail object yields nothing', () => {
    // Not a wish, a warning. If this ever starts returning matches, the
    // early-return contract changed and the helper's reason for existing
    // should be re-read before anyone deletes it.
    expect(compMatches(detail())).toEqual([]);
  });

  it('accepts a flat competition object, so the aggregate shape still works', () => {
    const flat = { ...detail().config, poolMatches: detail().poolMatches, pools: [], bracket: null };
    expect(compMatchesForCompetition(flat, flat).map((m) => m.id))
      .toEqual(['Swiss-R1-0', 'Swiss-R1-1']);
  });

  it('is published on window, which two admin surfaces depend on', () => {
    // admin_scoring_shared.jsx's withdrawal panel and admin_scoring_team.jsx's
    // reopen-conflict panel both read this off window rather than importing it
    // (the same way they already read window.compMatches), and the team one
    // GUARDS on it: if the publish ever disappeared, that panel would silently
    // return early and go back to naming a blocker by its raw match id.
    expect(typeof window.compMatchesForCompetition).toBe('function');
    expect(window.compMatchesForCompetition(detail().config, detail()).map((m) => m.id))
      .toEqual(['Swiss-R1-0', 'Swiss-R1-1']);
  });

  it('returns [] rather than throwing when there is no competition', () => {
    expect(compMatchesForCompetition(null, detail())).toEqual([]);
    expect(compMatchesForCompetition(undefined, undefined)).toEqual([]);
  });
});
