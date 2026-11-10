import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

// The Registration desk on the outbox (bc-obox C1/C2). A check-in is queued and
// sent when the link returns, so the desk shows the tap at once, follows the
// outbox's participant patches (a later tap of another desk that won, a refused
// or discarded check-in putting the row back) and never keeps an optimistic
// copy of its own. The walk-up ADD stays a direct write; its check-in follows
// it through the outbox.
//
// The API is a stand-in with one fake outbox: toggleCheckIn/bulkCheckIn answer
// at once with a pending answer (`settled` stays open until the test settles
// it) and tell the local participant listeners, as api_client does. The doors
// themselves are pinned in outbox_checkin_announce_settings.test.jsx.

const noop = () => {};
const Stub = (name) => {
  const C = () => <div data-stub={name} />;
  C.displayName = `Stub(${name})`;
  return C;
};

let listeners;
let doors;
let API;

function emit(competitionId, participants) {
  const event = { type: 'checkin_updated', local: true, data: { competitionId, participants } };
  act(() => { listeners.forEach((cb) => cb(event)); });
}

function makeAPI() {
  listeners = new Set();
  doors = [];
  return {
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    subscribeLocalParticipantPatch: (cb) => { listeners.add(cb); return () => listeners.delete(cb); },
    toggleCheckIn: vi.fn(async (compID, pid, checkedIn, _password, opts = {}) => {
      let settle;
      const settled = new Promise((resolve) => { settle = resolve; });
      doors.push({ compID, pid, checkedIn, opts, settle });
      const event = { type: 'checkin_updated', local: true, data: { competitionId: compID, participants: [{ id: pid, checkedIn }] } };
      listeners.forEach((cb) => cb(event));
      return { applied: true, pending: true, key: `k${doors.length}`, settled };
    }),
    bulkCheckIn: vi.fn(async (compID, ids, _password, opts = {}) => {
      let settle;
      const settled = new Promise((resolve) => { settle = resolve; });
      doors.push({ compID, ids, bulk: true, opts, settle });
      const event = { type: 'checkin_updated', local: true, data: { competitionId: compID, participants: ids.map((id) => ({ id, checkedIn: true })) } };
      listeners.forEach((cb) => cb(event));
      return { applied: true, pending: true, key: `k${doors.length}`, settled };
    }),
    addParticipant: vi.fn(),
  };
}

let restoreGlobals;
let AdminRegistrationDeskPage;

beforeAll(async () => {
  API = makeAPI();
  restoreGlobals = installWindowStubs({
    AdminTopbar: Stub('AdminTopbar'),
    Breadcrumbs: Stub('Breadcrumbs'),
    StatusBadge: Stub('StatusBadge'),
    pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
    API,
  });
  await import('../../admin_registration_desk.jsx');
  AdminRegistrationDeskPage = window.AdminRegistrationDeskPage;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  const fresh = makeAPI();
  Object.assign(window.API, fresh);
});

function tournamentWith(players, comp = {}) {
  return {
    name: 'Spring Taikai',
    competitions: [{
      id: 'men', name: "Men's Individual", kind: 'individual', status: 'setup', checkInEnabled: true,
      players, ...comp,
    }],
  };
}

async function mount(tournament, showToast = noop) {
  let result;
  await act(async () => {
    result = render(
      <AdminRegistrationDeskPage
        tournament={tournament}
        onBack={noop}
        password="pw"
        showToast={showToast}
        onUpdate={noop}
        onLogout={noop}
        onViewerMode={noop}
      />
    );
  });
  return result;
}

const akira = (extra = {}) => ({ id: 'p1', name: 'Akira Tanaka', dojo: 'Gyokusen', checkedIn: false, ...extra });

