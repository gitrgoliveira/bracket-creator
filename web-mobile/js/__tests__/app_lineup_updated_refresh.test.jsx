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

  it('does both halves in the same call, not one or the other', () => {
    // The two cases above mock one collaborator each; this one asserts a
    // single call drives BOTH, which is the actual contract.
    //
    // Only one payload shape is exercised because only one exists: all six
    // EventLineupUpdated producers (four in handlers_lineup.go, two in
    // handlers_squad.go) send gin.H{"competitionId": compID}, deletes
    // included. An earlier version of this test also passed `undefined` on
    // the guess that a delete sends no detail; it does not, and a test for an
    // unreachable input only invites defensive code to satisfy it.
    const notify = vi.fn();
    const refreshList = vi.fn();
    handleLineupUpdated({ competitionId: 'c1' }, { notify, refreshList });
    expect(notify).toHaveBeenCalledTimes(1);
    expect(refreshList).toHaveBeenCalledTimes(1);
  });
});
