import React from 'react';
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, afterEach, vi } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { AdminCalendarPage } from '../../admin_calendar.jsx';
import { toastSnapshot } from '../../toast_queue.jsx';

const STUBBED_GLOBALS = {
  AdminTopbar: () => <div data-testid="admin-topbar" />,
  Breadcrumbs: () => <nav data-testid="breadcrumbs" />,
};

const CONFIG = {
  name: 'Open', date: '14-03-2026', startTime: '09:00', format: 'mixed', kind: 'individual',
  poolSize: 4, poolWinners: 2, poolSizeMode: 'min', courts: ['A', 'B'], roundRobin: false,
  withZekkenName: false, teamSize: 0, numberPrefix: 'O', poolFormat: '',
  poolMatchDurationSeconds: 180, knockoutMatchDurationSeconds: 240, swissRounds: 0,
  naginata: false, engi: false, checkInEnabled: false, leagueTiebreakTopN: 0, twoThirdPlaces: true,
  teamMatchType: '', extraQualifiers: '',
};

const placed = (id, name, status) => ({
  id, name, status, configVersion: 'v1', date: '14-03-2026',
  placement: { placed: true, day: 1, courts: ['A'], windows: { A: { start: 540, end: 600 } }, lengthSource: 'draw' },
  clashPartners: [],
});

const CALENDAR = {
  days: [{ day: 1, date: '14-03-2026', breaks: [] }, { day: 2, date: '15-03-2026', breaks: [] }],
  courts: ['A', 'B'],
  unusedPauses: [],
  unreadableCompetitions: [],
  competitions: [placed('c1', 'Open', 'setup'), placed('c2', 'Drawn', 'draw-ready')],
};

