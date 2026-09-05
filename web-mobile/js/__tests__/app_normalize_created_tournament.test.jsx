// Regression test for M10 (the app.jsx onCreated ingress): state.Tournament
// has no Competitions field, so POST /api/tournament's response body carries
// no `competitions` key. Every other tournament producer normalises to an
// array (load() assigns the fetched list; admin_shell's refresh spreads
// `competitions`; admin.jsx's mergeCompetitionsIntoTournament defaults via
// `|| []`); normalizeCreatedTournament is the fix for the one ingress that
// did not, extracted from app.jsx's onCreated handler so it is unit-testable
// without mounting App/CreateTournament.
import { describe, it, expect } from 'vitest';
import { normalizeCreatedTournament } from '../app.jsx';

describe('normalizeCreatedTournament', () => {
  it('defaults a missing competitions key to an empty array', () => {
    const raw = { id: 't1', name: 'Wizard Fresh Tournament', courts: ['A'] };
    const result = normalizeCreatedTournament(raw);
    expect(Array.isArray(result.competitions)).toBe(true);
    expect(result.competitions).toEqual([]);
  });

  it('preserves an existing competitions array untouched', () => {
    const comps = [{ id: 'c1', name: 'Men Individual' }];
    const raw = { id: 't1', name: 'T', courts: ['A'], competitions: comps };
    const result = normalizeCreatedTournament(raw);
    expect(result.competitions).toBe(comps);
  });

  it('preserves every other field on the tournament', () => {
    const raw = { id: 't1', name: 'T', courts: ['A'], theme: { primaryColor: '#fff' } };
    const result = normalizeCreatedTournament(raw);
    expect(result.id).toBe('t1');
    expect(result.name).toBe('T');
    expect(result.courts).toEqual(['A']);
    expect(result.theme).toEqual({ primaryColor: '#fff' });
  });
});
