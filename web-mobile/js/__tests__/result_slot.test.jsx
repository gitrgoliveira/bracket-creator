// result_slot.jsx owns ONE display rule, shared by the viewer/display
// scoreboard (match_scoreboard.jsx) and the admin team score editor
// (admin_scoring_team.jsx): a result mark naming one competitor fills that
// side's next FREE ippon slot, outside-to-inside, and never the shared centre.
import { describe, it, expect } from 'vitest';
import { resultSlot, sideSlotOrder } from '../result_slot.jsx';

describe('resultSlot (which slot a result mark takes)', () => {
  it('takes the outer slot when the side has no points (a 0-0 bout)', () => {
    // The normal daihyosen case: one-point sudden death, so a tied one is 0-0
    // and both slots are free. Slot 0 is the outer, name-side one.
    expect(resultSlot([])).toEqual({ slot: 0, loose: false });
    expect(resultSlot(['', ''])).toEqual({ slot: 0, loose: false });
  });

  it('takes the free INNER slot when a point was already struck', () => {
    // The 1-1 hantei case: the letter keeps its outer slot and the mark fills
    // inward, giving [K][ ] vs [Ht][M] once the aka side is rendered reversed.
    expect(resultSlot(['K'])).toEqual({ slot: 1, loose: false });
    expect(resultSlot(['K', ''])).toEqual({ slot: 1, loose: false });
  });

  it('reports loose when both slots are full, so no point is overwritten', () => {
    // NOT a reachable state: sanbon-shobu ends at 2, so 2-2 cannot occur, and
    // both the editors' ippon entry and validateIppons refuse it. This
    // pins the behaviour for hand-edited files only, where the rule is that a
    // recorded point is never overwritten to make room for a mark. Each caller
    // then applies its own policy to `loose` (see result_slot.jsx).
    expect(resultSlot(['K', 'M'])).toEqual({ slot: -1, loose: true });
  });

  it('tolerates a missing or short array', () => {
    expect(resultSlot(undefined)).toEqual({ slot: 0, loose: false });
    expect(resultSlot(null)).toEqual({ slot: 0, loose: false });
  });

  it('treats only empty strings as free, not falsy-looking letters', () => {
    // Guard against a future letter set that includes "0"-ish tokens: the rule
    // is "no letter recorded", not "falsy".
    expect(resultSlot(['0'])).toEqual({ slot: 1, loose: false });
  });
});

// sideSlotOrder is the VISUAL half of the same rule: slot 0 is a side's outer
// (name-side) cell on BOTH sides, so Aka renders its pair reversed to keep that
// cell nearest the Aka name (FIK Table 2, p.16). It was previously spelled two
// different ways — `cells.toReversed()` in the scoreboard and `[1, 0]` in the
// team editor — which is one rule with two implementations and no test.
describe('sideSlotOrder (where a side\'s slots appear)', () => {
  it('renders shiro left-to-right and aka mirrored', () => {
    expect(sideSlotOrder('shiro')).toEqual([0, 1]);
    expect(sideSlotOrder('aka')).toEqual([1, 0]);
  });

  it('treats any non-aka side as the unmirrored one', () => {
    // The scoreboard passes "shiro" | "aka"; the team editor passes rs.color.
    // Anything else (undefined, a future side label) must not silently mirror.
    for (const side of [undefined, null, '', 'left', 'SHIRO']) {
      expect(sideSlotOrder(side)).toEqual([0, 1]);
    }
  });

  it('is a permutation of the two slots, never a filter', () => {
    // Guards the failure that would silently drop a recorded ippon: both
    // indices must survive, whichever side it is.
    for (const side of ['shiro', 'aka']) {
      expect([...sideSlotOrder(side)].sort()).toEqual([0, 1]);
    }
  });

  // The pairing that matters: resultSlot picks a LOGICAL slot and sideSlotOrder
  // places it. At 1-1 the mark takes slot 1 (the inner cell), which for Aka is
  // rendered FIRST — that is what produces `[K][ ] vs [Ht][M]`.
  it('puts the 1-1 mark in the cell nearest the centre on both sides', () => {
    const { slot } = resultSlot(['M', '']);
    expect(slot).toBe(1);
    expect(sideSlotOrder('aka').indexOf(slot)).toBe(0);
    expect(sideSlotOrder('shiro').indexOf(slot)).toBe(1);
  });
});
