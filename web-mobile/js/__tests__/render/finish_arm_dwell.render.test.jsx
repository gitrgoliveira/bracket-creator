// bc-dtfn: a double tap must not pass the two-tap Finish / End match guard.
//
// The first tap arms the button and the second commits. Preact renders on a
// microtask, so the bounce of the arming tap (16-44ms later on a touchscreen)
// used to land on the armed render and commit: it finished the bout and, with
// Finish + Start Next, started the next one. The guard now needs the button to
// have been armed for TAP_BOUNCE_MS before a POINTER tap commits
// (useArmedConfirm, tap_guard.jsx). A keyboard-synthesized click (detail 0)
// is never treated as a bounce.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js): fireEvent.click's
// default of 0 is what the guard exempts, so the tests could never fail.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });

const individualMatch = () => ({
  id: 'm-ind', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
  ipponsB: ['M'],
});

// Every numbered bout carries a result, so bc-tmfn's "unfought bout" refusal
// does not stand in front of Finish.
const teamMatch = () => ({
  id: 'm-team', compId: 'comp1', status: 'running', phase: 'pool', poolName: 'Pool 1', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' },
  sideB: { id: 'team-osaka', name: 'Osaka' },
  subResults: [
    { position: 1, sideA: '', sideB: '', ipponsA: [], ipponsB: ['M'], winner: 'Osaka' },
    { position: 2, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' },
    { position: 3, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' },
  ],
});

const kachinukiMatch = () => ({
  id: 'm-kachi', compId: 'comp1', status: 'running', phase: 'bracket',
  round: 'Semi-final', matchNumber: 1, court: 'A',
  compKind: 'team', teamSize: 5, compFormat: 'knockout', teamMatchType: 'kachinuki',
  sideA: { id: 'team-A', name: 'Team A' },
  sideB: { id: 'team-B', name: 'Team B' },
  subResults: [
    { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M', 'K'], winner: 'B1' },
    { position: 2, sideA: 'A2', sideB: 'B1', ipponsA: ['M', 'K'], ipponsB: [], winner: 'A2' },
    { position: 3, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
  ],
});

async function mount(match, { withNext = false } = {}) {
  const onSubmit = vi.fn().mockResolvedValue(undefined);
  const onSubmitAndNext = withNext ? vi.fn().mockResolvedValue(undefined) : undefined;
  if (match.compFormat === 'knockout') {
    window.API.fetchCompetitionDetails.mockResolvedValue({
      id: 'comp1', config: { format: 'knockout', teamMatchType: 'kachinuki', naginata: false, players: [] },
    });
  } else {
    window.API.fetchCompetitionDetails.mockResolvedValue(null);
  }
  await act(async () => {
    render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} onSubmitAndNext={onSubmitAndNext} password="" />);
  });
  const completed = () => [...onSubmit.mock.calls, ...(onSubmitAndNext ? onSubmitAndNext.mock.calls : [])]
    .filter(([p]) => p && p.status === 'completed').length;
  return { completed };
}

const SITES = [
  { name: 'individual Finish', match: individualMatch, button: () => screen.getByText('Finish'), armed: 'Tap again to finish' },
  { name: 'individual Finish + Start Next', match: individualMatch, withNext: true, button: () => screen.getByText('Finish + Start Next →'), armed: 'Tap again to finish →' },
  { name: 'team Finish', match: teamMatch, button: () => screen.getByText('Finish'), armed: 'Tap again to finish' },
  { name: 'team Finish + Start Next', match: teamMatch, withNext: true, button: () => screen.getByText('Finish + Start Next →'), armed: 'Tap again to finish →' },
  { name: 'kachinuki End match', match: kachinukiMatch, button: () => screen.getByTestId('kachinuki-end-match-button'), armed: /^Tap again/ },
];

describe.each(SITES)('bc-dtfn: $name', ({ match, withNext, button, armed }) => {
  it('a double tap only arms the button', async () => {
    const { completed } = await mount(match(), { withNext });
    const btn = button();
    await pointerTap(btn);
    await pointerTap(btn);
    expect(btn.textContent).toMatch(armed);
    expect(completed()).toBe(0);
  });

  it('a deliberate second tap after the window commits once', async () => {
    const { completed } = await mount(match(), { withNext });
    const btn = button();
    await pointerTap(btn);
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(btn);
    await wait(10);
    expect(completed()).toBe(1);
  });

  it('a keyboard-synthesized click (detail 0) on the armed button commits at once', async () => {
    const { completed } = await mount(match(), { withNext });
    const btn = button();
    await pointerTap(btn);
    await keyboardClick(btn);
    await wait(10);
    expect(completed()).toBe(1);
  });
});
