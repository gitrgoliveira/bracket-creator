import { describe, it, expect } from 'vitest';
import { sameCompetitor, idOf, nameOf, competitorKey } from '../competitor_identity.jsx';

// bc-pnum: the ONE predicate for competitor-identity
// attribution. both-id / neither-id / mixed, per the operator ruling.
describe('sameCompetitor', () => {
  it('decides by id when both carry a non-empty id', () => {
    expect(sameCompetitor({ id: 'S1', name: 'Sato' }, { id: 'S1', name: 'Sato' })).toBe(true);
    expect(sameCompetitor({ id: 'S1', name: 'Sato' }, { id: 'S2', name: 'Sato' })).toBe(false);
  });

  it('decides by name when NEITHER carries an id (fully id-less legacy pair)', () => {
    expect(sameCompetitor({ id: '', name: 'Sato' }, { id: '', name: 'Sato' })).toBe(true);
    expect(sameCompetitor({ id: '', name: 'Sato' }, { id: '', name: 'Tanaka' })).toBe(false);
  });

  it('never guesses when exactly one side carries an id (the mixed case)', () => {
    expect(sameCompetitor({ id: 'S1', name: 'Sato' }, { id: '', name: 'Sato' })).toBe(false);
    expect(sameCompetitor({ id: '', name: 'Sato' }, { id: 'S1', name: 'Sato' })).toBe(false);
  });

  it('treats a bare name string as carrying no id (team sub-bouts have no ids on the wire)', () => {
    expect(sameCompetitor('Sato', 'Sato')).toBe(true);
    expect(sameCompetitor('Sato', 'Tanaka')).toBe(false);
    // Mixed: a bare string vs an id-carrying object never guesses.
    expect(sameCompetitor('Sato', { id: 'S1', name: 'Sato' })).toBe(false);
  });

  it('never matches on an empty/blank name', () => {
    expect(sameCompetitor({ id: '', name: '' }, { id: '', name: '' })).toBe(false);
    expect(sameCompetitor('', '')).toBe(false);
  });

  it('is false for null/undefined records', () => {
    expect(sameCompetitor(null, { id: 'S1', name: 'Sato' })).toBe(false);
    expect(sameCompetitor(undefined, undefined)).toBe(false);
  });
});

describe('idOf / nameOf', () => {
  it('reads id/name off an object, "" for a bare string', () => {
    expect(idOf({ id: 'S1', name: 'Sato' })).toBe('S1');
    expect(idOf('Sato')).toBe('');
    expect(nameOf({ id: 'S1', name: 'Sato' })).toBe('Sato');
    expect(nameOf('Sato')).toBe('Sato');
  });
});

// bc-pnum item 2: competitorKey is the id-decides-else-name rule as a
// single string, so sameCompetitor is expressible as "equal non-empty
// keys" and every consumer Set (watchlist, picked) builds off the same
// primitive instead of restating the rule.
describe('competitorKey', () => {
  it('keys by id ("id:"+id) whenever the record carries one', () => {
    expect(competitorKey({ id: 'S1', name: 'Sato' })).toBe('id:S1');
  });

  it('keys by name ("nm:"+name) whenever the record carries no id', () => {
    expect(competitorKey({ id: '', name: 'Sato' })).toBe('nm:Sato');
    expect(competitorKey('Sato')).toBe('nm:Sato');
  });

  it('is "" (unkeyable) for a record with neither an id nor a name', () => {
    expect(competitorKey({ id: '', name: '' })).toBe('');
    expect(competitorKey('')).toBe('');
    expect(competitorKey(null)).toBe('');
  });

  it('applies the normalizer only to the name branch, never to an id', () => {
    const upper = (s) => s.toUpperCase();
    expect(competitorKey({ id: 'S1', name: 'sato' }, upper)).toBe('id:S1');
    expect(competitorKey({ id: '', name: 'sato' }, upper)).toBe('nm:SATO');
  });

  it('sameCompetitor is exactly "equal non-empty keys" with the identity normaliser', () => {
    expect(sameCompetitor({ id: 'S1', name: 'Sato' }, { id: 'S1', name: 'Sato' }))
      .toBe(competitorKey({ id: 'S1', name: 'Sato' }) === competitorKey({ id: 'S1', name: 'Sato' }));
    // Mixed pair: an "id:" key and a "nm:" key are never equal, whatever
    // their values -- this is what makes sameCompetitor refuse to guess.
    expect(competitorKey({ id: 'S1', name: 'Sato' })).not.toBe(competitorKey({ id: '', name: 'Sato' }));
  });

  // The watchlist Set (buildWatchedSets, viewer_watchlist_core.jsx) folds
  // case/whitespace on the name branch; the picked-player Set
  // (buildPickedSets, viewer_schedule.jsx) uses the identity normaliser
  // (exact-case), on purpose (see that file's own comment on why). The two
  // conventions must differ ONLY in that folding -- same "id:"/"nm:" shape,
  // same mutual-exclusion behaviour -- never in the underlying rule.
  it('the watchlist (case-insensitive) and picked (identity) key conventions differ only in case folding', () => {
    const p = { id: '', name: 'SATO' };
    const watchlistNormalize = (s) => s.trim().toLowerCase();
    const watchlistKey = competitorKey(p, watchlistNormalize);
    const pickedKey = competitorKey(p);
    expect(watchlistKey).toBe('nm:sato');
    expect(pickedKey).toBe('nm:SATO');
    expect(watchlistKey).not.toBe(pickedKey);
    // Same shape once folded: both are name-fallback keys for the same
    // underlying name, differing only in case.
    expect(watchlistKey).toBe(pickedKey.toLowerCase());
  });
});
