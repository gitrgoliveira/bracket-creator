import { describe, it, expect } from 'vitest';
import { courtsOutsideTournament, courtPillOptions, orphanedShiaijoError } from '../admin_helpers.jsx';

// Mirrors internal/engine/court_validation_test.go (CourtsOutsideTournament /
// ValidateCourtsInTournament). A competition's shiaijo are a SUBSET of the
// tournament's; reducing the venue's court count used to leave competitions
// holding a court that no longer exists, with no operator view and no hint
// anywhere in the UI.
describe('courtsOutsideTournament', () => {
  const cases = [
    { name: 'every court still exists', tourn: ['A', 'B', 'C'], sel: ['A', 'B'], want: [] },
    { name: 'one dropped court', tourn: ['A', 'B', 'C'], sel: ['A', 'B', 'C', 'D'], want: ['D'] },
    { name: 'several dropped courts, in the competition order', tourn: ['A'], sel: ['A', 'D', 'B'], want: ['D', 'B'] },
    { name: 'identical lists', tourn: ['A', 'B'], sel: ['A', 'B'], want: [] },
    { name: 'no allocation yet (inherits the tournament)', tourn: ['A', 'B'], sel: [], want: [] },
    // An empty tournament list means "not loaded yet", never "the venue has
    // no courts": treating it as the latter would flag every competition
    // during the bootstrap window.
    { name: 'tournament courts not loaded', tourn: [], sel: ['A'], want: [] },
    { name: 'non-array inputs', tourn: undefined, sel: null, want: [] },
    { name: 'a duplicate orphan is reported once', tourn: ['A'], sel: ['D', 'D'], want: ['D'] },
  ];

  cases.forEach(({ name, tourn, sel, want }) => {
    it(name, () => {
      expect(courtsOutsideTournament(tourn, sel)).toEqual(want);
    });
  });
});

describe('courtPillOptions', () => {
  it('renders one pill per tournament court, selected per the competition', () => {
    expect(courtPillOptions(['A', 'B', 'C'], ['A', 'C'])).toEqual([
      { court: 'A', selected: true, inTournament: true },
      { court: 'B', selected: false, inTournament: true },
      { court: 'C', selected: true, inTournament: true },
    ]);
  });

  it('appends a flagged pill for a court the tournament no longer has', () => {
    // The exact defect: stored [A B C D] under a 3-shiaijo tournament used to
    // render three pills, so the screen showed 3 courts while 4 were on disk.
    expect(courtPillOptions(['A', 'B', 'C'], ['A', 'B', 'C', 'D'])).toEqual([
      { court: 'A', selected: true, inTournament: true },
      { court: 'B', selected: true, inTournament: true },
      { court: 'C', selected: true, inTournament: true },
      { court: 'D', selected: true, inTournament: false },
    ]);
  });

  it('INVARIANT: the selected pills are exactly the stored allocation', () => {
    // What is shown is what would be saved. This is the property the settings
    // screen depends on; the count that the pairing rule reads must be the
    // count the operator can see.
    const stored = ['A', 'B', 'C', 'D'];
    const selected = courtPillOptions(['A', 'B', 'C'], stored).filter((p) => p.selected).map((p) => p.court);
    expect(selected.sort()).toEqual([...stored].sort());
  });

  it('survives a not-yet-loaded tournament without dropping the selection', () => {
    expect(courtPillOptions(undefined, ['A', 'B'])).toEqual([]);
  });
});

describe('orphanedShiaijoError', () => {
  it('returns null while every assigned shiaijo exists', () => {
    expect(orphanedShiaijoError(['A', 'B', 'C'], ['A', 'B'])).toBeNull();
    expect(orphanedShiaijoError(['A', 'B'], [])).toBeNull();
  });

  it('names the missing shiaijo and the fix, in the singular', () => {
    const err = orphanedShiaijoError(['A', 'B', 'C'], ['A', 'B', 'C', 'D']);
    expect(err).toContain('Shiaijo D');
    expect(err).toContain('is no longer part of this tournament');
    expect(err).toContain('Deselect it and save');
  });

  it('agrees in the plural', () => {
    const err = orphanedShiaijoError(['A'], ['A', 'C', 'D']);
    expect(err).toContain('Shiaijo C, D');
    expect(err).toContain('are no longer part of this tournament');
    expect(err).toContain('Deselect them and save');
  });
});
