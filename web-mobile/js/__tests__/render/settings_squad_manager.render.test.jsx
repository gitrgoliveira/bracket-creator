import { act, fireEvent, waitFor } from '@testing-library/react';
import { describe, it, expect, beforeEach } from 'vitest';
import { installSettingsHarness, mountSettings, makeSettingsCompetition } from './settings_mount_harness.jsx';

// bc-pnum: the Squad members section on the competition Settings page --
// see internal/state/squad.go's own header for the model. A squad member
// is `{id, index, name}`; a blank name is a normal "unfilled position"
// state, never an absence. Filling/renaming (the same wire operation,
// PUT .../members/:id) stay allowed at any time; clearing (DELETE, blanks
// the name, keeps id+index) is refused once the competition has started.
// Creating a NEW member (POST) is confirmed first, per the operator's
// ruling that creation must be rare and deliberate.
//
// Mounted through the public AdminCompetition entry (AdminSettings is
// module-internal). The shared harness lives in settings_mount_harness.jsx.

installSettingsHarness({ competitionKindLabel: () => 'Team' });

function makeTeamCompetition(overrides = {}) {
  return makeSettingsCompetition({
    kind: 'team',
    teamSize: 3,
    format: 'playoffs',
    players: [{ id: 'team-1', name: 'Tora A', number: 'T10' }],
    ...overrides,
  });
}

const squadSection = (container) => container.querySelector('[data-testid="settings-squad-section"]');
const teamBlock = (container, teamId) => container.querySelector(`[data-testid="settings-squad-team-${teamId}"]`);
const memberRow = (container, memberId) => container.querySelector(`[data-testid="settings-squad-member-${memberId}"]`);
const buttonNamed = (scope, text) =>
  Array.from(scope.querySelectorAll('button')).find((b) => b.textContent.trim() === text);

// The squad API + confirmDialog mocks are shared window globals across this
// whole file (installed once by installSettingsHarness's beforeAll), so
// call counts and return values from one test would otherwise leak into
// the next -- same reason admin_competition.render.test.jsx and
// autosave_debounce.render.test.jsx reset their own shared mocks here.
beforeEach(() => {
  window.API.fetchSquads.mockClear().mockResolvedValue({});
  window.API.addTeamMember.mockClear();
  window.API.renameTeamMember.mockClear();
  window.API.clearTeamMember.mockClear();
  window.confirmDialog.mockClear().mockResolvedValue(false);
});

