// The note for a write that was recorded and moved a later change of the match to its
// history (match_groups.jsx displacedGroupsNote, through keptInHistoryNote). The server
// gives two reasons for it, and the note must word each as it is:
//   - heldReason "needs_winner": the later change would have left the finished match
//     without a winner, so it was moved (a knockout tie, an engi count that is not valid);
//   - no heldReason: a representative's pick stamped after the representative bout's
//     removal, which leaves it nothing to stand on. The match lacks no winner, and the
//     note must not say it does.
import { describe, it, expect } from 'vitest';
import { displacedGroupsNote, keptInHistoryNote } from '../match_groups.jsx';

const WITH_A_WINNER = "Saved. A later change to points would have left the finished match without a winner, so it was moved to the match's history.";

describe('displacedGroupsNote', () => {
  it('words a change moved because the match needs a winner as it always did', () => {
    expect(displacedGroupsNote(['points'], true)).toBe(WITH_A_WINNER);
  });

  it('words a change moved for any other reason without a winner the match does not lack', () => {
    expect(displacedGroupsNote(['repPickB'], false))
      .toBe("Saved. A later change to Shiro's pick for the representative bout was moved to the match's history.");
    expect(displacedGroupsNote(['repPickA', 'repPickB']))
      .toBe("Saved. A later change to Aka's pick for the representative bout and Shiro's pick for the representative bout was moved to the match's history.");
  });

  it('says nothing when nothing was moved', () => {
    expect(displacedGroupsNote([], true)).toBeNull();
    expect(displacedGroupsNote([])).toBeNull();
  });
});

describe('keptInHistoryNote, by the reason the answer gives', () => {
  it('needs_winner keeps today\'s wording', () => {
    expect(keptInHistoryNote({ id: 'm1', status: 'completed', displacedGroups: ['points'], heldReason: 'needs_winner' })).toBe(WITH_A_WINNER);
  });

  it('a displacement with no reason (the representative bout was removed) is worded plainly', () => {
    expect(keptInHistoryNote({ id: 'm1', status: 'running', displacedGroups: ['repPickB'] }))
      .toBe("Saved. A later change to Shiro's pick for the representative bout was moved to the match's history.");
  });

  it('a part held and a part moved for no reason say both', () => {
    expect(keptInHistoryNote({ id: 'm1', status: 'completed', heldGroups: ['encho'], displacedGroups: ['bout:2'] })).toBe(
      "Kept in the match's history, not applied: overtime. A newer change to the same thing was recorded first. "
      + "Saved. A later change to bout 2 was moved to the match's history.");
  });
});
