// bc-pnum gap closure: resolveMemberIdForName / resolveMemberIdsForPositions
// (admin_lineup.jsx) are the ONE shared resolve/mint contract the two
// match-scoped lineup writers (admin_schedule_lineup.jsx's free-text panel,
// admin_scoring_team.jsx's inline in-modal picker) use to turn a typed/
// picked NAME into a squad member id. Pinned directly here, without
// mounting anything.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { resolveMemberIdForName, resolveMemberIdsForPositions } from '../admin_lineup.jsx';

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