describe('moving a competition on the calendar', () => {
  let restoreGlobals;
  let originalAPI;
  beforeAll(() => { originalAPI = window.API; restoreGlobals = installWindowStubs(STUBBED_GLOBALS); });
  afterAll(() => { restoreGlobals(); window.API = originalAPI; });

  beforeEach(() => {
    window.API = {
      fetchCalendar: vi.fn().mockResolvedValue(CALENDAR),
      subscribeToEvents: vi.fn(() => () => {}),
      fetchCompetitions: vi.fn().mockResolvedValue([
        { ...CONFIG, id: 'c1', name: 'Open', status: 'setup', configVersion: 'v1' },
        { ...CONFIG, id: 'c2', name: 'Drawn', status: 'draw-ready', configVersion: 'v1' },
      ]),
      previewMove: vi.fn().mockResolvedValue({
        placement: { windows: { A: { start: 630, end: 690 } }, lengthSource: 'draw' },
        overlaps: [],
      }),
      updateCompetition: vi.fn(),
    };
  });
  afterEach(() => { vi.restoreAllMocks(); });

  const open = async () => {
    render(<AdminCalendarPage tournament={{ name: 'T', courts: ['A', 'B'] }} password="pw" onBack={() => {}} />);
    await waitFor(() => expect(screen.getByText('Open')).toBeInTheDocument());
  };

  it('moves a competition that is not drawn with the Move button, and Undo puts it back', async () => {
    window.API.updateCompetition
      .mockResolvedValueOnce({ ...CONFIG, id: 'c1', configVersion: 'v2', startTime: '10:30' })
      .mockResolvedValueOnce({ ...CONFIG, id: 'c1', configVersion: 'v3' });
    await open();

    fireEvent.click(screen.getByRole('button', { name: 'Move Open' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });
    await waitFor(() => expect(window.API.previewMove).toHaveBeenCalled());
    expect(await within(dialog).findByText('Runs 10:30 to 11:30.')).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
    const [id, body, pw] = window.API.updateCompetition.mock.calls[0];
    expect([id, pw]).toEqual(['c1', 'pw']);
    expect(body.startTime).toBe('10:30');
    expect(body.configVersion).toBe('v1');
    expect(body.restoreMatchTimes).toBeUndefined();

    const line = await screen.findByTestId('cal-moved-line');
    expect(line).toHaveTextContent('just now. Undo puts it back where it was.');
    // The toast names where it went from and to; the line carries Undo.
    expect(toastSnapshot()?.message).toMatch(/^Moved .+ from .+ at 09:00 to .+ at 10:30\.$/);
    fireEvent.click(within(line).getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(2));
    const undoBody = window.API.updateCompetition.mock.calls[1][1];
    expect(undoBody.startTime).toBe('09:00');
    expect(undoBody.configVersion).toBe('v2');
    await waitFor(() => expect(screen.queryByTestId('cal-moved-line')).toBeNull());
  });

  it('shows what a drawn competition\'s matches would do, keeps its shiaijo, and Undo sends the exact times back', async () => {
    window.API.previewMove.mockResolvedValue({
      placement: {}, overlaps: [],
      retime: { anyChange: true, matchCount: 7, notFreshCount: 2, courts: [{ court: 'A', oldStart: 540, oldEnd: 600, newStart: 630, newEnd: 690 }] },
    });
    const times = [{ matchId: 'Pool A-0', was: '09:00', now: '10:30' }];
    window.API.updateCompetition
      .mockResolvedValueOnce({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v2', previousTimes: times })
      .mockResolvedValueOnce({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v3' });
    await open();

    fireEvent.click(screen.getByRole('button', { name: 'Move Drawn' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });

    const warning = await within(dialog).findByTestId('retime-warning');
    expect(warning).toHaveTextContent('Moving it moves its 7 matches');
    expect(warning).toHaveTextContent('Shiaijo A: 10:30 to 11:30 (was 09:00 to 10:00)');
    expect(warning).toHaveTextContent('2 of its matches no longer have the times a fresh draw would give them now');
    expect(within(dialog).getByRole('button', { name: 'Shiaijo (court) A' })).toBeDisabled();
    expect(within(dialog).getByText('Its shiaijo stay as drawn. To use other shiaijo, discard the draw first.')).toBeInTheDocument();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
    expect(window.API.updateCompetition.mock.calls[0][1].courts).toEqual(['A', 'B']);

    const line = await screen.findByTestId('cal-moved-line');
    fireEvent.click(within(line).getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(2));
    expect(window.API.updateCompetition.mock.calls[1][1].restoreMatchTimes).toEqual(times);
  });

  it('warns about a clash at the new time for a drawn competition, and still moves it', async () => {
    window.API.previewMove.mockResolvedValue({
      placement: {},
      overlaps: [{ competitionId: 'c1', name: 'Open', court: 'A', start: 630, end: 690 }],
      retime: { anyChange: true, matchCount: 7, notFreshCount: 0, courts: [{ court: 'A', oldStart: 540, oldEnd: 600, newStart: 630, newEnd: 690 }] },
    });
    window.API.updateCompetition.mockResolvedValue({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v2', previousTimes: [] });
    await open();

    fireEvent.click(screen.getByRole('button', { name: 'Move Drawn' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });

    expect(await within(dialog).findByText('Overlaps Open on Shiaijo A. You can still move it.')).toBeInTheDocument();
    expect(within(dialog).getByTestId('retime-warning')).toHaveTextContent('Moving it moves its 7 matches');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
  });

  it('keeps the dialog and the server\'s sentence when the move is refused', async () => {
    const err = new Error('This competition changed while you were moving it. Nothing was moved; try again.');
    err.code = 'retime_retry';
    window.API.updateCompetition.mockRejectedValue(err);
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Move Drawn' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });
    await waitFor(() => expect(window.API.previewMove).toHaveBeenCalled());
    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Nothing was moved; try again.');
    expect(start.value).toBe('10:30');
    expect(screen.queryByTestId('cal-moved-line')).toBeNull();
  });

  it('closes the Move dialog on Escape, writing nothing', async () => {
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Move Open' }));
    await screen.findByTestId('cal-move-dialog');
    fireEvent.keyDown(window, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByTestId('cal-move-dialog')).toBeNull());
    expect(window.API.updateCompetition).not.toHaveBeenCalled();
  });

  it('keeps the Move dialog open on Escape while the move is being saved', async () => {
    let resolveSave;
    window.API.updateCompetition.mockReturnValue(new Promise((r) => { resolveSave = r; }));
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Move Open' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });
    await waitFor(() => expect(window.API.previewMove).toHaveBeenCalled());
    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(screen.getByTestId('cal-move-dialog')).toBeInTheDocument();
    resolveSave({ ...CONFIG, id: 'c1', configVersion: 'v2', startTime: '10:30' });
    await waitFor(() => expect(screen.queryByTestId('cal-move-dialog')).toBeNull());
  });

  it('says so and drops the Undo line when something changed since the move', async () => {
    const stale = new Error('Changed since the move; Undo not applied.');
    stale.code = 'undo_stale';
    window.API.updateCompetition
      .mockResolvedValueOnce({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v2', previousTimes: [] })
      .mockRejectedValueOnce(stale);
    await open();
    fireEvent.click(screen.getByRole('button', { name: 'Move Drawn' }));
    const dialog = await screen.findByTestId('cal-move-dialog');
    const start = within(dialog).getByLabelText('Start time');
    await waitFor(() => expect(start.value).toBe('09:00'));
    fireEvent.change(start, { target: { value: '10:30' } });
    await waitFor(() => expect(window.API.previewMove).toHaveBeenCalled());
    fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
    const line = await screen.findByTestId('cal-moved-line');
    fireEvent.click(within(line).getByRole('button', { name: 'Undo' }));
    await waitFor(() => expect(screen.queryByTestId('cal-moved-line')).toBeNull());
    expect(toastSnapshot()?.message).toBe('Changed since the move; Undo not applied.');
  });

  it('offers no Move on a competition that has started', async () => {
    window.API.fetchCalendar.mockResolvedValue({
      ...CALENDAR,
      competitions: [placed('c1', 'Open', 'pools'), placed('c2', 'Drawn', 'completed')],
    });
    await open();
    expect(screen.queryByRole('button', { name: /^Move / })).toBeNull();
    expect(screen.getByText('Running: cannot be moved')).toBeInTheDocument();
  });

  describe('dragging a block', () => {
    let restoreRect;
    beforeEach(() => {
      const real = Element.prototype.getBoundingClientRect;
      Element.prototype.getBoundingClientRect = function rect() {
        const box = (left, top, width, height) => ({ left, top, width, height, right: left + width, bottom: top + height, x: left, y: top });
        const day = this.getAttribute && this.getAttribute('data-cal-day');
        if (day) return box(day === '1' ? 100 : 500, 100, 380, 900);
        if (this.classList && this.classList.contains('cal-block')) return box(100, 160, 100, 60); // 540 minutes: an hour after the day's 08:00 top
        return real.call(this);
      };
      restoreRect = () => { Element.prototype.getBoundingClientRect = real; };
    });
    afterEach(() => { restoreRect(); });

    const drag = (from, to) => {
      const block = screen.getByRole('button', { name: /^Open, / });
      fireEvent.pointerDown(block, { pointerId: 1, pointerType: 'mouse', button: 0, clientX: from.x, clientY: from.y });
      fireEvent.pointerMove(window, { pointerId: 1, pointerType: 'mouse', clientX: to.x, clientY: to.y });
      fireEvent.pointerUp(window, { pointerId: 1, pointerType: 'mouse', clientX: to.x, clientY: to.y });
    };

    it('drops a competition that is not drawn on another day and saves it, keeping the start', async () => {
      window.API.updateCompetition.mockResolvedValue({ ...CONFIG, id: 'c1', configVersion: 'v2' });
      await open();
      drag({ x: 105, y: 165 }, { x: 505, y: 165 });
      await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
      const body = window.API.updateCompetition.mock.calls[0][1];
      expect(body.date).toBe('15-03-2026');
      expect(body.startTime).toBe('09:00');
      expect(body.configVersion).toBe('v1');
      expect(await screen.findByTestId('cal-moved-line')).toHaveTextContent('just now. Undo puts it back where it was.');
    });

    it('writes nothing for a drop where the block already is, nor for a click', async () => {
      await open();
      drag({ x: 105, y: 165 }, { x: 108, y: 166 });
      drag({ x: 105, y: 165 }, { x: 105, y: 165 });
      expect(window.API.updateCompetition).not.toHaveBeenCalled();
      expect(screen.queryByTestId('cal-moved-line')).toBeNull();
    });

    it('asks first when a drawn competition\'s matches would change, and only then saves', async () => {
      window.API.previewMove.mockResolvedValue({
        placement: {}, overlaps: [],
        retime: { anyChange: true, matchCount: 3, notFreshCount: 0, courts: [{ court: 'A', oldStart: 540, oldEnd: 600, newStart: 540, newEnd: 600 }] },
      });
      window.API.updateCompetition.mockResolvedValue({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v2', previousTimes: [] });
      await open();
      const block = screen.getByRole('button', { name: /^Drawn, / });
      fireEvent.pointerDown(block, { pointerId: 1, pointerType: 'mouse', button: 0, clientX: 105, clientY: 165 });
      fireEvent.pointerMove(window, { pointerId: 1, pointerType: 'mouse', clientX: 505, clientY: 165 });
      fireEvent.pointerUp(window, { pointerId: 1, pointerType: 'mouse', clientX: 505, clientY: 165 });
      const dialog = await screen.findByTestId('cal-move-dialog');
      expect(window.API.updateCompetition).not.toHaveBeenCalled();
      await within(dialog).findByTestId('retime-warning');
      fireEvent.click(within(dialog).getByRole('button', { name: 'Move' }));
      await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
      expect(window.API.updateCompetition.mock.calls[0][1].date).toBe('15-03-2026');
    });

    it('saves a drawn competition at once when no match would change its time', async () => {
      window.API.previewMove.mockResolvedValue({
        placement: {}, overlaps: [],
        retime: { anyChange: false, matchCount: 3, notFreshCount: 0, courts: [] },
      });
      window.API.updateCompetition.mockResolvedValue({ ...CONFIG, id: 'c2', status: 'draw-ready', configVersion: 'v2', previousTimes: [] });
      await open();
      const block = screen.getByRole('button', { name: /^Drawn, / });
      fireEvent.pointerDown(block, { pointerId: 1, pointerType: 'mouse', button: 0, clientX: 105, clientY: 165 });
      fireEvent.pointerMove(window, { pointerId: 1, pointerType: 'mouse', clientX: 505, clientY: 165 });
      fireEvent.pointerUp(window, { pointerId: 1, pointerType: 'mouse', clientX: 505, clientY: 165 });
      await waitFor(() => expect(window.API.updateCompetition).toHaveBeenCalledTimes(1));
      expect(screen.queryByTestId('cal-move-dialog')).toBeNull();
    });
  });
});
