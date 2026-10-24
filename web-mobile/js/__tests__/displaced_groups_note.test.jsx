// The note for a write that was recorded and moved a later change of the match to its
// history (match_groups.jsx displacedGroupsNote, through keptInHistoryNote). The server
// gives two reasons for it, and the note must word each as it is:
//   - heldReason "match_closed": the later change was made after the match was finished
//     (the closed-match rule), so it was moved;
//   - no heldReason: a representative's pick stamped after the representative bout's
//     removal, which leaves it nothing to stand on. The match lacks no winner, and the
//     note must not say it does.
import { describe, it, expect } from 'vitest';
import { displacedGroupsNote, keptInHistoryNote } from '../match_groups.jsx';

const AFTER_FINISH = "Saved. A later change to points was made after the match was finished, so it was moved to the match's history.";

describe('displacedGroupsNote', () => {
  it('words a change moved because it was made after the finish', () => {
    expect(displacedGroupsNote(['points'], true)).toBe(AFTER_FINISH);
  });

  it('words a change moved for any other reason plainly', () => {
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
  it('match_closed says the change came after the finish', () => {
    expect(keptInHistoryNote({ id: 'm1', status: 'completed', displacedGroups: ['points'], heldReason: 'match_closed' })).toBe(AFTER_FINISH);
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
