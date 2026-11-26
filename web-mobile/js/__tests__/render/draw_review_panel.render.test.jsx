// bc-drev FR2, FR8, FR9, FR13: the Overview's Draw review card. Dojo
// separation comes first and on its own (operator note P0); changing the draw
// is a separate, optional step below it, with the swaps the server suggests
// and two pickers. Every swap goes through the check panel. The card reads its
// two answers again after a swap and on a draw_generated event for its
// competition, and hides the swap step once a result is recorded.
import React from 'react';
import { render, act, screen, cleanup, fireEvent, within } from '@testing-library/react';
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

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  ({ DrawReviewPanel } = await import('../../admin_draw_review.jsx'));
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
const pools = [
  { poolName: 'Pool A', players: [{ id: 'p1', name: 'Arthur Dick', dojo: 'Team Omega', number: 'IMS1', seed: 1 }, { id: 'p3', name: 'Xu Lin', dojo: 'Team Omega', number: 'IMS3' }] },
  { poolName: 'Pool B', players: [{ id: 'p2', name: 'Lena Park', dojo: 'Team Kappa', number: 'IMS2' }, { id: 'p4', name: 'Yan Wu', dojo: 'Team Kappa', number: 'IMS4' }] },
];
const dryRun = {
  applied: false, dryRun: true, moves: [], separation: { before: [], after: [] }, lineupsMoved: 0, lineupsRemoved: 0,
};

beforeEach(() => {
  eventListeners = [];
  window.API.fetchDojoSeparation = vi.fn().mockResolvedValue(meetings);
  window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [suggestion], stuck: [] });
  window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
  window.API.subscribeToEvents = vi.fn((cb) => { eventListeners.push(cb); return () => { eventListeners = eventListeners.filter((l) => l !== cb); }; });
});
afterEach(() => cleanup());

