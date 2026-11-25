// bc-drpb: a draft whose draw the public may not see yet reads, on the public
// competition page, as a competition still in setup: Overview only (Swiss
// keeps its Standings tab), with the "Draw being prepared" card, and the
// header badge says "Draw being prepared". Publishing brings the Pools and
// Bracket tabs back with no reload; unpublishing takes them away again, and a
// reader sitting on a tab that went lands on Overview. The events themselves
// only refetch (app.jsx); what this pins is that the page follows the refetched
// competition on the next render.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

vi.mock('../../viewer_standings.jsx', async (importOriginal) => {
  const real = await importOriginal();
  return {
    ...real,
    PoolsViewer: () => <div data-testid="pools-viewer" />,
    SwissStandingsViewer: () => <div data-testid="swiss-standings" />,
    LeagueStandingsViewer: () => <div data-testid="league-standings" />,
  };
});

function StubBracketTree() {
  return <div data-testid="bracket-tree" />;
}

let restoreGlobals;
let ViewerCompetition;
let StatusBadge;

beforeAll(async () => {
  const ui = await import('../../ui.jsx');
  ({ StatusBadge } = ui);
  restoreGlobals = installWindowStubs({
    StatusBadge: ui.StatusBadge,
    EmptyState: ui.EmptyState,
    formatDate: ui.formatDate,
    BracketTree: StubBracketTree,
    bracketRoundLabel: () => 'Final',
    hasBothSides: (m) => !!(m && m.sideA && m.sideA.name && m.sideB && m.sideB.name),
    API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null) },
    Term: ({ children }) => <span>{children}</span>,
    GlossaryHint: ({ name }) => <span title={name} />,
    ResizeObserver: class { observe() {} disconnect() {} },
  });
  ({ ViewerCompetition } = await import('../../viewer_competition.jsx'));
});

afterAll(() => restoreGlobals());

const players = [
  { id: 'p1', name: 'Yamada', dojo: 'Kodokan' },
  { id: 'p2', name: 'Tanaka', dojo: 'Kodokan' },
  { id: 'p3', name: 'Suzuki', dojo: 'Mumeishi' },
];

const competition = (overrides = {}) => ({
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'mixed',
  status: 'draw-ready', startTime: '10:30', courts: ['A'], players,
  ...overrides,
});

// What the server answers a published draft: its pools and bracket.
const drawn = {
  pools: [{ poolName: 'Pool A', players }],
  poolMatches: [{ id: 'Pool A-1', sideA: players[0], sideB: players[1], status: 'scheduled', court: 'A' }],
  bracket: { rounds: [[{ id: 'k1', round: 0, status: 'scheduled', sideA: { id: '', name: 'Pool A-1st' }, sideB: { id: '', name: 'Pool B-1st' } }]] },
};
// What it answers the public for a hidden draft: the competition as in setup.
const asSetup = { pools: [], poolMatches: [], bracket: null };

const OFFICIATED = { mode: 'officiated', name: 'T', competitions: [] };

// The page with the tab state held by the host, as app.jsx holds it, so a
// correction the page asks for (a gone tab) shows as the tab it lands on.
function Host({ comp, data, initialTab = null, tournament = OFFICIATED, tabLog }) {
  const [tab, setTab] = React.useState(initialTab);
  return (
    <ViewerCompetition
      tournament={tournament}
      competition={comp}
      pools={data.pools}
      poolMatches={data.poolMatches}
      standings={{}}
      bracket={data.bracket}
      onBack={vi.fn()}
      authed={false}
      tweaks={{ cardVariant: 1, showDojo: true }}
      activeTab={tab}
      onTabChange={(id, replace) => { if (tabLog) tabLog.push([id, replace]); setTab(id); }}
    />
  );
}

const tabNames = () => [...document.querySelectorAll('.viewer__tab')].map((b) => b.textContent);
const activeTab = () => document.querySelector('.viewer__tab.is-active')?.textContent;

describe('StatusBadge: a hidden draft on a public surface (bc-drpb)', () => {
  it('reads "Draw being prepared" where the public page asks, "Draw ready" for the organiser', () => {
    const { rerender } = render(<StatusBadge status="draw-ready" drawPreparing />);
    expect(screen.getByText('Draw being prepared').className).toContain('badge--draw-preparing');
    rerender(<StatusBadge status="draw-ready" />);
    expect(screen.getByText('Draw ready')).toBeTruthy();
    // A started competition is never "being prepared", whatever is passed.
    rerender(<StatusBadge status="pools" drawPreparing />);
    expect(screen.getByText('Pools')).toBeTruthy();
  });
});

