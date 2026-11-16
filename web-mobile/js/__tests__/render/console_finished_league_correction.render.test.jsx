// The court console corrects a finished league's result like every other host:
// the server's league_finished refusal is turned into the "Change it anyway"
// confirm (attemptScoreWrite, never gated on allowCourtForce, which the console
// does not send), a yes resends the same write with confirmFinished, and a
// cancel resends nothing. The unit tests pin attemptScoreWrite; this one drives
// it from the console's own editor, through admin_shiaijo.jsx's onEditScore.

import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { createOwnStatusMoves } from '../helpers/own_status_moves.js';
import { attemptScoreWrite, leagueFinishedConfirm } from '../../write_result.jsx';

const probe = { props: null };
const outbox = createOwnStatusMoves();

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => {
    probe.props = props;
    return <div data-testid="probe-score-editor" />;
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
    reopenMatch: vi.fn(),
    ...outbox.api,
  },
  confirmDialog: vi.fn(),
  PoolsViewer: () => null,
  compMatches: () => [],
};

let restoreGlobals;
let AdminShiaijoPage;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  Element.prototype.scrollIntoView = vi.fn();
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shiaijo.jsx');
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

const running = {
  id: 'Pool A-1', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool',
  court: 'A', scheduledAt: '10:00', modifiedAt: 100, scoreVersion: 3,
  sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
};
const tournament = { name: 'T', courts: ['A'], competitions: [{ id: 'c1', name: 'Cup' }] };

function leagueFinishedError(removedCourts = []) {
  return Object.assign(new Error('This league has finished.'), {
    code: 'league_finished', leagueFinished: { removedCourts },
  });
}

// The console's onEditScore, wired as admin.jsx's editMatchScore wires it.
function editMatchScoreLike(recordScore) {
  return vi.fn((compId, matchId, result, match, opts = {}) => attemptScoreWrite({
    recordScore, confirmDialog: window.confirmDialog, compId, matchId, result, password: '', match,
    onRefusal: opts && opts.onRefusal, allowCourtForce: !!(opts && opts.allowCourtForce),
  }));
}

async function mountConsole(onEditScore) {
  window.tournamentMatches = () => [running];
  await act(async () => {
    render(
      <AdminShiaijoPage
        tournament={tournament}
        court="A"
        onBack={vi.fn()}
        onEditScore={outbox.scoreDoor(onEditScore)}
        onMoveCourt={vi.fn()}
        onLogout={vi.fn()}
        onViewerMode={vi.fn()}
        password=""
        showToast={vi.fn()}
        tweaks={{}}
        onSwitchCourt={vi.fn()}
      />,
    );
  });
  expect(probe.props, 'the editor is mounted').toBeTruthy();
}

beforeEach(() => {
  probe.props = null;
  outbox.clear();
  window.confirmDialog = vi.fn();
});

const finish = { status: 'completed', winner: 'Yamada', correctionReason: 'typed on the wrong side' };

describe('a result corrected from the court console on a finished league', () => {
  it('asks the finished-league confirm and resends the write with confirmFinished on a yes', async () => {
    window.confirmDialog.mockResolvedValue(true);
    const recordScore = vi.fn()
      .mockRejectedValueOnce(leagueFinishedError(['C']))
      .mockResolvedValueOnce({ applied: true });
    await mountConsole(editMatchScoreLike(recordScore));

    await act(async () => { await probe.props.onSubmit({ ...finish }, {}); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0]).toEqual(leagueFinishedConfirm({ removedCourts: ['C'] }));
    expect(recordScore).toHaveBeenCalledTimes(2);
    expect(recordScore.mock.calls[0][2].confirmFinished).toBeUndefined();
    expect(recordScore.mock.calls[1][2].confirmFinished).toBe(true);
    // The console never sends the court override beside it.
    expect(recordScore.mock.calls[1][2].forceCourt).toBeFalsy();
  });

  it('resends nothing on a cancel, and the refusal carries the cancel mark', async () => {
    window.confirmDialog.mockResolvedValue(false);
    const refusal = leagueFinishedError();
    const recordScore = vi.fn().mockRejectedValue(refusal);
    await mountConsole(editMatchScoreLike(recordScore));

    await act(async () => { try { await probe.props.onSubmit({ ...finish }, {}); } catch { /* the editor shows it */ } });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(refusal.leagueFinishedCancelled).toBe(true);
  });
});
