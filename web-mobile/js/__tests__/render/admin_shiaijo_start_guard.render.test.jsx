import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, probe, makeMinimalTournament, renderPage, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';
import { startWhileStartingMessage, CLOCK_SKEW_REASON_TEXT, startWasBlockedByStartMessage } from '../../write_result.jsx';
import { scoreRowMatchName } from '../../pool_ids.jsx';
import { START_SUPERSEDED_MESSAGE } from '../../start_match.jsx';

setupShiaijoConsole();

// One start at a time (bc-aadv): while a Start is still out, a Start of another
// match is refused on its own row BEFORE the defer, so the running bout is not
// sent back to the queue for a start that never happens (the bc-crpn rule,
// applied to starts). A repeat for the match already being started is silent.
describe('a Start tapped while another start is still out is refused before anything is sent back (bc-aadv)', () => {
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const startButton = (el) => [...el.querySelectorAll('button')].find((b) => /^start/i.test(b.textContent.trim()));
  // m1 runs on the court; m2 (Up next) and m3 wait. Up next's start is held in
  // flight until the test calls land().
  const setup = async () => {
    let land;
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm2'
      ? new Promise((r) => { land = r; })
      : Promise.resolve({ applied: true })));
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')], { onEditScore });
    await act(async () => { startButton(c.utils.container.querySelector('.shiaijo-upnext__card')).click(); });
    await act(async () => {});
    expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m2']);
    // m1 was sent back for m2's start; the assertions below are about what a
    // later tap does, so the count starts again from here.
    window.API.revertMatchToQueue.mockClear();
    return { c, onEditScore, land: (res) => land(res) };
  };

  it('a) a Start on another row is refused on that row: the running bout is not sent back and no second start goes out', async () => {
    const { c, onEditScore } = await setup();
    try {
      await act(async () => { startButton(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      const text = startWhileStartingMessage({ label: scoreRowMatchName(courtMatch('m2', 'scheduled')) });
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(c.editorMatch()).toBe('m1');
      expect(rowOf(c, 'Aka m3').querySelector('[role="alert"]').textContent).toBe(text);
      expect(c.showToast).toHaveBeenCalledWith(text, 'error');
    } finally { c.restore(); }
  });

  it('b) the refusal becomes a past-tense notice on that row once the held start lands, and a Start there clears it', async () => {
    const { c, onEditScore, land } = await setup();
    try {
      await act(async () => { startButton(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      const label = scoreRowMatchName(courtMatch('m2', 'scheduled'));
      expect(rowOf(c, 'Aka m3').querySelector('[role="alert"]').textContent).toBe(startWhileStartingMessage({ label }));
      await act(async () => { land({ applied: true }); });
      await act(async () => {});
      // m3 was asked for and never started; the court is not free, so it is not
      // started on its own, and the row says so. With m2 started, m3 is the Up
      // next card (stage 6 review 1: a started bout leaves the queue).
      const m3 = () => rowOf(c, 'Aka m3') || c.utils.container.querySelector('.shiaijo-upnext__card');
      expect(m3().textContent).toContain('Aka m3');
      expect(m3().querySelector('.shiaijo-upnext__error').textContent).toBe(startWasBlockedByStartMessage({ label }));
      expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m2']);
      // The operator starts it: the sentence goes and the start is sent.
      await act(async () => { startButton(m3()).click(); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m2', 'm3']);
      expect(c.utils.container.querySelector('.shiaijo-upnext__error')).toBeNull();
    } finally { c.restore(); }
  });

  // The start a refused tap waited for can FAIL instead of landing. Then the court
  // is free, so "was being started when this match was asked for" would be false,
  // and the failed start's OWN refusal is the thing the operator needs: it stays on
  // its card, and the waiting match shows nothing.
  it('b2) a held start that is refused keeps its own refusal on its card, and the waiting row shows nothing', async () => {
    const { c, onEditScore, land } = await setup();
    try {
      await act(async () => { startButton(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      expect(rowOf(c, 'Aka m3').querySelector('[role="alert"]')).toBeTruthy();
      await act(async () => { land({ applied: false, reason: 'clock_skew' }); });
      await act(async () => {});
      expect(c.utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error')?.textContent)
        .toContain(CLOCK_SKEW_REASON_TEXT);
      expect(rowOf(c, 'Aka m3').querySelector('[role="alert"]'), 'the court is free: no past-tense notice').toBeNull();
      expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m2']);
    } finally { c.restore(); }
  });

  it('c) a repeat Start on the match already being started does nothing: no revert, no notice', async () => {
    const { c, onEditScore } = await setup();
    try {
      // While its start is out, Up next's own button reads "Starting…" and is disabled.
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      const again = startButton(card);
      expect(again.disabled).toBe(true);
      await act(async () => { again.click(); });
      await act(async () => {});
      expect(window.API.revertMatchToQueue).not.toHaveBeenCalled();
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(card.querySelector('[role="alert"]')).toBeNull();
      expect(c.showToast).not.toHaveBeenCalled();
    } finally { c.restore(); }
  });

  // A pick that defers the running bout is a start in flight from the moment
  // it is made, not from the moment the deferred bout's revert answers: a second
  // Start on another row during that wait must not send the running bout back
  // a second time.
  it('d) a second Start tapped while a pick is still deferring the running bout is refused, and the bout is not reverted twice', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled'), courtMatch('m4', 'scheduled')], { onEditScore });
    let landRevert;
    window.API.revertMatchToQueue = vi.fn(() => new Promise((r) => { landRevert = r; }));
    try {
      // The first pick: a queue row's Start, which reverts m1 and waits on it.
      await act(async () => { startButton(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);

      // A second Start on another row while that wait is out.
      await act(async () => { startButton(rowOf(c, 'Aka m4')).click(); });
      await act(async () => {});
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
      expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]').textContent)
        .toBe(startWhileStartingMessage({ label: scoreRowMatchName(courtMatch('m3', 'scheduled')) }));

      // Once the revert answers, the first pick starts and only it.
      await act(async () => { landRevert(true); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m3']);
    } finally { c.restore(); }
  });

  // The past-tense sentence says the blocker WAS being started, so it is written only
  // for a start that went out. A pick whose deferral of the running bout fails never
  // starts anything: the court is exactly as it was, and "m3 was being started" would
  // be false.
  describe('the past-tense notice follows a start that happened', () => {
    const pickBlocker = async (onEditScore) => {
      const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled'), courtMatch('m4', 'scheduled')], { onEditScore });
      let settleRevert;
      window.API.revertMatchToQueue = vi.fn(() => new Promise((resolve, reject) => { settleRevert = { resolve, reject }; }));
      // The pick: a queue row's Start, which sends m1 back and waits on it.
      await act(async () => { startButton(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      // A Start on another row while that wait is out is refused as "still being started".
      await act(async () => { startButton(rowOf(c, 'Aka m4')).click(); });
      await act(async () => {});
      expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]').textContent)
        .toBe(startWhileStartingMessage({ label: scoreRowMatchName(courtMatch('m3', 'scheduled')) }));
      return { c, settleRevert: () => settleRevert };
    };

    it('e) a pick whose deferral fails never started, so the refused row gets no past-tense notice', async () => {
      const onEditScore = vi.fn().mockResolvedValue({ applied: true });
      const { c, settleRevert } = await pickBlocker(onEditScore);
      try {
        await act(async () => { settleRevert().reject(new Error('Could not defer the current bout')); });
        await act(async () => {});
        expect(onEditScore, 'the pick was abandoned: nothing was started').not.toHaveBeenCalled();
        expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]'), 'the court is as it was: no sentence about a start').toBeNull();
        expect(c.utils.container.textContent).not.toContain('was being started');
      } finally { c.restore(); }
    });

    it('f) a pick that does start leaves the past-tense notice on the refused row', async () => {
      const onEditScore = vi.fn().mockResolvedValue({ applied: true });
      const { c, settleRevert } = await pickBlocker(onEditScore);
      try {
        await act(async () => { settleRevert().resolve(true); });
        await act(async () => {});
        expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m3']);
        expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]').textContent)
          .toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(courtMatch('m3', 'scheduled')) }));
      } finally { c.restore(); }
    });

    // A superseded start that HOLDS its result group is HTTP 200
    // {applied:false, reason:'superseded', heldGroups:['result']}: a different
    // result is already stored (a send-back, a correction) and nothing of the start
    // was written. (Round 19: the same answer WITHOUT heldGroups is an echo of a
    // start that already landed, a start that went out; see the echo tests below.)
    // For a PICK that is not "as it was": the running bout was sent back
    // to the queue before the start, so the court is IDLE (and m3 shows the
    // superseded refusal like any refused start; see the describe below). "m3 was
    // being started" would be false, and the pick must not pin the panel on a
    // match the server did not start.
    const supersededStartOfM3 = () => vi.fn((_compId, matchId) => Promise.resolve(
      matchId === 'm3' ? { applied: false, reason: 'superseded', heldGroups: ['result'] } : { applied: true }
    ));

    it('g) a pick whose start is superseded wrote nothing, so the refused row gets no past-tense notice', async () => {
      const onEditScore = supersededStartOfM3();
      const { c, settleRevert } = await pickBlocker(onEditScore);
      try {
        await act(async () => { settleRevert().resolve(true); });
        await act(async () => {});
        expect(onEditScore.mock.calls.map((x) => x[1]), 'the pick did send its start').toEqual(['m3']);
        expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]'), 'the start wrote nothing: no sentence about a start').toBeNull();
        expect(c.utils.container.textContent).not.toContain('was being started');
      } finally { c.restore(); }
    });

    it('h) a pick whose start is superseded does not pin the panel on the match the server did not start', async () => {
      const landedPick = await pickBlocker(vi.fn().mockResolvedValue({ applied: true }));
      try {
        await act(async () => { landedPick.settleRevert().resolve(true); });
        await act(async () => {});
        expect(landedPick.c.editorMatch(), 'a start that went out pins the panel on the picked match').toBe('m3');
      } finally { landedPick.c.utils.unmount(); landedPick.c.restore(); }

      const { c, settleRevert } = await pickBlocker(supersededStartOfM3());
      try {
        await act(async () => { settleRevert().resolve(true); });
        await act(async () => {});
        expect(c.editorMatch(), 'nothing was started: the panel stays where it was').not.toBe('m3');
      } finally { c.restore(); }
    });

    // PR #463 round 19 (S1). The same answer WITHOUT heldGroups is an echo-hold:
    // the stored result already equals the start, so another device's start of m3
    // landed first and m3 IS running. That start went out, exactly like a landed
    // one (tests f and the landed half of h): the refused row waiting on it gets
    // the past-tense notice, and the pick pins the panel on m3.
    const echoStartOfM3 = () => vi.fn((_compId, matchId) => Promise.resolve(
      matchId === 'm3' ? { applied: false, reason: 'superseded' } : { applied: true }
    ));

    it('i) a pick whose start is held as an echo went out, so the refused row gets the past-tense notice', async () => {
      const onEditScore = echoStartOfM3();
      const { c, settleRevert } = await pickBlocker(onEditScore);
      try {
        await act(async () => { settleRevert().resolve(true); });
        await act(async () => {});
        expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m3']);
        expect(rowOf(c, 'Aka m4').querySelector('[role="alert"]').textContent)
          .toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(courtMatch('m3', 'scheduled')) }));
        expect(c.utils.container.textContent).not.toContain(START_SUPERSEDED_MESSAGE);
      } finally { c.restore(); }
    });

    it('j) a pick whose start is held as an echo pins the panel on the match, which is running', async () => {
      const { c, settleRevert } = await pickBlocker(echoStartOfM3());
      try {
        await act(async () => { settleRevert().resolve(true); });
        await act(async () => {});
        expect(c.editorMatch(), 'a start that went out pins the panel on the picked match').toBe('m3');
      } finally { c.restore(); }
    });
  });
});

// The one-start guard is judged from the start in flight NOW. An advance awaits
// its finish write and then starts the next match from the render that began the
// await, so a guard that reads that render's state sees a start begun meanwhile
// as absent, and one that has since landed as still out.
describe('the one-start guard reads the start in flight now, not the render that began an advance', () => {
  const deferred = () => {
    let resolve;
    const promise = new Promise((r) => { resolve = r; });
    return { promise, resolve };
  };
  const side = (id, name) => ({ id, name });
  const runM = {
    id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
    court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
  };
  const openM = {
    id: 'm-open', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:10', sideA: side('s', 'Sato'), sideB: side('k', 'Kato'),
  };
  const laterM = {
    id: 'm-later', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:15', sideA: side('q1', 'Ito'), sideB: side('q2', 'Ishii'),
  };
  const winner = side('p1', 'Yamada');
  // The queue row's Start button for the match whose side carries `name`.
  const queueStart = (utils, name) => {
    const row = [...utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(name));
    return [...row.querySelectorAll('button')].find((b) => /^start match$/i.test(b.textContent.trim()));
  };
  let prevRevert;
  beforeEach(() => {
    prevRevert = window.API.revertMatchToQueue;
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({});
    window.tournamentMatches = () => [runM, openM, laterM];
    window.filterMatchesByCourt = (m) => m;
  });
  afterEach(() => {
    window.API.revertMatchToQueue = prevRevert;
  });

  it('a) a finish that answers after a queue start began sends no second start, and says why Up next was not started', async () => {
    const finish = deferred();
    // The queue start hangs; the finish of the running bout waits on `finish`.
    const onEditScore = vi.fn((_compId, matchId) => {
      if (matchId === 'm-run') return finish.promise;
      if (matchId === 'm-later') return new Promise(() => {});
      return Promise.resolve({ applied: true });
    });
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    // The editor holds this closure while its finish is out.
    const advance = probe.props.onSubmitAndNext;
    let pending;
    await act(async () => { pending = advance({ status: 'completed', winner }); });
    await act(async () => { queueStart(utils, 'Ito').click(); });
    await act(async () => {});
    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-run', 'm-later']);
    await act(async () => { finish.resolve({ applied: true }); await pending; });
    // No second start went out: the advance was refused, and the refusal shows.
    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-run', 'm-later']);
    expect(utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error').textContent)
      .toBe(startWhileStartingMessage({ label: scoreRowMatchName(laterM) }));
  });

  it('b) a finish that answers after a start that has since landed is not refused as still starting', async () => {
    const laterStart = deferred();
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm-later' ? laterStart.promise : Promise.resolve({ applied: true })));
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    await act(async () => { queueStart(utils, 'Ito').click(); });
    await act(async () => {});
    // The editor holds this closure while the queue start is still out.
    const advance = probe.props.onSubmitAndNext;
    await act(async () => { laterStart.resolve({ applied: true }); });
    await act(async () => {});
    await act(async () => { await advance({ status: 'completed', winner }); });
    // The next match was started: the start that had landed no longer blocks it.
    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-later', 'm-run', 'm-open']);
    expect(utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error')).toBeNull();
  });

  // A Start refused while another start is out names that start. Once that start
  // has landed the refusal is a past-tense notice on the refused match, and the
  // next start the operator makes, an advance included, clears it: it never
  // names the second start of the same match.
  it('c) the notice made when a start landed goes when that match is started again by an advance, and never names the second start', async () => {
    const firstStart = deferred();
    const secondStart = deferred();
    const openStarts = [firstStart, secondStart];
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm-open'
      ? openStarts.shift().promise
      : Promise.resolve({ applied: true })));
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    // Finishing the running bout advances to m-open, and that start is held.
    let firstAdvance;
    await act(async () => { firstAdvance = probe.props.onSubmitAndNext({ status: 'completed', winner }); });
    await act(async () => {});
    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-run', 'm-open']);
    // A Start of the queue row below is refused while m-open's start is out.
    const itoRow = () => [...utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes('Ito'));
    await act(async () => { queueStart(utils, 'Ito').click(); });
    await act(async () => {});
    expect(itoRow().querySelector('[role="alert"]')?.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(openM) }));
    // The start lands: Ito was asked for and never started, so the refusal is
    // kept, in the past tense, on Ito's row.
    await act(async () => { firstStart.resolve({ applied: true }); await firstAdvance; });
    await act(async () => {});
    expect(itoRow().querySelector('[role="alert"]')?.textContent).toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(openM) }));
    // m-open is started again by an advance, and that start is still out.
    await act(async () => { probe.props.onSubmitAndNext({ status: 'completed', winner }); });
    await act(async () => {});
    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-run', 'm-open', 'm-run', 'm-open']);
    expect(itoRow().querySelector('[role="alert"]'), 'the old notice must not name the second start').toBeNull();
  });
});