describe('ViewerCompetition: a hidden draft reads as setup (bc-drpb)', () => {
  it('shows Overview only, the "Draw being prepared" card and badge', async () => {
    await act(async () => { render(<Host comp={competition({ drawHidden: true })} data={asSetup} />); });
    expect(tabNames()).toEqual(['Overview']);
    const card = screen.getByTestId('draw-preparing-card');
    expect(card.textContent).toContain('Draw being prepared');
    expect(card.textContent).toContain('Starts at 10:30. 3 competitors entered. The draw shows here once it is published.');
    expect(document.querySelector('.viewer__head .badge').textContent).toBe('Draw being prepared');
    expect(screen.queryByText('Draw is ready')).toBeNull();
  });

  it('hides the draw even when the payload still carries it (the organiser\'s own read)', async () => {
    await act(async () => { render(<Host comp={competition({ drawHidden: true })} data={drawn} initialTab="pools" />); });
    expect(tabNames()).toEqual(['Overview']);
    expect(activeTab()).toBe('Overview');
    expect(screen.queryByTestId('pools-viewer')).toBeNull();
    expect(screen.queryByText(/Up next/)).toBeNull();
    expect(screen.getByTestId('draw-preparing-card')).toBeTruthy();
  });

  it('counts teams for a team competition and leaves out a missing start time', async () => {
    await act(async () => {
      render(<Host comp={competition({ drawHidden: true, kind: 'team', teamSize: 3, startTime: '', players: players.slice(0, 2) })} data={asSetup} />);
    });
    expect(screen.getByTestId('draw-preparing-card').textContent).toContain('2 teams entered. The draw shows here once it is published.');
    expect(screen.getByTestId('draw-preparing-card').textContent).not.toContain('Starts at');
  });

  it('keeps the Standings tab for Swiss', async () => {
    await act(async () => { render(<Host comp={competition({ drawHidden: true, format: 'swiss' })} data={asSetup} />); });
    expect(tabNames()).toEqual(['Overview', 'Standings']);
    expect(screen.getByTestId('draw-preparing-card')).toBeTruthy();
  });

  it('a self-run tournament shows its draw as soon as it is made', async () => {
    await act(async () => {
      render(<Host comp={competition({ drawHidden: true })} data={drawn} tournament={{ mode: 'self-run', name: 'T', competitions: [] }} />);
    });
    expect(tabNames()).toEqual(['Overview', 'Pools', 'Bracket']);
    expect(screen.queryByTestId('draw-preparing-card')).toBeNull();
    expect(document.querySelector('.viewer__head .badge').textContent).toBe('Draw ready');
  });
});

describe('ViewerCompetition: tabs follow publish and unpublish with no reload (bc-drpb)', () => {
  for (const tab of [['pools', 'Pools', 'pools-viewer'], ['bracket', 'Bracket', 'bracket-tree']]) {
    const [id, label, testId] = tab;
    it(`unpublish while on ${label} lands on Overview; publish brings ${label} back`, async () => {
      const tabLog = [];
      let view;
      // Published, the reader on the tab.
      await act(async () => { view = render(<Host comp={competition()} data={drawn} initialTab={id} tabLog={tabLog} />); });
      expect(tabNames()).toEqual(['Overview', 'Pools', 'Bracket']);
      expect(activeTab()).toBe(label);
      expect(screen.getByTestId(testId)).toBeTruthy();
      expect(document.querySelector('.viewer__head .badge').textContent).toBe('Draw ready');

      // draw_visibility_changed {hidden: true}: the refetch answers as setup.
      await act(async () => { view.rerender(<Host comp={competition({ drawHidden: true })} data={asSetup} initialTab={id} tabLog={tabLog} />); });
      expect(tabNames()).toEqual(['Overview']);
      expect(activeTab()).toBe('Overview');
      expect(screen.queryByTestId(testId)).toBeNull();
      expect(screen.getByTestId('draw-preparing-card')).toBeTruthy();
      // The gone tab is corrected in place (replace), so Back does not return to it.
      expect(tabLog).toContainEqual([null, true]);

      // draw_visibility_changed {hidden: false}: the draw is back.
      await act(async () => { view.rerender(<Host comp={competition()} data={drawn} initialTab={id} tabLog={tabLog} />); });
      expect(tabNames()).toEqual(['Overview', 'Pools', 'Bracket']);
      expect(screen.queryByTestId('draw-preparing-card')).toBeNull();
      expect(screen.getByText('Draw is ready')).toBeTruthy();
      expect(document.querySelector('.viewer__head .badge').textContent).toBe('Draw ready');
    });
  }
});
