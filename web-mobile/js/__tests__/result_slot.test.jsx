// result_slot.jsx owns ONE display rule, shared by the viewer/display
// scoreboard (match_scoreboard.jsx) and the admin team score editor
// (admin_scoring_team.jsx): a result mark naming one competitor fills that
// side's next FREE ippon slot, outside-to-inside, and never the shared centre.
import { describe, it, expect } from 'vitest';
import { resultSlot, sideSlotOrder, attributeWinnerSide, placeHtForWinner } from '../result_slot.jsx';

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

// attributeWinnerSide (bc-dmsr follow-up): "which side does a winner name",
// id-first. Mirrors Go's internal/domain.AttributeWinnerSide exactly. This is
// the fix for the finding that the Ht mark's side attribution was NAME-only,
// sideA-first, everywhere - so a same-name/different-dojo pair (legal: the
// duplicate guard keys name+dojo) placed the mark on sideA even when the
// id-carrying winnerId named sideB.
describe('attributeWinnerSide (id-first winner attribution)', () => {
  it('SAME NAME, ids disagree with name-first: attributes to B by id (the bug, now fixed)', () => {
    // Both sides are named "Tanaka Kenji" (different dojos). The id-carrying
    // winnerId names sideB; the old name-only rule (sideA-first on a name
    // collision) would have wrongly picked "a".
    const side = attributeWinnerSide({
      winnerId: 'id-mumeishi', sideAId: 'id-kenshikan', sideBId: 'id-mumeishi',
      winner: 'Tanaka Kenji', sideA: 'Tanaka Kenji', sideB: 'Tanaka Kenji',
    });
    expect(side).toBe('b');
  });

  it('ids present but winnerId matches neither side: unattributable, no mark', () => {
    const side = attributeWinnerSide({
      winnerId: 'id-someone-else', sideAId: 'id-a', sideBId: 'id-b',
      winner: 'Someone Else', sideA: 'Player A', sideB: 'Player B',
    });
    expect(side).toBeNull();
  });

  it('ids win over names even when they disagree (distinct names, id says B)', () => {
    const side = attributeWinnerSide({
      winnerId: 'id-b', sideAId: 'id-a', sideBId: 'id-b',
      winner: 'Player A', sideA: 'Player A', sideB: 'Player B',
    });
    expect(side).toBe('b');
  });

  it('the id branch fires even with an empty winner NAME, matching Go\'s unconditional ordering', () => {
    // Go's AttributeWinnerSide checks the id triple before its `winner == ""`
    // guard (the guard belongs to the name-fallback branch only). A caller
    // with an id but no name must still attribute by id, not fall through to
    // the name path's empty-winner short-circuit.
    const side = attributeWinnerSide({
      winnerId: 'id-b', sideAId: 'id-a', sideBId: 'id-b',
      winner: '', sideA: 'Player A', sideB: 'Player B',
    });
    expect(side).toBe('b');
  });

  it('any id missing falls back to the name comparison unchanged', () => {
    // sideBId missing (legacy/id-less data): the id branch never fires even
    // though winnerId and sideAId are both present.
    const side = attributeWinnerSide({
      winnerId: 'id-a', sideAId: 'id-a', sideBId: '',
      winner: 'Player A', sideA: 'Player A', sideB: 'Player B',
    });
    expect(side).toBe('a');
  });

  it('id-less: winner name matches sideA -> "a"', () => {
    expect(attributeWinnerSide({ winner: 'A', sideA: 'A', sideB: 'B' })).toBe('a');
  });

  it('id-less: winner name matches sideB -> "b"', () => {
    expect(attributeWinnerSide({ winner: 'B', sideA: 'A', sideB: 'B' })).toBe('b');
  });

  it('id-less: winner name matches BOTH sides resolves sideA-first (unchanged convention)', () => {
    expect(attributeWinnerSide({ winner: 'Tanaka Kenji', sideA: 'Tanaka Kenji', sideB: 'Tanaka Kenji' })).toBe('a');
  });

  it('id-less: winner names neither side -> unattributable', () => {
    expect(attributeWinnerSide({ winner: 'Charlie', sideA: 'Alice', sideB: 'Bob' })).toBeNull();
  });

  it('an empty winner is always unattributable, ids or not', () => {
    expect(attributeWinnerSide({ winner: '', sideA: 'A', sideB: 'B', winnerId: '', sideAId: 'id-a', sideBId: 'id-b' })).toBeNull();
    expect(attributeWinnerSide()).toBeNull();
  });
});

describe('placeHtForWinner (delegates side attribution to attributeWinnerSide)', () => {
  it('places the mark on B when ids attribute there despite a same-name sideA-first collision', () => {
    const [a, b] = placeHtForWinner(
      'Tanaka Kenji', 'Tanaka Kenji', 'Tanaka Kenji', ['M'], ['M'],
      'id-mumeishi', 'id-kenshikan', 'id-mumeishi');
    expect(a).toEqual(['M']);
    expect(b).toEqual(['M', 'Ht']);
  });

  it('leaves both arrays untouched when ids attribute to neither side', () => {
    const [a, b] = placeHtForWinner(
      'Someone Else', 'Player A', 'Player B', ['M'], ['K'],
      'id-x', 'id-a', 'id-b');
    expect(a).toEqual(['M']);
    expect(b).toEqual(['K']);
  });

  it('id-less call site (no trailing args) behaves exactly as before', () => {
    const [a, b] = placeHtForWinner('A', 'A', 'B', ['M'], ['K']);
    expect(a).toEqual(['M', 'Ht']);
    expect(b).toEqual(['K']);
  });
});
