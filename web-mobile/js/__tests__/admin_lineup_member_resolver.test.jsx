// bc-pnum gap closure: resolveMemberIdForName / resolveMemberIdsForPositions
// (admin_lineup.jsx) are the ONE shared resolve/mint contract the two
// match-scoped lineup writers (admin_schedule_lineup.jsx's free-text panel,
// admin_scoring_team.jsx's inline in-modal picker) use to turn a typed/
// picked NAME into a squad member id. Pinned directly here, without
// mounting anything.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolveMemberIdForName, resolveMemberIdsForPositions, memberIdentityWarning, blankMemberForPosition } from '../admin_lineup.jsx';

const SQUAD = [
  { id: 'mem-sato', index: 0, name: 'Sato' },
  { id: 'mem-tanaka', index: 1, name: 'Tanaka' },
];

describe('resolveMemberIdForName', () => {
  it('finds a member by an exact name match', () => {
    expect(resolveMemberIdForName(SQUAD, 'Sato')).toEqual(SQUAD[0]);
  });

  it('finds a member under the same normalization the server duplicate-name floor uses (case/whitespace)', () => {
    // bc-tmdup: squad-wide uniqueness is enforced under normalization, so a
    // case/whitespace-different typed name still resolves to the ONE
    // existing member rather than reading as "not found".
    expect(resolveMemberIdForName(SQUAD, '  sato  ')).toEqual(SQUAD[0]);
  });

  it('returns null when no member matches', () => {
    expect(resolveMemberIdForName(SQUAD, 'Suzuki')).toBeNull();
  });

  it('returns null for an empty name or an empty/missing squad', () => {
    expect(resolveMemberIdForName(SQUAD, '')).toBeNull();
    expect(resolveMemberIdForName([], 'Sato')).toBeNull();
    expect(resolveMemberIdForName(undefined, 'Sato')).toBeNull();
  });
});

