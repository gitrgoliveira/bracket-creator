// bc-wlpl: the home screen's address bar mirrors the watchlist as `?w=`
// (operator ruling 2026-09-23), so a bookmark or a reload carries the list.
//
// Mounted for real, because the rule that matters most here is an ORDER inside
// one effect, and that effect is where three review rounds of this PR found a
// defect: an inbound ?w= is read-only until it settles, and only then does the
// tab write its own list over it. Mirroring earlier overwrites a token that is
// still waiting for its competition's roster, and the next pass, reading the
// rewritten query, finds nothing outstanding and drops that entry in silence.
// The pure pieces (mirrorWatchlistParam, sharedLinkPass) are pinned in
// watchlist_link.test.jsx and watchlist_merge.test.jsx; no unit test can see
// the order.
import React from 'react';
import { render, act, cleanup, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';

let ViewerHome;

beforeAll(async () => {
  await import('../../ui.jsx');
  await import('../../viewer.jsx');
  await import('../../viewer_watchlist.jsx');
  ViewerHome = (await import('../../viewer_home.jsx')).ViewerHome;
});

beforeEach(() => {
  cleanup();
  window.localStorage.clear();
  window.sessionStorage.clear();
});

const comp = (id, prefix, players, extra = {}) => ({
  id, name: id, status: 'running', checkInEnabled: false, numberPrefix: prefix, players, ...extra,
});
const ALICE = { id: 'a1', name: 'Alice Abe', dojo: 'Nara', number: 'K1' };
const BOB = { id: 'a2', name: 'Bob Baba', dojo: 'Kobe', number: 'K2' };
const MEI = { id: 'b1', name: 'Mei Mori', dojo: 'Kobe', number: 'M1' };

const watchedIds = () => JSON.parse(window.localStorage.getItem('bc_watchlist') || '[]').map((e) => e.id);

async function mount(tournament, search) {
  window.history.replaceState(null, '', '/' + search);
  let view;
  await act(async () => { view = render(<ViewerHome tournament={tournament} />); });
  return view;
}

describe('the home address bar mirrors the watchlist', () => {
  it('a device with a list and no query gets the list in its address bar', async () => {
    window.localStorage.setItem('bc_watchlist', JSON.stringify([
      { type: 'player', id: 'a1', name: 'Alice Abe', dojo: 'Nara' },
    ]));
    await mount({ name: 'T', competitions: [comp('A', 'K', [ALICE, BOB])] }, '');
    expect(window.location.search).toBe('?w=K1');
  });

  it('removing an entry rewrites the bar, so a reload does not bring it back', async () => {
    window.localStorage.setItem('bc_watchlist', JSON.stringify([
      { type: 'player', id: 'a1', name: 'Alice Abe', dojo: 'Nara' },
      { type: 'player', id: 'a2', name: 'Bob Baba', dojo: 'Kobe' },
    ]));
    await mount({ name: 'T', competitions: [comp('A', 'K', [ALICE, BOB])] }, '');
    expect(window.location.search).toBe('?w=K1,K2');
    await act(async () => { fireEvent.click(screen.getByLabelText('Remove Bob Baba')); });
    expect(window.location.search).toBe('?w=K1');
    // The last entry out takes `w` with it rather than leaving `?w=`.
    await act(async () => { fireEvent.click(screen.getByLabelText('Remove Alice Abe')); });
    expect(window.location.search).toBe('');
  });

  it('an inbound link is not rewritten while a token still waits for its roster', async () => {
    // THE ORDER. M1's competition has not loaded; K1 lands on the first pass.
    // Mirroring then would write ?w=K1 and lose M1 for good.
    const partial = { name: 'T', competitions: [
      comp('A', 'K', [ALICE]),
      comp('B', 'M', [], { rosterAvailable: false }),
    ] };
    const view = await mount(partial, '?w=K1,M1');
    expect(watchedIds()).toEqual(['a1']);
    expect(window.location.search).toBe('?w=K1,M1');

    // The roster heals: M1 lands, the link settles, and only now does the bar
    // become the list -- which here is exactly the link.
    const healed = { name: 'T', competitions: [comp('A', 'K', [ALICE]), comp('B', 'M', [MEI])] };
    await act(async () => { view.rerender(<ViewerHome tournament={healed} />); });
    expect(watchedIds()).toEqual(['a1', 'b1']);
    expect(window.location.search).toBe('?w=K1,M1');
  });

  it('an inbound link ADDS, and the bar then shows the whole merged list', async () => {
    window.localStorage.setItem('bc_watchlist', JSON.stringify([
      { type: 'player', id: 'a2', name: 'Bob Baba', dojo: 'Kobe' },
    ]));
    await mount({ name: 'T', competitions: [comp('A', 'K', [ALICE, BOB])] }, '?w=K1');
    expect(watchedIds()).toEqual(['a2', 'a1']);
    expect(window.location.search).toBe('?w=K2,K1');
  });

  it("a printed tag's ?playerNumber= survives beside the mirrored list", async () => {
    await mount({ name: 'T', competitions: [comp('A', 'K', [ALICE, BOB])] }, '?playerNumber=K1');
    expect(watchedIds()).toEqual(['a1']);
    expect(window.location.search).toBe('?playerNumber=K1&w=K1');
  });
});
