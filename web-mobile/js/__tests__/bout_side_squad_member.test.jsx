// bc-pnum: extend the squad member label to the team scoring surfaces.
//
// TeamScoreEditorModal cannot be mounted in vitest (see
// tie_button_no_term.test.jsx's header / build_inline_lineup_write.test.jsx),
// so this pins the pure logic the row renderer composes at each of its two
// call sites (renderReadOnlyBout, the editable rowSides builder) via
// admin_scoring_team.jsx's `squadLabelFor`:
//
//   const member = resolveSquadMember(squad, memberId, name);
//   return member ? squadMemberLabel(teamNumber, member.index) : "";
//
// resolveBoutSideMemberId and resolveSquadMember (lineup_resolver.jsx) are
// exercised directly, then composed with squadMemberLabel exactly as
// squadLabelFor composes them, so this is a faithful pin of what an operator
// sees beside a bout row's fighter name -- not just of the two primitives in
// isolation.

import { describe, it, expect } from 'vitest';
import { resolveBoutSideMemberId, resolveSquadMember } from '../lineup_resolver.jsx';
import { squadMemberLabel } from '../squad_member_label.jsx';

// Mirrors squadLabelFor (admin_scoring_team.jsx) exactly: id-first squad
// lookup, then the ONE label-composing primitive, never restated.
function labelForBoutSide(squad, teamNumber, memberId, name) {
  const member = resolveSquadMember(squad, memberId, name);
  return member ? squadMemberLabel(teamNumber, member.index) : "";
}

const SQUAD_A = [
  { id: 'mem-1', index: 1, name: 'Sato' },
  { id: 'mem-2', index: 2, name: 'Tanaka' },
];

describe('resolveBoutSideMemberId (mirrors resolveBoutSideName priority)', () => {
  it('kachinuki numbered bout: the existing server member id wins over the lineup id', () => {
    expect(resolveBoutSideMemberId({
      isKachinuki: true, isDaihyosen: false,
      existingMemberId: 'mem-server', lineupMemberId: 'mem-lineup',
    })).toBe('mem-server');
  });

  it('kachinuki numbered bout: falls back to the lineup id when the server has none (bootstrapped bout 1)', () => {
    expect(resolveBoutSideMemberId({
      isKachinuki: true, isDaihyosen: false,
      existingMemberId: '', lineupMemberId: 'mem-lineup',
    })).toBe('mem-lineup');
  });

  it('fixed-format bout: lineup-first', () => {
    expect(resolveBoutSideMemberId({
      isKachinuki: false, isDaihyosen: false,
      existingMemberId: 'mem-old', lineupMemberId: 'mem-new',
    })).toBe('mem-new');
  });

  it('fixed-format bout: falls back to the recorded id when no lineup pick exists', () => {
    expect(resolveBoutSideMemberId({
      isKachinuki: false, isDaihyosen: false,
      existingMemberId: 'mem-recorded', lineupMemberId: '',
    })).toBe('mem-recorded');
  });

  it('daihyosen row is lineup-first even in a kachinuki match', () => {
    expect(resolveBoutSideMemberId({
      isKachinuki: true, isDaihyosen: true,
      existingMemberId: 'mem-old', lineupMemberId: 'mem-rep',
    })).toBe('mem-rep');
  });

  it('returns "" when neither source has an id', () => {
    expect(resolveBoutSideMemberId({ isKachinuki: true, isDaihyosen: false, existingMemberId: '', lineupMemberId: '' })).toBe('');
    expect(resolveBoutSideMemberId({ isKachinuki: false, isDaihyosen: false, existingMemberId: undefined, lineupMemberId: undefined })).toBe('');
  });
});

