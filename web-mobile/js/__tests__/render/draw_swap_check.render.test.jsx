// bc-drev FR7: "If you swap them". Every swap door opens this panel first: it
// asks the server for a dry run (which saves nothing), words the answer, and
// only "Swap them" sends the swap. A draw that moved under the page closes it
// for the host to read the draw again; any other refusal stays in the panel.
// The panel is opened by a tap, so it takes that tap's bounce, and focus goes
// in to Cancel, which is the safe answer to Enter.
import React from 'react';
import { render, act, screen, cleanup, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = { API: { swapDrawPlaces: vi.fn() } };

let restore;
let DrawSwapCheck;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  ({ DrawSwapCheck } = await import('../../admin_draw_swap_check.jsx'));
});
afterAll(() => restore());
afterEach(() => { cleanup(); vi.useRealTimers(); });

const comp = { id: 'c1', format: 'mixed', status: 'draw-ready' };
const a = { id: 'a', name: 'Arthur Dick', number: 'IMS4', place: { pool: 'Pool B', position: 2 } };
const b = { id: 'b', name: 'Lena Park', number: 'IMS8', place: { pool: 'Pool C', position: 1 } };

const dryRun = {
  applied: false, dryRun: true,
  places: { a: b.place, b: a.place },
  moves: [
    { id: 'a', name: 'Arthur Dick', from: a.place, to: { pool: 'Pool C', position: 1 } },
    { id: 'b', name: 'Lena Park', from: b.place, to: { pool: 'Pool B', position: 2 } },
  ],
  numbers: [{ id: 'a', name: 'Arthur Dick', from: 'IMS4', to: 'IMS8' }, { id: 'b', name: 'Lena Park', from: 'IMS8', to: 'IMS4' }],
  seededWarning: [{ id: 'a', name: 'Arthur Dick', seed: 2, to: { pool: 'Pool C', position: 1 } }],
  bracketRebuilt: true, timesRescheduled: false, lineupsMoved: 0, lineupsRemoved: 0,
  separation: {
    before: [{ dojo: 'Team Omega', a: { id: 'a', name: 'Arthur Dick' }, b: { id: 'x', name: 'Xu' }, samePool: 'Pool B' }],
    after: [{ dojo: 'Team Omega', a: { id: 'a', name: 'Arthur Dick' }, b: { id: 'x', name: 'Xu' }, earliest: { matchId: 'r1-m0', round: 1, rounds: 3 } }],
  },
};

const refusal = (code, message) => Object.assign(new Error(message), { code });

async function open(props = {}) {
  const handlers = { onClose: vi.fn(), onSwapped: vi.fn(), onDrawChanged: vi.fn() };
  await act(async () => {
    render(<DrawSwapCheck comp={comp} bracket={null} a={a} b={b} password="pw" {...handlers} {...props} />);
  });
  return handlers;
}

