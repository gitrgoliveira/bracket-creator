// bc-adna: the organiser's admin views are not the court. A start or a reopen
// they make on a shiaijo that already runs a bout is refused `court_busy` by
// the server; the admin hosts (the Scores tab and page, Pools, Bracket) turn
// that refusal into a warning the organiser can override ("Start anyway",
// "Reopen anyway"), resent with the request-only `forceCourt`. The court
// console never passes `allowCourtForce` (console_never_forces_court).
//
// Pinned here:
//   - attemptScoreWrite's court_busy arm (write_result.jsx): the words, the
//     resend with forceCourt, Cancel, the outbox's later refusal (claim path),
//     no second ask for a forced write, and nothing without allowCourtForce;
//   - each admin host hands the editor `allowCourtForce` and adds it to the
//     options of every score write, keeping the editor's own refusal claim;
//   - useMatchReopen + ReopenFeedback with allowCourtForce: the warning and
//     Reopen anyway, which resends the reopen with forceCourt.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { attemptScoreWrite } from '../../write_result.jsx';
import { useMatchReopen, ReopenFeedback } from '../../admin_scoring_shared.jsx';

const probe = { props: null };
function ProbeScoreEditor(props) {
  probe.props = props;
  return React.createElement('div', { 'data-testid': 'probe-score-editor' });
}

const STUBBED_GLOBALS = {
  // MODULE-EVAL captures: must exist before the imports in beforeAll
  ScoreEditorModal: ProbeScoreEditor,
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  BracketTree: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: () => [],
  confirmDialog: vi.fn().mockResolvedValue(true),
  PoolsViewer: () => null,
  LeagueStandingsViewer: () => null,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchCourtMatches: vi.fn().mockResolvedValue([]),
    subscribeToEvents: () => () => {},
    recordScore: vi.fn().mockResolvedValue(undefined),
    reopenMatch: vi.fn(),
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
  },
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let AdminPools, AdminBracket, AdminScoreEditor;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  Element.prototype.scrollIntoView = vi.fn();
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_pools.jsx');
  await import('../../admin_competition_bracket.jsx');
  const sched = await import('../../admin_schedule_score_editor.jsx');
  AdminPools = window.AdminPools;
  AdminBracket = window.AdminBracket;
  AdminScoreEditor = sched.AdminScoreEditor;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  probe.props = null;
});

const COURT_BUSY_HEADING = 'Shiaijo B is already running a bout';
const COURT_BUSY_BODY = 'Pool A · Match 3 is running on Shiaijo B. If you start this bout too, two bouts will be running on Shiaijo B at once. The court\'s score table will show both, and both can still be scored; the court\'s TV board shows only one of them. To get back to one bout at a time, the court\'s operator sends one of them back to the queue.';

function courtBusyError(court = 'B', label = 'Pool A · Match 3') {
  return Object.assign(new Error(`Shiaijo ${court} is running ${label}. Finish it or send it back to the queue first.`), {
    code: 'court_busy', court, label, matchId: 'Pool A-3', compId: 'c2',
  });
}

function scheduledMatch(overrides = {}) {
  return {
    id: 'm1', compId: 'c1', compName: 'Comp', status: 'scheduled',
    phase: 'pool', poolName: 'Pool 1', court: 'B',
    sideA: { id: 'p1', name: 'Yamada' },
    sideB: { id: 'p2', name: 'Tanaka' },
    ...overrides,
  };
}

