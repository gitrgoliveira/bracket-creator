// Stage 5 review 9, M1: a running-strip chip takes the operator to another
// competition's Scores tab, which drops a score editor the page shows inline.
// The page hands the topbar its leave question (`beforeLeave`, R1): the chip
// goes only once it answers true. A page with no editor to protect passes none
// and the chip goes at once.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from 'vitest';

let AdminTopbar;
const running = { id: 'm1', compId: 'c2', status: 'running', court: 'B', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } };
const tournament = { name: 'Spring Taikai', competitions: [{ id: 'c2', name: 'Yudansha' }] };

beforeAll(async () => {
  await import('../../admin_shell.jsx');
  AdminTopbar = window.AdminTopbar;
});

let prevMatches;
beforeEach(() => {
  window.subscribeSyncStatus = (fn) => { fn('synced'); return () => {}; };
  window.subscribeUnsentWrites = undefined;
  prevMatches = window.compMatches;
  window.compMatches = (cc) => (cc.id === 'c2' ? [running] : []);
  window.__adminNavigateToScore = vi.fn();
});
afterEach(() => {
  window.compMatches = prevMatches;
  delete window.__adminNavigateToScore;
});

const chip = (c) => c.querySelector('.running-strip__chip');

describe('the running strip asks the page before it leaves', () => {
  it('Keep entries stays; Discard entries goes', async () => {
    let answer = false;
    const beforeLeave = vi.fn(async () => answer);
    const { container } = render(<AdminTopbar onLogout={() => {}} onViewerMode={() => {}} tournament={tournament} beforeLeave={beforeLeave} />);
    await act(async () => { fireEvent.click(chip(container)); });
    expect(beforeLeave).toHaveBeenCalledTimes(1);
    expect(window.__adminNavigateToScore).not.toHaveBeenCalled();
    answer = true;
    await act(async () => { fireEvent.click(chip(container)); });
    expect(window.__adminNavigateToScore).toHaveBeenCalledWith('c2');
  });

  it('with no question to ask the chip goes in the same tick', () => {
    const { container } = render(<AdminTopbar onLogout={() => {}} onViewerMode={() => {}} tournament={tournament} />);
    fireEvent.click(chip(container));
    expect(window.__adminNavigateToScore).toHaveBeenCalledWith('c2');
  });
});
