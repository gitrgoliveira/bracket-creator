import React from 'react';
import { render, screen, act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { DataIssueBanner, MissingParticipantIDsNotice } from '../../data_integrity.jsx';

// The banner is the LOUD-class surface: a whole file will not parse, every
// write to it is refused, and the operator has to choose between repairing it
// and resetting. Mounted for real, because what matters is which of those two
// options it actually offers.

describe('DataIssueBanner', () => {
  const bracketBroken = [{ file: 'bracket.json', line: 47, column: 12, detail: "invalid character 'x'" }];

  it('renders nothing when there is nothing to repair', () => {
    const { container } = render(<DataIssueBanner issues={[]} competition={{ format: 'mixed' }} />);
    expect(container.firstChild).toBeNull();
  });

  it('names the file and position, and says the file is untouched', () => {
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} />);
    expect(screen.getByRole('alert').textContent).toContain('bracket.json, line 47, column 12');
    // The reassurance is the whole reason repair is the recommended option.
    expect(screen.getByRole('alert').textContent).toMatch(/still exactly as it was last saved/);
  });

  it('offers the reset for a pool-fed competition, and states what it costs', () => {
    const onReset = vi.fn();
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} onReset={onReset} />);
    const text = screen.getByRole('alert').textContent;
    // The four facts an operator needs before choosing.
    expect(text).toMatch(/never deleted/);
    expect(text).toMatch(/Pools, participants and pool results are untouched/);
    expect(text).toMatch(/re-entered from the score sheets/);
    expect(text).toMatch(/Check the rebuilt pairings against your printed bracket/);
    screen.getByRole('button', { name: /Reset the knockout stage/ }).click();
    expect(onReset).toHaveBeenCalled();
  });

  it('offers a plain move-aside for a league, and promises no rebuild', () => {
    // A league has no knockout stage, so every word about resetting one, about
    // bouts to re-enter and about pairings to check against a printed bracket
    // would be false. The button is still offered: the file blocks the load and
    // moving it aside costs nothing.
    const onReset = vi.fn();
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'league' }} onReset={onReset} />);
    const text = screen.getByRole('alert').textContent;
    expect(text).toMatch(/no knockout stage/);
    expect(text).toMatch(/Nothing is rebuilt/);
    expect(text).not.toMatch(/re-entered from the score sheets/);
    expect(text).not.toMatch(/printed bracket/);
    expect(screen.queryByRole('button', { name: /Reset the knockout stage/ })).toBeNull();
    screen.getByRole('button', { name: /Move the file aside/ }).click();
    expect(onReset).toHaveBeenCalled();
  });

  it('offers nothing for an unrecognised format, which is drawn as direct elimination', () => {
    // A typo in a hand-edited config.md. It takes the draw pipeline's default
    // branch, so the file IS its draw and a rebuild would invent a new one.
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'not-a-format' }} onReset={vi.fn()} />);
    expect(screen.queryByRole('button')).toBeNull();
    expect(screen.getByRole('alert').textContent).toMatch(/only record of who was drawn against whom/);
  });

  it('does NOT offer the reset for direct elimination, and explains why', () => {
    // An action that can only fail is worse than an explanation: the server
    // refuses this case, so the button must not be offered at all.
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'knockout' }} onReset={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /Reset the knockout stage/ })).toBeNull();
    expect(screen.getByRole('alert').textContent)
      .toMatch(/only record of who was drawn against whom/);
  });

  it('does not offer a reset when the bracket is fine and another file is not', () => {
    render(<DataIssueBanner issues={[{ file: 'pools.csv', line: 2, column: 1, detail: 'bare quote' }]}
      competition={{ format: 'mixed' }} onReset={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /Reset the knockout stage/ })).toBeNull();
    expect(screen.getByRole('alert').textContent).toContain('pools.csv, line 2, column 1');
  });

  it('disables the reset while it is running, and says what it is doing', () => {
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} onReset={vi.fn()} resetting />);
    expect(screen.getByRole('button', { name: /Resetting/ }).disabled).toBe(true);
  });

  it('does not say "resetting" while moving a league file aside', () => {
    // Nothing is being reset there, so the busy label follows the action too.
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'league' }} onReset={vi.fn()} resetting />);
    expect(screen.queryByRole('button', { name: /Resetting/ })).toBeNull();
    expect(screen.getByRole('button', { name: /Moving/ }).disabled).toBe(true);
  });

  // bc-pnum ruling 1b: a legacy roster with id-less rows is neither loud
  // (nothing failed to parse) nor blocking (nothing is refused), so it must
  // read differently from the corrupt-file banner above.
  const missingIDs = [{ kind: 'missing-ids', file: 'participants.csv', detail: 'Dave: no id on file. Save the roster once and the ids are assigned.' }];

  it('renders the missing-ids notice, not the corrupt-file banner, for a missing-ids-only list', () => {
    render(<DataIssueBanner issues={missingIDs} competition={{ format: 'mixed' }} />);
    expect(screen.queryByRole('alert')).toBeNull();
    const notice = screen.getByRole('status');
    expect(notice.textContent).toContain('Save the roster once and the ids are assigned.');
  });

  it('renders both notices when a file is also corrupt', () => {
    render(<DataIssueBanner issues={[...bracketBroken, ...missingIDs]} competition={{ format: 'mixed' }} onReset={vi.fn()} />);
    // The corrupt-file banner still only lists the corrupt entry, not the
    // missing-ids one.
    const alert = screen.getByRole('alert');
    expect(alert.textContent).toContain('bracket.json, line 47, column 12');
    expect(alert.textContent).not.toContain('Save the roster once');
    expect(screen.getByRole('status').textContent).toContain('Save the roster once and the ids are assigned.');
  });

  // Operator ruling bc-pnum: participants.csv, pools.csv and pool-matches.csv
  // are each checked and reported independently, so an operator repairing
  // the roster must still see the OTHER two notices rather than have them
  // silently hidden by a single-entry picker.
  it('renders one notice line per missing-ids entry when multiple files are affected', () => {
    const threeIssues = [
      { kind: 'missing-ids', file: 'participants.csv', detail: 'Dave: no id on file. Save the roster once and the ids are assigned.' },
      { kind: 'missing-ids', file: 'pools.csv', detail: '2 competitors: no id in the pool draw and could not be matched to a participant automatically. No player number is assigned; regenerate the draw while it is still draw-ready to fix it.' },
      { kind: 'missing-ids', file: 'pool-matches.csv', detail: '1 match(es): a side or winner has no id and could not be resolved automatically. They are not counted in standings; re-enter the result to assign a winner id, and regenerate the draw while it is still draw-ready to restore a missing side id.' },
    ];
    render(<DataIssueBanner issues={threeIssues} competition={{ format: 'mixed' }} />);
    expect(screen.queryByRole('alert')).toBeNull();
    const notices = screen.getAllByRole('status');
    expect(notices).toHaveLength(3);
    const text = notices.map((n) => n.textContent).join(' | ');
    expect(text).toContain('Dave: no id on file');
    expect(text).toContain('no id in the pool draw');
    expect(text).toContain('a side or winner has no id');
  });

  it('renders nothing when there are no issues at all (missing-ids absent too)', () => {
    const { container } = render(<DataIssueBanner issues={[]} competition={{ format: 'mixed' }} />);
    expect(container.firstChild).toBeNull();
  });
});

