// bc-pnum gap closure: resolveMemberIdForName / resolveMemberIdsForPositions
// (admin_lineup.jsx) are the ONE shared resolve/mint contract the two
// match-scoped lineup writers (admin_schedule_lineup.jsx's free-text panel,
// admin_scoring_team.jsx's inline in-modal picker) use to turn a typed/
// picked NAME into a squad member id. Pinned directly here, without
// mounting anything.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolveMemberIdForName, resolveMemberIdsForPositions, memberIdentityWarning } from '../admin_lineup.jsx';

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
    expect(msg).toContain('squad list could not be loaded');
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

  it('uses operator vocabulary ("squad member"), never internal jargon ("member id")', () => {
    const msg = memberIdentityWarning(
      [{ position: 'senpo', name: 'Sato', reason: 'offline' }], false,
    );
    expect(msg).toContain('squad member');
    expect(msg.toLowerCase()).not.toContain('member id');
  });

  it('ignores entries with no position key defensively (never throws on malformed input)', () => {
    expect(() => memberIdentityWarning([null, {}, { name: 'Sato' }], false)).not.toThrow();
    expect(memberIdentityWarning([null, {}, { name: 'Sato' }], false)).toBe('');
    expect(memberIdentityWarning(undefined, false)).toBe('');
  });
});
