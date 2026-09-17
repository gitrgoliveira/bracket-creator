// lineup_resolver.jsx (bc-dnst): the shared roster-building trio behind the
// "one position per member" rule. memberPlacedElsewhere is the predicate a
// writer checks before saving; squadRosterEntries is the ONE builder of a
// picker's offered list; rosterWithoutPlacedElsewhere filters that list
// against a lineup-so-far so a member already fielded elsewhere is not
// offered twice.

import { describe, it, expect, afterEach } from 'vitest';
import {
  memberPlacedElsewhere,
  squadRosterEntries,
  rosterWithoutPlacedElsewhere,
  resolveBoutSideDisplayName,
} from '../lineup_resolver.jsx';

describe('memberPlacedElsewhere (bc-dnst)', () => {
  it('returns the OTHER position key already holding the id', () => {
    const memberIds = { senpo: 'mem-1', jiho: 'mem-2' };
    expect(memberPlacedElsewhere(memberIds, 'taisho', 'mem-1')).toBe('senpo');
  });

  it('returns "" when the id sits only at posKey itself', () => {
    const memberIds = { senpo: 'mem-1' };
    expect(memberPlacedElsewhere(memberIds, 'senpo', 'mem-1')).toBe('');
  });

  it('returns "" when the id is nowhere in memberIds', () => {
    const memberIds = { senpo: 'mem-1' };
    expect(memberPlacedElsewhere(memberIds, 'jiho', 'mem-9')).toBe('');
  });

  it('returns "" for an empty id, even against a non-empty memberIds map', () => {
    expect(memberPlacedElsewhere({ senpo: 'mem-1' }, 'jiho', '')).toBe('');
  });
});

describe('squadRosterEntries (bc-dnst, the one builder of a lineup picker\'s list)', () => {
  afterEach(() => {
    delete window.AdminLineupHelpers;
  });

  it('lists every squad slot in index order, blank ones by label only', () => {
    const squad = [
      { id: 'm2', index: 2, name: 'Tanaka' },
      { id: 'm1', index: 1, name: 'Sato' },
      { id: 'm3', index: 3, name: '' },
    ];
    const entries = squadRosterEntries({ teamNumber: 'T10', squad, legacyNames: [], lineup: {} });
    expect(entries).toEqual([
      { id: 'm1', index: 1, name: 'Sato', label: 'T10.1' },
      { id: 'm2', index: 2, name: 'Tanaka', label: 'T10.2' },
      { id: 'm3', index: 3, name: '', label: 'T10.3' },
    ]);
  });

  it('bc-dnst: a squad present yields ONLY the squad entries, legacy names dropped entirely', () => {
    const squad = [{ id: 'm1', index: 1, name: 'Sato' }];
    const entries = squadRosterEntries({
      teamNumber: 'T10',
      squad,
      legacyNames: ['SATO', 'Ito', 'ito', 'Ito'],
      lineup: {},
    });
    // A squad member list already answers for every fighter the legacy
    // roster could offer, so mixing the two would only reintroduce the
    // stale pre-rename spellings resolveBoutSideDisplayName exists to stop
    // showing (bc-dnst).
    expect(entries).toEqual([
      { id: 'm1', index: 1, name: 'Sato', label: 'T10.1' },
    ]);
  });

  it('bc-dnst: an EMPTY squad falls back to the legacy names, case-insensitively de-duplicated', () => {
    const entries = squadRosterEntries({
      teamNumber: 'T10',
      squad: [],
      legacyNames: ['SATO', 'Ito', 'ito', 'Ito'],
      lineup: {},
    });
    // "no squad" covers both a legacy team with no id at all and a failed
    // squad fetch; either way the legacy roster is the only thing left to
    // offer, de-duplicated case-insensitively.
    expect(entries).toEqual(['SATO', 'Ito']);
  });

  it('tolerates a missing squad/legacyNames and an absent AdminLineupHelpers merge', () => {
    expect(squadRosterEntries({ teamNumber: 'T10', squad: undefined, legacyNames: undefined, lineup: {} }))
      .toEqual([]);
  });
});

