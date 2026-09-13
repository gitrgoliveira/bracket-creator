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

function applyButton(container) {
  return Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Apply changes');
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
