// F5 (decision q-f5-card-unsent): while this phone holds a finished result it
// has not sent yet, the public match card reads the SERVER's copy, which knows
// nothing of it: it offered Report result as though nothing had been entered,
// so a competitor could open the editor and enter the result a second time over
// their own. While API.hasPendingTerminalWrite is true the card says the result
// is waiting on this phone and does not offer Report result; it refreshes on the
// two signals the held-writes count uses (sync status, unsent-write count).
import React from 'react';
import { render, act, cleanup, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const WAITING = 'Result entered on this phone, not sent yet. It is sent when the connection returns.';

const listeners = { sync: new Set(), unsent: new Set() };
const subscribeTo = (set) => (fn) => { set.add(fn); return () => set.delete(fn); };
const pending = { value: false };

const STUBBED_GLOBALS = {
  ScoreEditorModal: () => <div data-testid="probe-score-editor" />,
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  subscribeSyncStatus: subscribeTo(listeners.sync),
  subscribeUnsentWrites: subscribeTo(listeners.unsent),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    hasPendingTerminalWrite: vi.fn((compId, matchId) => pending.value && compId === 'c1' && matchId === 'm1'),
  },
};

let restoreGlobals;
let MatchViewerModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  pending.value = false;
  listeners.sync.clear();
  listeners.unsent.clear();
  STUBBED_GLOBALS.API.hasPendingTerminalWrite.mockClear();
});

afterEach(() => cleanup());

const scheduledMatch = () => ({
  id: 'm1', compId: 'c1', compName: 'Comp', status: 'scheduled',
  phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' },
  sideB: { id: 'p2', name: 'Tanaka' },
});

async function mount() {
  await act(async () => {
    render(<MatchViewerModal match={scheduledMatch()} onClose={vi.fn()} tournament={{ mode: 'self-run' }} compId="c1" />);
  });
}

describe('the public match card while a finished result waits on this phone', () => {
  it('says the result is waiting and does not offer Report result', async () => {
    pending.value = true;
    await mount();
    expect(screen.getByText(WAITING)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Report result' })).toBeNull();
    expect(STUBBED_GLOBALS.API.hasPendingTerminalWrite).toHaveBeenCalledWith('c1', 'm1');
  });

  it('offers Report result, with no waiting line, when nothing is waiting', async () => {
    await mount();
    expect(screen.getByRole('button', { name: 'Report result' })).toBeTruthy();
    expect(screen.queryByText(WAITING)).toBeNull();
  });

  it('follows the unsent-write signal both ways while the card is open', async () => {
    await mount();
    expect(screen.getByRole('button', { name: 'Report result' })).toBeTruthy();

    pending.value = true;
    await act(async () => { listeners.unsent.forEach((fn) => fn()); });
    expect(screen.getByText(WAITING)).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Report result' })).toBeNull();

    pending.value = false;
    await act(async () => { listeners.sync.forEach((fn) => fn()); });
    expect(screen.queryByText(WAITING)).toBeNull();
    expect(screen.getByRole('button', { name: 'Report result' })).toBeTruthy();
  });
});
