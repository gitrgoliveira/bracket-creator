import { describe, it, expect } from 'vitest';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';
import { rosterUnreadableIssues, ROSTER_UNREADABLE_DETAIL } from '../../data_integrity.jsx';

// D21: a participants.csv the server cannot read arrives as an EMPTY roster
// (players [], rosterAvailable false, the located entry on dataIssues). The
// Participants page read only `players` and showed "Add your roster to begin"
// with an empty paste box over it. It now shows the file's notice instead,
// with the server's sentence naming the lines, and nothing to paste into.

installParticipantsHarness();

const DUPLICATE = {
  kind: 'corrupt-file',
  file: 'participants.csv',
  line: 4,
  column: 1,
  detail: 'lines 1 and 4 hold the same competitor once names and dojos are title-cased and zekken are in capitals; remove one row of each pair or tell them apart',
};

describe('AdminParticipants over an unreadable participants.csv (D21)', () => {
  it('shows the file notice with the server sentence, never the empty-roster page', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [],
      rosterAvailable: false,
      dataIssues: [DUPLICATE],
    }));

    const banner = container.querySelector('.data-issue--banner');
    expect(banner).toBeTruthy();
    expect(banner.getAttribute('role')).toBe('alert');
    expect(banner.textContent).toContain('A file for this competition could not be read.');
    expect(banner.textContent).toContain(`participants.csv, line 4, column 1: ${DUPLICATE.detail}`);
    expect(container.querySelector('textarea')).toBeNull();
    expect(container.textContent).not.toContain('Add your roster to begin');
    expect(container.textContent).not.toContain('Participant list');
  });

  it('names the file when the roster failed to load with no located entry', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [],
      rosterAvailable: false,
    }));

    const banner = container.querySelector('.data-issue--banner');
    expect(banner).toBeTruthy();
    expect(banner.textContent).toContain(`participants.csv: ${ROSTER_UNREADABLE_DETAIL}`);
    expect(container.querySelector('textarea')).toBeNull();
  });

  it('keeps the page for an empty roster that loaded, and for another file\'s issue', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [],
      rosterAvailable: true,
      dataIssues: [{ kind: 'corrupt-file', file: 'bracket.json', line: 2, column: 3, detail: 'bad byte' }],
    }));

    expect(container.querySelector('.data-issue--banner')).toBeNull();
    expect(container.querySelector('textarea')).toBeTruthy();
    expect(container.textContent).toContain('Add your roster to begin');
  });
});

describe('rosterUnreadableIssues', () => {
  it('ignores an advisory missing-ids entry on participants.csv', () => {
    expect(rosterUnreadableIssues({
      players: [{ id: 'p-1' }],
      dataIssues: [{ kind: 'missing-ids', file: 'participants.csv', detail: 'No id on file.' }],
    })).toEqual([]);
  });

  it('reads a payload with no rosterAvailable as loaded', () => {
    expect(rosterUnreadableIssues({ players: [] })).toEqual([]);
    expect(rosterUnreadableIssues(null)).toEqual([]);
  });
});
