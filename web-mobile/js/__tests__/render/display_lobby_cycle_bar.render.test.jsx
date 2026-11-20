// The lobby's page-cycle bar is styled by .lobby-cycle-bar in styles.css,
// which also hides it under prefers-reduced-motion. The page sets only the
// cycle length, so no inline animation or injected keyframes can slip past
// that rule again (the impeccable audit of PR #507; DESIGN.md §3 Motion).
import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let LobbyDisplay;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({ isNonPublicOrigin: () => false });
  await import('../../bracket.jsx');
  ({ LobbyDisplay } = await import('../../display_lobby.jsx'));
});

afterEach(() => cleanup());
afterAll(() => restoreGlobals());

// A competition with one queued pool bout on each court given, so the lobby
// counts those courts as in use.
const compOn = (courts) => [{
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'pools', status: 'pools',
  poolMatches: courts.map((court, i) => ({
    id: `Pool A-${i + 1}`, pool: 'Pool A', court, status: 'scheduled', scheduledAt: '10:00',
    sideA: { id: `a${i}`, name: `Shiro ${i}`, dojo: 'D' }, sideB: { id: `b${i}`, name: `Aka ${i}`, dojo: 'D' },
  })),
}];

describe('LobbyDisplay page-cycle bar', () => {
  it('is the stylesheet class with the cycle length, and nothing inline animates', () => {
    // Three courts make two pages (two courts a page), so the bar shows.
    const tournament = { name: 'Spring Taikai', courts: ['A', 'B', 'C'] };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={compOn(['A', 'B', 'C'])} />);
    const bar = container.querySelector('.lobby-cycle-bar');
    expect(bar).not.toBeNull();
    expect(bar.style.getPropertyValue('--lobby-cycle-ms')).toBe('10000ms');
    expect(bar.style.animation).toBe('');
    expect(container.querySelectorAll('style').length).toBe(0);
  });

  it('is not drawn when every court fits on one page', () => {
    const tournament = { name: 'Spring Taikai', courts: ['A', 'B'] };
    const { container } = render(<LobbyDisplay tournament={tournament} competitions={compOn(['A', 'B'])} />);
    expect(container.querySelector('.lobby-cycle-bar')).toBeNull();
  });
});