describe('Registration desk on the outbox', () => {
  it('shows a tap at once and keeps it while the check-in is still queued', async () => {
    const { getByRole } = await mount(tournamentWith([akira()]));
    const box = getByRole('checkbox', { name: /check in akira tanaka/i });
    expect(box.getAttribute('aria-checked')).toBe('false');
    await act(async () => { fireEvent.click(box); });
    // The answer is pending (nothing sent, nothing settled), the row is ticked.
    expect(doors).toHaveLength(1);
    expect(getByRole('checkbox', { name: /undo check-in for akira tanaka/i }).getAttribute('aria-checked')).toBe('true');
    // What the row had is handed to the outbox, to put back on a refusal.
    expect(doors[0].opts.was).toEqual({ p1: false });
    expect(doors[0].opts.name).toBe('Akira Tanaka');
  });

  it('puts the row back and says why, once, when the check-in is refused', async () => {
    const showToast = vi.fn();
    const { getByRole } = await mount(tournamentWith([akira()]), showToast);
    await act(async () => { fireEvent.click(getByRole('checkbox', { name: /check in akira tanaka/i })); });
    expect(showToast).not.toHaveBeenCalled();
    // The outbox: the server turned it away; it puts the row back, then asks
    // whether the desk claims the refusal.
    emit('men', [{ id: 'p1', checkedIn: false }]);
    let claimed;
    act(() => { claimed = doors[0].opts.onRefusal(new Error('Self-run mode has no check-in.')); });
    expect(claimed).toBe(true);
    expect(showToast).toHaveBeenCalledTimes(1);
    expect(showToast).toHaveBeenCalledWith('1 of 1 check-in(s) not saved: Self-run mode has no check-in.', 'error');
    expect(getByRole('checkbox', { name: /check in akira tanaka/i }).getAttribute('aria-checked')).toBe('false');
  });

  it('adopts the stored participant of a superseded answer and says it in one line', async () => {
    const showToast = vi.fn();
    const { getByRole } = await mount(tournamentWith([akira()]), showToast);
    await act(async () => { fireEvent.click(getByRole('checkbox', { name: /check in akira tanaka/i })); });
    // Another desk's later tap took Akira back out: the outbox adopts it ...
    emit('men', [{ id: 'p1', checkedIn: false }]);
    // ... and hands the sentence to the desk, which shows it as a warning.
    let claimed;
    act(() => {
      claimed = doors[0].opts.onRefusal(Object.assign(
        new Error('Akira Tanaka was changed on another desk: now not checked in.'), { reason: 'superseded' },
      ));
    });
    expect(claimed).toBe(true);
    expect(showToast).toHaveBeenCalledWith('Akira Tanaka was changed on another desk: now not checked in.', 'warning');
    expect(getByRole('checkbox', { name: /check in akira tanaka/i }).getAttribute('aria-checked')).toBe('false');
  });

  it('leaves a refusal to the outbox once the desk has been left', async () => {
    const showToast = vi.fn();
    const { getByRole, unmount } = await mount(tournamentWith([akira()]), showToast);
    await act(async () => { fireEvent.click(getByRole('checkbox', { name: /check in akira tanaka/i })); });
    unmount();
    expect(doors[0].opts.onRefusal(new Error('No.'))).toBe(false);
    expect(showToast).not.toHaveBeenCalled();
    // The listener is gone with the page: a late patch changes nothing and throws nothing.
    expect(listeners.size).toBe(0);
  });

  it('ignores a patch for another competition or one that matches no id', async () => {
    const { getByRole } = await mount(tournamentWith([akira()]));
    emit('other', [{ id: 'p1', checkedIn: true }]);
    emit('men', [{ id: 'nobody', checkedIn: true }]);
    emit('men', [{ id: '', checkedIn: true }]);
    expect(getByRole('checkbox', { name: /check in akira tanaka/i }).getAttribute('aria-checked')).toBe('false');
  });

  it('counts the refusals of one tap that wrote several competitions', async () => {
    const showToast = vi.fn();
    const tournament = {
      name: 'T',
      competitions: [
        { id: 'men', name: 'Men', kind: 'individual', status: 'setup', checkInEnabled: true, players: [{ id: 'a', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }] },
        { id: 'kata', name: 'Kata', kind: 'individual', status: 'setup', checkInEnabled: true, players: [{ id: 'b', name: 'Kenji Sato', dojo: 'Mumeishi', checkedIn: false }] },
      ],
    };
    const { getByRole } = await mount(tournament, showToast);
    await act(async () => { fireEvent.click(getByRole('checkbox', { name: /check in kenji sato for all their competitions/i })); });
    expect(doors.map((d) => d.pid).sort()).toEqual(['a', 'b']);
    act(() => { doors[0].opts.onRefusal(new Error('No.')); });
    act(() => { doors[1].opts.onRefusal(new Error('No.')); });
    expect(showToast).toHaveBeenNthCalledWith(1, '1 of 2 check-in(s) not saved: No.', 'error');
    expect(showToast).toHaveBeenNthCalledWith(2, '2 of 2 check-in(s) not saved: No.', 'error');
  });

  it('checks a whole dojo in through the bulk door and reloads when ids were not matched', async () => {
    const showToast = vi.fn();
    const fetchCompetitions = vi.fn().mockResolvedValue([]);
    window.API.fetchCompetitions = fetchCompetitions;
    const { container, getByRole, getByText } = await mount(tournamentWith([
      akira(), { id: 'p2', name: 'Kenji Sato', dojo: 'Gyokusen', checkedIn: false },
    ]), showToast);
    fireEvent.click(container.querySelectorAll('.rd-rail__item')[1]);
    fireEvent.click(getByText('Group by dojo'));
    await act(async () => { fireEvent.click(getByText(/Check in all 2/)); });
    expect(window.API.bulkCheckIn).toHaveBeenCalledTimes(1);
    expect(doors[0].opts.was).toEqual({ p1: false, p2: false });
    // Both rows are ticked while the bulk write waits.
    expect(getByRole('checkbox', { name: /undo check-in for akira tanaka/i })).toBeTruthy();
    expect(getByRole('checkbox', { name: /undo check-in for kenji sato/i })).toBeTruthy();
    expect(fetchCompetitions).not.toHaveBeenCalled();
    // The answer lands later and names an id the server could not resolve.
    await act(async () => { doors[0].settle({ checkedIn: 1, alreadyCheckedIn: 0, notFound: ['p2'], superseded: [] }); });
    expect(showToast).toHaveBeenCalledWith('1 not matched: reloading', 'error');
    expect(fetchCompetitions).toHaveBeenCalled();
  });
});

