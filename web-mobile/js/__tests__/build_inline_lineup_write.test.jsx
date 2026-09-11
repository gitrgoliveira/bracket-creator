// bc-pnum gap closure: buildInlineLineupWrite / mergeLineupIdsForPosition
// (admin_scoring_team.jsx) compute exactly what the in-modal inline lineup
// picker (submitInlineLineup, inside TeamScoreEditorModal) sends to
// putMatchLineup. Pinned directly here rather than through the editor:
// TeamScoreEditorModal cannot be mounted in vitest (see
// tie_button_no_term.test.jsx's header) because the hook stubs only
// support initial renders and this flow needs a full interaction.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { buildInlineLineupWrite, mergeLineupIdsForPosition } from '../admin_scoring_team.jsx';

describe('mergeLineupIdsForPosition', () => {
  it('carries existing ids forward and sets the resolved id for the changed position', () => {
    const out = mergeLineupIdsForPosition({ senpo: 'mem-a' }, 'jiho', 'mem-b');
    expect(out).toEqual({ senpo: 'mem-a', jiho: 'mem-b' });
  });

  it('clears the changed position\'s id when resolvedId is falsy, even if one existed', () => {
    // The case the coordinator flagged: a stale id left behind after a
    // position is cleared would attribute a bout to whoever used to
    // occupy that slot (kachinuki retirement keys on the member id).
    const out = mergeLineupIdsForPosition({ senpo: 'mem-a', jiho: 'mem-b' }, 'jiho', null);
    expect(out).toEqual({ senpo: 'mem-a' });
    expect('jiho' in out).toBe(false);
  });

  it('tolerates a missing existingIds map', () => {
    expect(mergeLineupIdsForPosition(undefined, 'senpo', 'mem-a')).toEqual({ senpo: 'mem-a' });
    expect(mergeLineupIdsForPosition(undefined, 'senpo', null)).toEqual({});
  });
});

describe('buildInlineLineupWrite', () => {
  let originalHelpers;
  beforeEach(() => { originalHelpers = global.window.AdminLineupHelpers; });
  afterEach(() => { global.window.AdminLineupHelpers = originalHelpers; });

  const lineup = { positions: { senpo: 'Sato' }, memberIds: { senpo: 'mem-sato' } };

  it('resolves a name already on the squad to its member id (via the shared resolver)', async () => {
    const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: { jiho: 'mem-tanaka' },
      squad: [{ id: 'mem-tanaka', name: 'Tanaka' }],
    });
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite(
      'comp1', 'team1', lineup, [{ id: 'mem-tanaka', name: 'Tanaka' }], 'jiho', 'Tanaka', 'pw'
    );

    expect(resolveMemberIdsForPositions).toHaveBeenCalledWith(
      'comp1', 'team1', { jiho: 'Tanaka' }, [{ id: 'mem-tanaka', name: 'Tanaka' }], 'pw'
    );
    // Existing position untouched; the changed one added.
    expect(out.positions).toEqual({ senpo: 'Sato', jiho: 'Tanaka' });
    // Existing id carried forward; the changed one resolved.
    expect(out.memberIds).toEqual({ senpo: 'mem-sato', jiho: 'mem-tanaka' });
  });

  it('mints a new member for a name not on the squad and uses the returned id', async () => {
    const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: { taisho: 'mem-new' },
      squad: [{ id: 'mem-new', name: 'Yamada' }],
    });
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'taisho', 'Yamada', 'pw');

    expect(out.positions.taisho).toBe('Yamada');
    expect(out.memberIds.taisho).toBe('mem-new');
    expect(out.squad).toEqual([{ id: 'mem-new', name: 'Yamada' }]);
  });

  it('a FAILING mint still writes the lineup: the position\'s NAME is kept, its id is simply absent, and the pre-existing ids for other positions are untouched', async () => {
    // This is the one that matters most: the operator is never blocked by
    // a resolve/mint failure (offline venue wifi).
    const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: {}, // the resolver's own mint failed and reported it instead
      squad: [],
      failures: [{ position: 'taisho', name: 'Yamada', reason: 'offline' }],
    });
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'taisho', 'Yamada', 'pw');

    expect(out.positions).toEqual({ senpo: 'Sato', taisho: 'Yamada' }); // name written regardless
    expect(out.memberIds).toEqual({ senpo: 'mem-sato' }); // taisho absent, senpo's prior id untouched
    expect('taisho' in out.memberIds).toBe(false);
    // bc-cse: the failure is carried through, not discarded, so the caller
    // (submitInlineLineup) can warn without ever blocking this write.
    expect(out.failures).toEqual([{ position: 'taisho', name: 'Yamada', reason: 'offline' }]);
  });

  it('propagates NO failures when the resolver reports none', async () => {
    const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
      memberIds: { taisho: 'mem-new' },
      squad: [{ id: 'mem-new', name: 'Yamada' }],
      failures: [],
    });
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'taisho', 'Yamada', 'pw');
    expect(out.failures).toEqual([]);
  });

  it('clearing a position (falsy value) removes it from positions AND clears its member id, without calling the resolver', async () => {
    const resolveMemberIdsForPositions = vi.fn();
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'senpo', '', 'pw');

    expect('senpo' in out.positions).toBe(false);
    expect('senpo' in out.memberIds).toBe(false);
    expect(resolveMemberIdsForPositions).not.toHaveBeenCalled(); // nothing to resolve
  });

  it('is a no-op resolver-wise when AdminLineupHelpers.resolveMemberIdsForPositions is unavailable (older bundle / unmocked test)', async () => {
    global.window.AdminLineupHelpers = {}; // no resolver present
    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'taisho', 'Yamada', 'pw');
    expect(out.positions.taisho).toBe('Yamada');
    expect('taisho' in out.memberIds).toBe(false);
    expect(out.failures).toEqual([]);
  });

  it('defense in depth: the write still lands even when the resolver itself REJECTS outright (not just a per-position mint miss)', async () => {
    // A harder failure mode than resolveMemberIdsForPositions returning an
    // incomplete map: the helper call throws entirely. The operator must
    // still not be blocked -- the name is written, the id is simply absent.
    const resolveMemberIdsForPositions = vi.fn().mockRejectedValue(new Error('boom'));
    global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

    const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'taisho', 'Yamada', 'pw');

    expect(out.positions).toEqual({ senpo: 'Sato', taisho: 'Yamada' });
    expect(out.memberIds).toEqual({ senpo: 'mem-sato' });
    expect('taisho' in out.memberIds).toBe(false);
    // The resolver rejected outright (never even returned a failures list):
    // buildInlineLineupWrite's own defense-in-depth catch has nothing to
    // report, so failures is simply empty, not a fabricated entry.
    expect(out.failures).toEqual([]);
  });
});