async function mount(props = {}) {
  const handlers = { showToast: vi.fn(), onRefreshCompetition: vi.fn() };
  let view;
  await act(async () => {
    view = render(<DrawReviewPanel c={comp} pools={pools} poolMatches={[]} bracket={null} password="pw" {...handlers} {...props} />);
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

  it('is headed by its title, an h2 over the h3 and h4 of its steps, with a one-line verdict under it', async () => {
    await mount();
    const card = screen.getByTestId('draw-review');
    expect(within(card).getByRole('heading', { level: 2 }).textContent).toBe('Draw review');
    expect(within(card).getAllByRole('heading', { level: 3 }).map((h) => h.textContent)).toEqual(['Dojo separation', 'Change the draw (optional)']);
    expect(within(card).getByRole('heading', { level: 4 }).textContent).toBe('Or pick your own swap');
    expect(screen.getByTestId('draw-review-verdict').textContent).toBe('2 pairs from 2 dojos. 1 pair shares a pool.');
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
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [], stuck: [] });
    await mount();
    expect(screen.getByTestId('dojo-separation-empty').textContent).toBe('No two competitors share a dojo.');
    expect(screen.queryByTestId('draw-review-verdict')).toBeNull();
    expect(screen.getByText('No swap is needed.')).toBeInTheDocument();
  });

  it('says no swap is needed when every pair meets only in the Final, and that none helps otherwise', async () => {
    const fin = { matchId: 'r2-m0', round: 2, rounds: 3 };
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue([{ ...meetings[1], earliest: fin, ifBothWin: fin }]);
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({ suggestions: [], stuck: [] });
    await mount();
    expect(screen.getByTestId('draw-review-verdict').textContent).toBe('1 pair from 1 dojo. They can meet only in the Final.');
    expect(screen.getByText('No swap is needed.')).toBeInTheDocument();
    cleanup();
    window.API.fetchDojoSeparation = vi.fn().mockResolvedValue(meetings);
    await mount();
    expect(screen.getByText('No swap keeps dojo-mates apart for longer.')).toBeInTheDocument();
  });

  it('says it could not read the summary rather than showing it empty', async () => {
    window.API.fetchDojoSeparation = vi.fn().mockRejectedValue(new Error('Could not read the dojo separation. Check the connection and try again.'));
    await mount();
    expect(within(screen.getByTestId('dojo-separation')).getByRole('alert').textContent).toContain('Could not read the dojo separation');
    expect(screen.queryByTestId('dojo-separation-empty')).toBeNull();
  });

  it('lists the suggestions with their effect, and the dojos no swap helps', async () => {
    window.API.fetchSwapSuggestions = vi.fn().mockResolvedValue({
      suggestions: [suggestion],
      stuck: [{ dojo: 'Team Pi', meeting: { earliest: { matchId: 'r0-m1', round: 0, rounds: 3 } } }],
    });
    await mount();
    const item = screen.getByTestId('swap-suggestion');
    expect(item.textContent).toContain('Swap IMS3 Xu Lin (Pool A) and IMS4 Yan Wu (Pool B)');
    expect(item.textContent).toContain('Team Omega: shared Pool A; now not before the Semifinals.');
    expect(item.textContent).toContain('Nobody else meets earlier.');
    expect(screen.getByTestId('swap-stuck').textContent)
      .toBe('For Team Pi, no suggested swap makes their first meeting later without making another dojo meet earlier.');
  });

  it('opens the check for a suggestion with the places the server gave', async () => {
    await mount();
    await keyboardClick(within(screen.getByTestId('swap-suggestion')).getByRole('button'));
    expect(screen.getByTestId('draw-swap-check')).toBeInTheDocument();
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1',
      { a: { id: 'p3', place: suggestion.a.place }, b: { id: 'p4', place: suggestion.b.place }, dryRun: true }, 'pw');
  });

  it('offers Check this swap only for two different competitors, grouped by pool', async () => {
    await mount();
    const pickA = screen.getByTestId('swap-pick-a');
    const pickB = screen.getByTestId('swap-pick-b');
    expect([...pickA.querySelectorAll('optgroup')].map((g) => g.label)).toEqual(['Pool A', 'Pool B']);
    expect(pickA.querySelector('option[value="p1"]').textContent).toBe('IMS1 Arthur Dick (Team Omega), seed 1');
    const check = screen.getByTestId('swap-check-btn');
    expect(check).toBeDisabled();
    await act(async () => { fireEvent.change(pickA, { target: { value: 'p1' } }); });
    expect(check).toBeDisabled();
    expect(pickB.querySelector('option[value="p1"]'), 'Swap with leaves out the competitor already picked').toBeNull();
    expect(pickB.querySelector('option[value="p3"]')).not.toBeNull();
    await act(async () => { fireEvent.change(pickB, { target: { value: 'p2' } }); });
    expect(check).not.toBeDisabled();
    await keyboardClick(check);
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1',
      { a: { id: 'p1', place: { pool: 'Pool A', position: 1 } }, b: { id: 'p2', place: { pool: 'Pool B', position: 1 } }, dryRun: true }, 'pw');
    expect(check, 'disabled while the check is open').toBeDisabled();
  });

  it('drops the second pick when the first picker takes the same competitor', async () => {
    await mount();
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-b'), { target: { value: 'p2' } }); });
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-a'), { target: { value: 'p2' } }); });
    expect(screen.getByTestId('swap-pick-b').value).toBe('');
    expect(screen.getByTestId('swap-check-btn'), 'the same competitor twice is no swap').toBeDisabled();
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-a'), { target: { value: 'p1' } }); });
    expect(screen.getByTestId('swap-pick-b').value, 'the dropped pick does not come back').toBe('');
  });

  it('after an applied swap tells it, resets the pickers and reads everything again', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockResolvedValueOnce({ ...dryRun, dryRun: undefined, applied: true, lineupsMoved: 1 });
    const h = await mount();
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-a'), { target: { value: 'p1' } }); });
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-b'), { target: { value: 'p2' } }); });
    await keyboardClick(screen.getByTestId('swap-check-btn'));
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(h.showToast).toHaveBeenCalledWith('Swapped Arthur Dick and Lena Park. 1 lineup moved.');
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(screen.getByTestId('swap-pick-a').value).toBe('');
    expect(screen.getByTestId('swap-pick-b').value).toBe('');
    expect(h.onRefreshCompetition).toHaveBeenCalledTimes(1);
    expect(window.API.fetchDojoSeparation).toHaveBeenCalledTimes(2);
    expect(window.API.fetchSwapSuggestions).toHaveBeenCalledTimes(2);
  });

  it('on draw_changed closes the check, resets the pickers, says why and reads everything again', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun)
      .mockRejectedValueOnce(Object.assign(new Error('The draw has changed since you opened it.'), { code: 'draw_changed' }));
    const h = await mount();
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-a'), { target: { value: 'p1' } }); });
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-b'), { target: { value: 'p2' } }); });
    await keyboardClick(screen.getByTestId('swap-check-btn'));
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(h.showToast).toHaveBeenCalledWith('The draw has changed since you opened it.', 'error');
    expect(screen.queryByTestId('draw-swap-check')).toBeNull();
    expect(screen.getByTestId('swap-pick-a').value).toBe('');
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

  it('lists a knockout draw\'s competitors by first-round match', async () => {
    const bracket = { rounds: [[
      { id: 'r0-m0', matchNumber: 1, sideA: { id: 'k1', name: 'Ann', dojo: 'D1' }, sideB: { id: 'k2', name: 'Bo', dojo: 'D2' } },
      { id: 'r0-m1', matchNumber: 2, sideA: { id: 'k3', name: 'Cy', dojo: 'D1' }, sideB: { id: '', name: '' } },
    ]] };
    await mount({ c: { ...comp, format: 'knockout' }, pools: [], bracket });
    const groups = [...screen.getByTestId('swap-pick-a').querySelectorAll('optgroup')];
    expect(groups.map((g) => g.label)).toEqual(['First-round Match 1', 'First-round Match 2']);
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-a'), { target: { value: 'k2' } }); });
    await act(async () => { fireEvent.change(screen.getByTestId('swap-pick-b'), { target: { value: 'k3' } }); });
    await keyboardClick(screen.getByTestId('swap-check-btn'));
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1',
      { a: { id: 'k2', place: { slot: 1 } }, b: { id: 'k3', place: { slot: 2 } }, dryRun: true }, 'pw');
  });
});
