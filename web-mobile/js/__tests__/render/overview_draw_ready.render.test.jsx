// bc-drev: the Overview of a draw-ready competition. For a draw the Draw
// review covers (pools + knockout, knockout only) the page holds the Preview
// action card and then the review card: the header line says what to do, so
// the Draw ready and To start cards are gone. A league or a Swiss competition
// keeps both, its Draw ready meta line joined by a comma, never a middle dot.
import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  API: {
    fetchDojoSeparation: vi.fn().mockResolvedValue([]),
    fetchSwapSuggestions: vi.fn().mockResolvedValue({ suggestions: [], stuck: [] }),
    subscribeToEvents: vi.fn(() => () => {}),
    estimateCompetitionSchedule: vi.fn(() => new Promise(() => {})),
  },
};

let restore;
let AdminCompOverview;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_competition_overview.jsx');
  AdminCompOverview = window.AdminCompOverview;
});
afterAll(() => restore());
afterEach(() => cleanup());

const players = [{ id: 'p1', name: 'Ann', dojo: 'D1', seed: 1 }, { id: 'p2', name: 'Bo', dojo: 'D2' }];

async function mount(c) {
  await act(async () => {
    render(<AdminCompOverview c={{ id: 'c1', name: 'Open', status: 'draw-ready', players, ...c }} tournament={{ courts: ['A'] }}
      pools={[]} poolMatches={[]} bracket={null} onSection={() => {}} password="pw" showToast={() => {}} onRefreshCompetition={() => {}} />);
  });
}

describe('the Overview of a draw-ready competition', () => {
  for (const format of ['mixed', 'knockout']) {
    it(`holds the Preview card and then the Draw review for a ${format} draw, and no Draw ready or To start card`, async () => {
      await mount({ format });
      const preview = screen.getByTestId('draw-ready-preview-btn');
      const review = screen.getByTestId('draw-review');
      expect(preview.compareDocumentPosition(review) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
      expect(screen.queryByTestId('draw-ready-card')).toBeNull();
      expect(screen.queryByText('To start')).toBeNull();
    });
  }

  for (const format of ['league', 'swiss']) {
    it(`keeps the Draw ready and To start cards for a ${format} competition, its meta line joined by a comma`, async () => {
      await mount({ format });
      const card = screen.getByTestId('draw-ready-card');
      expect(card.querySelector('.card__sub').textContent).toBe('2 Participants, 1 seeded');
      expect(card.textContent).not.toContain('·');
      expect(screen.getByText('To start')).toBeInTheDocument();
      expect(screen.queryByTestId('draw-review')).toBeNull();
    });
  }
});
