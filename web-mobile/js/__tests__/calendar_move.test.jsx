import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  snapStart, dropTarget, shiftClock, shiftLanes, saveMove, undoMove, moveErrorMessage,
  isMovable, isDrawn, moveFieldsReady, overlapSentence, drawnDragHint,
} from '../calendar_move.jsx';

describe('overlapSentence', () => {
  it('names each competition once, with all the shiaijo it shares', () => {
    expect(overlapSentence([
      { name: "Women's Individual", court: 'A' },
      { name: "Women's Individual", court: 'B' },
      { name: 'Kids', court: 'C' },
    ])).toBe("Overlaps Women's Individual on Shiaijo A and B, Kids on Shiaijo C. You can still move it.");
  });

  it('lists three or more shiaijo with commas', () => {
    expect(overlapSentence([
      { name: 'Open', court: 'A' }, { name: 'Open', court: 'B' }, { name: 'Open', court: 'C' },
    ])).toBe('Overlaps Open on Shiaijo A, B and C. You can still move it.');
  });

  it('is empty with no overlaps', () => {
    expect(overlapSentence([])).toBe('');
    expect(overlapSentence(undefined)).toBe('');
  });
});

const BASE = {
  id: 'c1', name: "Men's Individual", date: '14-03-2026', startTime: '09:00', format: 'mixed',
  kind: 'individual', poolSize: 4, poolWinners: 2, poolSizeMode: 'min', courts: ['A', 'B'],
  roundRobin: false, withZekkenName: false, teamSize: 0, numberPrefix: 'M', poolFormat: '',
  poolMatchDurationSeconds: 180, knockoutMatchDurationSeconds: 240, swissRounds: 0, naginata: false,
  engi: false, checkInEnabled: false, leagueTiebreakTopN: 0, twoThirdPlaces: true, teamMatchType: '',
  extraQualifiers: '', status: 'setup', configVersion: 'v1',
};

describe('snapStart', () => {
  it('snaps to 5 minutes', () => {
    expect(snapStart(542, [])).toBe(540);
    expect(snapStart(543, [])).toBe(545);
  });
  it('moves a start dropped inside a pause to the end of the pause', () => {
    const breaks = [{ startMins: 720, endMins: 780 }];
    expect(snapStart(725, breaks)).toBe(780);
    expect(snapStart(720, breaks)).toBe(780);
    expect(snapStart(780, breaks)).toBe(780);
    expect(snapStart(715, breaks)).toBe(715);
  });
  it('keeps a start dropped inside a pause matches go on through', () => {
    expect(snapStart(725, [{ startMins: 720, endMins: 780, matchesGoOn: true }])).toBe(725);
  });
  it('stays inside the day', () => {
    expect(snapStart(-30, [])).toBe(0);
    expect(snapStart(2000, [])).toBe(1435);
  });
  it('is null when a pause ends too late for any start, never a start inside the pause', () => {
    expect(snapStart(23 * 60 + 45, [{ startMins: 23 * 60 + 40, endMins: 24 * 60 }])).toBeNull();
    expect(snapStart(2000, [{ startMins: 23 * 60, endMins: 24 * 60 }])).toBeNull();
    expect(snapStart(23 * 60 + 45, [{ startMins: 23 * 60 + 40, endMins: 23 * 60 + 55 }])).toBe(1435);
  });
});

describe('shiftClock', () => {
  it('moves a start and refuses to leave the day', () => {
    expect(shiftClock('09:00', 75)).toBe('10:15');
    expect(shiftClock('09:00', -540)).toBe('00:00');
    expect(shiftClock('09:00', -541)).toBeNull();
    expect(shiftClock('23:00', 60)).toBeNull();
    expect(shiftClock('', 10)).toBeNull();
  });
});

describe('shiftLanes', () => {
  it('keeps the count and clamps at the edges', () => {
    expect(shiftLanes(['A', 'B'], ['A', 'B', 'C', 'D'], 1)).toEqual(['B', 'C']);
    expect(shiftLanes(['C', 'D'], ['A', 'B', 'C', 'D'], 3)).toEqual(['C', 'D']);
    expect(shiftLanes(['B'], ['A', 'B', 'C'], -5)).toEqual(['A']);
  });
  it('never loses a shiaijo that has no lane: the list stays as it is', () => {
    expect(shiftLanes(['A', 'B', 'Q'], ['A', 'B', 'C'], 1)).toEqual(['A', 'B', 'Q']);
  });
});