// D27/D28/D29: the advice follows each case, the file is named by its place
// in the data folder, and only the locator is set in monospace.
describe('DataIssueBanner advice per case', () => {
  const located = { kind: 'corrupt-file', file: 'participants.csv', line: 4, column: 1, detail: 'lines 1 and 4 hold the same competitor' };
  const unopened = { kind: 'corrupt-file', file: 'participants.csv', line: 0, column: 0, detail: 'could not be opened' };
  const comp = { id: 'kendo', format: 'mixed' };

  it('keeps the repair paragraph for a located bad line', () => {
    render(<DataIssueBanner issues={[located]} competition={comp} />);
    const text = screen.getByRole('alert').textContent;
    expect(text).toMatch(/fix the\s+position above/);
    expect(text).not.toMatch(/could not open this file/);
  });

  it('tells the operator to fix the cause outside the file when it could not be opened', () => {
    render(<DataIssueBanner issues={[unopened]} competition={comp} />);
    const text = screen.getByRole('alert').textContent.replace(/\s+/g, ' ');
    expect(text).toContain('The server’s log names the reason, for example a permission problem. Fix that, then reload this page.');
    // The list item says "could not be opened"; the paragraph does not say it again.
    expect(text.match(/could not (be )?open/g)).toHaveLength(1);
    expect(text).not.toMatch(/position above/);
    expect(text).not.toMatch(/will not parse/);
  });

  // A row the parser refuses is located on its FILE line, so it takes the
  // bad-line advice and the banner never claims a way of counting rows.
  it('gives the repair advice for a refused row, located on its file line', () => {
    const refused = { kind: 'corrupt-file', file: 'participants.csv', line: 8, column: 1,
      detail: "invalid format: expected 'Name, Dojo' or 'Name, DisplayName, Dojo'" };
    render(<DataIssueBanner issues={[refused]} competition={comp} onReload={vi.fn()} />);
    const text = screen.getByRole('alert').textContent.replace(/\s+/g, ' ');
    expect(text).toContain('competitions/kendo/participants.csv, line 8, column 1');
    expect(text).toContain('Repair the file. Open it, fix the position above and save it, then press Reload.');
    expect(text).not.toMatch(/Rows are counted/);
    expect(text).not.toMatch(/clears on its own/);
    expect(text).not.toMatch(/could not (be )?open/);
  });

  // participants.csv holds no results and blocks the roster and the draw, not
  // scoring; the advice names the Reload button when there is one.
  it('words the roster banner for the roster, and names the Reload button', () => {
    render(<DataIssueBanner issues={[located]} competition={comp} onReload={vi.fn()} />);
    const text = screen.getByRole('alert').textContent.replace(/\s+/g, ' ');
    expect(text).toContain('The roster and the draw are blocked until this file loads.');
    expect(text).toContain('fix the position above and save it, then press Reload.');
    expect(text).not.toMatch(/Scoring is blocked/);
    expect(text).not.toMatch(/results included/);
  });

  it('keeps the scoring and results wording for a draw file', () => {
    render(<DataIssueBanner issues={[{ kind: 'corrupt-file', file: 'pool-matches.csv', line: 3, column: 2, detail: 'x' }]} competition={comp} />);
    const text = screen.getByRole('alert').textContent.replace(/\s+/g, ' ');
    expect(text).toContain('Scoring is blocked for whatever that file holds.');
    expect(text).toContain('results included');
  });

  it('says the repair loses nothing only when a reset is also offered', () => {
    const { unmount } = render(<DataIssueBanner issues={[located]} competition={comp} />);
    expect(screen.getByRole('alert').textContent).not.toMatch(/loses nothing/);
    unmount();
    render(<DataIssueBanner issues={[{ file: 'bracket.json', line: 2, column: 1, detail: 'x' }]} competition={comp} onReset={vi.fn()} />);
    expect(screen.getByRole('alert').textContent).toMatch(/This is the option that loses nothing\./);
  });

  it('names the file by its place in the data folder, and only that is code', () => {
    const { container } = render(<DataIssueBanner issues={[located]} competition={comp} />);
    const code = container.querySelector('.data-issue__list code');
    expect(code.textContent).toBe('competitions/kendo/participants.csv, line 4, column 1');
    expect(code.closest('li').textContent).toBe(`competitions/kendo/participants.csv, line 4, column 1: ${located.detail}`);
    expect(container.querySelectorAll('.data-issue__list code')).toHaveLength(1);
  });

  it('draws the SVG warning icon, never the emoji-prone glyph', () => {
    const { container } = render(<DataIssueBanner issues={[unopened]} competition={comp} />);
    expect(container.querySelector('.data-issue__icon')?.tagName.toLowerCase()).toBe('svg');
    expect(container.textContent).not.toContain('⚠');
  });

  it('offers Reload only when the host can reload', async () => {
    const { unmount } = render(<DataIssueBanner issues={[unopened]} competition={comp} />);
    expect(screen.queryByRole('button', { name: /Reload/ })).toBeNull();
    unmount();
    const onReload = vi.fn().mockResolvedValue({ ok: true });
    render(<DataIssueBanner issues={[unopened]} competition={comp} onReload={onReload} />);
    await act(async () => { screen.getByRole('button', { name: /Reload/ }).click(); });
    expect(onReload).toHaveBeenCalled();
  });

  // Reload shows it is working, then what it found: still unreadable, or a
  // request that failed (which used to reach the console alone).
  it('shows a busy Reload, then says the file is still unreadable', async () => {
    let finish;
    const onReload = vi.fn(() => new Promise((resolve) => { finish = resolve; }));
    render(<DataIssueBanner issues={[unopened]} competition={comp} onReload={onReload} />);
    await act(async () => { screen.getByRole('button', { name: /Reload/ }).click(); });
    // The label does not change while busy (the spinner takes the icon's
    // place), so the button keeps its width under the finger.
    const busy = screen.getByRole('button', { name: /^Reload$/ });
    expect(busy.disabled).toBe(true);
    expect(busy.getAttribute('aria-busy')).toBe('true');
    await act(async () => { finish({ ok: true }); });
    expect(screen.getByRole('button', { name: /^Reload$/ }).disabled).toBe(false);
    expect(screen.getByTestId('data-issue-reload-note').textContent).toBe('Reloaded: the file still could not be read.');
  });

  it('says a failed reload in plain words, and keeps the raw text for the console', async () => {
    const logged = vi.spyOn(console, 'error').mockImplementation(() => {});
    const onReload = vi.fn().mockResolvedValue({ ok: false, message: 'Failed to fetch' });
    render(<DataIssueBanner issues={[unopened]} competition={comp} onReload={onReload} />);
    await act(async () => { screen.getByRole('button', { name: /Reload/ }).click(); });
    expect(screen.getByTestId('data-issue-reload-note').textContent).toBe('Could not reach the server. Check the connection and press Reload again.');
    expect(logged).toHaveBeenCalledWith('Reload failed:', 'Failed to fetch');
    logged.mockRestore();
  });
});

describe('MissingParticipantIDsNotice', () => {
  it('renders the server-supplied sentence verbatim', () => {
    render(<MissingParticipantIDsNotice issues={[{ file: 'participants.csv', detail: 'Alice (Dojo A): no id on file. Save the roster once and the ids are assigned.' }]} />);
    expect(screen.getByRole('status').textContent).toContain('Alice (Dojo A)');
  });

  it('renders one status line per entry, in order', () => {
    render(<MissingParticipantIDsNotice issues={[
      { file: 'participants.csv', detail: 'first notice' },
      { file: 'pools.csv', detail: 'second notice' },
    ]} />);
    const notices = screen.getAllByRole('status');
    expect(notices).toHaveLength(2);
    expect(notices[0].textContent).toContain('first notice');
    expect(notices[1].textContent).toContain('second notice');
  });

  it('renders nothing without any issues', () => {
    const { container } = render(<MissingParticipantIDsNotice issues={null} />);
    expect(container.firstChild).toBeNull();
  });

  it('renders nothing for an empty issues array', () => {
    const { container } = render(<MissingParticipantIDsNotice issues={[]} />);
    expect(container.firstChild).toBeNull();
  });
});
