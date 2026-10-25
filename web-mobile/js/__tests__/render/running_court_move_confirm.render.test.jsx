import React from 'react';
import { render, act, fireEvent, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { confirmMatchLabel, SCORE_KEPT_SENTENCE, runningCourtMoveText } from '../../court_move.jsx';
import { UNSAVED_ENTRIES_DISCARDED } from '../../unsaved_entries.jsx';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';

const probe = {
  scoreEditor: null,
  bracketTree: null,
};

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => {
    probe.scoreEditor = props;
    return <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />;
  },
  BracketTree: (props) => {
    probe.bracketTree = props;
    return (
      <div data-testid="bracket-tree">
        {props.bracket?.rounds?.[0]?.map((m, idx) => (
          <button key={m.id} data-testid={`pick-match-${m.id}`} onClick={() => props.onMatchClick(m, 0, idx)}>
            {`Pick ${m.id}`}
          </button>
        ))}
      </div>
    );
  },
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchHighlightedBy: () => false,
  ResizeObserver: class { observe() {} disconnect() {} unobserve() {} },
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: (c) => c?.poolMatches || [],
  applyFilters: (arr) => arr,
  StableInput: (props) => <input data-testid="stable-input" {...props} />,
  PlayerMultiFilter: () => null,
  PoolsViewer: () => null,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    moveMatchCourt: vi.fn(),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
    fetchCourtMatches: vi.fn().mockResolvedValue([]),
    subscribeToEvents: () => () => {},
    subscribeLocalMatchPatch: () => () => {},
    revertMatchToQueue: vi.fn().mockResolvedValue(true),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
  },
};

let restoreGlobals;
let AdminApp;
let AdminShiaijoPage;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  Element.prototype.scrollTo = function scrollTo() {};
  Element.prototype.scrollIntoView = function scrollIntoView() {};
  if (typeof globalThis.ResizeObserver === 'undefined') {
    globalThis.ResizeObserver = class { observe() {} disconnect() {} unobserve() {} };
  }
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_shell.jsx');
  await import('../../admin_competition_bracket.jsx');
  await import('../../admin_competition.jsx');
  await import('../../admin_schedule.jsx');
  await import('../../admin_shiaijo.jsx');
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
  AdminShiaijoPage = window.AdminShiaijoPage;
});

afterAll(() => restoreGlobals());

const mkMatch = (id, status, court, scheduledAt = '10:00', a = 'Yamada', b = 'Tanaka') => ({
  id,
  compId: 'c1',
  compName: 'Tournament Cup',
  status,
  court,
  scheduledAt,
  phase: 'pool',
  poolName: 'Pool A',
  sideA: { id: `${id}-a`, name: a },
  sideB: { id: `${id}-b`, name: b },
});

const mkTournament = (matches) => ({
  id: 't1',
  name: 'Tournament 1',
  courts: ['A', 'B'],
  competitions: [
    {
      id: 'c1',
      name: 'Tournament Cup',
      status: 'running',
      poolMatches: matches,
    },
  ],
});

const selectCourtInPicker = async (container, court) => {
  const trigger = within(container).getByTitle('Change shiaijo');
  await act(async () => {
    trigger.click();
  });
  const option = within(container).getByRole('option', { name: court });
  await act(async () => {
    option.click();
  });
};

