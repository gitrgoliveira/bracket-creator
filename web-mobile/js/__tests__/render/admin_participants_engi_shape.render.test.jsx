import { describe, it, expect } from 'vitest';
import { fireEvent } from '@testing-library/react';
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

  it('a started competition says the rows and the setting can no longer change', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      status: 'pools',
      engi: true,
      engiSolo: true,
      players: [PAIR_ROW, SOLO_ROW],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.textContent).toContain('Alice Smith - Bob Jones');
    expect(n.textContent).toContain('The competition has started, so these rows and the "One competitor per side" setting can no longer be changed.');
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
