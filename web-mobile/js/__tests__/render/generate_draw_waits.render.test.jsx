import React from 'react';
import { render, act, fireEvent, waitFor, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// bc-obox C1: Generate draw, and Start from setup (which draws), wait for the
// check-ins this device still has queued. The screen says what it waits for
// and offers "Generate now anyway" (whose confirm says who that leaves out) and
// Cancel; the draw is made once the last check-in has left the queue. A
// check-in the server keeps refusing is named with Discard as the way out.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

let queued = [];
let held = [];
// What each queued check-in's row showed before the tap (API.queuedCheckInsWere).
let were = [];
const listeners = new Set();
function tellOutbox() {
  for (const fn of listeners) fn({ total: queued.length });
}

const STUBBED_GLOBALS = {
  AdminTopbar: Stub('AdminTopbar'),
  Breadcrumbs: Stub('Breadcrumbs'),
  StatusBadge: Stub('StatusBadge'),
  CourtPicker: Stub('CourtPicker'),
  AdminParticipants: Stub('AdminParticipants'),
  AdminPools: Stub('AdminPools'),
  AdminScoreEditor: Stub('AdminScoreEditor'),
  AdminExport: Stub('AdminExport'),
  BracketTree: Stub('BracketTree'),
  AdminTeamLineupsList: Stub('AdminTeamLineupsList'),
  competitionKindLabel: () => 'Individual',
  formatDate: (d) => String(d ?? ''),
  matchMedia: () => ({
    matches: false,
    addEventListener: noop, removeEventListener: noop,
    addListener: noop, removeListener: noop,
  }),
  confirmDialog: vi.fn().mockResolvedValue(false),
  promptDialog: vi.fn().mockResolvedValue(null),
  subscribeUnsentWrites: (fn) => {
    listeners.add(fn);
    fn({ total: queued.length });
    return () => listeners.delete(fn);
  },
  API: {
    estimateCompetitionSchedule: vi.fn().mockResolvedValue(null),
    fetchDrawWarnings: vi.fn().mockResolvedValue([]),
    generateDraw: vi.fn().mockResolvedValue({}),
    startCompetition: vi.fn().mockResolvedValue({}),
    discardHeldWrite: vi.fn(() => true),
    previewMove: vi.fn().mockResolvedValue({}),
    getScheduleClashes: vi.fn().mockResolvedValue([]),
    CHECK_IN_WRITE_KINDS: Object.freeze(['checkin', 'checkin-bulk']),
    queuedWritesOf: (compID, kinds) => queued.filter((d) => d.compID === compID && kinds.includes(d.kind)).length,
    queuedCheckInsWere: () => new Map(were),
    heldWrites: () => held,
  },
};

let restoreGlobals;
let AdminCompetition;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_competition.jsx');
  AdminCompetition = window.AdminCompetition;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  queued = [];
  held = [];
  were = [];
  window.confirmDialog.mockReset();
  window.confirmDialog.mockResolvedValue(false);
  window.API.generateDraw.mockClear();
  window.API.startCompetition.mockClear();
  window.API.discardHeldWrite.mockClear();
});

function makeCompetition(overrides = {}) {
  return {
    id: 'c1',
    name: 'Mudansha',
    status: 'setup',
    format: 'pools',
    kind: 'individual',
    checkInEnabled: true,
    // The fetch lays this device's queued check-ins over the roster, so p2's
    // queued check-in already shows here.
    players: [
      { id: 'p1', name: 'Yamada', dojo: 'A', checkedIn: true },
      { id: 'p2', name: 'Tanaka', dojo: 'B', checkedIn: true },
      { id: 'p3', name: 'Suzuki', dojo: 'C', checkedIn: false },
    ],
    courts: ['A'],
    startTime: '09:00',
    date: '14-03-2026',
    fightingSpiritAwards: [],
    swissCurrentRound: 0,
    swissRounds: 0,
    ...overrides,
  };
}

async function mount(comp = makeCompetition(), { section = 'participants', onUpdate = noop } = {}) {
  const t = { name: 'Spring Taikai', courts: ['A', 'B'], competitions: [comp] };
  let result;
  await act(async () => {
    result = render(
      <AdminCompetition
        tournament={t}
        competition={comp}
        pools={[]}
        poolMatches={[]}
        standings={[]}
        bracket={null}
        section={section}
        onSection={noop}
        onBack={noop}
        onOpenCompetition={noop}
        onUpdate={onUpdate}
        onRefreshCompetition={noop}
        onMoveCourt={noop}
        onEditScore={noop}
        onLogout={noop}
        onViewerMode={noop}
        tweaks={{}}
        password="pw"
        showToast={noop}
      />,
    );
  });
  return result;
}

const button = (name) => screen.getByRole('button', { name });

