import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-shcr: one shiaijo hosting two competitions. Pins only what the operator
// rulings call correct (2026-09-26/27: one match at a time on a court; the
// operator may switch the console to the other competition on the same court;
// an automatic advance stays in the scored match's competition). It does NOT
// pin which competition the console shows by itself when nothing was picked.

// Captures the latest ScoreEditorModal props so a test can drive
// onSubmitAndNext / onAfterDecision directly.
const probe = { props: null };

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import, so set before the dynamic import.
  AdminTopbar: ({ children }) => <div data-testid="topbar">{children}</div>,
  Breadcrumbs: () => null,
  ScoreEditorModal: (props) => { probe.props = props; return <div data-testid="score-editor" data-match={props.match ? props.match.id : ''} />; },
  CourtPicker: () => null,
  BracketTree: () => null,
  Icon: ({ name }) => <span>{name}</span>,
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchCourtMatches: vi.fn().mockResolvedValue([]),
    subscribeToEvents: () => () => {},
    sendAnnouncement: vi.fn(),
    updateMatchTime: vi.fn(),
    startMatch: vi.fn(),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
    reinstateCompetitor: vi.fn().mockResolvedValue({}),
    revertMatchToQueue: vi.fn().mockResolvedValue(true),
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

afterEach(() => {
  window.tournamentMatches = STUBBED_GLOBALS.tournamentMatches;
  probe.props = null;
});

// Two competitions on Shiaijo A: Cup (c1, individual pools) and League (c2).
const side = (id, name) => ({ id, name });
const row = (id, status, at, over = {}) => ({
  id, compId: 'c1', compName: 'Cup', status, phase: 'pool', poolName: 'Pool A', court: 'A', scheduledAt: at,
  sideA: side(`${id}-a`, `Aka ${id}`), sideB: side(`${id}-b`, `Shiro ${id}`),
  ...over,
});
const league = { compId: 'c2', compName: 'League' };

async function mountCourt(initial, onEditScore = vi.fn().mockResolvedValue({ status: 'running' })) {
  const feed = { current: initial };
  window.tournamentMatches = () => feed.current;
  let utils;
  await act(async () => {
    utils = render(
      <AdminShiaijoPage tournament={{ name: 'T', courts: ['A', 'B'], competitions: [] }} court="A"
        onBack={vi.fn()} onEditScore={onEditScore} onMoveCourt={vi.fn()} onLogout={vi.fn()}
        onViewerMode={vi.fn()} password="" showToast={vi.fn()} tweaks={{}} onSwitchCourt={vi.fn()} />
    );
  });
  const picker = () => utils.container.querySelector('select[aria-label="Select competition to officiate"]');
  return {
    utils,
    feed,
    onEditScore,
    picker,
    pick: async (compId) => { await act(async () => { fireEvent.change(picker(), { target: { value: compId } }); }); },
    refresh: async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); },
    nudges: () => utils.queryAllByTestId('shiaijo-nudge').map((n) => n.querySelector('.shiaijo-nudge__text').textContent),
    upNext: () => {
      const title = [...utils.container.querySelectorAll('.section-title')].find((t) => t.textContent === 'Up next');
      return title ? title.parentElement.textContent : '';
    },
  };
}

describe('the competition picker on a shared court (bc-shcr)', () => {
  it('is not offered when only one competition is on the court', async () => {
    const c = await mountCourt([row('m1', 'running', '09:01'), row('m2', 'scheduled', '09:02')]);
    expect(c.picker(), 'one competition: a plain heading, no picker').toBeNull();
    expect(c.utils.container.querySelector('.shiaijo-officiating__name').textContent).toBe('Cup');
  });

  it('is offered with both competitions once a second one is on the court', async () => {
    const c = await mountCourt([
      row('m1', 'running', '09:01'),
      row('m2', 'scheduled', '09:02'),
      row('m3', 'scheduled', '09:03', league),
    ]);
    expect(c.picker(), 'two competitions: the picker').toBeTruthy();
    expect([...c.picker().options].map((o) => o.textContent)).toEqual(['Cup', 'League']);
  });

  it("keeps an explicit pick and shows the picked competition's queue", async () => {
    const c = await mountCourt([
      row('m1', 'running', '09:01'),
      row('m2', 'scheduled', '09:02'),
      row('m3', 'scheduled', '09:03', league),
    ]);
    await c.pick('c2');
    expect(c.picker().value).toBe('c2');
    expect(c.upNext(), "League's queue").toContain('Shiro m3');
    expect(c.upNext()).not.toContain('Shiro m2');
    // A refetch keeps the operator's pick even though Cup's bout still runs here.
    await c.refresh();
    expect(c.picker().value, 'the pick is held across a refresh').toBe('c2');
    expect(c.upNext()).toContain('Shiro m3');
  });
});