describe('running court move confirmation across all doors', () => {
  let showToast;
  let moveMatchCourtSpy;
  let confirmDialogSpy;

  beforeEach(() => {
    probe.scoreEditor = null;
    probe.bracketTree = null;
    showToast = vi.fn();
    moveMatchCourtSpy = vi.fn().mockResolvedValue({
      applied: true,
      pending: true,
      key: 'k-move',
      settled: Promise.resolve({}),
    });
    confirmDialogSpy = vi.fn().mockResolvedValue(true);
    window.API.moveMatchCourt = moveMatchCourtSpy;
    window.API.fetchCompetitions = vi.fn().mockResolvedValue([]);
    window.confirmDialog = confirmDialogSpy;
  });

  // Door 6: Court console
  describe('door 6: court console', () => {
    const rowOf = (container, text) =>
      [...container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
    const moveDialog = (container) =>
      container.querySelector('.shiaijo-move-confirm[role="dialog"]');

    it('a match shown running asks with running words, Cancel sends nothing, and confirm calls onMoveCourt with confirmed: true', async () => {
      const onMoveCourt = vi.fn().mockResolvedValue(true);
      const onEditScore = vi.fn().mockResolvedValue({
        applied: true,
        pending: true,
        key: 'k-start',
        settled: new Promise(() => {}),
      });

      const m2 = mkMatch('m2', 'scheduled', 'A', '10:00', 'Aka m2', 'Shiro m2');
      const m3 = mkMatch('m3', 'scheduled', 'A', '10:05', 'Aka m3', 'Shiro m3');
      m2.modifiedAt = 100;
      m3.modifiedAt = 100;

      const feed = { current: [m2, m3] };
      window.tournamentMatches = () => feed.current;
      window.API.fetchCourtMatches = vi.fn().mockResolvedValue([{ id: 'c1', name: 'Tournament Cup' }]);

      let utils;
      await act(async () => {
        utils = render(
          <AdminShiaijoPage
            tournament={mkTournament([m2, m3])}
            court="A"
            onBack={vi.fn()}
            onEditScore={onEditScore}
            onMoveCourt={onMoveCourt}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            password="pw"
            showToast={showToast}
            tweaks={{}}
            onSwitchCourt={vi.fn()}
          />
        );
      });

      let now = Date.now() + 1000;
      const dateSpy = vi.spyOn(Date, 'now').mockImplementation(() => now);

      try {
        // Start m3 to put it in the console's startedFrom snapshot
        const startBtn = [...rowOf(utils.container, 'Aka m3').querySelectorAll('button')].find((b) =>
          /^start/i.test(b.textContent.trim())
        );
        await act(async () => { startBtn.click(); });
        await act(async () => {});

        // Re-render m3 queue row and restore modifiedAt so isStartedSnapshot(mm) is true
        const m3Row = { ...m3, modifiedAt: 250 };
        feed.current = [m2, m3Row];
        await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); });
        m3Row.modifiedAt = 100;

        // Open move confirm for running match m3
        await selectCourtInPicker(rowOf(utils.container, 'Aka m3'), 'B');

        const dialog = moveDialog(utils.container);
        expect(dialog).toBeTruthy();
        expect(dialog.querySelector('.shiaijo-move-confirm__title').textContent).toBe('Move to Shiaijo B?');
        expect(dialog.querySelector('.shiaijo-move-confirm__body').textContent).toBe(
          `Shiro m3 vs Aka m3 is running on Shiaijo A. It stops there and goes first in the queue on Shiaijo B. ${SCORE_KEPT_SENTENCE}`
        );

        // Cancel sends nothing
        const cancelBtn = within(dialog).getByRole('button', { name: 'Cancel' });
        await act(async () => { cancelBtn.click(); });
        expect(onMoveCourt).not.toHaveBeenCalled();
        expect(moveDialog(utils.container)).toBeNull();

        // Open again and confirm
        await selectCourtInPicker(rowOf(utils.container, 'Aka m3'), 'B');
        const dialog2 = moveDialog(utils.container);
        now += TAP_BOUNCE_MS;
        const confirmBtn = within(dialog2).getByRole('button', { name: 'Move to Shiaijo B' });
        await act(async () => { fireEvent.click(confirmBtn, { detail: 1 }); });

        expect(onMoveCourt).toHaveBeenCalledTimes(1);
        expect(onMoveCourt).toHaveBeenCalledWith('c1', 'm3', 'B', {
          confirmed: true,
          shown: expect.objectContaining({ id: 'm3', compId: 'c1', status: 'running' }),
        });
      } finally {
        dateSpy.mockRestore();
      }
    });

    it('a match shown waiting asks with waiting words, Cancel sends nothing, and confirm calls onMoveCourt with confirmed: true', async () => {
      const onMoveCourt = vi.fn().mockResolvedValue(true);
      const m1 = mkMatch('m1', 'scheduled', 'A', '10:00', 'Aka m1', 'Shiro m1');
      const m2 = mkMatch('m2', 'scheduled', 'A', '10:05', 'Aka m2', 'Shiro m2');
      m1.modifiedAt = 100;
      m2.modifiedAt = 200;

      const feed = { current: [m1, m2] };
      window.tournamentMatches = () => feed.current;
      window.API.fetchCourtMatches = vi.fn().mockResolvedValue([{ id: 'c1', name: 'Tournament Cup' }]);

      let utils;
      await act(async () => {
        utils = render(
          <AdminShiaijoPage
            tournament={mkTournament([m1, m2])}
            court="A"
            onBack={vi.fn()}
            onEditScore={vi.fn()}
            onMoveCourt={onMoveCourt}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            password="pw"
            showToast={showToast}
            tweaks={{}}
            onSwitchCourt={vi.fn()}
          />
        );
      });

      await selectCourtInPicker(rowOf(utils.container, 'Aka m2'), 'B');

      const dialog = moveDialog(utils.container);
      expect(dialog).toBeTruthy();
      expect(dialog.querySelector('.shiaijo-move-confirm__title').textContent).toBe('Move to Shiaijo B?');
      expect(dialog.querySelector('.shiaijo-move-confirm__body').textContent).toBe(
        'Shiro m2 vs Aka m2 leaves Shiaijo A and joins the queue on Shiaijo B.'
      );

      // Cancel sends nothing
      await act(async () => {
        within(dialog).getByRole('button', { name: 'Cancel' }).click();
      });
      expect(onMoveCourt).not.toHaveBeenCalled();
      expect(moveDialog(utils.container)).toBeNull();

      // Open again and confirm
      await selectCourtInPicker(rowOf(utils.container, 'Aka m2'), 'B');
      const dialog2 = moveDialog(utils.container);
      await act(async () => {
        within(dialog2).getByRole('button', { name: 'Move to Shiaijo B' }).click();
      });

      expect(onMoveCourt).toHaveBeenCalledTimes(1);
      expect(onMoveCourt).toHaveBeenCalledWith('c1', 'm2', 'B', {
        confirmed: true,
        shown: expect.objectContaining({ id: 'm2', compId: 'c1', status: 'scheduled', modifiedAt: 200 }),
      });
    });
  });

  // Door 1: Schedule page picker, running match
  describe('door 1: schedule page picker on running match', () => {
    it('choosing another court opens confirm once; Cancel does not call API; confirm calls API with stopsRunning: true', async () => {
      const running = mkMatch('m1', 'running', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([running]);
      window.tournamentMatches = () => [running];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'schedule' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      // 1. Cancel branch
      confirmDialogSpy.mockResolvedValueOnce(false);
      const row = utils.container.querySelector('.tw-match');
      expect(row).toBeTruthy();

      await selectCourtInPicker(row, 'B');

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(confirmDialogSpy).toHaveBeenCalledWith(expect.objectContaining({
        title: 'Move to Shiaijo B?',
        confirmLabel: 'Move to Shiaijo B',
        cancelLabel: 'Cancel',
      }));
      expect(confirmDialogSpy.mock.calls[0][0].message).toContain('Jane Austen vs Quinn Walker is running on Shiaijo A.');
      expect(confirmDialogSpy.mock.calls[0][0].message).toContain(SCORE_KEPT_SENTENCE);
      expect(moveMatchCourtSpy).not.toHaveBeenCalled();

      // 2. Confirm branch
      confirmDialogSpy.mockClear();
      confirmDialogSpy.mockResolvedValueOnce(true);

      await selectCourtInPicker(row, 'B');

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledWith('c1', 'm1', 'B', 'pw', expect.objectContaining({
        stopsRunning: true,
        was: { court: 'A' },
      }));
    });
  });

  // Door 2: Schedule page drop on running match
  describe('door 2: schedule page drop on running match', () => {
    it('dropping running match card on another court column asks confirm; Cancel sends nothing; confirm sends stopsRunning: true', async () => {
      const running = mkMatch('m1', 'running', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([running]);
      window.tournamentMatches = () => [running];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'schedule' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      const courtColumns = utils.container.querySelectorAll('.tw-court');
      const courtBCol = [...courtColumns].find((c) => c.textContent.includes('SHIAIJO B'));
      const courtBList = courtBCol.querySelector('.tw-court__list');
      expect(courtBList).toBeTruthy();

      const dropEventData = {
        dataTransfer: {
          getData: (type) => (type === 'application/json' ? JSON.stringify({ compId: 'c1', matchId: 'm1' }) : ''),
        },
      };

      // Cancel branch
      confirmDialogSpy.mockResolvedValueOnce(false);
      await act(async () => {
        fireEvent.drop(courtBList, dropEventData);
      });

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).not.toHaveBeenCalled();

      // Confirm branch
      confirmDialogSpy.mockClear();
      confirmDialogSpy.mockResolvedValueOnce(true);
      await act(async () => {
        fireEvent.drop(courtBList, dropEventData);
      });

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledWith('c1', 'm1', 'B', 'pw', expect.objectContaining({
        stopsRunning: true,
        was: { court: 'A' },
      }));
    });
  });

  // Door 3: Schedule page picker on waiting match
  describe('door 3: schedule page picker on waiting match', () => {
    it('a waiting match moves with no confirm dialog and API called without stopsRunning', async () => {
      const waiting = mkMatch('m1', 'scheduled', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([waiting]);
      window.tournamentMatches = () => [waiting];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'schedule' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      const row = utils.container.querySelector('.tw-match');
      expect(row).toBeTruthy();

      await selectCourtInPicker(row, 'B');

      expect(confirmDialogSpy).not.toHaveBeenCalled();
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      const [compId, matchId, court, pw, opts] = moveMatchCourtSpy.mock.calls[0];
      expect([compId, matchId, court, pw]).toEqual(['c1', 'm1', 'B', 'pw']);
      expect(opts.stopsRunning).toBeUndefined();
      expect(opts.was).toEqual({ court: 'A' });
    });
  });

  // Door 4: Scores row picker
  describe('door 4: scores row picker', () => {
    it('running match asks once, Cancel sends nothing, confirm sends stopsRunning: true', async () => {
      const running = mkMatch('m1', 'running', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([running]);
      window.tournamentMatches = () => [running];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'scoreEditor' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      const row = utils.container.querySelector('.score-edit-row');
      expect(row).toBeTruthy();

      // Cancel
      confirmDialogSpy.mockResolvedValueOnce(false);
      await selectCourtInPicker(row, 'B');
      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).not.toHaveBeenCalled();

      // Confirm
      confirmDialogSpy.mockClear();
      confirmDialogSpy.mockResolvedValueOnce(true);
      await selectCourtInPicker(row, 'B');
      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledWith('c1', 'm1', 'B', 'pw', expect.objectContaining({
        stopsRunning: true,
        was: { court: 'A' },
      }));
    });

    it('waiting match moves with no confirm dialog and without stopsRunning', async () => {
      const waiting = mkMatch('m1', 'scheduled', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([waiting]);
      window.tournamentMatches = () => [waiting];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'scoreEditor' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      const row = utils.container.querySelector('.score-edit-row');
      expect(row).toBeTruthy();

      await selectCourtInPicker(row, 'B');

      expect(confirmDialogSpy).not.toHaveBeenCalled();
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      const [compId, matchId, court, pw, opts] = moveMatchCourtSpy.mock.calls[0];
      expect([compId, matchId, court, pw]).toEqual(['c1', 'm1', 'B', 'pw']);
      expect(opts.stopsRunning).toBeUndefined();
      expect(opts.was).toEqual({ court: 'A' });
    });
  });

  // Door 5: Bracket RunningMatchPanel picker
  describe('door 5: bracket running match panel picker', () => {
    it('running match with dirty inline editor includes discard warning; Cancel keeps entries, confirm drops them and sends stopsRunning', async () => {
      const running = mkMatch('m1', 'running', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const comp = {
        id: 'c1',
        name: 'Tournament Cup',
        status: 'running',
        format: 'knockout',
      };
      const t = {
        id: 't1',
        name: 'Tournament 1',
        courts: ['A', 'B'],
        competitions: [comp],
      };

      window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
        config: comp,
        bracket: {
          rounds: [[running]],
        },
      });

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'competition', id: 'c1', section: 'bracket' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      // Pick the match in BracketTree
      const matchCard = utils.container.querySelector('.bc-match');
      expect(matchCard).toBeTruthy();
      await act(async () => {
        matchCard.click();
      });

      // Verify RunningMatchPanel is mounted with ScoreEditorModal
      expect(probe.scoreEditor).toBeTruthy();
      expect(probe.scoreEditor.hostUnsavedRef).toBeTruthy();

      // Make inline editor dirty
      const discard = vi.fn();
      const dirty = () => true;
      dirty.discard = discard;
      probe.scoreEditor.hostUnsavedRef.current = dirty;

      const panel = utils.container.querySelector('.running-panel__head');
      expect(panel).toBeTruthy();

      // Cancel branch
      confirmDialogSpy.mockResolvedValueOnce(false);
      await selectCourtInPicker(panel, 'B');

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(confirmDialogSpy.mock.calls[0][0].message).toContain(UNSAVED_ENTRIES_DISCARDED);
      expect(confirmDialogSpy.mock.calls[0][0].message).toContain(SCORE_KEPT_SENTENCE);
      expect(discard).not.toHaveBeenCalled();
      expect(moveMatchCourtSpy).not.toHaveBeenCalled();

      // Confirm branch
      confirmDialogSpy.mockClear();
      confirmDialogSpy.mockResolvedValueOnce(true);
      await selectCourtInPicker(panel, 'B');

      expect(confirmDialogSpy).toHaveBeenCalledTimes(1);
      expect(discard).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledTimes(1);
      expect(moveMatchCourtSpy).toHaveBeenCalledWith('c1', 'm1', 'B', 'pw', expect.objectContaining({
        stopsRunning: true,
        was: { court: 'A' },
      }));
    });
  });

  // Door 7: Refusal
  describe('door 7: refusal callback', () => {
    it('refusal shows the server sentence once in a toast and shows the old court in the row', async () => {
      const sentence = 'Jane Austen vs Quinn Walker was started on Shiaijo A before your move reached the server, so it was not moved. Move it again to stop it there and put it first on Shiaijo B.';

      moveMatchCourtSpy.mockImplementationOnce(async (_cid, _mid, _court, _pw, opts) => {
        if (opts && typeof opts.onRefusal === 'function') {
          opts.onRefusal(new Error(sentence));
        }
        return { applied: false, refused: true };
      });

      const waiting = mkMatch('m1', 'scheduled', 'A', '10:00', 'Quinn Walker', 'Jane Austen');
      const t = mkTournament([waiting]);
      window.tournamentMatches = () => [waiting];

      let utils;
      await act(async () => {
        utils = render(
          <AdminApp
            tournament={t}
            onUpdate={vi.fn()}
            onLogout={vi.fn()}
            onViewerMode={vi.fn()}
            onPasswordChange={vi.fn()}
            tweaks={{}}
            password="pw"
            view={{ kind: 'schedule' }}
            setView={vi.fn()}
            showToast={showToast}
            authConfig={{}}
          />
        );
      });

      const row = utils.container.querySelector('.tw-match');
      await selectCourtInPicker(row, 'B');

      // Refusal sentence shown once via toast
      expect(showToast).toHaveBeenCalledTimes(1);
      expect(showToast).toHaveBeenCalledWith(sentence, 'error');

      // The match still displays on court A
      const trigger = within(row).getByTitle('Change shiaijo');
      expect(trigger.textContent).toContain('A');
    });
  });
});