describe('Generate draw waits for queued check-ins', () => {
  it('draws at once when no check-in is queued', async () => {
    window.confirmDialog.mockResolvedValue(true);
    await mount();
    await act(async () => { fireEvent.click(button('Generate draw')); });
    await waitFor(() => expect(window.API.generateDraw).toHaveBeenCalledWith('c1', 'pw'));
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  it('says what it waits for, and draws once the last check-in has left the queue', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }, { compID: 'c1', kind: 'checkin-bulk' }, { compID: 'c2', kind: 'checkin' }];
    // The exclusion confirm, asked when the draw is made.
    window.confirmDialog.mockResolvedValue(true);
    await mount();
    await act(async () => { fireEvent.click(button('Generate draw')); });
    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 2 check-ins to be sent. The draw is made once they arrive.');
    expect(window.API.generateDraw).not.toHaveBeenCalled();
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(button('Generate draw')).toBeDisabled();

    queued = queued.slice(1);
    await act(async () => { tellOutbox(); await Promise.resolve(); });
    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 1 check-in to be sent. The draw is made once it arrives.');
    expect(window.API.generateDraw).not.toHaveBeenCalled();

    queued = queued.filter((d) => d.compID !== 'c1');
    await act(async () => { tellOutbox(); await Promise.resolve(); });
    await waitFor(() => expect(window.API.generateDraw).toHaveBeenCalledTimes(1));
    // The draw asked its own confirm about who is not checked in (Suzuki).
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog.mock.calls[0][0].message).toMatch(/^1 participant is not checked in/);
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  it('Cancel drops the draw: nothing is drawn when the check-ins arrive', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await mount();
    await act(async () => { fireEvent.click(button('Generate draw')); });
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-cancel')); });
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
    queued = [];
    await act(async () => { tellOutbox(); await Promise.resolve(); });
    expect(window.API.generateDraw).not.toHaveBeenCalled();
    expect(button('Generate draw')).not.toBeDisabled();
  });

  it('Generate now anyway says who is left out, and draws after the confirm', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await mount();
    await act(async () => { fireEvent.click(button('Generate draw')); });

    // Refused: still waiting, nothing drawn.
    window.confirmDialog.mockResolvedValueOnce(false);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
    expect(window.API.generateDraw).not.toHaveBeenCalled();
    expect(screen.getByTestId('queued-wait-text')).toBeInTheDocument();

    window.confirmDialog.mockResolvedValueOnce(true);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
    await waitFor(() => expect(window.API.generateDraw).toHaveBeenCalledTimes(1));
    const ask = window.confirmDialog.mock.calls[1][0];
    expect(ask.message).toMatch(/^1 check-in made on this device has not reached the server yet\./);
    expect(ask.message).toMatch(/a competitor whose check-in has not arrived is left out of the draw\. 1 other is not checked in and is left out too\./);
    expect(ask.message).toMatch(/Generate the draw now anyway\?$/);
    expect(ask.confirmLabel).toBe('Generate now');
    // Only the anyway confirm: the exclusion is in it.
    expect(window.confirmDialog).toHaveBeenCalledTimes(2);
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });

  // The opt-in rule draws everyone while the server holds no check-in, so the
  // confirm must not say anyone is left out then.
  const noneOnServer = () => makeCompetition({
    players: [
      { id: 'p1', name: 'Yamada', dojo: 'A', checkedIn: false },
      // Shown checked in by this device's queued check-in only.
      { id: 'p2', name: 'Tanaka', dojo: 'B', checkedIn: true },
      { id: 'p3', name: 'Suzuki', dojo: 'C', checkedIn: false },
    ],
  });
  const NONE_HELD = 'No check-in has reached the server yet, so a draw made now includes every competitor.';

  for (const [door, label, verb] of [
    ['Generate draw', 'Generate draw', 'Generate the draw'],
    ['Start from setup', 'Start competition →', 'Start'],
  ]) {
    it(`${door} now anyway says every competitor is drawn when no check-in has reached the server`, async () => {
      queued = [{ compID: 'c1', kind: 'checkin' }];
      were = [['p2', false]];
      await mount(noneOnServer());
      await act(async () => { fireEvent.click(button(label)); });
      window.confirmDialog.mockResolvedValueOnce(true);
      await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
      const ask = window.confirmDialog.mock.calls[0][0];
      expect(ask.message).toBe(
        `1 check-in made on this device has not reached the server yet. ${NONE_HELD}\n\n${verb} now anyway?`,
      );
      expect(ask.message).not.toMatch(/left out/);
    });

    it(`${door} now anyway keeps the left-out wording when the server holds a check-in this device is taking back`, async () => {
      // p1 is checked in on the server; this device's queued check-out shows
      // it as not checked in. A draw made now still sees p1's check-in.
      queued = [{ compID: 'c1', kind: 'checkin' }, { compID: 'c1', kind: 'checkin' }];
      were = [['p1', true], ['p2', false]];
      await mount(noneOnServer());
      await act(async () => { fireEvent.click(button(label)); });
      window.confirmDialog.mockResolvedValueOnce(true);
      await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
      const ask = window.confirmDialog.mock.calls[0][0];
      expect(ask.message).toMatch(/^2 check-ins made on this device have not reached the server yet\. A draw made now uses only the check-ins the server holds: a competitor whose check-in has not arrived is left out of the draw\./);
      expect(ask.message).not.toContain(NONE_HELD);
    });
  }

  it('names a check-in the server keeps refusing, with Discard as the way out', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    held = [{ key: 'k1', compID: 'c1', kind: 'checkin', keepsFailing: true, participantIds: ['p2'], where: '' }];
    await mount();
    await act(async () => { fireEvent.click(button('Generate draw')); });
    const blockers = screen.getByTestId('queued-wait-blockers');
    expect(blockers.textContent).toContain('Mudansha · Check-in: Tanaka');
    window.confirmDialog.mockResolvedValueOnce(true);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-discard')); });
    expect(window.API.discardHeldWrite).toHaveBeenCalledWith('k1');
  });

  it('Start from setup draws, so it waits too', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    window.confirmDialog.mockResolvedValue(true);
    await mount();
    await act(async () => { fireEvent.click(button('Start competition →')); });
    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 1 check-in to be sent. The draw is made and the competition starts once it arrives.');
    expect(screen.getByTestId('queued-wait-now').textContent).toBe('Start now anyway');
    expect(window.API.startCompetition).not.toHaveBeenCalled();
    queued = [];
    await act(async () => { tellOutbox(); await Promise.resolve(); });
    await waitFor(() => expect(window.API.startCompetition).toHaveBeenCalledWith('c1', 'pw'));
  });

  it('waits for nothing when check-in does not apply to the draw', async () => {
    queued = [{ compID: 'c1', kind: 'checkin' }];
    await mount(makeCompetition({ checkInEnabled: false }));
    await act(async () => { fireEvent.click(button('Generate draw')); });
    await waitFor(() => expect(window.API.generateDraw).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });
});