describe('the switch nudge on a shared court (bc-shcr)', () => {
  it('says "Switch to" with the other competition\'s count only when the picked one has nothing running or scheduled here', async () => {
    const c = await mountCourt([
      row('m1', 'completed', '09:01', { winner: side('m1-a', 'Aka m1') }),
      row('m2', 'scheduled', '09:02', league),
      row('m3', 'scheduled', '09:03', league),
    ]);
    await c.pick('c1');
    expect(c.nudges()).toEqual(['Switch to League: 2 matches waiting on this court.']);
    expect(c.utils.getByTestId('shiaijo-nudge').classList.contains('alert--warn')).toBe(true);
  });

  it('stays quiet while the picked competition still has a scheduled match here', async () => {
    const c = await mountCourt([
      row('m1', 'scheduled', '09:01'),
      row('m2', 'scheduled', '09:02', league),
      row('m3', 'scheduled', '09:03', league),
    ]);
    await c.pick('c1');
    expect(c.nudges()).toEqual(['2 League matches also waiting on this court.']);
    expect(c.utils.getByTestId('shiaijo-nudge').classList.contains('alert--warn')).toBe(false);
  });

  it('stays quiet while the picked competition still has a running match here', async () => {
    const c = await mountCourt([
      row('m1', 'running', '09:01'),
      row('m2', 'scheduled', '09:02', league),
    ]);
    await c.pick('c1');
    expect(c.nudges()).toEqual(['1 League match also waiting on this court.']);
  });
});

describe('the "Another bout is running" alert on a shared court (bc-shcr)', () => {
  it('lists every running bout beyond the first, with its competition', async () => {
    const c = await mountCourt([
      row('m1', 'running', '09:01'),
      row('m2', 'running', '09:02', league),
      row('m3', 'running', '09:03', league),
    ]);
    const alert = c.utils.container.querySelector('.shiaijo-also-running');
    expect(alert, 'two bouts running on one court is flagged').toBeTruthy();
    expect(alert.querySelector('.shiaijo-also-running__title').textContent).toBe('Another bout is running on Shiaijo A');
    const items = [...alert.querySelectorAll('.shiaijo-also-running__list li')].map((li) => li.textContent);
    expect(items).toEqual(['Shiro m2 vs Aka m2 · League', 'Shiro m3 vs Aka m3 · League']);
  });

  it('is not shown while only one bout runs on the court', async () => {
    const c = await mountCourt([
      row('m1', 'running', '09:01'),
      row('m2', 'scheduled', '09:02', league),
    ]);
    expect(c.utils.container.querySelector('.shiaijo-also-running')).toBeNull();
  });
});

describe('the automatic advance stays in the scored competition on a shared court (bc-shcr)', () => {
  // League's m2 is scheduled EARLIER than Cup's m3: the advance must still
  // start Cup's next match, never the other competition's.
  const sharedQueue = () => [
    row('m1', 'running', '09:01'),
    row('m2', 'scheduled', '09:02', league),
    row('m3', 'scheduled', '09:03'),
  ];

  it("Finish + Start Next starts the scored competition's next match", async () => {
    const onEditScore = vi.fn().mockImplementation((_cid, _mid, patch) =>
      Promise.resolve({ applied: true, status: patch && patch.status }));
    const c = await mountCourt(sharedQueue(), onEditScore);
    expect(probe.props.match?.id).toBe('m1');
    await act(async () => {
      await probe.props.onSubmitAndNext({ status: 'completed', winner: 'Aka m1' });
    });
    expect(c.onEditScore).toHaveBeenCalledTimes(2);
    expect(c.onEditScore.mock.calls[0].slice(0, 2), 'the finish').toEqual(['c1', 'm1']);
    expect(c.onEditScore.mock.calls[1].slice(0, 2), "Cup's next match, not League's earlier one").toEqual(['c1', 'm3']);
  });

  it("the start after a withdrawal starts the scored competition's next match", async () => {
    const c = await mountCourt(sharedQueue());
    expect(probe.props.match?.id).toBe('m1');
    await act(async () => {
      await probe.props.onAfterDecision({
        id: 'm1', sideA: 'Aka m1', sideB: 'Shiro m1', sideAId: 'm1-a', sideBId: 'm1-b',
        winner: 'Shiro m1', winnerId: 'm1-b', status: 'completed', decision: 'kiken-voluntary', decisionBy: 'aka',
      });
    });
    expect(c.onEditScore).toHaveBeenCalledTimes(1);
    expect(c.onEditScore.mock.calls[0].slice(0, 2), "Cup's next match, not League's earlier one").toEqual(['c1', 'm3']);
  });

  it("the start after a no-show starts the scored competition's next match", async () => {
    const c = await mountCourt(sharedQueue());
    await act(async () => {
      await probe.props.onAfterDecision({
        id: 'm1', sideA: 'Aka m1', sideB: 'Shiro m1', sideAId: 'm1-a', sideBId: 'm1-b',
        winner: 'Aka m1', winnerId: 'm1-a', status: 'completed', decision: 'fusenpai', decisionBy: 'shiro',
      });
    });
    expect(c.onEditScore).toHaveBeenCalledTimes(1);
    expect(c.onEditScore.mock.calls[0].slice(0, 2)).toEqual(['c1', 'm3']);
  });
});
