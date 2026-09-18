// bc-pnum gap closure: buildInlineLineupWrite / mergeLineupIdsForPosition
// (lineup_resolver.jsx) compute exactly what the in-modal inline lineup
// picker (submitInlineLineup, inside TeamScoreEditorModal in
// admin_scoring_team.jsx) sends to putMatchLineup. Pinned directly here
// rather than through the editor: TeamScoreEditorModal cannot be mounted in
// vitest (see tie_button_no_term.test.jsx's header) because the hook stubs
// only support initial renders and this flow needs a full interaction.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { buildInlineLineupWrite, mergeLineupIdsForPosition } from '../lineup_resolver.jsx';

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

    // bc-dnst: the lineup's OWN memberIds (lineup.memberIds) rides as the
    // resolver's 6th (currentIds) argument on every call, member or not --
    // it is what lets a name typed into a slot PICKED by number rename the
    // ALREADY-PICKED member instead of falling back to the index default.
    expect(resolveMemberIdsForPositions).toHaveBeenCalledWith(
      'comp1', 'team1', { jiho: 'Tanaka' }, [{ id: 'mem-tanaka', name: 'Tanaka' }], 'pw', lineup.memberIds
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

  // bc-dnst: LineupNameInput's object-entry shape hands back the picked
  // squad-member itself as buildInlineLineupWrite's 8th argument. When it
  // carries an id, the write goes BY ID directly and the resolver -- which
  // only knows how to resolve/mint by NAME -- is never consulted.
  describe('with a picked squad-member (bc-dnst)', () => {
    it('writes by id directly and never calls the resolver', async () => {
      const resolveMemberIdsForPositions = vi.fn();
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const member = { id: 'mem-picked', index: 3, name: 'Picked Fighter' };
      const out = await buildInlineLineupWrite(
        'comp1', 'team1', lineup, [], 'jiho', 'Picked Fighter', 'pw', member
      );

      expect(resolveMemberIdsForPositions).not.toHaveBeenCalled();
      expect(out.positions).toEqual({ senpo: 'Sato', jiho: 'Picked Fighter' });
      expect(out.memberIds).toEqual({ senpo: 'mem-sato', jiho: 'mem-picked' });
    });

    it('keeps the position even when the picked member\'s name is empty (a blank slot picked by number)', async () => {
      const resolveMemberIdsForPositions = vi.fn();
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const blankMember = { id: 'mem-blank', index: 6, name: '' };
      const out = await buildInlineLineupWrite(
        'comp1', 'team1', lineup, [], 'jiho', '', 'pw', blankMember
      );

      expect(resolveMemberIdsForPositions).not.toHaveBeenCalled();
      // The position stays present with a blank name -- a picked blank
      // slot is a real placement, not a clear.
      expect('jiho' in out.positions).toBe(true);
      expect(out.positions.jiho).toBe('');
      expect(out.memberIds).toEqual({ senpo: 'mem-sato', jiho: 'mem-blank' });
    });

    it('a falsy value with NO member still clears the position (old path unchanged)', async () => {
      const resolveMemberIdsForPositions = vi.fn();
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'senpo', '', 'pw');

      expect(resolveMemberIdsForPositions).not.toHaveBeenCalled();
      expect('senpo' in out.positions).toBe(false);
      expect('senpo' in out.memberIds).toBe(false);
    });

    it('a typed name with no member still runs the old resolver path', async () => {
      const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
        memberIds: { jiho: 'mem-tanaka' },
        squad: [{ id: 'mem-tanaka', name: 'Tanaka' }],
      });
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const out = await buildInlineLineupWrite('comp1', 'team1', lineup, [], 'jiho', 'Tanaka', 'pw');

      expect(resolveMemberIdsForPositions).toHaveBeenCalledTimes(1);
      expect(out.positions.jiho).toBe('Tanaka');
      expect(out.memberIds.jiho).toBe('mem-tanaka');
    });
  });

  // bc-dnst: a squad member may only occupy ONE position in a lineup at a
  // time. buildInlineLineupWrite refuses the write entirely (no
  // positions/memberIds are returned, so submitInlineLineup never calls
  // putMatchLineup) rather than letting the server's own duplicate-member
  // guard (domain.TeamLineup.ValidatePositions) 400 a write the operator
  // never needed to send.
  describe('duplicate member guard (bc-dnst)', () => {
    const twoSlotLineup = {
      positions: { senpo: 'Sato', jiho: 'Tanaka' },
      memberIds: { senpo: 'mem-sato', jiho: 'mem-tanaka' },
    };

    it('picked member already elsewhere is refused, not written', async () => {
      const resolveMemberIdsForPositions = vi.fn();
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const member = { id: 'mem-sato', index: 1, name: 'Sato' };
      const out = await buildInlineLineupWrite(
        'comp1', 'team1', twoSlotLineup, [], 'chuken', 'Sato', 'pw', member
      );

      expect(out.refused).toEqual({ position: 'senpo', name: 'Sato' });
      expect(out.positions).toBeUndefined();
      expect(out.memberIds).toBeUndefined();
      // Picked-by-id path never calls the resolver anyway, but confirm the
      // refusal doesn't trigger it either.
      expect(resolveMemberIdsForPositions).not.toHaveBeenCalled();
    });

    it('a typed name resolving to a member already placed elsewhere is refused', async () => {
      const resolveMemberIdsForPositions = vi.fn().mockResolvedValue({
        // The resolver matched the typed name to the EXISTING "Sato" member,
        // who is already at senpo.
        memberIds: { chuken: 'mem-sato' },
        squad: [{ id: 'mem-sato', name: 'Sato' }],
        failures: [],
      });
      global.window.AdminLineupHelpers = { resolveMemberIdsForPositions };

      const out = await buildInlineLineupWrite(
        'comp1', 'team1', twoSlotLineup, [{ id: 'mem-sato', name: 'Sato' }], 'chuken', 'Sato', 'pw'
      );

      expect(out.refused).toEqual({ position: 'senpo', name: 'Sato' });
      expect(out.positions).toBeUndefined();
      expect(out.memberIds).toBeUndefined();
    });

    it('the same member re-picked at its OWN position is written normally', async () => {
      const member = { id: 'mem-sato', index: 1, name: 'Sato' };
      const out = await buildInlineLineupWrite(
        'comp1', 'team1', twoSlotLineup, [], 'senpo', 'Sato', 'pw', member
      );

      expect(out.refused).toBeUndefined();
      expect(out.positions).toEqual({ senpo: 'Sato', jiho: 'Tanaka' });
      expect(out.memberIds).toEqual({ senpo: 'mem-sato', jiho: 'mem-tanaka' });
    });
  });
});