describe('resolveSquadMember (id first, exact name fallback)', () => {
  it('resolves by member id when one is available', () => {
    expect(resolveSquadMember(SQUAD_A, 'mem-2', 'Some Other Name')).toEqual(SQUAD_A[1]);
  });

  it('falls back to an exact name match when no id is available', () => {
    expect(resolveSquadMember(SQUAD_A, '', 'Tanaka')).toEqual(SQUAD_A[1]);
    expect(resolveSquadMember(SQUAD_A, undefined, 'Sato')).toEqual(SQUAD_A[0]);
  });

  it('returns null when the id does not resolve AND the name matches nobody', () => {
    expect(resolveSquadMember(SQUAD_A, 'mem-unknown', 'Nobody Here')).toBeNull();
  });

  it('returns null for an empty/not-yet-loaded squad', () => {
    expect(resolveSquadMember([], 'mem-1', 'Sato')).toBeNull();
    expect(resolveSquadMember(undefined, 'mem-1', 'Sato')).toBeNull();
  });

  it('returns null when neither an id nor a name is given', () => {
    expect(resolveSquadMember(SQUAD_A, '', '')).toBeNull();
  });
});

describe('labelForBoutSide (the composed render-site logic: resolveSquadMember + squadMemberLabel)', () => {
  it('a bout row whose fighter resolves by member id shows that member\'s label', () => {
    expect(labelForBoutSide(SQUAD_A, 'T10', 'mem-1', 'Whatever Name Rode The Row')).toBe('T10.1');
  });

  it('a bout row whose fighter resolves only by name (no member id on the row) still shows the label', () => {
    expect(labelForBoutSide(SQUAD_A, 'T10', '', 'Tanaka')).toBe('T10.2');
  });

  it('a fighter matching no squad member shows no label (name alone, no stray separator)', () => {
    expect(labelForBoutSide(SQUAD_A, 'T10', '', 'A Guest Not On The Squad')).toBe('');
  });

  it('a team with no competitor number shows no label even for a resolved squad member', () => {
    expect(labelForBoutSide(SQUAD_A, '', 'mem-1', 'Sato')).toBe('');
    expect(labelForBoutSide(SQUAD_A, undefined, 'mem-1', 'Sato')).toBe('');
  });
});

// bc-pnum: the id a TYPED fighter name resolves to. This is the one that gets
// WRITTEN onto the bout row, so it is gated harder than the label above: a row
// carrying an id is immune to a later rename, and a row carrying none has to
// be matched by a name that may by then belong to a different member.
describe('squadMemberIdForUniqueName (the id a typed name may be written under)', () => {
  let squadMemberIdForUniqueName;
  beforeEach(async () => {
    vi.resetModules();
    ({ squadMemberIdForUniqueName } = await import('../lineup_resolver.jsx'));
  });

  const SQUAD = [
    { id: 'm1', index: 1, name: 'Sato' },
    { id: 'm2', index: 2, name: 'Ito' },
    { id: 'm3', index: 3, name: '' },
  ];

  it('resolves a name exactly one member carries', () => {
    expect(squadMemberIdForUniqueName(SQUAD, 'Ito')).toBe('m2');
    expect(squadMemberIdForUniqueName(SQUAD, '  Ito  ')).toBe('m2');
  });

  it('resolves nothing for a name two teammates share', () => {
    // Grandfathered rosters hold these, and the first match is a guess that
    // would be written into the record of who fought.
    const twins = [{ id: 'm1', index: 1, name: 'Sato' }, { id: 'm2', index: 2, name: 'Sato' }];
    expect(squadMemberIdForUniqueName(twins, 'Sato')).toBe('');
  });

  it('resolves nothing for an unknown, blank or missing name, or no squad', () => {
    expect(squadMemberIdForUniqueName(SQUAD, 'Nobody')).toBe('');
    expect(squadMemberIdForUniqueName(SQUAD, '')).toBe('');
    expect(squadMemberIdForUniqueName(SQUAD, '   ')).toBe('');
    expect(squadMemberIdForUniqueName(SQUAD, undefined)).toBe('');
    expect(squadMemberIdForUniqueName(null, 'Ito')).toBe('');
    expect(squadMemberIdForUniqueName(undefined, 'Ito')).toBe('');
  });

  it('never matches the blank name of an unfilled position', () => {
    expect(squadMemberIdForUniqueName(SQUAD, '')).toBe('');
  });
});
