// pickManualBoutName (admin_scoring_team.jsx) is the write behind a name typed or picked
// on a bout row that has no lineup slot to route through. A rename the server accepted is
// handed to onRenamed as the member the server answered, stamped, so the sheet can merge
// it over a list of the team's members that predates it. A rename the server refused, a
// pick that renames nothing and a substitution hand nothing over.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { pickManualBoutName } from '../admin_scoring_team.jsx';
import { answered } from './helpers/team_members.js';

const BLANK = { id: 'b3', index: 3, name: '' };
const NAMED = { id: 'b4', index: 4, name: 'Kai' };

let renameTeamMember;
let savedApi;

beforeEach(() => {
  savedApi = window.API;
  renameTeamMember = vi.fn((_comp, _team, id, name) => Promise.resolve(answered({ id, index: 3, name: '' }, { name })));
  window.API = { renameTeamMember };
});

afterEach(() => { window.API = savedApi; });

// The row's own sub holds the member the operator picked there (`pickedId`).
async function pick({ value = 'Ito', member, pickedId = 'b3' } = {}) {
  const spies = { updateSub: vi.fn(), onRenamed: vi.fn(), onRenameFailed: vi.fn() };
  await pickManualBoutName({
    sub: { bMemberIdOverride: pickedId },
    idx: 1,
    sideKey: 'bName',
    memberIdKey: 'bMemberIdOverride',
    squad: [BLANK, NAMED],
    compId: 'c1',
    teamId: 'team-B',
    password: 'pw',
    ...spies,
  }, value, member);
  return spies;
}

describe('pickManualBoutName hands a rename to onRenamed', () => {
  it('as the member the server answered, stamp included, once it accepted the rename', async () => {
    const { onRenamed, onRenameFailed } = await pick();

    expect(renameTeamMember).toHaveBeenCalledWith('c1', 'team-B', 'b3', 'Ito', 'pw');
    const answeredMember = await renameTeamMember.mock.results[0].value;
    expect(answeredMember.modifiedAt).toBeGreaterThan(0);
    expect(onRenamed).toHaveBeenCalledTimes(1);
    expect(onRenamed.mock.calls[0][0]).toEqual([answeredMember]);
    expect(onRenameFailed).not.toHaveBeenCalled();
  });

  it('not when the server refused it', async () => {
    const refusal = new Error('refused');
    renameTeamMember.mockRejectedValue(refusal);

    const { onRenamed, onRenameFailed } = await pick();

    expect(onRenamed).not.toHaveBeenCalled();
    expect(onRenameFailed).toHaveBeenCalledWith('Ito', refusal);
  });

  it('not for a member picked from the list, which renames nobody', async () => {
    const { onRenamed, updateSub } = await pick({ value: '', member: BLANK });

    expect(renameTeamMember).not.toHaveBeenCalled();
    expect(updateSub).toHaveBeenCalledTimes(1);
    expect(onRenamed).not.toHaveBeenCalled();
  });

  it('not for a name typed over a named member, which is a substitution', async () => {
    const { onRenamed } = await pick({ pickedId: 'b4' });

    expect(renameTeamMember).not.toHaveBeenCalled();
    expect(onRenamed).not.toHaveBeenCalled();
  });
});
