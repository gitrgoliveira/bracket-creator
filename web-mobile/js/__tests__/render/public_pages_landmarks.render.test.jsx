// The public pages read to a screen reader as pages (the impeccable audit of
// PR #507): each is one main landmark headed by one h1, the page's own title,
// and every filter dropdown says what it filters (WCAG 1.3.1, 4.1.2).
import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  ScoreEditorModal: () => null,
  bracketRoundLabel: () => 'Final',
  matchScoreStr: () => '',
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordScore: vi.fn() },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ViewerHome;
let ViewerSchedule;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  ({ ViewerHome } = await import('../../viewer_home.jsx'));
  ({ ViewerSchedule } = await import('../../viewer_schedule.jsx'));
});

afterAll(() => restoreGlobals());

const comp = {
  id: 'c1', name: 'Open', kind: 'individual', teamSize: 0, format: 'knockout',
  status: 'knockout', courts: ['A', 'B'], players: [], poolMatches: [],
  bracket: { rounds: [] },
};
const tournament = { name: 'London Cup', date: '', courts: ['A', 'B'], competitions: [comp] };

describe('public pages are one main landmark under one h1', () => {
  it('Home: headed by the tournament name, its court filter named', () => {
    render(<ViewerHome tournament={tournament} onSelectCompetition={vi.fn()} onAdminClick={vi.fn()} onOpenSchedule={vi.fn()} onRegister={vi.fn()} onOpenResults={vi.fn()} />);
    expect(screen.getByRole('main')).toBeTruthy();
    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s.map((h) => h.textContent)).toEqual(['London Cup']);
    expect(screen.getByRole('combobox', { name: 'Filter by shiaijo' })).toBeTruthy();
  });

  it('Schedule: headed "Schedule", both filters named', () => {
    render(<ViewerSchedule tournament={tournament} onBack={vi.fn()} tweaks={{ showDojo: true }} />);
    expect(screen.getByRole('main')).toBeTruthy();
    const h1s = screen.getAllByRole('heading', { level: 1 });
    expect(h1s.map((h) => h.textContent)).toEqual(['Schedule']);
    expect(screen.getByRole('combobox', { name: 'Filter by competition' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'Filter by shiaijo' })).toBeTruthy();
  });
});
