import { describe, it, expect, vi } from 'vitest';
import { act } from '@testing-library/react';
import { installParticipantsHarness, makeParticipantsCompetition, mountParticipants } from './admin_participants_mount_harness.jsx';
import { rosterUnreadableIssues } from '../../data_integrity.jsx';

// D21: a participants.csv the server cannot read arrives as an EMPTY roster
// (players [], rosterAvailable false, a participants.csv entry on dataIssues). The
// Participants page read only `players` and showed "Add your roster to begin"
// with an empty paste box over it. It now shows the file's notice instead,
// with the server's sentence naming the lines, and nothing to paste into.

installParticipantsHarness();

const DUPLICATE = {
  kind: 'corrupt-file',
  file: 'participants.csv',
  line: 4,
  column: 1,
  detail: 'lines 1 and 4 hold the same competitor (name, zekken and dojo, ignoring case); remove one row of each pair or tell them apart',
};

// The entry the server sends for a roster that failed with no position in the
// file (handlers_viewer.go rosterUnreadableIssue).
const UNREADABLE = {
  kind: 'corrupt-file',
  file: 'participants.csv',
  line: 0,
  column: 0,
  detail: 'could not be opened',
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
    expect(banner.textContent).toContain(`competitions/c1/participants.csv, line 4, column 1: ${DUPLICATE.detail}`);
    expect(banner.textContent).toMatch(/fix the\s+position above/);
    expect(container.querySelector('textarea')).toBeNull();
    expect(container.textContent).not.toContain('Add your roster to begin');
    expect(container.textContent).not.toContain('Participant list');
  });

  it('names the file when the server reports the roster with no position', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [],
      rosterAvailable: false,
      dataIssues: [UNREADABLE],
    }));

    const banner = container.querySelector('.data-issue--banner');
    expect(banner).toBeTruthy();
    // D28: named by its place inside the tournament data folder.
    expect(banner.textContent).toContain(`competitions/c1/participants.csv: ${UNREADABLE.detail}`);
    // D27: the advice for a file the server could not open, not the repair one.
    expect(banner.textContent).toMatch(/server’s\s+log names the reason, for example a permission problem/);
    expect(banner.textContent).not.toMatch(/position above/);
    expect(container.querySelector('textarea')).toBeNull();
  });

  it('offers a Reload that re-fetches the competition (D28)', async () => {
    const onRefreshCompetition = vi.fn().mockResolvedValue({ ok: true });
    const { container } = await mountParticipants(makeParticipantsCompetition({
      players: [],
      rosterAvailable: false,
      dataIssues: [UNREADABLE],
    }), { onRefreshCompetition });

    const reload = Array.from(container.querySelectorAll('.data-issue--banner button'))
      .find((b) => b.textContent.trim() === 'Reload');
    expect(reload).toBeTruthy();
    await act(async () => { reload.click(); });
    expect(onRefreshCompetition).toHaveBeenCalledTimes(1);
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

// D29: the page's normal state, around the same paste card. jsdom does not lay
// out, so the one-line rule is pinned by its classes (styles.css keeps
// .card__head-actions buttons on one line and lets the row wrap) and the
// screenshots at 1180 and 390 wide show it.
describe('the Participant list card head (D29)', () => {
  it('holds Paste clipboard and Apply changes in the wrapping actions row', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition());
    const head = container.querySelector('.card__head--wrap');
    expect(head).toBeTruthy();
    const labels = Array.from(head.querySelectorAll('.card__head-actions .btn')).map((b) => b.textContent);
    expect(labels).toEqual(['Paste clipboard', 'Apply changes']);
  });

  it('styles Download CSV template as a link button and quotes the format with curly quotes', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition());
    const link = Array.from(container.querySelectorAll('button')).find((b) => b.textContent === 'Download CSV template');
    expect(link.className).toBe('btn btn--link');
    expect(link.getAttribute('style')).toBeNull();
    const hint = link.closest('.field__hint').textContent;
    expect(hint).toMatch(/Format: “[^”]+”/);
    expect(hint).not.toContain('"');
  });

  // The CSV drop zone is a real button: reachable by Tab, operated by Enter or
  // Space (a native button), worded for touch as well as a mouse, drawn with
  // the SVG icon set, and it no longer repeats the head's format and example.
  it('makes the CSV drop zone a keyboard-operable button', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition());
    const zone = container.querySelector('.dropzone');
    expect(zone.tagName).toBe('BUTTON');
    expect(zone.getAttribute('type')).toBe('button');
    expect(zone.disabled).toBe(false);
    expect(zone.textContent).toBe('Choose or drop a CSV file to import');
    expect(zone.querySelector('svg.dropzone__icon')).toBeTruthy();
    expect(container.textContent).not.toContain('📥');
    expect(zone.querySelector('input')).toBeNull();
    const input = zone.parentElement.querySelector(':scope > input[type="file"]');
    const pick = vi.spyOn(input, 'click').mockImplementation(() => {});
    zone.click();
    expect(pick).toHaveBeenCalledTimes(1);
  });

  it('disables the drop zone while a draw is ready', async () => {
    const { container } = await mountParticipants(makeParticipantsCompetition({ status: 'draw-ready' }));
    expect(container.querySelector('.dropzone').disabled).toBe(true);
  });
});

describe('rosterUnreadableIssues', () => {
  it('ignores an advisory missing-ids entry on participants.csv', () => {
    expect(rosterUnreadableIssues({
      players: [{ id: 'p-1' }],
      dataIssues: [{ kind: 'missing-ids', file: 'participants.csv', detail: 'No id on file.' }],
    })).toEqual([]);
  });

  it('reads a payload with no participants.csv entry as loaded', () => {
    expect(rosterUnreadableIssues({ players: [] })).toEqual([]);
    expect(rosterUnreadableIssues(null)).toEqual([]);
    // The server owns "the roster did not load": the flag alone synthesises nothing.
    expect(rosterUnreadableIssues({ players: [], rosterAvailable: false })).toEqual([]);
  });
});
