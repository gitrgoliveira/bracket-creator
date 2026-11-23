import { describe, it, expect } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';

// The stored-rows notice: "One competitor per side" stays editable at every
// status, so a roster saved under one setting can hold rows the other refuses.
// The Participants page names those rows (judged by engiEntryProblem) in an
// inline role="status" notice and says how to fix them; it never blocks.

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

  it('a started competition gets the copy that points at Edit on each row', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      status: 'pools',
      engi: true,
      engiSolo: true,
      players: [PAIR_ROW, SOLO_ROW],
    }));
    const n = notice(container);
    expect(n).toBeTruthy();
    expect(n.textContent).toContain('Alice Smith - Bob Jones');
    expect(n.textContent).toContain('The competition has started, so competitors cannot be added or pasted.');
    expect(n.textContent).toContain('Fix each row with Edit');
    expect(n.textContent).toContain('change "One competitor per side" back in Settings');
    expect(n.textContent).not.toContain('Edit the rows');
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
