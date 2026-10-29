// bc-adna: the court console IS the court, so it never overrides a busy
// shiaijo. Only the organiser's admin hosts pass `allowCourtForce`
// (admin_hosts_force_court); the console keeps its own court_busy handling
// (its refusal on the match, pick another bout, send back) and never shows
// the "Start anyway" or "Reopen anyway" override.

import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { createOwnStatusMoves } from '../helpers/own_status_moves.js';
import { attemptScoreWrite, courtBusyMessage } from '../../write_result.jsx';
import { useMatchReopen, ReopenFeedback } from '../../admin_scoring_shared.jsx';

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
  confirmDialog: vi.fn().mockResolvedValue(true),
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
const scheduled = {
  id: 'Pool A-2', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool',
  court: 'A', scheduledAt: '10:05', modifiedAt: 50,
  sideA: { id: 'p3', name: 'Sato' }, sideB: { id: 'p4', name: 'Suzuki' },
};
const tournament = { name: 'T', courts: ['A'], competitions: [{ id: 'c1', name: 'Cup' }] };

function courtBusyError(court = 'A', label = 'Teams · Match 2') {
  return Object.assign(new Error(courtBusyMessage({ court, label })), {
    code: 'court_busy', court, label, matchId: 'Teams-2', compId: 'c2',
  });
}

// The console's onEditScore, wired as admin.jsx's editMatchScore wires it:
// the force arm is on only when the host's options say allowCourtForce.
function editMatchScoreLike(recordScore) {
  return vi.fn((compId, matchId, result, match, opts = {}) => attemptScoreWrite({
    recordScore, confirmDialog: window.confirmDialog, compId, matchId, result, password: '', match,
    onRefusal: opts && opts.onRefusal, allowCourtForce: !!(opts && opts.allowCourtForce),
  }));
}

function renderConsole(onEditScore) {
  return render(
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
    />
  );
}

beforeEach(() => {
  probe.props = null;
  outbox.clear();
  window.confirmDialog = vi.fn().mockResolvedValue(true);
});

describe('the court console never forces a busy shiaijo (bc-adna)', () => {
  it('its editor is given no allowCourtForce, and its writes carry none', async () => {
    window.tournamentMatches = () => [running, scheduled];
    const onEditScore = vi.fn().mockResolvedValue({ status: 'running' });
    await act(async () => { renderConsole(onEditScore); });
    expect(probe.props, 'the editor is mounted').toBeTruthy();
    expect(probe.props.allowCourtForce).toBeUndefined();
    const claim = () => true;
    await act(async () => { await probe.props.onSubmit({ status: 'running', winner: null }, { onRefusal: claim }); });
    expect(onEditScore).toHaveBeenCalled();
    for (const call of onEditScore.mock.calls) {
      expect(call[4] && call[4].allowCourtForce).toBeFalsy();
      expect(call[2] && call[2].forceCourt).toBeFalsy();
    }
  });

  it('a start refused court_busy asks nothing, resends nothing and shows the console\'s own refusal', async () => {
    window.tournamentMatches = () => [scheduled];
    const recordScore = vi.fn().mockRejectedValue(courtBusyError());
    const onEditScore = editMatchScoreLike(recordScore);
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      await act(async () => { renderConsole(onEditScore); });
      const start = screen.getAllByRole('button', { name: 'Start match' })[0];
      await act(async () => { fireEvent.click(start); });
      await act(async () => { await Promise.resolve(); });
    } finally {
      logError.mockRestore();
    }
    expect(onEditScore).toHaveBeenCalledTimes(1);
    expect(onEditScore.mock.calls[0][1]).toBe('Pool A-2');
    expect(onEditScore.mock.calls[0][4] && onEditScore.mock.calls[0][4].allowCourtForce).toBeFalsy();
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(recordScore.mock.calls[0][2].forceCourt).toBeFalsy();
    const asked = window.confirmDialog.mock.calls.filter((c) => c[0] && c[0].confirmLabel === 'Start anyway');
    expect(asked).toHaveLength(0);
    // The console's own court_busy sentence, naming the blocker by its label.
    expect(document.body.textContent).toContain('Shiaijo A is running Teams · Match 2.');
  });
});

function ReopenHarness({ match }) {
  // The console's editors call useMatchReopen with no allowCourtForce.
  const ctl = useMatchReopen({ match, password: '', isComplete: true, own: null });
  return (
    <div>
      <button type="button" data-testid="do-reopen" onClick={() => ctl.reopen('')}>Reopen</button>
      <ReopenFeedback ctl={ctl} testIdPrefix="t" />
    </div>
  );
}

describe('a reopen on the court console keeps the court-busy panel as it was (bc-adna)', () => {
  it('offers Queue it and reopen and Leave it running, never Reopen anyway', async () => {
    window.API.reopenMatch = vi.fn().mockRejectedValueOnce(courtBusyError());
    const finished = { ...scheduled, id: 'Pool A-3', status: 'completed', winner: { id: 'p3', name: 'Sato' } };
    await act(async () => { render(<ReopenHarness match={finished} />); });
    await act(async () => { fireEvent.click(screen.getByTestId('do-reopen')); });
    expect(screen.getByTestId('t-conflict')).toBeTruthy();
    expect(document.querySelector('.reopen-conflict__head').textContent)
      .toBe('Shiaijo A is running Teams · Match 2. Finish it or send it back to the queue first.');
    expect(screen.queryByTestId('t-conflict-force-warning')).toBeNull();
    expect(screen.queryByTestId('t-reopen-anyway-button')).toBeNull();
    expect(screen.getByTestId('t-requeue-button')).toBeTruthy();
    expect(screen.getByTestId('t-conflict-dismiss')).toBeTruthy();
    expect(window.API.reopenMatch.mock.calls[0][3].forceCourt).toBe(false);
    expect(window.confirmDialog).not.toHaveBeenCalled();
  });
});