describe('resolveBoutSideDisplayName (bc-dnst)', () => {
  it('resolves the CURRENT squad member name by id, over a stale stored name', () => {
    const squad = [{ id: 'm1', index: 1, name: 'Renamed Fighter' }];
    expect(resolveBoutSideDisplayName({ squad, memberId: 'm1', storedName: 'Old Spelling' }))
      .toBe('Renamed Fighter');
  });

  it('falls back to storedName when the id resolves to a still-blank member', () => {
    const squad = [{ id: 'm1', index: 1, name: '' }];
    expect(resolveBoutSideDisplayName({ squad, memberId: 'm1', storedName: 'Old Spelling' }))
      .toBe('Old Spelling');
  });

  it('falls back to storedName when there is no memberId at all', () => {
    const squad = [{ id: 'm1', index: 1, name: 'Renamed Fighter' }];
    expect(resolveBoutSideDisplayName({ squad, memberId: '', storedName: 'Old Spelling' }))
      .toBe('Old Spelling');
  });

  it('never matches a squad member by NAME alone, only by id', () => {
    const squad = [{ id: 'm1', index: 1, name: 'Old Spelling' }];
    // memberId 'm2' names nobody in the squad; the fact that a DIFFERENT
    // member happens to carry the stored name must not cause a match.
    expect(resolveBoutSideDisplayName({ squad, memberId: 'm2', storedName: 'Old Spelling' }))
      .toBe('Old Spelling');
  });

  it('returns "" when there is neither a resolved member nor a stored name', () => {
    expect(resolveBoutSideDisplayName({ squad: [], memberId: '', storedName: '' })).toBe('');
    expect(resolveBoutSideDisplayName({ squad: [], memberId: '', storedName: undefined })).toBe('');
  });
});

describe('rosterWithoutPlacedElsewhere (bc-dnst)', () => {
  it('drops an OBJECT entry whose id is placed at another position', () => {
    const roster = [
      { id: 'm1', index: 1, name: 'Sato', label: 'T10.1' },
      { id: 'm2', index: 2, name: 'Tanaka', label: 'T10.2' },
    ];
    const lineup = { positions: { senpo: 'Sato' }, memberIds: { senpo: 'm1' } };
    const filtered = rosterWithoutPlacedElsewhere(roster, lineup, 'jiho');
    expect(filtered).toEqual([{ id: 'm2', index: 2, name: 'Tanaka', label: 'T10.2' }]);
  });

  it('drops a STRING entry whose name is placed at another position', () => {
    const roster = ['Sato', 'Tanaka'];
    const lineup = { positions: { senpo: 'Sato' }, memberIds: {} };
    const filtered = rosterWithoutPlacedElsewhere(roster, lineup, 'jiho');
    expect(filtered).toEqual(['Tanaka']);
  });

  it('keeps a blank-named object entry unless ITS id is placed elsewhere', () => {
    const blank = { id: 'm3', index: 3, name: '', label: 'T10.3' };
    const roster = [blank];
    // Not placed anywhere: kept.
    expect(rosterWithoutPlacedElsewhere(roster, { positions: {}, memberIds: {} }, 'jiho')).toEqual([blank]);
    // Placed (by id) at another position: dropped, even though its blank
    // name can never match another blank slot's own empty name.
    const placedElsewhere = { positions: {}, memberIds: { senpo: 'm3' } };
    expect(rosterWithoutPlacedElsewhere(roster, placedElsewhere, 'jiho')).toEqual([]);
  });

  it('keeps an entry that is placed at posKey ITSELF (the position asking for the list)', () => {
    const entry = { id: 'm1', index: 1, name: 'Sato', label: 'T10.1' };
    const lineup = { positions: { senpo: 'Sato' }, memberIds: { senpo: 'm1' } };
    expect(rosterWithoutPlacedElsewhere([entry], lineup, 'senpo')).toEqual([entry]);
  });
});

// bc-dnst: same-name teammates. A name arm beside the id arm hid an UNPLACED
// member from every picker because a teammate elsewhere shared their display
// name, while memberPlacedElsewhere -- the write-time predicate this list must
// agree with -- is id-only and would have allowed them.
describe('rosterWithoutPlacedElsewhere: same-name teammates', () => {
  const roster = [
    { id: 'A', index: 1, name: 'Tanaka', label: 'T1.1' },
    { id: 'B', index: 5, name: 'Tanaka', label: 'T1.5' },
  ];
  const lineup = { positions: { senpo: 'Tanaka' }, memberIds: { senpo: 'A' } };

  it('still offers the unplaced namesake at another position', () => {
    const out = rosterWithoutPlacedElsewhere(roster, lineup, 'jiho');
    expect(out.map(e => e.id)).toEqual(['B']);
  });

  it('agrees with memberPlacedElsewhere, which is id-only', () => {
    expect(memberPlacedElsewhere(lineup.memberIds, 'jiho', 'B')).toBe('');
    expect(memberPlacedElsewhere(lineup.memberIds, 'jiho', 'A')).toBe('senpo');
  });
});