describe('attemptScoreWrite: the court_busy arm (bc-adna)', () => {
  const startPatch = { status: 'running', winner: null };

  it('asks with the plan\'s words and resends with forceCourt on Start anyway', async () => {
    const landed = { applied: true, status: 'running' };
    const recordScore = vi.fn().mockRejectedValueOnce(courtBusyError()).mockResolvedValueOnce(landed);
    const confirmDialog = vi.fn().mockResolvedValue(true);
    const res = await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch,
      password: 'pw', allowCourtForce: true,
    });
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(confirmDialog.mock.calls[0][0]).toEqual({
      title: COURT_BUSY_HEADING,
      message: COURT_BUSY_BODY,
      confirmLabel: 'Start anyway',
      cancelLabel: 'Cancel',
      danger: true,
    });
    expect(recordScore).toHaveBeenCalledTimes(2);
    expect(recordScore.mock.calls[0][2].forceCourt).toBeUndefined();
    expect(recordScore.mock.calls[1][2]).toEqual({ ...startPatch, forceCourt: true });
    expect(res).toBe(landed);
  });

  it('Cancel sends nothing more and hands the refusal back', async () => {
    const err = courtBusyError();
    const recordScore = vi.fn().mockRejectedValueOnce(err);
    const confirmDialog = vi.fn().mockResolvedValue(false);
    await expect(attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch,
      password: 'pw', allowCourtForce: true,
    })).rejects.toBe(err);
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(err.courtForceCancelled).toBe(true);
  });

  it('without allowCourtForce nothing is asked and the refusal is thrown as before', async () => {
    const err = courtBusyError();
    const recordScore = vi.fn().mockRejectedValueOnce(err);
    const confirmDialog = vi.fn().mockResolvedValue(true);
    await expect(attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch, password: 'pw',
    })).rejects.toBe(err);
    expect(confirmDialog).not.toHaveBeenCalled();
    expect(recordScore).toHaveBeenCalledTimes(1);
  });

  it('a forced start refused court_busy again is not asked a second time', async () => {
    const second = courtBusyError('B', 'Pool A · Match 4');
    const recordScore = vi.fn().mockRejectedValueOnce(courtBusyError()).mockRejectedValueOnce(second);
    const confirmDialog = vi.fn().mockResolvedValue(true);
    await expect(attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch,
      password: 'pw', allowCourtForce: true,
    })).rejects.toBe(second);
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledTimes(2);
  });

  it('a refusal the outbox brings later is claimed, asked about and resent with forceCourt', async () => {
    let refuse;
    const forcedAnswer = { applied: true, pending: true, key: 'k2', settled: new Promise(() => {}) };
    const recordScore = vi.fn()
      .mockImplementationOnce(async (_c, _m, _r, _pw, _match, o) => {
        refuse = (e) => o.onRefusal(e);
        return { applied: true, pending: true, key: 'k1', settled: new Promise(() => {}) };
      })
      .mockResolvedValueOnce(forcedAnswer);
    const confirmDialog = vi.fn().mockResolvedValue(true);
    const onRefusal = vi.fn();
    const onForced = vi.fn();
    await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch,
      password: 'pw', allowCourtForce: true, onRefusal, onForced,
    });
    const claimed = refuse(courtBusyError());
    await vi.waitFor(() => expect(onForced).toHaveBeenCalledWith(forcedAnswer));
    expect(claimed).toBe(true);
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(recordScore.mock.calls[1][2]).toEqual({ ...startPatch, forceCourt: true });
    expect(onRefusal).not.toHaveBeenCalled();
  });

  it('a later refusal the organiser cancels goes to the caller, marked', async () => {
    let refuse;
    const recordScore = vi.fn().mockImplementationOnce(async (_c, _m, _r, _pw, _match, o) => {
      refuse = (e) => o.onRefusal(e);
      return { applied: true, pending: true, key: 'k1', settled: new Promise(() => {}) };
    });
    const confirmDialog = vi.fn().mockResolvedValue(false);
    const onRefusal = vi.fn().mockReturnValue(true);
    await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: startPatch,
      password: 'pw', allowCourtForce: true, onRefusal,
    });
    const err = courtBusyError();
    refuse(err);
    await vi.waitFor(() => expect(onRefusal).toHaveBeenCalledWith(err));
    expect(err.courtForceCancelled).toBe(true);
    expect(recordScore).toHaveBeenCalledTimes(1);
  });
});