describe('Registration desk walk-up add (direct) then check-in (outbox)', () => {
  async function openWalkUp(showToast) {
    const result = await mount(tournamentWith([akira()]), showToast);
    fireEvent.click(result.container.querySelectorAll('.rd-rail__item')[1]);
    fireEvent.click(result.getByText('+ Add walk-up'));
    return result;
  }

  function fill(result, name, dojo) {
    const inputs = result.container.querySelectorAll('.rd-walkup input');
    fireEvent.change(inputs[0], { target: { value: name } });
    fireEvent.change(inputs[1], { target: { value: dojo } });
  }

  it('adds directly, then queues the check-in for the id the add returned', async () => {
    window.API.addParticipant = vi.fn().mockResolvedValue({ id: 'new-1', name: 'Yuki Ito', dojo: 'Tora' });
    const showToast = vi.fn();
    const result = await openWalkUp(showToast);
    fill(result, 'Yuki Ito', 'Tora');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(window.API.addParticipant).toHaveBeenCalledWith('men', { name: 'Yuki Ito', dojo: 'Tora' }, 'pw');
    expect(doors).toHaveLength(1);
    expect(doors[0]).toMatchObject({ compID: 'men', pid: 'new-1', checkedIn: true });
    expect(doors[0].opts.was).toEqual({ 'new-1': false });
    expect(doors[0].opts.name).toBe('Yuki Ito');
    // The check-in follows the add: it was never queued before the id existed.
    expect(window.API.addParticipant.mock.invocationCallOrder[0])
      .toBeLessThan(window.API.toggleCheckIn.mock.invocationCallOrder[0]);
    expect(showToast).not.toHaveBeenCalled();
  });

  it('says the walk-up was added but not checked in when the check-in is refused later', async () => {
    window.API.addParticipant = vi.fn().mockResolvedValue({ id: 'new-1', name: 'Yuki Ito', dojo: 'Tora' });
    const showToast = vi.fn();
    const result = await openWalkUp(showToast);
    fill(result, 'Yuki Ito', 'Tora');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    let claimed;
    act(() => { claimed = doors[0].opts.onRefusal(new Error('No.')); });
    expect(claimed).toBe(true);
    expect(showToast).toHaveBeenCalledWith('Yuki Ito was added but not checked in: No.', 'error');
  });

  it('keeps a failed add direct: no check-in is queued and the error is shown', async () => {
    window.API.addParticipant = vi.fn().mockRejectedValue(new Error('Not sent: no connection. Nothing was changed; try again once connected.'));
    const showToast = vi.fn();
    const result = await openWalkUp(showToast);
    fill(result, 'Yuki Ito', 'Tora');
    await act(async () => { fireEvent.click(result.getByText('Add & check in')); });
    expect(doors).toHaveLength(0);
    expect(showToast).toHaveBeenCalledWith('Not sent: no connection. Nothing was changed; try again once connected.', 'error');
  });
});
