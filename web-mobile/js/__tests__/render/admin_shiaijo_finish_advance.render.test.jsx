import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { createOwnStatusMoves } from '../helpers/own_status_moves.js';

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
// The outbox's status moves (M4): the stubbed score door holds the finish.
const outbox = createOwnStatusMoves();

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
    ...outbox.api,
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

function pageElement(onEditScore, t = tournament) {
  return (
    <AdminShiaijoPage
      tournament={t}
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

function renderPage(onEditScore) {
  return render(pageElement(onEditScore));
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
  outbox.clear();
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

// Stage 6 review 8 (M4): what the console shows of a finish or a decision it
// made is read from the outbox, never from the page's memory, so the console
// mounted again (the operator left the court and came back, or reloaded) still
// shows it finished while the outbox holds it.
describe('a finish or a decision the outbox holds shows after the console is mounted again (M4)', () => {
  it('a queued finish: still off the panel, finished with its winner', async () => {
    tournamentMatchesSpy.mockImplementation(() => [running]);
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
    const first = renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmit(finishPatch); });
    expect(shownMatch()).toBe(null);
    first.unmount();
    renderPage(onEditScore);
    expect(shownMatch(), 'the match the outbox finished is not live again').toBe(null);
    const row = document.querySelector('.shiaijo-qrow--complete');
    expect(row, 'it shows finished').not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--aka .bc-winner-tick'), 'with the winner the finish names').not.toBeNull();
  });

  it('a queued decision: finished, the side that did not withdraw the winner', async () => {
    tournamentMatchesSpy.mockImplementation(() => [running]);
    outbox.hold('c1', 'Pool A-1', { status: 'completed', kind: 'decision', payload: { decision: 'kiken', decisionBy: 'shiro' } }, pending('K:decision'));
    renderPage(vi.fn(async (_c, mid) => pending(`K:${mid}`)));
    expect(shownMatch()).toBe(null);
    const row = document.querySelector('.shiaijo-qrow--complete');
    expect(row).not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--aka .bc-winner-tick'), 'Aka, as Shiro withdrew').not.toBeNull();
    expect(row.querySelector('.shiaijo-qrow__side--shiro .bc-winner-tick')).toBeNull();
  });
});

describe('a start refused later takes its pin off the panel (Z1)', () => {
  it('Finish + Start Next whose start the server refuses leaves the refused match off the panel', async () => {
    let refuseStart;
    const startSettled = new Promise((r) => { refuseStart = r; });
    const onEditScore = vi.fn(async (_c, mid) => (mid === 'Pool A-2' ? pending(`K:${mid}`, startSettled) : pending(`K:${mid}`)));
    renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    expect(shownMatch()).toBe('Pool A-2');
    await act(async () => { refuseStart({ applied: false, reason: 'clock_skew' }); await startSettled; });
    // The finish is still out, so the console reads Pool A-1 as finished here
    // and the refused Pool A-2 is not the live bout: the panel is empty.
    expect(shownMatch()).toBe(null);
  });

  it('a refused finish then a start refused through onRefusal shows the match the server holds', async () => {
    let refuseFinish, refuseStart;
    const fin = new Promise((r) => { refuseFinish = r; });
    const st = new Promise((_r, j) => { refuseStart = j; });
    st.catch(() => {});
    const onEditScore = vi.fn(async (_c, mid, _patch, _m, opts) => {
      if (mid === 'Pool A-2') {
        st.catch((e) => opts && opts.onRefusal && opts.onRefusal(e));
        return pending(`K:${mid}`, st);
      }
      return pending(`K:${mid}`, fin);
    });
    renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    await act(async () => { refuseFinish({ applied: false, reason: 'superseded', heldGroups: ['result'] }); await fin; });
    await act(async () => {
      refuseStart(Object.assign(new Error('court busy'), { code: 'court_busy' }));
      try { await st; } catch { /* the refusal is the point */ }
    });
    expect(shownMatch()).toBe('Pool A-1');
    expect(captured.props.started).toBeFalsy();
  });

  it('a start the operator DISCARDS from the held-writes list takes the pin off a match the server never started', async () => {
    let discardStart;
    const st = new Promise((r) => { discardStart = r; });
    const onEditScore = vi.fn(async (_c, mid) => (mid === 'Pool A-2' ? pending(`K:${mid}`, st) : pending(`K:${mid}`)));
    renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    expect(shownMatch()).toBe('Pool A-2');
    expect(captured.props.started).toBe(true);
    await act(async () => { discardStart({ discarded: true }); await st; });
    // The finish is still out, so Pool A-1 reads as finished here and the
    // discarded Pool A-2 is not the live bout: the panel is empty.
    expect(shownMatch()).toBe(null);
  });
});

describe('a finish stays read as finished until a later change, not this device\'s own earlier taps (Z2)', () => {
  const landFinish = async (feedCopy) => {
    tournamentMatchesSpy.mockImplementation(() => [running]);
    let answer;
    const settled = new Promise((resolve) => { answer = resolve; });
    const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`, settled));
    const utils = renderPage(onEditScore);
    await act(async () => { await captured.props.onSubmit(finishPatch); });
    expect(shownMatch()).toBe(null);
    // The feed still shows the match running, now under another stamp, and the
    // finish's answer comes carrying the stamp the server gave the finish.
    tournamentMatchesSpy.mockImplementation(() => [feedCopy]);
    await act(async () => {
      utils.rerender(pageElement(onEditScore, { ...tournament, competitions: [{ id: 'c1', name: 'Cup' }] }));
    });
    await act(async () => { answer({ applied: true, modifiedAt: 200 }); await settled; });
  };

  it('a running copy stamped before the finish (this device\'s own taps) does not bring the match back', async () => {
    await landFinish({ ...running, modifiedAt: 150 });
    expect(shownMatch()).toBe(null);
  });

  it('a running copy stamped after the finish (a reopen elsewhere) does', async () => {
    await landFinish({ ...running, modifiedAt: 300 });
    expect(shownMatch()).toBe('Pool A-1');
  });
});