describe('resolveMemberIdsForPositions', () => {
  let originalAPI;
  beforeEach(() => { originalAPI = global.window.API; });
  afterEach(() => { global.window.API = originalAPI; });

  it('resolves a name already on the squad WITHOUT minting', async () => {
    const addTeamMember = vi.fn();
    global.window.API = { addTeamMember };
    const { memberIds, squad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, SQUAD, 'pw'
    );
    expect(memberIds).toEqual({ senpo: 'mem-sato' });
    expect(squad).toBe(SQUAD); // unchanged: no mint needed
    expect(addTeamMember).not.toHaveBeenCalled();
  });

  it('mints a new member for a name not on the squad and uses the returned id', async () => {
    const minted = { id: 'mem-new', index: 2, name: 'Yamada' };
    const addTeamMember = vi.fn().mockResolvedValue(minted);
    global.window.API = { addTeamMember };
    const { memberIds, squad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { taisho: 'Yamada' }, SQUAD, 'pw'
    );
    expect(addTeamMember).toHaveBeenCalledWith('comp1', 'team1', 'Yamada', 'pw');
    expect(memberIds).toEqual({ taisho: 'mem-new' });
    expect(squad).toEqual([...SQUAD, minted]);
  });

  it('a FAILING mint omits that position from memberIds and does not throw, while still resolving the OTHER positions', async () => {
    // This is the one that matters most: the operator is never blocked by
    // a mint failure (offline venue wifi). A resolvable position still
    // gets its id even when a sibling position's mint fails.
    const addTeamMember = vi.fn().mockRejectedValue(new Error('offline'));
    global.window.API = { addTeamMember };
    const { memberIds, squad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato', taisho: 'Yamada' }, SQUAD, 'pw'
    );
    expect(memberIds).toEqual({ senpo: 'mem-sato' }); // taisho's mint failed: simply absent
    expect(memberIds.taisho).toBeUndefined();
    expect(squad).toBe(SQUAD); // the failed mint never touched the squad copy
  });

  it('bc-cse: a FAILING mint is reported in `failures`, carrying the position, the name, and the server\'s own message', async () => {
    // The resolver used to discard this entirely (see resolveMemberIdsForPositions's
    // doc comment history). It must now report it WITHOUT throwing and WITHOUT
    // changing the write-proceeds-anyway behaviour pinned by the test above.
    const addTeamMember = vi.fn().mockRejectedValue(new Error('sato normalises onto an existing member'));
    global.window.API = { addTeamMember };
    const { memberIds, failures } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { taisho: 'Yamada' }, SQUAD, 'pw'
    );
    expect(memberIds.taisho).toBeUndefined();
    expect(failures).toEqual([
      { position: 'taisho', name: 'Yamada', reason: 'sato normalises onto an existing member' },
    ]);
  });

  it('bc-cse: a resolvable position reports no failure at all, and `failures` is empty when nothing fails', async () => {
    const addTeamMember = vi.fn();
    global.window.API = { addTeamMember };
    const { failures } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, SQUAD, 'pw'
    );
    expect(failures).toEqual([]);
  });

  it('bc-cse: reports one failures entry PER failed position, alongside the successful ones, never throwing', async () => {
    const addTeamMember = vi.fn()
      .mockRejectedValueOnce(new Error('offline'))
      .mockRejectedValueOnce(new Error('team not found'));
    global.window.API = { addTeamMember };
    const { memberIds, failures } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato', taisho: 'Yamada', jiho: 'Ito' }, SQUAD, 'pw'
    );
    expect(memberIds).toEqual({ senpo: 'mem-sato' });
    expect(failures).toEqual([
      { position: 'taisho', name: 'Yamada', reason: 'offline' },
      { position: 'jiho', name: 'Ito', reason: 'team not found' },
    ]);
  });

  it('mints a repeated new name only ONCE (sequential, not racing a duplicate add)', async () => {
    const minted = { id: 'mem-new', index: 2, name: 'Yamada' };
    const addTeamMember = vi.fn().mockResolvedValue(minted);
    global.window.API = { addTeamMember };
    const { memberIds } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Yamada', jiho: 'Yamada' }, SQUAD, 'pw'
    );
    expect(addTeamMember).toHaveBeenCalledTimes(1);
    expect(memberIds).toEqual({ senpo: 'mem-new', jiho: 'mem-new' });
  });

  it('skips blank/whitespace-only names entirely (nothing to resolve)', async () => {
    const addTeamMember = vi.fn();
    global.window.API = { addTeamMember };
    const { memberIds } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: '   ' }, SQUAD, 'pw'
    );
    expect(memberIds).toEqual({});
    expect(addTeamMember).not.toHaveBeenCalled();
  });

  // bc-dnst (operator ruling 2026-09-15): the number on a bout row belongs to
  // the SQUAD MEMBER, not the row. A fresh team is seeded with one blank
  // member per position (an id + index, no name yet), so a name typed into
  // an unnamed position must FILL that member's blank slot (rename, keeping
  // its id and its number) rather than minting a fresh, number-less member.
  const BLANK_SQUAD = [
    { id: 'm1', index: 1, name: '' },
    { id: 'm2', index: 2, name: '' },
  ];

  it('bc-dnst: a name typed into a named position (senpo) renames the blank member seeded at its index, never mints', async () => {
    const addTeamMember = vi.fn();
    const renameTeamMember = vi.fn().mockResolvedValue(true);
    global.window.API = { addTeamMember, renameTeamMember };
    const { memberIds, squad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, BLANK_SQUAD, 'pw'
    );
    expect(renameTeamMember).toHaveBeenCalledWith('comp1', 'team1', 'm1', 'Sato', 'pw');
    expect(addTeamMember).not.toHaveBeenCalled();
    expect(memberIds).toEqual({ senpo: 'm1' });
    expect(squad.find(m => m.id === 'm1').name).toBe('Sato');
  });

  it('bc-dnst: a numeric position key ("2") renames the blank member seeded at that index', async () => {
    const addTeamMember = vi.fn();
    const renameTeamMember = vi.fn().mockResolvedValue(true);
    global.window.API = { addTeamMember, renameTeamMember };
    const { memberIds, squad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { '2': 'Ito' }, BLANK_SQUAD, 'pw'
    );
    expect(renameTeamMember).toHaveBeenCalledWith('comp1', 'team1', 'm2', 'Ito', 'pw');
    expect(addTeamMember).not.toHaveBeenCalled();
    expect(memberIds).toEqual({ '2': 'm2' });
    expect(squad.find(m => m.id === 'm2').name).toBe('Ito');
  });

  it('bc-dnst: no blank member at that index falls back to minting, exactly as before', async () => {
    const minted = { id: 'mem-new', index: 1, name: 'Sato' };
    const addTeamMember = vi.fn().mockResolvedValue(minted);
    const renameTeamMember = vi.fn();
    global.window.API = { addTeamMember, renameTeamMember };
    const squad = [{ id: 'm1', index: 1, name: 'Ito' }]; // already named: not a blank slot
    const { memberIds } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, squad, 'pw'
    );
    expect(renameTeamMember).not.toHaveBeenCalled();
    expect(addTeamMember).toHaveBeenCalledWith('comp1', 'team1', 'Sato', 'pw');
    expect(memberIds).toEqual({ senpo: 'mem-new' });
  });

  it('bc-dnst: a name already on the squad resolves without renaming or minting', async () => {
    const addTeamMember = vi.fn();
    const renameTeamMember = vi.fn();
    global.window.API = { addTeamMember, renameTeamMember };
    const squad = [{ id: 'm1', index: 1, name: 'Sato' }];
    const { memberIds } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, squad, 'pw'
    );
    expect(renameTeamMember).not.toHaveBeenCalled();
    expect(addTeamMember).not.toHaveBeenCalled();
    expect(memberIds).toEqual({ senpo: 'm1' });
  });

  it('bc-dnst: a rename failure is reported in `failures`, leaving the position unresolved', async () => {
    const addTeamMember = vi.fn();
    const renameTeamMember = vi.fn().mockRejectedValue(new Error('offline'));
    global.window.API = { addTeamMember, renameTeamMember };
    const { memberIds, failures } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Sato' }, BLANK_SQUAD, 'pw'
    );
    expect(memberIds.senpo).toBeUndefined();
    expect(failures).toEqual([{ position: 'senpo', name: 'Sato', reason: 'offline' }]);
    expect(addTeamMember).not.toHaveBeenCalled();
  });

  // bc-dnst (currentIds, the 6th argument): a name typed into a slot that
  // was PICKED BY NUMBER (its memberId already recorded on this position,
  // e.g. via LineupNameInput's object-entry roster) must rename THAT
  // member, even when it is not the position's own index default. m6 here
  // is a reserve at index 6 -- nothing to do with senpo's own default (m1,
  // index 1) -- so this pins that currentIds is consulted BEFORE the
  // index-default fallback, not merely as a tie-break when they agree.
  it('bc-dnst: currentIds naming a blank member (not the index default) renames THAT member instead', async () => {
    const addTeamMember = vi.fn();
    const renameTeamMember = vi.fn().mockResolvedValue(true);
    global.window.API = { addTeamMember, renameTeamMember };
    const squad = [
      { id: 'm1', index: 1, name: '' }, // senpo's own index default: must NOT be touched
      { id: 'm6', index: 6, name: '' }, // the reserve actually picked into senpo
    ];
    const { memberIds, squad: nextSquad } = await resolveMemberIdsForPositions(
      'comp1', 'team1', { senpo: 'Picked Name' }, squad, 'pw', { senpo: 'm6' }
    );
    expect(renameTeamMember).toHaveBeenCalledWith('comp1', 'team1', 'm6', 'Picked Name', 'pw');
    expect(addTeamMember).not.toHaveBeenCalled();
    expect(memberIds).toEqual({ senpo: 'm6' });
    expect(nextSquad.find(m => m.id === 'm6').name).toBe('Picked Name');
    expect(nextSquad.find(m => m.id === 'm1').name).toBe('');
  });
});

