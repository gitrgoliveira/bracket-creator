import { describe, it, expect } from 'vitest';
import { act, fireEvent } from '@testing-library/react';
import { ENGI_SOLO_PROBLEM, ENGI_SOLO_PROBLEM_LOCKED } from '../../engi_entry.jsx';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// The stored-rows notice: a roster saved under one setting of "One competitor
// per side" can hold rows the other refuses. The Participants page names those
// rows (judged by engiEntryProblem) in an inline role="status" notice and says
// how to fix them with the steps the status still allows (the setting locks
// once the draw is made); it never blocks.

installParticipantsHarness();

const PAIR_ROW = { id: 'p-1', name: 'Alice Smith - Bob Jones', dojo: 'Dojo A' };
const SOLO_ROW = { id: 'p-2', name: 'Akira Tanaka', dojo: 'Dojo B' };

const notice = (container) => container.querySelector('[data-testid="engi-shape-notice"]');

describe('AdminParticipants stored-rows notice for an engi competition', () => {
  it('a solo competition holding a pair row names that row and not the fitting one', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: true,
      players: [PAIR_ROW, SOLO_ROW],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.getAttribute('role')).toBe('status');
    expect(n.textContent).toContain('1 saved row does not fit');
    expect(n.textContent).toContain('takes one competitor per side');
    expect(n.textContent).toContain('Alice Smith - Bob Jones');
    expect(n.textContent).not.toContain('Akira Tanaka');
    // Before the start: edit the rows or change the setting back.
    expect(n.textContent).toContain('Edit the rows, or change "One competitor per side" back in Settings.');
    expect(n.textContent).not.toContain('has started');
  });

  it('a pair competition holding a one-name row shows it', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: false,
      players: [PAIR_ROW, SOLO_ROW, { id: 'p-3', name: 'Mei Kato', dojo: 'Dojo C' }],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.textContent).toContain('2 saved rows do not fit');
    expect(n.textContent).toContain('takes pairs');
    expect(n.textContent).toContain('Akira Tanaka; Mei Kato');
    expect(n.textContent).not.toContain('Alice Smith');
  });

  it('shows no notice when every stored row fits, in either shape', async () => {
    const pair = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: false,
      players: [PAIR_ROW],
    }));
    expect(notice(pair.container)).toBeNull();
    pair.unmount();

    const solo = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: true,
      players: [SOLO_ROW, { id: 'p-4', name: 'Jean-Paul Martin', dojo: 'Dojo D' }],
    }));
    expect(notice(solo.container)).toBeNull();
  });

  it('shows no notice for a competition that is not engi, whatever the names hold', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [PAIR_ROW, SOLO_ROW],
    }));
    expect(notice(container)).toBeNull();
  });

  it('a draw-ready competition points at Edit and at discarding the draw, never at Settings alone', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      status: 'draw-ready',
      engi: true,
      engiSolo: true,
      players: [PAIR_ROW, SOLO_ROW],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.textContent).toContain('Alice Smith - Bob Jones');
    expect(n.textContent).toContain('Edit the rows, or discard the draw (from the competition header) to change "One competitor per side" in Settings.');
    expect(n.textContent).not.toContain('back in Settings');
    expect(n.textContent).not.toContain('has started');
  });

  it('a started competition says the setting can no longer change and a pasted list is refused', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      status: 'pools',
      engi: true,
      engiSolo: true,
      players: [PAIR_ROW, SOLO_ROW],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.textContent).toContain('Alice Smith - Bob Jones');
    expect(n.textContent).toContain('The competition has started, so the "One competitor per side" setting can no longer be changed. Applying a pasted list with rows like these is refused.');
    expect(n.textContent).not.toContain('can no longer be edited');
    expect(n.textContent).not.toContain('Edit the rows');
    expect(n.textContent).not.toContain('Settings');
    expect(container.querySelector('button[aria-label="Edit Alice Smith - Bob Jones"]')).toBeNull();
  });

  it('the sample buttons fill a pair competition with pair names the paste box accepts', async () => {
    const { container, getByText } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: false,
      players: [],
    }));
    fireEvent.click(getByText('Small (8)'));
    const lines = container.querySelector('textarea').value.split('\n').filter(Boolean);
    expect(lines).toHaveLength(8);
    for (const line of lines) expect(line.split(',')[0]).toContain(' - ');
  });

  it('the sample buttons fill a solo competition with single names', async () => {
    const { container, getByText } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: true,
      players: [],
    }));
    fireEvent.click(getByText('Small (8)'));
    const lines = container.querySelector('textarea').value.split('\n').filter(Boolean);
    expect(lines).toHaveLength(8);
    for (const line of lines) expect(line.split(',')[0]).not.toContain(' - ');
  });

  it('the Edit dialog labels the name field like the Add form: pair format for pairs, Name for solo', async () => {
    const pair = await mountParticipants(makeParticipantsCompetition({ engi: true, engiSolo: false, players: [PAIR_ROW] }));
    fireEvent.click(pair.container.querySelector('button[aria-label="Edit Alice Smith - Bob Jones"]'));
    expect(document.getElementById('participant-replace-name-label').textContent).toBe('Pair (Name 1 - Name 2) *');
    expect(document.querySelector('[aria-labelledby="participant-replace-name-label"]').getAttribute('placeholder')).toBe('Member 1 - Member 2');
    pair.unmount();

    const solo = await mountParticipants(makeParticipantsCompetition({ engi: true, engiSolo: true, players: [SOLO_ROW] }));
    fireEvent.click(solo.container.querySelector('button[aria-label="Edit Akira Tanaka"]'));
    expect(document.getElementById('participant-replace-name-label').textContent).toBe('Name *');
    expect(document.querySelector('[aria-labelledby="participant-replace-name-label"]').getAttribute('placeholder')).toBe('Replacement name');
  });

  it('the Edit dialog saves a row that no longer fits when its name is left as it is, and judges a changed name', async () => {
    const savedAPI = window.API;
    const savedMetadata = window.buildPlayerMetadata;
    const replaceParticipant = vi.fn().mockResolvedValue({ id: 'p-1', name: 'Alice Smith - Bob Jones', dojo: 'Dojo Z' });
    window.API = { ...savedAPI, replaceParticipant };
    window.buildPlayerMetadata = () => undefined;
    try {
      const showToast = vi.fn();
      const { container, getByText } = await mountParticipants(makeParticipantsCompetition({
        status: 'draw-ready', engi: true, engiSolo: true, players: [PAIR_ROW],
      }), { showToast });
      fireEvent.click(container.querySelector('button[aria-label="Edit Alice Smith - Bob Jones"]'));
      fireEvent.change(document.querySelector('[aria-labelledby="participant-replace-dojo-label"]'), { target: { value: 'Dojo Z' } });
      await act(async () => { fireEvent.click(getByText('Save')); });
      expect(showToast.mock.calls.filter((c) => c[1] === 'error')).toEqual([]);
      expect(replaceParticipant).toHaveBeenCalledTimes(1);
      expect(replaceParticipant.mock.calls[0][2]).toMatchObject({ name: 'Alice Smith - Bob Jones', dojo: 'Dojo Z' });

      // A name typed over it is judged as at every other door.
      fireEvent.click(container.querySelector('button[aria-label="Edit Alice Smith - Bob Jones"]'));
      fireEvent.change(document.querySelector('[aria-labelledby="participant-replace-name-label"]'), { target: { value: 'Alice Smith - Bob Smith' } });
      await act(async () => { fireEvent.click(getByText('Save')); });
      expect(replaceParticipant).toHaveBeenCalledTimes(1);
      const errors = showToast.mock.calls.filter((c) => c[1] === 'error');
      expect(errors).toHaveLength(1);
      // The draw exists, so the setting is locked and the sentence does not advise changing it.
      expect(errors[0][0]).toContain(ENGI_SOLO_PROBLEM_LOCKED);
      expect(errors[0][0]).not.toContain('Settings');
    } finally {
      window.API = savedAPI;
      window.buildPlayerMetadata = savedMetadata;
    }
  });

  it('the sample buttons give a pair its zekken as both members\' last names when zekken is on', async () => {
    const { container, getByText } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: false,
      withZekkenName: true,
      players: [],
    }));
    fireEvent.click(getByText('Small (8)'));
    const lines = container.querySelector('textarea').value.split('\n').filter(Boolean);
    expect(lines).toHaveLength(8);
    for (const line of lines) {
      const [name, zekken] = line.split(',').map((x) => x.trim());
      expect(name).toContain(' - ');
      const [a, b] = name.split(' - ').map((x) => x.split(' ').pop().toUpperCase());
      expect(zekken).toBe(`${a} - ${b}`);
    }
  });

  it('the Add form and the paste box refuse a pair typed into a solo competition', async () => {
    const onUpdate = vi.fn();
    const showToast = vi.fn();
    const { container, getByText, getAllByText } = await mountParticipants(makeParticipantsCompetition({
      engi: true, engiSolo: true, players: [SOLO_ROW],
    }), { onUpdate, showToast });
    const errors = () => showToast.mock.calls.filter((c) => c[1] === 'error').map((c) => c[0]);

    // The paste box: the row's reason follows the colon.
    fireEvent.change(container.querySelector('textarea'), { target: { value: 'Emi Sasaki - Ren Fujita, Dojo A' } });
    await act(async () => { fireEvent.click(getAllByText('Apply changes')[0]); });
    expect(onUpdate).not.toHaveBeenCalled();
    expect(errors().some((m) => m.includes(ENGI_SOLO_PROBLEM))).toBe(true);

    // The Add form: the one sentence, as a toast.
    showToast.mockClear();
    fireEvent.click(getByText('+ Add participant'));
    fireEvent.change(document.querySelector('[aria-labelledby="participant-add-name-label"]'), { target: { value: 'Emi Sasaki - Ren Fujita' } });
    fireEvent.change(document.querySelector('[aria-labelledby="participant-add-dojo-label"]'), { target: { value: 'Dojo A' } });
    await act(async () => { fireEvent.click(getByText('Add', { selector: 'button' })); });
    expect(onUpdate).not.toHaveBeenCalled();
    expect(errors()).toEqual([ENGI_SOLO_PROBLEM]);
  });

  it('names a stored row with a blank name as "(no name)", not an empty string', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: false,
      players: [PAIR_ROW, { id: 'p-9', name: '   ', dojo: 'Dojo' }],
    }));
    expect(notice(container).textContent).toContain('1 saved row does not fit this competition, which takes pairs: (no name).');
  });

  it('lists the first five names and counts the rest', async () => {
    const players = Array.from({ length: 7 }, (_, i) => ({ id: `p-${i}`, name: `Solo${i} - Mate${i}`, dojo: 'Dojo' }));
    const { container } = await mountParticipants(makeParticipantsCompetition({
      engi: true,
      engiSolo: true,
      players,
    }));
    const n = notice(container);
    expect(n.textContent).toContain('7 saved rows do not fit');
    expect(n.textContent).toContain('Solo4 - Mate4; and 2 more');
    expect(n.textContent).not.toContain('Solo5 - Mate5');
  });
});