describe('every admin host may force (bc-adna)', () => {
  const claim = () => true;

  async function expectForces(onEditScore, matchId) {
    expect(probe.props, 'the editor is mounted').toBeTruthy();
    expect(probe.props.allowCourtForce).toBe(true);
    await act(async () => { await probe.props.onSubmit({ status: 'running', winner: null }, { onRefusal: claim }); });
    const call = onEditScore.mock.calls[onEditScore.mock.calls.length - 1];
    expect(call[1]).toBe(matchId);
    expect(call[4]).toEqual({ onRefusal: claim, allowCourtForce: true });
  }

  it('the Scores tab and page (AdminScoreEditor)', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
    window.compMatches = () => [scheduledMatch()];
    await act(async () => {
      render(
        <AdminScoreEditor
          t={{ competitions: [{ id: 'c1', name: 'Comp' }] }}
          onEditScore={onEditScore}
          onMoveCourt={null}
          password="pw"
          showToast={vi.fn()}
        />
      );
    });
    await act(async () => { fireEvent.click(document.querySelector('button.test-score-open')); });
    await expectForces(onEditScore, 'm1');
  });

  it('the Pools tab (AdminPools)', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
    const rawPoolMatch = {
      id: 'Pool 1-1', status: 'scheduled',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
    };
    window.PoolsViewer = (props) => (
      <button data-testid="open-pool-match" onClick={() => props.onMatchClick(rawPoolMatch)}>open</button>
    );
    await act(async () => {
      render(
        <AdminPools
          c={{ id: 'c1', name: 'Comp', format: 'mixed', kind: 'individual', status: 'started' }}
          pools={[{ name: 'Pool 1', players: [] }]}
          poolMatches={[rawPoolMatch]}
          standings={[]}
          tweaks={{}}
          onEditScore={onEditScore}
          password="pw"
        />
      );
    });
    await act(async () => { fireEvent.click(screen.getByTestId('open-pool-match')); });
    await expectForces(onEditScore, 'Pool 1-1');
  });

  it('the Bracket panel (AdminBracket)', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
    const bm = scheduledMatch({ id: 'bm1', status: 'running' });
    delete bm.phase;
    delete bm.poolName;
    window.BracketTree = (props) => (
      <button data-testid="open-bracket-match" onClick={() => props.onMatchClick(bm, 0, 0)}>open</button>
    );
    await act(async () => {
      render(
        <AdminBracket
          c={{ id: 'c1', name: 'Comp', engi: false }}
          t={{ courts: ['B'] }}
          bracket={{ rounds: [[bm]] }}
          onMoveCourt={vi.fn()}
          onEditScore={onEditScore}
          tweaks={{}}
          password="pw"
        />
      );
    });
    await act(async () => { fireEvent.click(screen.getByTestId('open-bracket-match')); });
    await expectForces(onEditScore, 'bm1');
  });
});

function ReopenHarness({ match, allowCourtForce }) {
  const ctl = useMatchReopen({ match, password: 'pw', isComplete: true, own: null, allowCourtForce });
  return (
    <div>
      <button type="button" data-testid="do-reopen" onClick={() => ctl.reopen('')}>Reopen</button>
      <ReopenFeedback ctl={ctl} testIdPrefix="t" />
    </div>
  );
}

describe('Reopen anyway on an admin host (bc-adna)', () => {
  const finished = scheduledMatch({ id: 'm9', status: 'completed', winner: { id: 'p1', name: 'Yamada' }, scoreVersion: 4 });

  it('warns what a second running bout means and resends the reopen with forceCourt', async () => {
    const reopenMatch = vi.fn()
      .mockRejectedValueOnce(courtBusyError())
      .mockResolvedValueOnce({ applied: true, reopenedMatches: [] });
    window.API.reopenMatch = reopenMatch;
    await act(async () => { render(<ReopenHarness match={finished} allowCourtForce />); });
    await act(async () => { fireEvent.click(screen.getByTestId('do-reopen')); });

    expect(screen.getByTestId('t-conflict')).toBeTruthy();
    expect(document.querySelector('.reopen-conflict__head').textContent).toBe(COURT_BUSY_HEADING);
    expect(screen.getByTestId('t-conflict-force-warning').textContent).toBe(
      'Pool A · Match 3 is running on Shiaijo B. If you reopen this bout, two bouts will be running on Shiaijo B at once. The court\'s score table will show both; the court\'s TV board shows only one of them. Queue it and reopen sends Pool A · Match 3 back to the queue instead.',
    );
    // The existing remedies stay beside it.
    expect(screen.getByTestId('t-requeue-button').textContent).toBe('Queue it and reopen');
    expect(screen.getByTestId('t-conflict-dismiss').textContent).toBe('Leave it running');
    expect(reopenMatch.mock.calls[0][3].forceCourt).toBe(false);

    await act(async () => { fireEvent.click(screen.getByTestId('t-reopen-anyway-button')); });
    expect(reopenMatch).toHaveBeenCalledTimes(2);
    expect(reopenMatch.mock.calls[1].slice(0, 2)).toEqual(['c1', 'm9']);
    expect(reopenMatch.mock.calls[1][3]).toMatchObject({ forceCourt: true, force: false, reason: '' });
    expect(screen.queryByTestId('t-conflict')).toBeNull();
  });
});