// bc-dnst: blankMemberForPosition is the pure lookup resolveMemberIdsForPositions
// uses internally (rename vs mint) and commitAdd (admin_lineup.jsx's own
// "+ Add new member…" option) now shares, so the two "does this slot already
// have a home" checks cannot drift.
describe('blankMemberForPosition', () => {
  const BLANK_SQUAD = [
    { id: 'm1', index: 1, name: '' },
    { id: 'm2', index: 2, name: '' },
  ];

  it('finds the blank member seeded at the position\'s own index', () => {
    expect(blankMemberForPosition(BLANK_SQUAD, 'senpo', {})).toEqual(BLANK_SQUAD[0]);
    expect(blankMemberForPosition(BLANK_SQUAD, '2', {})).toEqual(BLANK_SQUAD[1]);
  });

  it('prefers the member named by currentIds over the index default', () => {
    const squad = [
      { id: 'm1', index: 1, name: '' }, // senpo's own index default
      { id: 'm6', index: 6, name: '' }, // the reserve actually picked into senpo
    ];
    expect(blankMemberForPosition(squad, 'senpo', { senpo: 'm6' })).toEqual(squad[1]);
  });

  it('returns null when the position has no blank slot (already named, or none at that index)', () => {
    const namedSquad = [{ id: 'm1', index: 1, name: 'Ito' }];
    expect(blankMemberForPosition(namedSquad, 'senpo', {})).toBeNull();
    expect(blankMemberForPosition(BLANK_SQUAD, 'taisho', {})).toBeNull();
  });

  it('tolerates a missing/empty squad', () => {
    expect(blankMemberForPosition([], 'senpo', {})).toBeNull();
    expect(blankMemberForPosition(undefined, 'senpo', {})).toBeNull();
  });

  // bc-cse: the seeded blank member at a position's own index is only free
  // to take that position's name when it is not already fielded ELSEWHERE
  // in the lineup (renaming it there would name the wrong row's fighter).
  it('does NOT return the index-seeded blank member when currentIds already holds its id at ANOTHER position', () => {
    const squad = [{ id: 'm1', index: 1, name: '' }];
    expect(blankMemberForPosition(squad, 'senpo', { taisho: 'm1' })).toBeNull();
  });

  it('DOES return the index-seeded blank member when currentIds holds it at THIS SAME position, or nowhere at all', () => {
    const squad = [{ id: 'm1', index: 1, name: '' }];
    expect(blankMemberForPosition(squad, 'senpo', { senpo: 'm1' })).toEqual(squad[0]);
    expect(blankMemberForPosition(squad, 'senpo', {})).toEqual(squad[0]);
  });
});

