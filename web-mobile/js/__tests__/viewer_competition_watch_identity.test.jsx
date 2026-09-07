import { describe, it, expect } from 'vitest';
import { matchInvolvesWatchedSet } from '../viewer_competition.jsx';

// bc-pnum (Opus review round): matchInvolvesWatchedSet backs
// ViewerCompetition's running/upcoming/recent match filtering by watchlist.
// A side WITH an id must match only a watched id; a side WITHOUT one
// matches by name only -- never an OR of both for the same side.
describe('matchInvolvesWatchedSet', () => {
  it('a side carrying a real id matches only a watched id, never by name', () => {
    const watchedIds = new Set(['sato-tokyo']);
    const watchedNames = new Set(['sato']);
    // Sato of Osaka: a DIFFERENT, unrelated, real-id-carrying competitor who
    // merely shares the display name with the watched Sato of Tokyo.
    const osakaMatch = { sideA: { id: 'sato-osaka', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(osakaMatch, watchedIds, watchedNames)).toBe(false);
  });

  it('a side carrying the watched id matches', () => {
    const watchedIds = new Set(['sato-tokyo']);
    const watchedNames = new Set(['sato']);
    const tokyoMatch = { sideA: { id: 'sato-tokyo', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(tokyoMatch, watchedIds, watchedNames)).toBe(true);
  });

  it('an id-less side matches by name (unresolved bracket row)', () => {
    const watchedIds = new Set();
    const watchedNames = new Set(['sato']);
    const idLessMatch = { sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(idLessMatch, watchedIds, watchedNames)).toBe(true);
  });

  it('an id-less side with an unrelated name does not match', () => {
    const watchedIds = new Set(['sato-tokyo']);
    const watchedNames = new Set(['sato']);
    const idLessMatch = { sideA: { id: '', name: 'Tanaka' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(idLessMatch, watchedIds, watchedNames)).toBe(false);
  });
});
