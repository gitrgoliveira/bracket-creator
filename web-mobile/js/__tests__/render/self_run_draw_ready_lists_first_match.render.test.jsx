// The walk's finding S1: a self-run competition stays draw-ready until its
// first result, and the competitors start it themselves (its first recorded
// result starts it). So on the public page of a self-run tournament its
// matches are listed where a competitor looks for them: the home page's Now
// and Up next strips, and the competition's Overview, which shows Up next in
// place of "Draw is ready". An officiated tournament keeps today's reading: a
// draw-ready competition there waits for the organiser, and nothing of it is
// listed as running or next.
import React from 'react';
import { render, act, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  bracketRoundLabel: () => 'Final',
  matchScoreStr: () => '',
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordScore: vi.fn() },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ViewerHome;
let ViewerOverview;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerHome } = await import('../../viewer_home.jsx'));
  ({ ViewerOverview } = await import('../../viewer_competition.jsx'));
});

afterAll(() => restoreGlobals());

const side = (id, name) => ({ id, name, dojo: 'Kita Dojo' });

const firstMatch = {
  id: 'Pool A-0', status: 'scheduled', court: 'A', scheduledAt: '10:00',
  sideA: side('p1', 'Akira Doors'), sideB: side('p2', 'Ben Doors'), ipponsA: [], ipponsB: [],
};

const comp = {
  id: 'c1', name: 'Doors Open', kind: 'individual', teamSize: 0, format: 'mixed',
  status: 'draw-ready', courts: ['A'], players: [side('p1', 'Akira Doors'), side('p2', 'Ben Doors')],
  poolMatches: [firstMatch], bracket: { rounds: [] },
};

const tournament = (mode) => ({ mode, name: 'Cup', date: '', courts: ['A'], competitions: [comp] });

async function mountHome(mode) {
  await act(async () => {
    render(
      <ViewerHome
        tournament={tournament(mode)}
        onSelectCompetition={vi.fn()} onAdminClick={vi.fn()} onOpenSchedule={vi.fn()}
        onRegister={vi.fn()} onOpenResults={vi.fn()}
      />,
    );
  });
}

async function mountOverview(mode) {
  const m = { ...firstMatch, compId: comp.id, compName: comp.name, phase: 'pool', poolName: 'Pool A' };
  await act(async () => {
    render(
      <ViewerOverview
        c={comp} tournament={tournament(mode)} compId={comp.id}
        currentMatch={m} runningMatches={[]} upcomingMatches={[m]} recentMatches={[]} allMatches={[m]}
        tweaks={{ showDojo: true }} standings={{}} pools={[]} poolMatches={[m]}
        onSwitchTab={vi.fn()} hasActiveFilter={false} filterLabel="" highlightPlayers={null}
      />,
    );
  });
}

const upNextTitle = () => document.querySelector('.viewer__upnext-title');

describe('a draw-ready competition on the public page', () => {
  it('self-run: the home page lists its first match under Up next', async () => {
    await mountHome('self-run');
    expect(upNextTitle(), 'the home page has an Up next strip').toBeTruthy();
    const strip = upNextTitle().parentElement;
    expect(strip.textContent).toContain('Akira Doors');
    expect(strip.textContent).toContain('Ben Doors');
  });

  it('officiated: the home page lists nothing of it as next', async () => {
    await mountHome('officiated');
    expect(upNextTitle()).toBeNull();
  });

  it('self-run: the Overview shows the first match under Up next, not "Draw is ready"', async () => {
    await mountOverview('self-run');
    expect(screen.queryByText('Draw is ready')).toBeNull();
    expect(screen.getByText(/Up next/)).toBeTruthy();
    expect(document.body.textContent).toContain('Akira Doors');
  });

  it('officiated: the Overview still says the draw is ready', async () => {
    await mountOverview('officiated');
    expect(screen.getByText('Draw is ready')).toBeTruthy();
  });
});