describe('dropTarget', () => {
  const geometry = {
    days: [
      { day: 1, left: 0, top: 100, right: 300, bottom: 1000, breaks: [] },
      { day: 2, left: 300, top: 100, right: 600, bottom: 1000, breaks: [{ startMins: 720, endMins: 780 }] },
    ],
    laneWidth: 100, startHour: 8, courts: ['A', 'B', 'C'],
  };
  it('finds the day, the snapped start and the shiaijo under the dragged block', () => {
    const t = dropTarget({ geometry, blockLeft: 310, blockTop: 100 + 62, pointerX: 330, pointerY: 200, blockCourts: ['A'] });
    expect(t).toEqual({ day: 2, startMins: 8 * 60 + 60, courts: ['A'] });
    const t2 = dropTarget({ geometry, blockLeft: 400, blockTop: 100 + 62, pointerX: 420, pointerY: 200, blockCourts: ['A'] });
    expect(t2.courts).toEqual(['B']);
  });
  it('lands a start dropped in a pause at its end', () => {
    const t = dropTarget({ geometry, blockLeft: 300, blockTop: 100 + (12 * 60 + 10 - 8 * 60), pointerX: 320, pointerY: 400, blockCourts: ['A'] });
    expect(t.startMins).toBe(780);
  });
  it('has no start when a late pause leaves none before midnight', () => {
    const late = { ...geometry, days: [{ day: 1, left: 0, top: 100, right: 300, bottom: 2000, breaks: [{ startMins: 23 * 60 + 40, endMins: 24 * 60 }] }] };
    const t = dropTarget({ geometry: late, blockLeft: 0, blockTop: 100 + (23 * 60 + 45 - 8 * 60), pointerX: 20, pointerY: 400, blockCourts: ['A'] });
    expect(t).toEqual({ day: 1, startMins: null, courts: ['A'] });
  });
  it('is null when the block is not over a day', () => {
    expect(dropTarget({ geometry, blockLeft: 0, blockTop: 0, pointerX: 900, pointerY: 400, blockCourts: ['A'] })).toBeNull();
  });
});

// The ghost is told the shiaijo under the pointer, not the ones a drawn
// competition keeps, so it can say the move leaves them as drawn.
describe('drawnDragHint', () => {
  const drawn = { status: 'draw-ready', placement: { courts: ['A', 'B'] } };
  it('says a drawn competition held over other shiaijo keeps its own', () => {
    expect(drawnDragHint(drawn, ['B', 'C'])).toBe('Its shiaijo stay as drawn.');
  });
  it('says nothing over its own shiaijo, or for one not drawn', () => {
    expect(drawnDragHint(drawn, ['A', 'B'])).toBe('');
    expect(drawnDragHint({ status: 'setup', placement: { courts: ['A'] } }, ['B'])).toBe('');
  });
});

describe('what can be moved', () => {
  it('moves until it starts', () => {
    expect(isMovable({ status: 'setup' })).toBe(true);
    expect(isMovable({ status: '' })).toBe(true);
    expect(isMovable({ status: 'draw-ready' })).toBe(true);
    expect(isMovable({ status: 'pools' })).toBe(false);
    expect(isMovable({ status: 'knockout' })).toBe(false);
    expect(isMovable({ status: 'completed' })).toBe(false);
    expect(isDrawn({ status: 'draw-ready' })).toBe(true);
    expect(isDrawn({ status: 'setup' })).toBe(false);
  });
  it('asks the server only for a complete move', () => {
    expect(moveFieldsReady({ date: '14-03-2026', startTime: '09:30', courts: ['A'] })).toBe(true);
    expect(moveFieldsReady({ date: '', startTime: '09:30', courts: ['A'] })).toBe(false);
    expect(moveFieldsReady({ date: '14-03-2026', startTime: '9.30', courts: ['A'] })).toBe(false);
    expect(moveFieldsReady({ date: '14-03-2026', startTime: '09:30', courts: [] })).toBe(false);
  });
});

