import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, fireEvent } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// bc-prow operator ruling: "After the participant list is applied, it should
// clear. A second apply should warn it will overwrite the current list."
// The paste box is an input for a new or replacement list, never a mirror
// of the saved roster: it mounts empty even when a roster exists, an Apply
// over an existing roster goes through confirmDialog first (and a cancel
// sends nothing), a successful Apply empties the box, and an empty box
// cannot be applied at all (Apply replaces the roster, so applying nothing
// would wipe it).

installParticipantsHarness();

const ROSTER = 'Carol, Dojo Carol\nDave, Dojo Dave';

function applyButtons(container) {
  return Array.from(container.querySelectorAll('button')).filter((b) => b.textContent === 'Apply changes');
}

function applyButton(container) {
  return applyButtons(container)[0];
}

async function typeList(container, value) {
  const box = container.querySelector('textarea');
  await act(async () => { fireEvent.change(box, { target: { value } }); });
}

describe('AdminParticipants Apply replaces the roster (bc-prow)', () => {
  let savedConfirm;

  beforeEach(() => {
    savedConfirm = window.confirmDialog;
  });

  afterEach(() => {
    window.confirmDialog = savedConfirm;
  });

  it('mounts with an empty box even when a roster exists, and Apply is disabled until something is typed', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition());

    expect(container.querySelector('textarea').value).toBe('');
    expect(applyButton(container).disabled).toBe(true);

    await typeList(container, ROSTER);
    expect(applyButton(container).disabled).toBe(false);
  });

  it('asks before replacing an existing roster and sends nothing when the operator cancels', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const onUpdate = vi.fn(async () => []);
    const { container } = await mountParticipants(makeParticipantsCompetition(), { onUpdate });

    await typeList(container, ROSTER);
    await act(async () => { fireEvent.click(applyButton(container)); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    const opts = window.confirmDialog.mock.calls[0][0];
    expect(opts.message).toContain('replaces the current 2 participants with the 2 in the box');
    expect(opts.confirmLabel).toBe('Replace list');
    expect(onUpdate).not.toHaveBeenCalled();
    // The typed list survives a cancel.
    expect(container.querySelector('textarea').value).toBe(ROSTER);
  });

  it('replaces the roster and clears the box when the operator confirms', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const onUpdate = vi.fn(async () => []);
    const { container } = await mountParticipants(makeParticipantsCompetition(), { onUpdate });

    await typeList(container, ROSTER);
    await act(async () => { fireEvent.click(applyButton(container)); });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    const saved = onUpdate.mock.calls[0][0].players.map((p) => p.name);
    expect(saved).toEqual(['Carol', 'Dave']);
    expect(container.querySelector('textarea').value).toBe('');
  });

  it('counts a one-person roster as one participant, and a team roster as teams', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const solo = await mountParticipants(makeParticipantsCompetition({
      players: [{ id: 'p-1', name: 'Alice', dojo: 'Dojo Alice' }],
    }));
    await typeList(solo.container, ROSTER);
    await act(async () => { fireEvent.click(applyButton(solo.container)); });
    expect(window.confirmDialog.mock.calls[0][0].message)
      .toContain('replaces the current 1 participant with the 2 in the box');
    solo.unmount();

    window.confirmDialog = vi.fn().mockResolvedValue(false);
    const teams = await mountParticipants(makeParticipantsCompetition({
      kind: 'team',
      players: [{ id: 'p-1', name: 'Tora A', dojo: 'Tora Dojo London' }, { id: 'p-2', name: 'Tora B', dojo: 'Tora Dojo London' }],
    }));
    await typeList(teams.container, 'Kamae A, Kamae Dojo');
    await act(async () => { fireEvent.click(applyButton(teams.container)); });
    expect(window.confirmDialog.mock.calls[0][0].message)
      .toContain('replaces the current 2 teams with the 1 in the box');
    teams.unmount();
  });

  // Both Apply buttons are the same action, so they carry the same disabled
  // rule and the same reason (applyDisabled / applyTitle). The bottom one
  // only differs in its render guard: there is nothing to scroll past until
  // the box holds a list.
  it('gives the repeated bottom Apply the same disabled rule and reason as the top one', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({ status: 'draw-ready' }));
    await typeList(container, ROSTER);

    const buttons = applyButtons(container);
    expect(buttons.length).toBe(2);
    for (const b of buttons) {
      expect(b.disabled).toBe(true);
      expect(b.getAttribute('title')).toBe('Discard the draw to apply roster changes');
    }
  });

  // The confirm dialog can stand open for as long as the operator takes to
  // read it. A colleague saving the roster from another desk in that window
  // re-renders this component with a fresher competition; the PUT must carry
  // THAT roster's ids and seeds (and its other fields), not the snapshot the
  // click captured, or confirming quietly reverts their save.
  it('mints from the roster as it stands when the dialog is answered, not the pre-dialog one', async () => {
    let resolveConfirm;
    window.confirmDialog = vi.fn(() => new Promise((res) => { resolveConfirm = res; }));
    const onUpdate = vi.fn(async () => []);

    const before = makeParticipantsCompetition({
      players: [
        { id: 'c1-p1', name: 'Alice', dojo: 'Dojo Alice' },
        { id: 'c1-p2', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    });
    const { container, rerender } = await mountParticipants(before, { onUpdate });

    await typeList(container, 'Alice, Dojo Alice\nCarol, Dojo Carol');
    await act(async () => { fireEvent.click(applyButton(container)); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(onUpdate).not.toHaveBeenCalled();

    // Another desk seeds Alice and renames the pool size while the dialog waits.
    const after = {
      ...before,
      poolSize: 6,
      players: [
        { id: 'c1-p1', name: 'Alice', dojo: 'Dojo Alice', seed: 3 },
        { id: 'c1-p2', name: 'Bob', dojo: 'Dojo Bob' },
      ],
    };
    await act(async () => {
      rerender(
        <window.AdminParticipants
          c={after}
          tournament={{ name: 'Spring Taikai', courts: ['A'] }}
          onUpdate={onUpdate}
          password=""
          showToast={() => {}}
          onSection={() => {}}
        />
      );
    });

    await act(async () => { resolveConfirm(true); });

    expect(onUpdate).toHaveBeenCalledTimes(1);
    const saved = onUpdate.mock.calls[0][0];
    // The colleague's other edits survive the PUT...
    expect(saved.poolSize).toBe(6);
    // ...and so does the seed they gave a name that is still on the list.
    expect(saved.players.map((p) => [p.name, p.seed])).toEqual([['Alice', 3], ['Carol', null]]);
  });

  it('does not ask when there is no roster yet', async () => {
    window.confirmDialog = vi.fn().mockResolvedValue(true);
    const onUpdate = vi.fn(async () => []);
    const { container } = await mountParticipants(makeParticipantsCompetition({ players: [] }), { onUpdate });

    await typeList(container, ROSTER);
    await act(async () => { fireEvent.click(applyButton(container)); });

    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onUpdate).toHaveBeenCalledTimes(1);
    expect(container.querySelector('textarea').value).toBe('');
  });
});
