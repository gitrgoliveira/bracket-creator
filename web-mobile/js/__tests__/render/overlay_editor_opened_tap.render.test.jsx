// bc-cfbd: an overlay score editor opened by a tap survives the bounce of that
// tap. The second click of a double tap lands on the fresh backdrop one render
// later, and the backdrop's onClick is the editor's dismiss, which closed an
// untouched editor silently. tap_guard.jsx's useOpenedTapGuard swallows it for
// TAP_BOUNCE_MS; a deliberate tap after the window dismisses as before. The
// inline court-console panel has no backdrop and is not covered.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.
import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
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

const base = {
  status: 'running',
  phase: 'pool',
  poolName: 'Pool 1',
  court: 'A',
};

const EDITORS = {
  individual: {
    id: 'm-ind',
    ...base,
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
  },
  team: {
    id: 'm-team',
    ...base,
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 'teamA', name: 'Team A' },
    sideB: { id: 'teamB', name: 'Team B' },
  },
  engi: {
    id: 'm-engi',
    ...base,
    compEngi: true,
    sideA: { id: 'p1', name: 'Aka One - Aka Two' },
    sideB: { id: 'p2', name: 'Shiro One - Shiro Two' },
  },
};

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const closeButton = (container) => (
  container.querySelector('[data-testid="engi-close-btn"]')
  || [...container.querySelectorAll('button')].find((b) => b.textContent.includes('Close'))
);
const backdrop = (container) => container.querySelector('[data-testid="scoring-modal-root"]');

function mount(match, props = {}) {
  const onClose = vi.fn();
  const utils = render(
    <ScoreEditorModal match={match} onClose={onClose} onSubmit={vi.fn().mockResolvedValue(undefined)} password="" {...props} />,
  );
  return { ...utils, onClose };
}

describe.each(Object.entries(EDITORS))('the %s overlay editor opened by a tap', (_kind, match) => {
  it('survives the bounce landing on its backdrop', async () => {
    const { container, onClose } = mount(match);
    await wait(30);
    await pointerTap(backdrop(container));
    expect(onClose).not.toHaveBeenCalled();
    expect(backdrop(container)).not.toBeNull();
  });

  it('does not swallow a tap on a control inside the editor', async () => {
    const { container, onClose } = mount(match);
    await wait(30);
    await pointerTap(closeButton(container));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('is dismissed by a deliberate tap on the backdrop after the window', async () => {
    const { container, onClose } = mount(match);
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop(container));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('is dismissed at once from the keyboard (detail 0 is never a bounce)', async () => {
    const { container, onClose } = mount(match);
    await wait(30);
    await keyboardClick(backdrop(container));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
