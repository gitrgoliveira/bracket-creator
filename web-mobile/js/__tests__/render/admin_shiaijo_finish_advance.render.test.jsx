import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-evts stage 3 review 11 (X1, X3): the court console's automatic start,
// after Finish + Start Next or after a decision, is queued AFTER the finish or
// the decision it follows (`after`, the pending answer's queue key), so the
// server never sees the start while this device's own finish is still out
// (court_busy). And the console moves on at once on the outbox's pending
// answer, for a POOL match too and offline: the match it finished here is no
// longer the court's live bout, and the match it started is, before any push
// or refetch says so.

const captured = { props: null };
const tournamentMatchesSpy = vi.fn();

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
const finishPatch = { status: 'completed', winner: { id: 'p1', name: 'Yamada' } };

const tournament = { name: 'T', courts: ['A'], competitions: [{ id: 'c1', name: 'Cup' }] };

function renderPage(onEditScore) {
  return render(
    <AdminShiaijoPage
      tournament={tournament}
      court="A"
      onBack={vi.fn()}
      onEditScore={onEditScore}
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

// The outbox's pending answer: queued, nothing from the server yet.
function pending(key, settled = new Promise(() => {})) {
  return { applied: true, pending: true, key, settled };
}

const shownMatch = () => {
  const el = document.querySelector('[data-testid="score-editor"]');
  return el ? el.getAttribute('data-match') : null;
};

beforeEach(() => {
  captured.props = null;
  tournamentMatchesSpy.mockReset();
  tournamentMatchesSpy.mockImplementation(() => [running, scheduled]);
  window.tournamentMatches = tournamentMatchesSpy;
});

describe('the court console orders its automatic start after the write it follows (X1)', () => {
  it('Finish + Start Next queues the start after the finish', async () => {
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    renderPage(onEditScore);
    expect(shownMatch()).toBe('Pool A-1');
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    expect(onEditScore).toHaveBeenCalledTimes(2);
    const [, startMid, startPatch] = onEditScore.mock.calls[1];
    expect(startMid).toBe('Pool A-2');
    expect(startPatch.status).toBe('running');
    expect(startPatch.after).toBe('K:Pool A-1');
  });

  it('the start after a decision is queued after the decision', async () => {
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    renderPage(onEditScore);
    await act(async () => {
      await captured.props.onAfterDecision({ ...pending('K:decision'), decision: 'fusenpai', decisionBy: 'shiro' });
    });
    expect(onEditScore).toHaveBeenCalledTimes(1);
    const [, startMid, startPatch] = onEditScore.mock.calls[0];
    expect(startMid).toBe('Pool A-2');
    expect(startPatch.after).toBe('K:decision');
  });
});

describe('the court console moves on at once on a pending finish (X3)', () => {
  it('Finish + Start Next on a POOL match shows the started match, with no push or refetch', async () => {
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    expect(shownMatch()).toBe('Pool A-2');
    expect(captured.props.started).toBe(true);
  });

  it('a plain Finish takes the finished match off the panel at once', async () => {
    tournamentMatchesSpy.mockImplementation(() => [running]);
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    renderPage(onEditScore);
    expect(shownMatch()).toBe('Pool A-1');
    await act(async () => { await captured.props.onSubmit(finishPatch); });
    expect(shownMatch()).toBe(null);
  });

  it('a decision on a POOL match shows the started match at once', async () => {
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    renderPage(onEditScore);
    await act(async () => {
      await captured.props.onAfterDecision({ ...pending('K:decision'), decision: 'fusenpai', decisionBy: 'shiro' });
    });
    expect(shownMatch()).toBe('Pool A-2');
  });

  it('a finish the server refuses puts the match back on the panel', async () => {
    tournamentMatchesSpy.mockImplementation(() => [running]);
    let refuse;
    const settled = new Promise((resolve) => { refuse = resolve; });
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`, settled));
    renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmit(finishPatch); });
    expect(shownMatch()).toBe(null);
    await act(async () => { refuse({ applied: false, reason: 'superseded', heldGroups: ['result'] }); await settled; });
    expect(shownMatch()).toBe('Pool A-1');
  });
});