describe('If you swap them', () => {
  it('asks for a dry run and words what the swap does', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    await open();
    expect(window.API.swapDrawPlaces).toHaveBeenCalledTimes(1);
    expect(window.API.swapDrawPlaces).toHaveBeenCalledWith('c1',
      { a: { id: 'a', place: a.place }, b: { id: 'b', place: b.place }, dryRun: true }, 'pw');
    const verdict = screen.getByTestId('swap-verdict');
    expect(verdict.textContent).toBe('Team Omega can meet later.');
    expect(verdict.className, 'no dojo is worse off: the normal style').toBe('draw-swap-check__line');
    expect(verdict.compareDocumentPosition(screen.getByTestId('swap-moves')) & Node.DOCUMENT_POSITION_FOLLOWING, 'the verdict comes first').toBeTruthy();
    expect(screen.getByTestId('swap-moves').textContent).toBe('IMS4 Arthur Dick goes to Pool C, IMS8 Lena Park to Pool B.');
    expect(screen.getByTestId('swap-separation').textContent).toContain('Team Omega: shared Pool B; now not before the Semifinals.');
    expect(screen.getByTestId('swap-numbers').textContent)
      .toBe('Numbers stay with the place: Arthur Dick takes IMS8, Lena Park takes IMS4. Swap their tags.');
    expect(screen.getByText('Arthur Dick is seed 2 and moves to Pool C.')).toBeInTheDocument();
    expect(screen.getByText('The knockout bracket is rebuilt from the new pools.')).toBeInTheDocument();
    expect(screen.getByText('The public pages update at once.').className, 'said in the body ink, not muted').toBe('draw-swap-check__line');
    expect(screen.getByTestId('swap-them')).not.toBeDisabled();
    expect(screen.getByTestId('swap-them').textContent).toBe('Swap them');
    expect(screen.getByTestId('swap-them').className).toBe('btn btn--primary');
  });

  it('warns first when the swap leaves a dojo worse off, and asks to Swap anyway', async () => {
    vi.useFakeTimers();
    const pair = (bId, m) => ({ dojo: 'Fudo', a: { id: 'a', name: 'Arthur Dick' }, b: { id: bId, name: bId }, ...m });
    const sf = { matchId: 'r1-m0', round: 1, rounds: 3, matchNumber: 5 };
    const qf = { matchId: 'r0-m0', round: 0, rounds: 3, matchNumber: 1 };
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue({
      ...dryRun,
      separation: {
        before: [pair('x', { earliest: sf }), pair('y', { earliest: sf }), pair('z', { earliest: sf })],
        after: [pair('x', { samePool: 'Pool C' }), pair('y', { samePool: 'Pool C' }), pair('z', { earliest: qf })],
      },
    });
    await open();
    await act(async () => { vi.advanceTimersByTime(1); });
    const verdict = screen.getByTestId('swap-verdict');
    expect(verdict.textContent, 'the dojo is named, judged as a whole').toBe('Fudo would share a pool.');
    expect(verdict.className).toBe('draw-swap-check__line draw-swap-check__line--warn');
    const swap = screen.getByTestId('swap-them');
    expect(swap.textContent).toBe('Swap anyway');
    expect(swap.className, 'the warning colour, in the primary\'s place').toBe('btn btn--warn');
    expect(swap.previousElementSibling.textContent).toBe('Cancel');
    expect(document.activeElement.textContent, 'Cancel keeps first focus').toBe('Cancel');
    // The per-dojo lines stay.
    expect(screen.getByTestId('swap-separation').textContent).toContain('Fudo (Arthur Dick and z): could first meet in the Semifinals (M5); now as early as the Quarterfinals (M1).');
  });

  it('offers Swap them only once the check is answered', async () => {
    let answer;
    window.API.swapDrawPlaces = vi.fn(() => new Promise((res) => { answer = res; }));
    await open();
    expect(screen.getByText('Working out what the swap does…')).toBeInTheDocument();
    expect(screen.getByTestId('swap-them')).toBeDisabled();
    await act(async () => { answer(dryRun); });
    expect(screen.getByTestId('swap-them')).not.toBeDisabled();
  });

  it('sends the same places without dryRun and hands the answer back', async () => {
    const applied = { ...dryRun, applied: true, dryRun: undefined };
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockResolvedValueOnce(applied);
    const h = await open();
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(window.API.swapDrawPlaces).toHaveBeenLastCalledWith('c1',
      { a: { id: 'a', place: a.place }, b: { id: 'b', place: b.place } }, 'pw');
    expect(h.onSwapped).toHaveBeenCalledWith(applied);
    expect(h.onClose).not.toHaveBeenCalled();
  });

  it('hands a resend of a swap that landed back as done', async () => {
    const resend = { applied: false, reason: 'already_swapped', moves: [] };
    window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockResolvedValueOnce(resend);
    const h = await open();
    await keyboardClick(screen.getByTestId('swap-them'));
    expect(h.onSwapped).toHaveBeenCalledWith(resend);
  });

  for (const code of ['draw_changed', 'draw_started', 'no_draw']) {
    it(`closes for the host to read the draw again on ${code}, from the check or the swap`, async () => {
      window.API.swapDrawPlaces = vi.fn().mockRejectedValue(refusal(code, 'The draw has changed since you opened it.'));
      const first = await open();
      expect(first.onDrawChanged).toHaveBeenCalledWith('The draw has changed since you opened it.');
      cleanup();
      window.API.swapDrawPlaces = vi.fn().mockResolvedValueOnce(dryRun).mockRejectedValueOnce(refusal(code, 'Moved.'));
      const second = await open();
      await keyboardClick(screen.getByTestId('swap-them'));
      expect(second.onDrawChanged).toHaveBeenCalledWith('Moved.');
      expect(second.onSwapped).not.toHaveBeenCalled();
    });
  }

  it('keeps any other refusal in the panel, with its sentence', async () => {
    window.API.swapDrawPlaces = vi.fn().mockRejectedValue(refusal('not_in_draw', 'Lena Park is not in the draw.'));
    const h = await open();
    expect(screen.getByRole('alert').textContent).toBe('Lena Park is not in the draw.');
    expect(screen.getByTestId('swap-them')).toBeDisabled();
    expect(h.onDrawChanged).not.toHaveBeenCalled();
  });

  it('cancels on Escape and on a tap outside, and sends nothing', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    const h = await open();
    await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
    expect(h.onClose).toHaveBeenCalledTimes(1);
    await keyboardClick(document.querySelector('.modal-backdrop'));
    expect(h.onClose).toHaveBeenCalledTimes(2);
    expect(window.API.swapDrawPlaces).toHaveBeenCalledTimes(1);
  });

  it('takes the bounce of the tap that opened it, so a double tap never swaps', async () => {
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    const h = await open();
    await pointerTap(screen.getByTestId('swap-them'));
    expect(h.onSwapped).not.toHaveBeenCalled();
    expect(window.API.swapDrawPlaces).toHaveBeenCalledTimes(1);
  });

  it('moves focus in to Cancel as it opens', async () => {
    vi.useFakeTimers();
    window.API.swapDrawPlaces = vi.fn().mockResolvedValue(dryRun);
    await open();
    await act(async () => { vi.advanceTimersByTime(1); });
    expect(document.activeElement.textContent).toBe('Cancel');
  });
});