// bc-cse gap closure: memberIdentityWarning is the ONE composer all three
// lineup-writing surfaces (admin_lineup.jsx, admin_schedule_lineup.jsx,
// admin_scoring_team.jsx) call to tell the operator a save's squad-member
// attachment fell short, without ever blocking the save that already
// succeeded.
describe('memberIdentityWarning', () => {
  it('returns "" when nothing failed and the squad loaded fine', () => {
    expect(memberIdentityWarning([], false)).toBe('');
  });

  it('names the affected position and fighter, and states the lineup was saved and scores are unaffected', () => {
    const msg = memberIdentityWarning(
      [{ position: 'senpo', name: 'Sato', reason: 'sato normalises onto an existing member' }],
      false,
    );
    expect(msg).toContain('Lineup saved');
    expect(msg).toContain('Senpo');
    expect(msg).toContain('Sato');
    expect(msg).toContain('sato normalises onto an existing member');
    expect(msg).toContain('Scores will still record normally');
  });

  it('names EVERY affected position and fighter when more than one position failed, not just a count', () => {
    const msg = memberIdentityWarning(
      [
        { position: 'senpo', name: 'Sato', reason: 'request timed out' },
        { position: 'taisho', name: 'Tanaka', reason: 'team not found' },
      ],
      false,
    );
    expect(msg).toContain('Senpo');
    expect(msg).toContain('Sato');
    expect(msg).toContain('Taisho');
    expect(msg).toContain('Tanaka');
    // Never a bare count in place of naming who/what was affected.
    expect(msg).not.toMatch(/\b2 positions\b/i);
  });

  it('labels a numeric (non-FIK) position key plainly', () => {
    const msg = memberIdentityWarning([{ position: '3', name: 'Ito', reason: 'offline' }], false);
    expect(msg).toContain('Ito');
    expect(msg).toContain('3');
  });

  it('produces the ONE root-cause sentence, not a per-position list, when the squad itself could not be loaded', () => {
    // squadUnavailable wins even when failures also carries entries: those
    // per-position reasons would all be misleading duplicates of the one
    // real cause (every name looked "new" because the squad never loaded).
    const msg = memberIdentityWarning(
      [
        { position: 'senpo', name: 'Sato', reason: 'duplicate name' },
        { position: 'taisho', name: 'Tanaka', reason: 'duplicate name' },
      ],
      true,
    );
    expect(msg).toContain('Lineup saved');
    expect(msg).toContain('team member list could not be loaded');
    expect(msg).toContain('Scores will still record normally');
    expect(msg).not.toContain('Sato');
    expect(msg).not.toContain('Tanaka');
    expect(msg).not.toContain('Senpo');
  });

  it('the squad-unavailable sentence alone is exactly ONE sentence naming the cause, not a list', () => {
    const msg = memberIdentityWarning([], true);
    // "not a per-position list": no position/name placeholders appear, and
    // the message reads as a single flowing warning rather than enumerated
    // items (no semicolons/bullets joining multiple clauses).
    expect(msg).not.toMatch(/;/);
  });

  it('never uses the word "live" or an em-dash (repo copy rules)', () => {
    const withFailure = memberIdentityWarning(
      [{ position: 'senpo', name: 'Sato', reason: 'offline' }], false,
    );
    const withSquadDown = memberIdentityWarning([], true);
    for (const msg of [withFailure, withSquadDown]) {
      expect(msg.toLowerCase()).not.toContain('live');
      expect(msg).not.toContain('—'); // em-dash
    }
  });

  it('uses operator vocabulary ("team member"), never internal jargon ("member id")', () => {
    const msg = memberIdentityWarning(
      [{ position: 'senpo', name: 'Sato', reason: 'offline' }], false,
    );
    expect(msg).toContain('team member');
    expect(msg.toLowerCase()).not.toContain('member id');
  });

  it('ignores entries with no position key defensively (never throws on malformed input)', () => {
    expect(() => memberIdentityWarning([null, {}, { name: 'Sato' }], false)).not.toThrow();
    expect(memberIdentityWarning([null, {}, { name: 'Sato' }], false)).toBe('');
    expect(memberIdentityWarning(undefined, false)).toBe('');
  });
});
