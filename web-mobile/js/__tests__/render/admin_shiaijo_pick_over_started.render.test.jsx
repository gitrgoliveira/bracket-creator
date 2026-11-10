// bc-sssf: a pick right after this console's own start sends THAT bout back.
//
// A start this console made is not in the feed's `running` until a refetch shows
// it (the refetch is jittered, and with the start queued offline it waits for the
// connection). Until then the console holds the bout only as its start pin
// (`startedFrom`, `pickedKey`), which the editor already treats as the court's
// live bout. A pick of another bout in that window found "nothing running",
// sent nothing back and started over it: the server refused with `court_busy`
// and told the operator to do by hand what the console exists to do for them.
//
// Three cases, one court, the feed never saying the started bout is running:
//   1. a pick (Start match) of X, then a pick of Y: Y is checked, X is sent
//      back, Y's start is queued after that send back;
//   2. the same when X was started by Finish + Start Next;
//   3. Send back to queue is offered for X at once, not only after the refetch.

import React from 'react';
import { render, act, screen, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { pointerTap, keyboardClick } from '../helpers/tap_events.js';
import { createOwnStatusMoves } from '../helpers/own_status_moves.js';

const captured = { props: null };
// The outbox's status moves: the stubbed score door holds a finish as queued.
const outbox = createOwnStatusMoves();

const STUBBED_GLOBALS = {
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => {
    captured.props = props;
    return <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />;
  },
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    sendAnnouncement: vi.fn(),
    deleteAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    revertMatchToQueue: vi.fn(),
    checkStart: vi.fn(),
    startCheckUntrusted: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
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

const REVERT_KEY = 'c1:m1\u001fsendback';

beforeEach(() => {
  captured.props = null;
  outbox.clear();
  window.API.revertMatchToQueue = vi.fn(async () => ({ applied: true, pending: true, key: REVERT_KEY, settled: Promise.resolve(true) }));
  window.API.checkStart = vi.fn(async () => ({ ok: true }));
  window.API.startCheckUntrusted = vi.fn(() => false);
});

afterEach(() => {
  cleanup();
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  window.filterMatchesByCourt = STUBBED_GLOBALS.filterMatchesByCourt;
});

const base = { compId: 'c1', court: 'A', phase: 'pool', poolName: 'Pool 1', modifiedAt: 50 };
const fighters = (a, b) => ({ sideA: { id: `p-${a}`, name: a }, sideB: { id: `p-${b}`, name: b } });
// The feed as the court's refetch last left it: the bout this console started
// is still `scheduled` in it.
const feed = (firstStatus = 'scheduled') => [
  { ...base, id: 'm1', status: firstStatus, scoreVersion: 3, ...fighters('Yamada', 'Tanaka') },
  { ...base, id: 'm2', status: 'scheduled', ...fighters('Suzuki', 'Sato') },
  { ...base, id: 'm3', status: 'scheduled', ...fighters('Ito', 'Kato') },
];
const finishPatch = { status: 'completed', winner: { id: 'p-Yamada', name: 'Yamada' } };

// The outbox's pending answer: queued, nothing from the server yet.
const pending = (key) => ({ applied: true, pending: true, key, settled: new Promise(() => {}) });

function renderConsole(list) {
  window.tournamentMatches = () => list;
  window.filterMatchesByCourt = (ms) => ms;
  const onEditScore = vi.fn(async (_c, mid) => pending(`K:${mid}`));
  const showToast = vi.fn();
  const page = () => (
    <AdminShiaijoPage
      tournament={{ name: 'Test Tournament', courts: ['A'], competitions: [{ id: 'c1', name: 'Cup' }] }}
      court="A"
      onEditScore={outbox.scoreDoor(onEditScore)}
      password="pw"
      showToast={showToast}
      tweaks={{}}
      onBack={vi.fn()}
      onMoveCourt={vi.fn()}
      onLogout={vi.fn()}
      onViewerMode={vi.fn()}
      onSwitchCourt={vi.fn()}
    />
  );
  const { rerender } = render(page());
  // The court as a refetch leaves it: the console renders `next`.
  const courtBecomes = (next) => {
    window.tournamentMatches = () => next;
    rerender(page());
  };
  return { onEditScore, showToast, courtBecomes };
}

// The start writes the console made, as [matchId, patch].
const starts = (onEditScore) => onEditScore.mock.calls.filter((c) => c[2] && c[2].status === 'running').map((c) => [c[1], c[2]]);
const upNextStart = () => screen.getAllByRole('button', { name: 'Start match' })[0];
const settle = () => act(async () => { await Promise.resolve(); await Promise.resolve(); });
const shownMatch = () => {
  const el = document.querySelector('[data-testid="score-editor"]');
  return el ? el.getAttribute('data-match') : null;
};

// The picks and the advance below leave the feed untouched: only the console's
// own start pin says the started bout is running.
describe('a pick right after this console\'s own start sends that bout back first (bc-sssf)', () => {
  it('a pick of the next bout checks it, sends the bout just started back, and starts it after that send back', async () => {
    const { onEditScore } = renderConsole(feed());
    // X = m1: nothing runs, so it starts with no check and no send back.
    await pointerTap(upNextStart());
    await settle();
    expect(starts(onEditScore).map(([id]) => id)).toEqual(['m1']);
    expect(window.API.checkStart).not.toHaveBeenCalled();
    expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
    expect(shownMatch(), 'the started bout is the court\'s live bout').toBe('m1');

    // Y = m2, tapped before any refetch shows m1 running.
    await keyboardClick(upNextStart());
    await settle();

    expect(window.API.checkStart).toHaveBeenCalledTimes(1);
    expect(window.API.checkStart).toHaveBeenCalledWith('c1', 'm2', 'A', 'pw');
    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    expect(window.API.revertMatchToQueue.mock.calls[0].slice(0, 2)).toEqual(['c1', 'm1']);
    const started = starts(onEditScore);
    expect(started.map(([id]) => id)).toEqual(['m1', 'm2']);
    expect(started[1][1].after).toBe(REVERT_KEY);
    expect(window.API.checkStart.mock.invocationCallOrder[0])
      .toBeLessThan(window.API.revertMatchToQueue.mock.invocationCallOrder[0]);
    expect(window.API.revertMatchToQueue.mock.invocationCallOrder[0])
      .toBeLessThan(onEditScore.mock.invocationCallOrder[1]);
  });

  it('the same after Finish + Start Next started the bout', async () => {
    const { onEditScore } = renderConsole(feed('running'));
    expect(shownMatch()).toBe('m1');
    // Finish m1 and start m2 (the e2e journey's path); the feed still says
    // m1 running and m2 scheduled.
    await act(async () => { await captured.props.onSubmitAndNext(finishPatch); });
    await settle();
    expect(starts(onEditScore).map(([id]) => id)).toEqual(['m2']);
    expect(shownMatch(), 'the started bout is the court\'s live bout').toBe('m2');

    // Y = m3, the next bout in Up next.
    await keyboardClick(upNextStart());
    await settle();

    expect(window.API.checkStart).toHaveBeenCalledTimes(1);
    expect(window.API.checkStart).toHaveBeenCalledWith('c1', 'm3', 'A', 'pw');
    expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
    expect(window.API.revertMatchToQueue.mock.calls[0].slice(0, 2)).toEqual(['c1', 'm2']);
    const started = starts(onEditScore);
    expect(started.map(([id]) => id)).toEqual(['m2', 'm3']);
    expect(started[1][1].after).toBe(REVERT_KEY);
  });

  it('Send back to queue is offered for the bout just started, before any refetch shows it running', async () => {
    renderConsole(feed());
    expect(screen.queryByRole('button', { name: 'Send back to queue' })).toBeNull();
    await pointerTap(upNextStart());
    await settle();
    expect(shownMatch()).toBe('m1');
    expect(screen.getByRole('button', { name: 'Send back to queue' })).toBeTruthy();
  });

  it('the "Another bout is running" list names another device\'s bout while the panel shows the start just made', async () => {
    const { courtBecomes } = renderConsole(feed());
    await pointerTap(upNextStart());
    await settle();
    expect(shownMatch()).toBe('m1');
    expect(document.querySelector('.shiaijo-also-running')).toBeNull();

    // A refetch the server answered before m1's start landed: m1 is still the
    // same scheduled copy, and another device has started m3 on this court.
    const [m1, m2, m3] = feed();
    await act(async () => { courtBecomes([m1, m2, { ...m3, status: 'running', modifiedAt: 60 }]); });
    await settle();

    expect(shownMatch(), 'the panel keeps this console\'s own start').toBe('m1');
    const alert = document.querySelector('.shiaijo-also-running');
    expect(alert, 'another device\'s running bout is listed').not.toBeNull();
    const items = [...alert.querySelectorAll('.shiaijo-also-running__list li')].map((li) => li.textContent);
    expect(items).toHaveLength(1);
    expect(items[0]).toContain('Ito');
    expect(items[0]).not.toContain('Yamada');
  });
});