describe('saveMove and undoMove', () => {
  beforeEach(() => {
    window.API = { fetchCompetitions: vi.fn(), updateCompetition: vi.fn() };
  });
  afterEach(() => { delete window.API; });

  it('saves a setup move under the version just read, with date, start and shiaijo', async () => {
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE }]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE, configVersion: 'v2' });
    const move = await saveMove({ password: 'pw', competitionId: 'c1', date: '15-03-2026', deltaMins: 90, courts: ['B'] });
    const [id, body, pw] = window.API.updateCompetition.mock.calls[0];
    expect(id).toBe('c1');
    expect(pw).toBe('pw');
    expect(body.date).toBe('15-03-2026');
    expect(body.startTime).toBe('10:30');
    expect(body.courts).toEqual(['B']);
    expect(body.configVersion).toBe('v1');
    expect(body.players).toBeUndefined();
    expect(move.before).toEqual({ date: '14-03-2026', startTime: '09:00', courts: ['A', 'B'] });
    expect(move.drawn).toBe(false);
  });

  it('keeps a drawn competition on its shiaijo', async () => {
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE, status: 'draw-ready' }]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE, status: 'draw-ready', configVersion: 'v2', previousTimes: [] });
    const move = await saveMove({ password: 'pw', competitionId: 'c1', date: '14-03-2026', startTime: '11:00', courts: ['B'] });
    expect(window.API.updateCompetition.mock.calls[0][1].courts).toEqual(['A', 'B']);
    expect(move.drawn).toBe(true);
  });

  it('refuses a competition that has started, writing nothing', async () => {
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE, status: 'pools' }]);
    await expect(saveMove({ password: 'pw', competitionId: 'c1', date: '15-03-2026', startTime: '10:00' })).rejects.toThrow(/started/);
    expect(window.API.updateCompetition).not.toHaveBeenCalled();
  });

  it('refuses a start that leaves the day', async () => {
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE }]);
    await expect(saveMove({ password: 'pw', competitionId: 'c1', date: '15-03-2026', deltaMins: -600 })).rejects.toThrow(/out of the day/);
    expect(window.API.updateCompetition).not.toHaveBeenCalled();
  });

  it('undoes a drawn move with the matches\' previous times, even when there are none', async () => {
    const moved = { ...BASE, status: 'draw-ready', date: '15-03-2026', startTime: '11:00', configVersion: 'v2' };
    window.API.fetchCompetitions.mockResolvedValue([moved]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE, status: 'draw-ready', configVersion: 'v3' });
    const times = [{ matchId: 'Pool A-0', was: '09:00', now: '11:00' }];
    await undoMove({
      password: 'pw',
      move: {
        competitionId: 'c1', drawn: true, answer: { configVersion: 'v2', previousTimes: times },
        before: { date: '14-03-2026', startTime: '09:00', courts: ['A', 'B'] },
      },
    });
    let body = window.API.updateCompetition.mock.calls[0][1];
    expect(body.configVersion).toBe('v2');
    expect(body.date).toBe('14-03-2026');
    expect(body.startTime).toBe('09:00');
    expect(body.restoreMatchTimes).toEqual(times);

    await undoMove({
      password: 'pw',
      move: {
        competitionId: 'c1', drawn: true, answer: { configVersion: 'v2', previousTimes: [] },
        before: { date: '14-03-2026', startTime: '09:00', courts: ['A', 'B'] },
      },
    });
    body = window.API.updateCompetition.mock.calls[1][1];
    expect(body.restoreMatchTimes).toEqual([]);
  });

  // bc-obox T1: a drawn competition is not moved while a time change this
  // device made is still on its way, unless the operator chose to.
  it('does not move a drawn competition while time changes are queued, and says so', async () => {
    window.API.queuedWritesOf = vi.fn((compID, kinds) => (compID === 'c1' && kinds.includes('time') ? 2 : 0));
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE, status: 'draw-ready' }]);
    const err = await saveMove({ password: 'pw', competitionId: 'c1', date: '14-03-2026', startTime: '11:00' }).catch((e) => e);
    expect(err.code).toBe('time_changes_queued');
    expect(err.count).toBe(2);
    expect(moveErrorMessage(err)).toBe('Not moved yet: 2 time changes made on this device are still being sent. Open Move to wait for them, or to move it now anyway.');
    expect(window.API.queuedWritesOf).toHaveBeenCalledWith('c1', ['time']);
    expect(window.API.updateCompetition).not.toHaveBeenCalled();
  });

  it('moves a drawn competition with time changes queued when the operator says now anyway', async () => {
    window.API.queuedWritesOf = vi.fn(() => 1);
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE, status: 'draw-ready' }]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE, status: 'draw-ready', configVersion: 'v2', previousTimes: [] });
    const move = await saveMove({ password: 'pw', competitionId: 'c1', date: '14-03-2026', startTime: '11:00', nowAnyway: true });
    expect(move.drawn).toBe(true);
    expect(window.API.updateCompetition).toHaveBeenCalledTimes(1);
    expect(window.API.updateCompetition.mock.calls[0][1].nowAnyway).toBeUndefined();
  });

  it('moves a competition not yet drawn whatever time changes are queued', async () => {
    window.API.queuedWritesOf = vi.fn(() => 3);
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE }]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE, configVersion: 'v2' });
    await saveMove({ password: 'pw', competitionId: 'c1', date: '15-03-2026', startTime: '10:00', courts: ['A'] });
    expect(window.API.updateCompetition).toHaveBeenCalledTimes(1);
    expect(window.API.queuedWritesOf).not.toHaveBeenCalled();
  });

  it('undoes a setup move without a restore list and with its shiaijo back', async () => {
    window.API.fetchCompetitions.mockResolvedValue([{ ...BASE, courts: ['B'], configVersion: 'v2' }]);
    window.API.updateCompetition.mockResolvedValue({ ...BASE });
    await undoMove({
      password: 'pw',
      move: { competitionId: 'c1', drawn: false, answer: { configVersion: 'v2' }, before: { date: '14-03-2026', startTime: '09:00', courts: ['A', 'B'] } },
    });
    const body = window.API.updateCompetition.mock.calls[0][1];
    expect(body.restoreMatchTimes).toBeUndefined();
    expect(body.courts).toEqual(['A', 'B']);
  });
});

describe('moveErrorMessage', () => {
  it('tells the version refusal in the app\'s words and passes the server\'s sentence on', () => {
    expect(moveErrorMessage({ code: 'config_changed', message: 'x' })).toBe('This competition changed, reload it.');
    expect(moveErrorMessage({ code: 'undo_stale', message: 'Changed since the move; Undo not applied.' })).toBe('Changed since the move; Undo not applied.');
    expect(moveErrorMessage(null)).toBe('Could not move the competition.');
  });
});
