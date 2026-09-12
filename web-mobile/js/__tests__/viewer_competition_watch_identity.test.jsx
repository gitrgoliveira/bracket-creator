import { describe, it, expect } from 'vitest';
import { matchInvolvesWatchedSet, buildWatchedSets } from '../viewer_watchlist_core.jsx';

// bc-pnum: matchInvolvesWatchedSet backs every match-level watch filter
// (ViewerCompetition's running/upcoming/recent, viewer_home.jsx's
// filterSecondaryOnDeck, viewer_schedule.jsx's buildWatchlistUpcoming). A
// side WITH an id must match only a watched id; a side WITHOUT one matches
// by name only -- never an OR of both for the same side.
//
// `watched` is built via buildWatchedSets (the ONE producer of the Set
// every case-insensitive watch surface consults), fed a
// realistic resolvedWatched list, rather than hand-assembled Sets: the old
// version of this file passed hand-picked Sets (an empty watchedIds
// alongside a hand-added name) that never exercised buildWatchedSets' own
// exclusivity, so it stayed green through a real production bug where the
// SEPARATE hand-rolled set builder in ViewerCompetition folded an
// id-carrying entry's name into watchedNames too.
describe('matchInvolvesWatchedSet', () => {
  it('a side carrying a real id matches only a watched id, never by name', () => {
    const watched = buildWatchedSets([{ id: 'sato-tokyo', name: 'Sato' }]);
    // Sato of Osaka: a DIFFERENT, unrelated, real-id-carrying competitor who
    // merely shares the display name with the watched Sato of Tokyo.
    const osakaMatch = { sideA: { id: 'sato-osaka', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(osakaMatch, watched)).toBe(false);
  });

  it('a side carrying the watched id matches', () => {
    const watched = buildWatchedSets([{ id: 'sato-tokyo', name: 'Sato' }]);
    const tokyoMatch = { sideA: { id: 'sato-tokyo', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(tokyoMatch, watched)).toBe(true);
  });

  it('an id-less side matches by name (unresolved bracket row) when the watched entry is itself id-less', () => {
    const watched = buildWatchedSets([{ id: '', name: 'Sato' }]);
    const idLessMatch = { sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(idLessMatch, watched)).toBe(true);
  });

  it('an id-less side with an unrelated name does not match', () => {
    const watched = buildWatchedSets([{ id: 'sato-tokyo', name: 'Sato' }]);
    const idLessMatch = { sideA: { id: '', name: 'Tanaka' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(idLessMatch, watched)).toBe(false);
  });

  // bc-pnum (MEDIUM): the case this file previously
  // missed entirely. Watching Sato-of-Tokyo (a real id) must not also list
  // an UNRELATED, id-less "Sato" row that merely shares the display name --
  // a mixed pair (watched entry has an id, this side doesn't) is never
  // guessed at by name, exactly like the highlight predicate. Before the
  // fix, ViewerCompetition's own watchedNames Set was built from EVERY
  // resolvedWatched entry's name (not just the id-less ones), so this
  // id-less Sato was listed in the on-deck banner even though the SAME
  // entry's card would correctly refuse to highlight it.
  it('an id-less side does not match an id-carrying watched entry that merely shares its name', () => {
    const watched = buildWatchedSets([{ id: 'sato-tokyo', name: 'Sato' }]);
    const idLessSameName = { sideA: { id: '', name: 'Sato' }, sideB: { id: 'other', name: 'Someone' } };
    expect(matchInvolvesWatchedSet(idLessSameName, watched)).toBe(false);
  });
});