// Stage 6 review 1: while this console's own Start is out (and long while
// offline), the editor runs the match it started, so the Up next card and the
// Upcoming list never offer that bout again (Start match, Call to court). A
// start refused later puts it back, as startedFrom's unpin does.
describe('an own Start still out takes the match out of Up next and Upcoming', () => {
  const upNextText = (c) => { const el = c.utils.container.querySelector('.shiaijo-upnext__card'); return el ? el.textContent : ''; };
  const startOnCard = (c) => [...c.utils.container.querySelector('.shiaijo-upnext__card').querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));

  it('the started bout leaves the card at once and the next bout is Up next', async () => {
    const pendingStart = { applied: true, pending: true, key: 'k', settled: new Promise(() => {}) };
    const onEditScore = vi.fn().mockImplementation(() => Promise.resolve(pendingStart));
    const c = await mountCourt([courtMatch('m1', 'scheduled', { modifiedAt: 100 }), courtMatch('m2', 'scheduled', { modifiedAt: 100 })], { onEditScore });
    try {
      expect(upNextText(c)).toContain('Aka m1');
      await act(async () => { startOnCard(c).click(); });
      await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
      expect(c.editorMatch(), 'the editor runs the started bout').toBe('m1');
      expect(upNextText(c), 'Up next moves on').toContain('Aka m2');
      expect(upNextText(c)).not.toContain('Aka m1');
      const queueText = [...c.utils.container.querySelectorAll('.shiaijo-queue-row, .shiaijo-queue__row, li')].map((n) => n.textContent).join(' ');
      expect(queueText, 'nor is it in Upcoming').not.toContain('Aka m1');
    } finally { c.restore(); }
  });

  it('a start refused later puts the bout back on Up next', async () => {
    const onEditScore = vi.fn().mockImplementation(() => Promise.resolve({ applied: true, pending: true, key: 'k', settled: new Promise(() => {}) }));
    const c = await mountCourt([courtMatch('m1', 'scheduled', { modifiedAt: 100 }), courtMatch('m2', 'scheduled', { modifiedAt: 100 })], { onEditScore });
    try {
      await act(async () => { startOnCard(c).click(); });
      await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
      expect(upNextText(c)).not.toContain('Aka m1');
      // The outbox hands a later refusal to the caller's onRefusal.
      const { onRefusal } = onEditScore.mock.calls[0][4];
      await act(async () => { onRefusal(new Error('Sato is fighting on shiaijo B')); });
      expect(upNextText(c), 'the refused bout is Up next again').toContain('Aka m1');
    } finally { c.restore(); }
  });
});
