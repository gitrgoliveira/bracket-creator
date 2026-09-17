// bc-dnst: a `lineup_updated` SSE event must do TWO things, and the second
// one shipped unpinned.
//
// RenameTeamMember / ClearTeamMemberName broadcast this same event as a lineup
// save does (handlers_squad.go), and a rename DOES change the competition
// object: the aggregate carries the team-members map that
// resolveBoutSideDisplayName reads to name an already-fought kachinuki bout
// row. Without the list refresh that map keeps the spelling the first load
// captured, so the TV board and the OBS overlay go on showing the old name.
//
// The refresh looks redundant next to the CustomEvent dispatch, which is
// exactly how it came to be missing: fixed-order rows DO update from the
// CustomEvent alone, because their names come from the lineup, so the bug is
// invisible unless the encounter is kachinuki. Deleting the refresh left the
// entire JS suite green, so nothing in the repo noticed.

import { describe, it, expect, vi } from 'vitest';
import { handleLineupUpdated } from '../app.jsx';

describe('a lineup_updated SSE event', () => {
  it('notifies the useTeamLineups subscribers with the payload', () => {
    const notify = vi.fn();
    handleLineupUpdated({ competitionId: 'c1' }, { notify, refreshList: vi.fn() });
    expect(notify).toHaveBeenCalledTimes(1);
    const sent = notify.mock.calls[0][0];
    expect(sent.type).toBe('lineup-updated');
    expect(sent.detail).toEqual({ competitionId: 'c1' });
  });

  it('ALSO refreshes the competition list, so a rename reaches a fought bout row', () => {
    const refreshList = vi.fn();
    handleLineupUpdated({ competitionId: 'c1' }, { notify: vi.fn(), refreshList });
    // This is the assertion that was missing. Deleting the refresh from the
    // SSE branch must redden a test, or the TV board silently keeps a stale
    // fighter name after a rename.
    expect(refreshList).toHaveBeenCalledTimes(1);
  });

  it('does both for every payload shape the server can send', () => {
    // ClearTeamMemberName sends the same {competitionId} shape as a rename,
    // and a lineup delete can arrive with no detail at all.
    for (const detail of [{ competitionId: 'c1' }, undefined]) {
      const notify = vi.fn();
      const refreshList = vi.fn();
      handleLineupUpdated(detail, { notify, refreshList });
      expect(notify).toHaveBeenCalledTimes(1);
      expect(refreshList).toHaveBeenCalledTimes(1);
    }
  });
});