// bc-obox T1: a Settings save that moves a drawn competition (a new date or
// start time) waits for the match time changes this device still has queued.
describe('Settings: moving a drawn competition waits for queued time changes', () => {
  const drawn = () => makeCompetition({ status: 'draw-ready', configVersion: 'v1' });

  async function editStart(container, value) {
    await act(async () => {
      fireEvent.change(container.querySelector('#settings-start-time'), { target: { value } });
    });
  }

  it('holds the save, keeps the form unsaved, and saves once the time changes arrive', async () => {
    queued = [{ compID: 'c1', kind: 'time' }, { compID: 'c1', kind: 'score' }];
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mount(drawn(), { section: 'settings', onUpdate });
    await editStart(container, '10:00');
    await act(async () => { fireEvent.click(screen.getAllByRole('button', { name: 'Save changes' })[0]); });
    expect(screen.getByTestId('queued-wait-text').textContent)
      .toBe('Waiting for 1 time change to be sent. The move is made once it arrives.');
    expect(screen.getByTestId('queued-wait-now').textContent).toBe('Move now anyway');
    expect(onUpdate).not.toHaveBeenCalled();
    for (const b of screen.getAllByRole('button', { name: 'Save changes' })) expect(b).toBeDisabled();

    queued = queued.filter((d) => d.kind !== 'time');
    await act(async () => { tellOutbox(); await Promise.resolve(); });
    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    expect(onUpdate.mock.calls[0][0].startTime).toBe('10:00');
  });

  it('Move now anyway saves after a confirm that says what a late time change does', async () => {
    queued = [{ compID: 'c1', kind: 'time' }, { compID: 'c1', kind: 'time' }];
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mount(drawn(), { section: 'settings', onUpdate });
    await editStart(container, '10:00');
    await act(async () => { fireEvent.click(screen.getAllByRole('button', { name: 'Save changes' })[0]); });
    window.confirmDialog.mockResolvedValueOnce(true);
    await act(async () => { fireEvent.click(screen.getByTestId('queued-wait-now')); });
    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    const ask = window.confirmDialog.mock.calls[0][0];
    expect(ask.message).toMatch(/^2 time changes made on this device have not reached the server yet\. If you move the competition now, a time changed before the move may land on the old day\./);
    expect(ask.confirmLabel).toBe('Move now');
  });

  it('saves at once a change that does not move the competition', async () => {
    queued = [{ compID: 'c1', kind: 'time' }];
    const onUpdate = vi.fn().mockResolvedValue({ configVersion: 'v2' });
    const { container } = await mount(drawn(), { section: 'settings', onUpdate });
    await act(async () => {
      fireEvent.change(container.querySelector('#settings-display-name'), { target: { value: 'Mudansha A' } });
    });
    await act(async () => { fireEvent.click(screen.getAllByRole('button', { name: 'Save changes' })[0]); });
    await waitFor(() => expect(onUpdate).toHaveBeenCalledTimes(1));
    expect(screen.queryByTestId('queued-wait-text')).toBeNull();
  });
});
