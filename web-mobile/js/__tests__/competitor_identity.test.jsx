import { describe, it, expect } from 'vitest';
import { sameCompetitor, idOf, nameOf } from '../competitor_identity.jsx';

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
