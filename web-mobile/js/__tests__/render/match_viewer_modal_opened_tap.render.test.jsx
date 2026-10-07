// bc-cfbd: the public MatchViewerModal, opened by a tap on a bracket card,
// survives the bounce of that tap. The second click of a double tap lands on
// the fresh backdrop one render later, and the backdrop's onClick is the
// modal's dismiss, which closed the card at once. tap_guard.jsx's
// useOpenedTapGuard swallows every pointer click in the layer for
// TAP_BOUNCE_MS after it opens; a deliberate tap after the window acts as
// before, and keyboard activation (detail 0) is never swallowed.
//
// Mounted through the real door, the competition page's Bracket tab. The card
// is a layer of its own when the self-run score editor closes (the editor
// replaces it while open), so the tap that closes the editor has a bounce of
// its own to survive.
//
// Pointer taps pass detail: 1 (helpers/tap_events.js), or the guard exempts
// them and these tests could never go red.
import React from 'react';
import { render, act, cleanup, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';

function ProbeScoreEditor({ onClose }) {
  return (
    <div data-testid="probe-score-editor">
      <button type="button" onClick={onClose}>Close editor</button>
    </div>
  );
}

// The tree stub hands back the row it was given, as BracketTree does.
function StubBracketTree({ rounds, onMatchClick }) {
  return (
    <div>
      {rounds.flat().map((m) => (
        <button type="button" key={m.id} onClick={() => onMatchClick(m, 0, 0, rounds.length)}>open {m.id}</button>
      ))}
    </div>
  );
}

const STUBBED_GLOBALS = {
  ScoreEditorModal: ProbeScoreEditor,
  BracketTree: StubBracketTree,
  bracketRoundLabel: () => 'Final',
  matchStateCell: () => 'vs',
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordScore: vi.fn() },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
  // jsdom has no ResizeObserver; the Bracket tab watches its canvas with one.
  ResizeObserver: class { observe() {} disconnect() {} },
};

let restoreGlobals;
let ViewerCompetition;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerCompetition } = await import('../../viewer_competition.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { cleanup(); vi.useRealTimers(); });

const final = {
  id: 'k1', round: 0, status: 'running', court: 'A',
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
  modifiedAt: 100, ipponsA: [], ipponsB: [],
};

const competition = {
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'knockout', startTime: '09:00', courts: ['A'], players: [],
};

const wait = (ms) => act(async () => { vi.advanceTimersByTime(ms); });
const dialog = () => screen.queryByRole('dialog');
const backdrop = () => document.querySelector('.modal-backdrop');
const closeCard = () => within(screen.getByRole('dialog')).getByRole('button', { name: 'Close' });

async function openTheMatch() {
  await act(async () => {
    render(
      <ViewerCompetition
        tournament={{ mode: 'self-run', name: 'T', competitions: [] }}
        competition={competition}
        pools={[]}
        poolMatches={[]}
        standings={{}}
        bracket={{ rounds: [[final]] }}
        onBack={vi.fn()}
        authed={false}
        tweaks={{ cardVariant: 1, showDojo: true }}
        activeTab="bracket"
        onTabChange={vi.fn()}
      />,
    );
  });
  await pointerTap(screen.getByRole('button', { name: 'open k1' }));
  expect(dialog()).not.toBeNull();
}

describe('the match modal opened by a tap', () => {
  it('survives the bounce landing on its backdrop', async () => {
    await openTheMatch();
    await wait(30);
    await pointerTap(backdrop());
    expect(dialog()).not.toBeNull();
  });

  it('survives the bounce landing on a control inside it', async () => {
    await openTheMatch();
    await wait(30);
    await pointerTap(closeCard());
    expect(dialog()).not.toBeNull();
  });

  it('is dismissed by a deliberate tap on the backdrop after the window', async () => {
    await openTheMatch();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(backdrop());
    expect(dialog()).toBeNull();
  });

  it('is dismissed at once from the keyboard (detail 0 is never a bounce)', async () => {
    await openTheMatch();
    await wait(30);
    await keyboardClick(closeCard());
    expect(dialog()).toBeNull();
  });

  it('survives the bounce of the tap that closes the score editor', async () => {
    await openTheMatch();
    await wait(TAP_BOUNCE_MS + 50);
    await pointerTap(screen.getByRole('button', { name: 'Report result' }));
    expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
    expect(dialog()).toBeNull();

    await pointerTap(screen.getByRole('button', { name: 'Close editor' }));
    expect(dialog()).not.toBeNull();
    await wait(30);
    await pointerTap(backdrop());
    expect(dialog()).not.toBeNull();
  });
});
