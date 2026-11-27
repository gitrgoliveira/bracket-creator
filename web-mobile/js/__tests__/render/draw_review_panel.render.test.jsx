// bc-drev FR2, FR8, FR9, FR13: the Overview's Draw review card. Dojo
// separation comes first and on its own (operator note P0), under the facts
// line and the conclusion line; changing the draw is a separate, optional step
// below it, with a button to the preview tab's Swap competitors mode and the
// swaps the server suggests. Every swap goes through the check panel, and
// focus comes back to the button that opened it. The card reads its two
// answers again after a swap and on a draw_generated event for its
// competition, and hides the swap step once a result is recorded.
import React from 'react';
import { render, act, screen, cleanup, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { keyboardClick } from '../helpers/tap_events.js';

let eventListeners = [];
const STUBBED_GLOBALS = {
  API: {
    fetchDojoSeparation: vi.fn(),
    fetchSwapSuggestions: vi.fn(),
    swapDrawPlaces: vi.fn(),
    subscribeToEvents: vi.fn(),
  },
};

let restore;
let DrawReviewPanel;
let takeSwapMode;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  ({ DrawReviewPanel } = await import('../../admin_draw_review.jsx'));
  ({ takeSwapMode } = await import('../../swap_mode_request.jsx'));
});
afterAll(() => restore());

const meetings = [
  { dojo: 'Team Omega', a: { id: 'p1', name: 'Arthur Dick', number: 'IMS1' }, b: { id: 'p3', name: 'Xu Lin', number: 'IMS3' }, samePool: 'Pool A' },
  {
    dojo: 'Team Kappa', a: { id: 'p2', name: 'Lena Park', number: 'IMS2' }, b: { id: 'p4', name: 'Yan Wu', number: 'IMS4' },
    earliest: { matchId: 'r1-m0', round: 1, rounds: 3 }, ifBothWin: { matchId: 'r2-m0', round: 2, rounds: 3 },
  },
];
const suggestion = {
  a: { id: 'p3', name: 'Xu Lin', number: 'IMS3', dojo: 'Team Omega', place: { pool: 'Pool A', position: 2 } },
  b: { id: 'p4', name: 'Yan Wu', number: 'IMS4', dojo: 'Team Kappa', place: { pool: 'Pool B', position: 2 } },
  improves: [{ dojo: 'Team Omega', before: meetings[0], after: { ...meetings[0], samePool: '', earliest: { matchId: 'r1-m0', round: 1, rounds: 3 } } }],
};
const comp = { id: 'c1', name: 'Open', format: 'mixed', status: 'draw-ready', numberPrefix: 'IMS' };
const dryRun = {
  applied: false, dryRun: true, moves: [], separation: { before: [], after: [] }, lineupsMoved: 0, lineupsRemoved: 0,
};

beforeEach(() => {
  eventListeners = [];
  window.API.fetchDojoSeparation = vi.fn().mockResolvedValue(meetings);
  window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [suggestion] });
  window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
  window.API.subscribeToEvents = vi.fn((cb) => { eventListeners.push(cb); return () => { eventListeners = eventListeners.filter((l) => l !== cb); }; });
});
afterEach(() => cleanup());

async function mount(props = {}) {
  const handlers = { showToast: vi.fn(), onRefreshCompetition: vi.fn(), onSection: vi.fn() };
  let view;
  await act(async () => {
    view = render(<DrawReviewPanel c={comp} poolMatches={[]} bracket={null} password="pw" {...handlers} {...props} />);
  });
  return { ...handlers, view };
}

const emit = (event) => act(async () => { eventListeners.forEach((l) => l(event)); });

