// result_slot.jsx owns ONE display rule, shared by the viewer/display
// scoreboard (match_scoreboard.jsx) and the admin team score editor
// (admin_scoring_team.jsx): a result mark naming one competitor fills that
// side's next FREE ippon slot, outside-to-inside, and never the shared centre.
import { describe, it, expect } from 'vitest';
import { resultSlot } from '../result_slot.jsx';

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
    // Reachable, not theoretical: the editor arms hantei on ANY tied daihyosen
    // scoreline, 2-2 included. Callers render the mark beside the slots rather
    // than dropping it.
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
