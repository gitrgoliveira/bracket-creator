// bc-dhas: a match opened from the public competition page's Bracket, Pools
// or League tab is read from the live data on every render, like the Overview
// tab's cards, so the self-run score editor it opens follows a result recorded
// or corrected on another device instead of the copy taken when it was tapped.
// What the tab added to the row when it was tapped (the round label, the
// competition's fields) still wins over the row's own values. A match that
// leaves the data closes the modal for good, because a draw discarded and made
// again reuses the match ids.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return React.createElement('div', { 'data-testid': 'probe-score-editor' });
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
};

let restoreGlobals;
let ViewerCompetition;
const hadResizeObserver = 'ResizeObserver' in globalThis;
const realResizeObserver = globalThis.ResizeObserver;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  // jsdom has no ResizeObserver; the Bracket tab watches its canvas with one.
  globalThis.ResizeObserver = class { observe() {} disconnect() {} };
  ({ ViewerCompetition } = await import('../../viewer_competition.jsx'));
});

afterAll(() => {
  restoreGlobals();
  if (hadResizeObserver) globalThis.ResizeObserver = realResizeObserver;
  else delete globalThis.ResizeObserver;
});

beforeEach(() => { probe.props = null; });

const yamada = { id: 'p1', name: 'Yamada' };
const tanaka = { id: 'p2', name: 'Tanaka' };

// A raw bracket row as the detail payload carries it: `round` is the engine's
// round index, which the tab's label must keep overriding.
const finalRow = (overrides = {}) => ({
  id: 'k1', round: 0, status: 'running', court: 'A',
  sideA: yamada, sideB: tanaka, modifiedAt: 100, ipponsA: [], ipponsB: [],
  ...overrides,
});

const competition = (overrides = {}) => ({
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'knockout', startTime: '09:00', courts: ['A'], players: [],
  ...overrides,
});

function page({ comp = competition(), bracket = { rounds: [[finalRow()]] }, pools = [], poolMatches = [], activeTab = 'bracket' } = {}) {
  return (
    <ViewerCompetition
      tournament={{ mode: 'self-run', name: 'T', competitions: [] }}
      competition={comp}
      pools={pools}
      poolMatches={poolMatches}
      standings={{}}
      bracket={bracket}
      onBack={vi.fn()}
      authed={false}
      tweaks={{ cardVariant: 1, showDojo: true }}
      activeTab={activeTab}
      onTabChange={vi.fn()}
    />
  );
}

async function openEditorOn(view, id) {
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: `open ${id}` })); });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
  expect(screen.getByTestId('probe-score-editor')).toBeTruthy();
  return view;
}

describe('ViewerCompetition: a match opened from a tab follows the live data (bc-dhas)', () => {
  it('the score editor follows the live row, and the tab\'s own fields still win', async () => {
    let view;
    await act(async () => { view = render(page()); });
    await openEditorOn(view, 'k1');
    expect(probe.props.match).toMatchObject({ id: 'k1', modifiedAt: 100, phase: 'bracket', round: 'Final', roundIndex: 0, compId: 'c1', compName: 'Open' });

    // A point recorded on another device reaches the page as a new row.
    await act(async () => {
      view.rerender(page({ bracket: { rounds: [[finalRow({ modifiedAt: 200, ipponsA: ['M'] })]] } }));
    });
    expect(probe.props.match.modifiedAt).toBe(200);
    expect(probe.props.match.ipponsA).toEqual(['M']);
    expect(probe.props.match).toMatchObject({ phase: 'bracket', round: 'Final', compId: 'c1' });
  });

  // The Pools tab builds the object it opens itself, so what it added is told
  // apart from the row by comparing the two.
  it('a match opened from the Pools tab follows the live row too', async () => {
    const poolRow = (overrides = {}) => ({
      id: 'Pool A-0', status: 'running', court: 'A', sideA: yamada, sideB: tanaka,
      modifiedAt: 100, ipponsA: [], ipponsB: [], ...overrides,
    });
    const pools = [{ poolName: 'Pool A', players: [yamada, tanaka] }];
    const poolsPage = (row) => page({ comp: competition({ format: 'mixed', status: 'pools' }), bracket: null, pools, poolMatches: [row], activeTab: 'pools' });
    let view;
    await act(async () => { view = render(poolsPage(poolRow())); });
    await act(async () => { fireEvent.click(document.querySelector('button.pool-match-numbered-row')); });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    expect(probe.props.match).toMatchObject({ id: 'Pool A-0', modifiedAt: 100, phase: 'pool', poolName: 'Pool A', compId: 'c1', compFormat: 'mixed' });

    await act(async () => { view.rerender(poolsPage(poolRow({ modifiedAt: 200, ipponsA: ['K'] }))); });
    expect(probe.props.match).toMatchObject({ modifiedAt: 200, ipponsA: ['K'], phase: 'pool', poolName: 'Pool A', compId: 'c1' });
  });

  it('a match that leaves the data closes the modal and does not come back with a new draw', async () => {
    let view;
    await act(async () => { view = render(page()); });
    await openEditorOn(view, 'k1');

    // The draw is discarded...
    await act(async () => {
      view.rerender(page({ comp: competition({ status: 'setup' }), bracket: null }));
    });
    expect(screen.queryByTestId('probe-score-editor')).toBeNull();
    expect(screen.queryByRole('dialog')).toBeNull();

    // ...and made again, reusing the id for another pairing.
    const suzuki = { id: 'p3', name: 'Suzuki' };
    await act(async () => {
      view.rerender(page({ bracket: { rounds: [[finalRow({ sideA: suzuki, modifiedAt: 0 })]] } }));
    });
    expect(screen.queryByTestId('probe-score-editor')).toBeNull();
    expect(screen.queryByRole('dialog')).toBeNull();
  });
});