describe('the Draw review card', () => {
  it('shows Dojo separation first, one row per pair in the server\'s order', async () => {
    await mount();
    const sep = screen.getByTestId('dojo-separation');
    const change = screen.getByTestId('draw-change');
    expect(sep.compareDocumentPosition(change) & Node.DOCUMENT_POSITION_FOLLOWING, 'the summary comes before the swaps').toBeTruthy();
    expect(within(sep).getByText('Every pair of competitors from the same dojo, earliest meeting first.')).toBeInTheDocument();
    const rows = screen.getAllByTestId('dojo-separation-row');
    expect(rows.map((r) => r.textContent)).toEqual([
      'Team OmegaIMS1 Arthur DickIMS3 Xu LinShare Pool A',
      'Team KappaIMS2 Lena ParkIMS4 Yan WuSemifinalsFinal if both win their pools',
    ]);
    // One competitor per line in the Pair cell, never joined by a dot, so a
    // narrow screen never wraps one name into the other.
    expect([...rows[0].querySelectorAll('td')[1].children].map((n) => n.textContent)).toEqual(['IMS1 Arthur Dick', 'IMS3 Xu Lin']);
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledWith('c1', 'pw');
  });

  it('is headed by its title, an h2 over the h3 of its steps, with the facts and the conclusion under it', async () => {
    await mount();
    const card = screen.getByTestId('draw-review');
    expect(within(card).getByRole('heading', { level: 2 }).textContent).toBe('Draw review');
    expect(within(card).getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual(['Dojo separation', 'Change the draw (optional)']);
    expect(within(card).queryByRole('heading', { level: 4 }), 'no picker step of its own').toBeNull();
    expect(screen.getByTestId('draw-review-verdict').textContent).toBe('2 pairs from 2 dojos. 1 pair shares a pool.');
    const conclusion = screen.getByTestId('draw-review-conclusion');
    expect(conclusion.textContent)
      .toBe('1 swap would keep dojo-mates apart longer. It is under Change the draw.');
    expect(screen.getByTestId('draw-review-verdict').compareDocumentPosition(conclusion) & Node.DOCUMENT_POSITION_FOLLOWING, 'the facts, then what they mean').toBeTruthy();
  });

  it('holds the conclusion line\'s place while the suggestions are read, and leaves it empty when they fail', async () => {
    window.API.fetchSwapSuggestions = vi.fn(() => new Promise(() => {}));
    await mount();
    const shown = () => screen.getByTestId('draw-review-conclusion');
    expect(shown().textContent, 'a no-break space holds the line').toBe('\u00a0');
    cleanup();
    window.API.fetchSwapSuggestions = vi.fn().mockRejectedValue(new Error('Could not read the swap suggestions. Check the connection and try again.'));
    await mount();
    expect(shown().textContent).toBe('\u00a0');
    expect(within(screen.getByTestId('draw-change')).getByRole('alert').textContent).toContain('Could not read the swap suggestions');
  });

  it('names each dojo once, its pairs grouped under it in the server\'s order', async () => {
    const qf = { matchId: 'r0-m0', round: 0, rounds: 3 };
    const fudo = (bId, bName, m) => ({ dojo: 'Fudo', a: { id: 'f1', name: 'Player 08' }, b: { id: bId, name: bName }, ...m });
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue([
      fudo('f2', 'Player 04', { earliest: qf }),
      { dojo: 'Mumei', a: { id: 'm1', name: 'Player 01' }, b: { id: 'm2', name: 'Player 05' }, earliest: qf },
      fudo('f3', 'Player 02', { samePool: 'Pool B' }),
    ]);
    await mount();
    const groups = screen.getAllByTestId('dojo-separation-group');
    expect(groups.map((g) => within(g).getAllByTestId('dojo-separation-row').length)).toEqual([2, 1]);
    const fudoRows = within(groups[0]).getAllByTestId('dojo-separation-row');
    expect(fudoRows[0].querySelector('td').textContent).toBe('Fudo');
    expect(fudoRows[0].querySelector('td').getAttribute('rowspan')).toBe('2');
    expect(fudoRows[1].querySelectorAll('td'), 'the second pair has no dojo cell of its own').toHaveLength(2);
    expect(fudoRows[1].textContent).toBe('Player 08Player 02Share Pool B');
    expect(fudoRows[1].className, 'a same-pool pair keeps its row mark').toBe('draw-review__row--same-pool');
    expect(screen.getByTestId('draw-review-verdict').textContent).toBe('3 pairs from 2 dojos. 1 pair shares a pool.');
  });

  it('says so when no two competitors share a dojo, with no verdict and no swap needed', async () => {
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue([]);
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [] });
    await mount();
    expect(screen.getByTestId('dojo-separation-empty').textContent).toBe('No two competitors share a dojo.');
    expect(screen.queryByTestId('draw-review-verdict')).toBeNull();
    expect(screen.getByText('No swap is needed.')).toBeInTheDocument();
  });

  it('says no swap is needed when every pair meets only in the Final, and that none can do better otherwise, once', async () => {
    const fin = { matchId: 'r2-m0', round: 2, rounds: 3 };
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue([{ ...meetings[1], earliest: fin, ifBothWin: fin }]);
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [] });
    await mount();
    expect(screen.getByTestId('draw-review-verdict').textContent).toBe('1 pair from 1 dojo. They can meet only in the Final.');
    expect(screen.getAllByText('No swap is needed.'), 'said in the conclusion alone').toHaveLength(1);
    cleanup();
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue(meetings);
    await mount();
    expect(screen.getAllByText('No swap can do better.')).toHaveLength(1);
    expect(screen.queryByTestId('swap-suggestion')).toBeNull();
  });

  it('says it could not read the summary rather than showing it empty', async () => {
    window.API.fetchDojoSeparation = vi.fn().mockRejectedValue(new Error('Could not read the dojo separation. Check the connection and try again.'));
    await mount();
    expect(within(screen.getByTestId('dojo-separation')).getByRole('alert').textContent).toContain('Could not read the dojo separation');
    expect(screen.queryByTestId('dojo-separation-empty')).toBeNull();
  });

  it('lists the suggestions with their effect', async () => {
    await mount();
    const item = screen.getByTestId('swap-suggestion');
    expect(item.textContent).toContain('Swap IMS3 Xu Lin (Pool A) and IMS4 Yan Wu (Pool B)');
    expect(item.textContent).toContain('Team Omega: shared Pool A; now not before the Semifinals.');
    expect(item.textContent).toContain('No dojo meets earlier.');
  });

  it('names the pair on each improvement of a dojo with several pairs, keyed by the pair', async () => {
    const second = { dojo: 'Team Omega', a: { id: 'p5', name: 'Ann Lee', number: 'IMS5' }, b: { id: 'p6', name: 'Bo Chen', number: 'IMS6' }, samePool: 'Pool B' };
    const after = { earliest: { matchId: 'r1-m0', round: 1, rounds: 3 } };
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue([...meetings, second]);
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({
      suggestions: [{ ...suggestion, improves: [
        { dojo: 'Team Omega', before: meetings[0], after: { ...meetings[0], samePool: '', ...after } },
        { dojo: 'Team Omega', before: second, after: { ...second, samePool: '', ...after } },
      ] }],
    });
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {});
    await mount();
    const lines = within(screen.getByTestId('swap-suggestion')).getAllByText(/^Team Omega \(/).map((n) => n.textContent);
    expect(lines).toHaveLength(2);
    expect(lines[0]).toContain('(Arthur Dick and Xu Lin)');
    expect(lines[1]).toContain('(Ann Lee and Bo Chen)');
    expect(errors.mock.calls.flat().join(' ')).not.toMatch(/same key/);
    errors.mockRestore();
  });

  it('opens the check for a suggestion with the places the server gave', async () => {
    await mount();
    await keyboardClick(within(screen.getByTestId('swap-suggestion')).getByRole('button'));
    expect(screen.getByTestId('draw-swap-check')).toBeInTheDocument();
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1',
      { a: { id: 'p3', place: suggestion.a.place }, b: { id: 'p4', place: suggestion.b.place }, dryRun: true }, 'pw');
  });

  it('gives focus back to the suggestion that opened the check when it is cancelled', async () => {
    await mount();
    const opener = within(screen.getByTestId('swap-suggestion')).getByRole('button');
    opener.focus();
    await keyboardClick(opener);
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(document.activeElement.textContent, 'focus went in to Cancel').toBe('Cancel');
    expect(opener, 'the opener stays usable, so focus can come back to it').not.toBeDisabled();
    await keyboardClick(screen.getByText('Cancel'));
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it('opens Pools (preview) with the Swap competitors mode on, for any swap by hand', async () => {
    const h = await mount();
    const button = screen.getByTestId('draw-review-swap-mode');
    expect(button.textContent).toBe('Swap competitors on Pools');
    // A normal-size button (44 px on a touch screen through the tap floor), not a small one.
    expect(button).toHaveClass('btn');
    expect(button).not.toHaveClass('btn--sm');
    expect(screen.queryByTestId('swap-pick-a'), 'no pickers of its own').toBeNull();
    await keyboardClick(button);
    expect(h.onSection).toHaveBeenCalledWith('pools');
    expect(takeSwapMode('c1'), 'the tab is asked to start in the mode').toBe(true);
    expect(takeSwapMode('c1'), 'once').toBe(false);
  });

  it('opens Bracket (preview) for a knockout-only draw', async () => {
    const h = await mount({ c: { ...comp, format: 'knockout' }, bracket: { rounds: [[]] } });
    const button = screen.getByTestId('draw-review-swap-mode');
    expect(button.textContent).toBe('Swap competitors on the Bracket');
    await keyboardClick(button);
    expect(h.onSection).toHaveBeenCalledWith('bracket');
    expect(takeSwapMode('other'), 'an ask is for its own competition').toBe(false);
  });

  it('after an applied swap tells it, closes the check and reads everything again', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockResolvedValueOnce({ ...dryRun, dryRun: undefined, applied: true, lineupsMoved: 1 });
    const h = await mount();
    await keyboardClick(within(screen.getByTestId('swap-suggestion')).getByRole('button'));
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(h.showToast).toHaveBeenCalledWith('Swapped Xu Lin and Yan Wu. 1 lineup moved.');
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(h.onRefreshCompetition).toHaveBeenCalledTimes(1);
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(2);
    expect(window.API.fetchSwapSuggestions).toHaveBeenCalledTimes(2);
  });

  it('after a swap made from a suggestion, focus goes to the conclusion line, since the re-read takes the suggestion away', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockResolvedValueOnce({ ...dryRun, dryRun: undefined, applied: true });
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValueOnce({ suggestions: [suggestion] }).mockResolvedValue({ suggestions: [] });
    await mount();
    const opener = within(screen.getByTestId('swap-suggestion')).getByRole('button');
    opener.focus();
    await keyboardClick(opener);
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    await keyboardClick(screen.getByTestId('swap-them'));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(screen.queryByTestId('swap-suggestion'), 'the re-read took the suggestion away').toBeNull();
    const conclusion = screen.getByTestId('draw-review-conclusion');
    expect(document.activeElement).toBe(conclusion);
    expect(conclusion.getAttribute('tabindex'), 'focusable for this alone').toBe('-1');
    expect(conclusion.textContent).toBe('No swap can do better.');
  });

  it('on draw_changed closes the check, says why and reads everything again', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun)
      .mockRejectedValueOnce(Object.assign(new Error('The draw has changed since you opened it.'), { code: 'draw_changed' }));
    const h = await mount();
    await keyboardClick(within(screen.getByTestId('swap-suggestion')).getByRole('button'));
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(h.showToast).toHaveBeenCalledWith('The draw has changed since you opened it.', 'error');
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(h.onRefreshCompetition).toHaveBeenCalledTimes(1);
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(2);
  });

  it('reads the summary again on a draw_generated event for its competition only', async () => {
    await mount();
    await emit({ type: 'draw_generated', data: { competitionId: 'other' } });
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(1);
    await emit({ type: 'match_updated', data: { competitionId: 'c1' } });
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(1);
    await emit({ type: 'draw_generated', data: { competitionId: 'c1' } });
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(2);
    expect(window.API.fetchSwapSuggestions).toHaveBeenCalledTimes(2);
  });

  it('hides the swap step once a result is recorded, and keeps the summary', async () => {
    const poolMatches = [{ id: 'Pool A-1', status: 'completed', sideA: { name: 'Arthur Dick' }, sideB: { name: 'Xu Lin' } }];
    await mount({ poolMatches });
    expect(screen.getByTestId('dojo-separation')).toBeInTheDocument();
    expect(screen.queryByTestId('draw-change')).toBeNull();
  });

  for (const c of [{ ...comp, format: 'league' }, { ...comp, format: 'swiss' }, { ...comp, status: 'pools' }, { ...comp, status: 'setup' }]) {
    it(`shows nothing and reads nothing for a ${c.format} competition in ${c.status}`, async () => {
      await mount({ c });
      expect(screen.queryByTestId('draw-review')).toBeNull();
      expect(window.API.fetchDojoSeparation).not.toHaveBeenCalled();
      expect(window.API.subscribeToEvents).not.toHaveBeenCalled();
    });
  }

});
