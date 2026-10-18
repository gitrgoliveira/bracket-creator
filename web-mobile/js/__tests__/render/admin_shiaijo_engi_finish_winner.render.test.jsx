import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-evts stage 6 (a defect the journeys found): the court console reads a
// match it finished here as finished at once, from the finish the outbox
// holds (finishedHere, X3), with the winner that finish names. An engi
// finish names none (the server decides it from the flags,
// engine.engiWinnerSide), so the court row showed the match finished with no
// winner tick until the server's copy came. The row and its winner arrive
// together: the side with more flags in the finish.

const captured = { props: null };

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => {
    captured.props = props;
    return <div data-testid="score-editor" data-match={props.match && props.match.id} />;
  },
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
  },
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

// sideA (Yamada) is Aka, the right-hand side; sideB (Tanaka) is Shiro.
const engiRunning = {
  id: 'Pool A-1', compId: 'c1', compName: 'Kata', status: 'running', phase: 'pool',
  court: 'A', scheduledAt: '10:00', modifiedAt: 100, scoreVersion: 3, flagsA: 2, flagsB: 0,
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
};
const tournament = { name: 'T', courts: ['A'], competitions: [{ id: 'c1', name: 'Kata' }] };
const pending = () => ({ applied: true, pending: true, key: 'k', settled: new Promise(() => {}) });

const page = (onEditScore) => (
  <AdminShiaijoPage
    tournament={tournament} court="A" onBack={vi.fn()} onEditScore={onEditScore} onMoveCourt={vi.fn()}
    onLogout={vi.fn()} onViewerMode={vi.fn()} password="" showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()}
  />
);
const completedRow = () => document.querySelector('.shiaijo-qrow--complete');

beforeEach(() => {
  captured.props = null;
  window.tournamentMatches = () => [engiRunning];
});

describe('an engi match finished here shows its winner on the court row at once', () => {
  it('the row reads finished with the winner the flags decide, before the server\'s copy', async () => {
    render(page(vi.fn(async () => pending())));
    // The engi Save: its counts, no winner (the server decides it).
    await act(async () => { await captured.props.onSubmit({ flagsA: 3, flagsB: 0, status: 'completed' }); });
    const row = completedRow();
    expect(row, 'the finished match is on the Completed list').not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--aka .bc-winner-tick'), 'Aka (three flags) carries the winner tick').not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--shiro .bc-winner-tick')).toBeNull();
  });

  it('Shiro\'s flags decide it the other way', async () => {
    render(page(vi.fn(async () => pending())));
    await act(async () => { await captured.props.onSubmit({ flagsA: 1, flagsB: 4, status: 'completed' }); });
    const row = completedRow();
    expect(row.querySelector('.shiaijo-qrow__side--shiro .bc-winner-tick')).not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--aka .bc-winner-tick')).toBeNull();
  });
});