describe('bc-pnum: Settings "Squad members" section', () => {
  it('renders nothing for an individual competition', async () => {
    const comp = makeSettingsCompetition({ kind: 'individual' });
    const { container } = await mountSettings(comp, () => {});
    expect(squadSection(container)).toBeNull();
  });

  it('renders a team\'s members in index order with their labels', async () => {
    const comp = makeTeamCompetition();
    // Deliberately out of index order in the fetch response: the section
    // must sort, not trust the wire order.
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [
        { id: 'm2', index: 2, name: 'Ito' },
        { id: 'm1', index: 1, name: 'Sato' },
      ],
    });
    const { container } = await mountSettings(comp, () => {});

    await waitFor(() => expect(teamBlock(container, 'team-1')).not.toBeNull());
    const block = teamBlock(container, 'team-1');
    const rows = Array.from(block.querySelectorAll('[data-testid^="settings-squad-member-"]'));
    expect(rows.map((r) => r.getAttribute('data-testid'))).toEqual([
      'settings-squad-member-m1',
      'settings-squad-member-m2',
    ]);
    expect(memberRow(container, 'm1').textContent).toContain('T10.1');
    expect(memberRow(container, 'm1').textContent).toContain('Sato');
    expect(memberRow(container, 'm2').textContent).toContain('T10.2');
    expect(memberRow(container, 'm2').textContent).toContain('Ito');
  });

  it('filling a blank member calls renameTeamMember with the typed name', async () => {
    const comp = makeTeamCompetition();
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [{ id: 'm1', index: 1, name: '' }],
    });
    window.API.renameTeamMember.mockResolvedValue(true);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(memberRow(container, 'm1')).not.toBeNull());

    const row = memberRow(container, 'm1');
    expect(row.textContent).toContain('(unfilled)');
    const fillBtn = buttonNamed(row, 'Fill');
    expect(fillBtn, 'expected a "Fill" control on a blank member').toBeTruthy();
    await act(async () => { fireEvent.click(fillBtn); });

    const input = memberRow(container, 'm1').querySelector('input');
    await act(async () => { fireEvent.change(input, { target: { value: 'Sato' } }); });
    const saveBtn = buttonNamed(memberRow(container, 'm1'), 'Save');
    await act(async () => { fireEvent.click(saveBtn); });

    await waitFor(() => expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-1', 'm1', 'Sato', ''));
  });

  it('renaming a filled member calls renameTeamMember with the typed name', async () => {
    const comp = makeTeamCompetition();
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [{ id: 'm1', index: 1, name: 'Sato' }],
    });
    window.API.renameTeamMember.mockResolvedValue(true);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(memberRow(container, 'm1')).not.toBeNull());

    const renameBtn = buttonNamed(memberRow(container, 'm1'), 'Rename');
    expect(renameBtn, 'expected a "Rename" control on a filled member').toBeTruthy();
    await act(async () => { fireEvent.click(renameBtn); });

    const input = memberRow(container, 'm1').querySelector('input');
    await act(async () => { fireEvent.change(input, { target: { value: 'Sato-Renamed' } }); });
    const saveBtn = buttonNamed(memberRow(container, 'm1'), 'Save');
    await act(async () => { fireEvent.click(saveBtn); });

    await waitFor(() => expect(window.API.renameTeamMember).toHaveBeenCalledWith('c1', 'team-1', 'm1', 'Sato-Renamed', ''));
  });

  it('clearing a filled member calls clearTeamMember with the right ids, before the competition has started', async () => {
    const comp = makeTeamCompetition({ status: 'draw-ready' });
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [{ id: 'm1', index: 1, name: 'Sato' }],
    });
    window.API.clearTeamMember.mockResolvedValue(true);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(memberRow(container, 'm1')).not.toBeNull());

    const clearBtn = buttonNamed(memberRow(container, 'm1'), 'Clear name');
    expect(clearBtn, 'expected a "Clear name" control, not "Delete"').toBeTruthy();
    expect(clearBtn.disabled, 'draw-ready is not "started": clearing must still be available').toBe(false);
    await act(async () => { fireEvent.click(clearBtn); });

    await waitFor(() => expect(window.API.clearTeamMember).toHaveBeenCalledWith('c1', 'team-1', 'm1', ''));
    // The row reflects the clear locally: name goes back to "(unfilled)"
    // and the Clear control disappears (nothing left to clear).
    await waitFor(() => expect(memberRow(container, 'm1').textContent).toContain('(unfilled)'));
    expect(buttonNamed(memberRow(container, 'm1'), 'Clear name')).toBeUndefined();
  });

  it('clearing is unavailable once the competition has started, and explains why', async () => {
    const comp = makeTeamCompetition({ status: 'pools' });
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [{ id: 'm1', index: 1, name: 'Sato' }],
    });
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(memberRow(container, 'm1')).not.toBeNull());

    const clearBtn = buttonNamed(memberRow(container, 'm1'), 'Clear name');
    expect(clearBtn, 'the control stays visible, just disabled, not hidden and not silently ignored').toBeTruthy();
    expect(clearBtn.disabled).toBe(true);
    expect(
      squadSection(container).textContent,
      'the operator must be told WHY the control is unavailable, not just left with a dead button'
    ).toContain('Clearing is locked once the competition has started.');
  });

  it('surfaces a 409 that arrives anyway, rather than swallowing it', async () => {
    // The UI thought clearing was still allowed (draw-ready), but another
    // device started the competition in between: the server refuses with a
    // 409, and its message must reach the operator.
    const comp = makeTeamCompetition({ status: 'draw-ready' });
    window.API.fetchSquads.mockResolvedValue({
      'team-1': [{ id: 'm1', index: 1, name: 'Sato' }],
    });
    window.API.clearTeamMember.mockRejectedValue(
      new Error("cannot clear a team member's name once the competition has started")
    );
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(memberRow(container, 'm1')).not.toBeNull());

    const clearBtn = buttonNamed(memberRow(container, 'm1'), 'Clear name');
    await act(async () => { fireEvent.click(clearBtn); });

    await waitFor(() => expect(window.API.clearTeamMember).toHaveBeenCalled());
    await waitFor(() => expect(teamBlock(container, 'team-1').textContent).toContain('once the competition has started'));
    // The write did not land: the name must still read "Sato", not cleared
    // locally on a refused write.
    expect(memberRow(container, 'm1').textContent).toContain('Sato');
  });

  it('creating a member asks for confirmation first, naming the team and the typed name', async () => {
    const comp = makeTeamCompetition();
    window.API.fetchSquads.mockResolvedValue({ 'team-1': [] });
    window.confirmDialog.mockResolvedValue(false);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(teamBlock(container, 'team-1')).not.toBeNull());

    const input = teamBlock(container, 'team-1').querySelector('input');
    await act(async () => { fireEvent.change(input, { target: { value: 'Ito' } }); });
    const addBtn = buttonNamed(teamBlock(container, 'team-1'), 'Add member');
    await act(async () => { fireEvent.click(addBtn); });

    await waitFor(() => expect(window.confirmDialog).toHaveBeenCalledTimes(1));
    const [opts] = window.confirmDialog.mock.calls[0];
    expect(opts.message).toContain('Ito');
    expect(opts.message).toContain('Tora A');
  });

  it('does not call the API when the confirmation is declined', async () => {
    const comp = makeTeamCompetition();
    window.API.fetchSquads.mockResolvedValue({ 'team-1': [] });
    window.confirmDialog.mockResolvedValue(false);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(teamBlock(container, 'team-1')).not.toBeNull());

    const input = teamBlock(container, 'team-1').querySelector('input');
    await act(async () => { fireEvent.change(input, { target: { value: 'Ito' } }); });
    const addBtn = buttonNamed(teamBlock(container, 'team-1'), 'Add member');
    await act(async () => { fireEvent.click(addBtn); });

    await waitFor(() => expect(window.confirmDialog).toHaveBeenCalledTimes(1));
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
  });

  it('calls addTeamMember once the confirmation is accepted', async () => {
    const comp = makeTeamCompetition();
    window.API.fetchSquads.mockResolvedValue({ 'team-1': [] });
    window.API.addTeamMember.mockResolvedValue({ id: 'm-new', index: 1, name: 'Ito' });
    window.confirmDialog.mockResolvedValue(true);
    const { container } = await mountSettings(comp, () => {});
    await waitFor(() => expect(teamBlock(container, 'team-1')).not.toBeNull());

    const input = teamBlock(container, 'team-1').querySelector('input');
    await act(async () => { fireEvent.change(input, { target: { value: 'Ito' } }); });
    const addBtn = buttonNamed(teamBlock(container, 'team-1'), 'Add member');
    await act(async () => { fireEvent.click(addBtn); });

    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('c1', 'team-1', 'Ito', ''));
    await waitFor(() => expect(memberRow(container, 'm-new')).not.toBeNull());
  });
});
