import { act } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { setupShiaijoConsole, probe, makeMinimalTournament, renderPage } from '../helpers/shiaijo_console.js';
import { startWhileStartingMessage, CLOCK_SKEW_REASON_TEXT, startWasBlockedByStartMessage } from '../../write_result.jsx';
import { scoreRowMatchName } from '../../pool_ids.jsx';

setupShiaijoConsole();

// bc-cse: a scheduled match a competitor is barred from (withdrew earlier)
// cannot be fought. No auto-pick may offer it (Up Next, Finish + Start Next);
// its queue row shows why and a one-tap way to resolve it instead of a Start
// button the server would just refuse.
describe('a barred match is skipped by every auto-pick (bc-cse)', () => {
  const side = (id, name) => ({ id, name });
  // Withdrawn on side B (Yama, kiken-voluntary): the default win goes to Umi.
  const barredMatch = (over = {}) => ({
    id: 'm-barred', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:05', sideA: side('u', 'Umi'), sideB: side('y', 'Yama'),
    ineligibleSides: { b: 'kiken-voluntary' },
    ...over,
  });
  const openMatch = {
    id: 'm-open', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
    court: 'A', scheduledAt: '09:10', sideA: side('s', 'Sato'), sideB: side('k', 'Kato'),
  };

  it('Up Next skips it, its queue row offers the fusensho action instead of Start, and no Reinstate for a non-reinstateable withdrawal', async () => {
    window.tournamentMatches = () => [barredMatch(), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const recordDecision = vi.fn().mockResolvedValue({ applied: true });
    const prevRD = window.API.recordDecision;
    window.API.recordDecision = recordDecision;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });

      // Up Next skips the barred match and offers the next fightable one.
      const card = utils.container.querySelector('.shiaijo-upnext__card');
      expect(card.textContent).toContain('Sato');
      expect(card.textContent).not.toContain('Umi');
      expect(card.textContent).not.toContain('Yama');

      // The barred match's own row: the note, one action, no Start, no Reinstate.
      const rows = [...utils.container.querySelectorAll('.shiaijo-qrow')];
      const row = rows.find((r) => r.textContent.includes('Yama'));
      expect(row).toBeTruthy();
      expect(row.textContent).toContain('Yama withdrew: record the fusensho.');
      const startBtn = [...row.querySelectorAll('button')].find((b) => /^start match$/i.test(b.textContent));
      expect(startBtn).toBeUndefined();
      expect(utils.queryByTestId('barred-match-reinstate')).toBeNull();
      const awardBtn = utils.getByTestId('barred-match-record-fusensho');
      expect(awardBtn.textContent).toBe('Record fusensho for Umi');

      await act(async () => { awardBtn.click(); });
      expect(recordDecision).toHaveBeenCalledWith('c1', 'm-barred', {
        decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Yama withdrawn',
        sideAId: 'u', sideBId: 'y',
      }, '', expect.objectContaining({ onRefusal: expect.any(Function) }));
    } finally {
      window.API.recordDecision = prevRD;
    }
  });

  it('Finish + Start Next skips it when advancing to the next match on the same court', async () => {
    const running = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [running, barredMatch(), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    await act(async () => { renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
    // The finishing write, then the start-next write -- skipping m-barred.
    expect(onEditScore).toHaveBeenCalledTimes(2);
    expect(onEditScore.mock.calls[0][0]).toBe('c1');
    expect(onEditScore.mock.calls[0][1]).toBe('m-run');
    expect(onEditScore.mock.calls[1][0]).toBe('c1');
    expect(onEditScore.mock.calls[1][1]).toBe('m-open');
  });

  // Every host hands the editor back what its write came back with: the finish
  // write's answer, a partial apply's held groups included, is the return value.
  it('Finish + Start Next hands the finish write\'s answer back to the editor', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [mRun, openMatch];
    window.filterMatchesByCourt = (m) => m;
    const answer = { applied: true, heldGroups: ['points'] };
    const onEditScore = vi.fn()
      .mockResolvedValueOnce(answer)
      .mockResolvedValueOnce({ status: 'ok' });
    await act(async () => { renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    let ret;
    await act(async () => { ret = await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
    // The start-next write still went out after the finish.
    expect(onEditScore).toHaveBeenCalledTimes(2);
    expect(ret).toEqual(answer);
  });

  // bc-aadv: Finish + Start Next keeps a refused start on the Up next card,
  // instead of swallowing the error silently.
  it('Finish + Start Next keeps a refused start on the Up next card', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [mRun, openMatch];
    window.filterMatchesByCourt = (m) => m;
    const onEditScore = vi.fn()
      .mockResolvedValueOnce({ status: 'ok' })
      .mockRejectedValueOnce(new Error('Sato is fighting on Shiaijo B.'));
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
    // The finishing write succeeds; the start-next write fails.
    expect(onEditScore).toHaveBeenCalledTimes(2);
    // The error is recorded on the Up next card.
    const errorEl = utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
    expect(errorEl).toBeTruthy();
    expect(errorEl.textContent).toContain('Sato is fighting on Shiaijo B.');
  });

  it('onAfterDecision keeps a refused start on the Up next card', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [mRun, openMatch];
    window.filterMatchesByCourt = (m) => m;
    const onEditScore = vi.fn()
      .mockRejectedValueOnce(new Error('Could not start the match: check eligibility and try again.'));
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    await act(async () => { await probe.props.onAfterDecision({ winner: side('p1', 'Yamada') }); });
    // The start-next write fails.
    expect(onEditScore).toHaveBeenCalledTimes(1);
    // The error is recorded on the Up next card.
    const errorEl = utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
    expect(errorEl).toBeTruthy();
    expect(errorEl.textContent).toContain('Could not start the match');
  });

  it('clock_skew on the auto-start is reported on the Up next card', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [mRun, openMatch];
    window.filterMatchesByCourt = (m) => m;
    const onEditScore = vi.fn()
      .mockResolvedValueOnce({ status: 'ok' })
      .mockResolvedValueOnce({ applied: false, reason: 'clock_skew' });
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
    expect(probe.props.match?.id).toBe('m-run');
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
    // The finishing write succeeds; the start-next write returns clock_skew.
    expect(onEditScore).toHaveBeenCalledTimes(2);
    // The clock-skew error is recorded on the Up next card.
    const errorEl = utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
    expect(errorEl).toBeTruthy();
    expect(errorEl.textContent).toContain(CLOCK_SKEW_REASON_TEXT);
  });

  // A Finish + Start Next that asks for Up next while a start of ANOTHER match
  // is still out is refused by the one-start guard. That refusal must say so on
  // Up next, not vanish (bc-aadv).
  it('Finish + Start Next says why Up next was not started while another match is being started', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    const mLater = {
      id: 'm-later', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:15', sideA: side('q1', 'Ito'), sideB: side('q2', 'Ishii'),
    };
    window.tournamentMatches = () => [mRun, openMatch, mLater];
    window.filterMatchesByCourt = (m) => m;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({});
    // The queued row's start hangs; the finish of the running bout lands.
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm-later' ? new Promise(() => {}) : Promise.resolve({ status: 'ok' })));
    const showToast = vi.fn();
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore, showToast }); });
      expect(probe.props.match?.id).toBe('m-run');
      await act(async () => { utils.container.querySelector('.shiaijo-row__pick').click(); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-later']);
      expect(probe.props.match?.id).toBe('m-run');
      await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
      // The finish landed and no second start went out: Up next was refused.
      expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-later', 'm-run']);
      const note = utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
      expect(note).toBeTruthy();
      expect(note.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(mLater) }));
      // The refusal is toasted as well as noted, the same as a refused tap.
      expect(showToast).toHaveBeenCalledWith(startWhileStartingMessage({ label: scoreRowMatchName(mLater) }), 'error');
    } finally {
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  // The refusal is derived from the start in flight while that start is out, so
  // the present-tense sentence goes once it lands. The match the operator asked
  // for was never started, though, and nothing else says so: the refusal is kept
  // on the refused match in the past tense (bc-aadv).
  it('the Up next note for a refused advance becomes a past-tense note once the blocking start lands', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    const mLater = {
      id: 'm-later', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:15', sideA: side('q1', 'Ito'), sideB: side('q2', 'Ishii'),
    };
    window.tournamentMatches = () => [mRun, openMatch, mLater];
    window.filterMatchesByCourt = (m) => m;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({});
    let landQueued;
    const queuedStart = new Promise((resolve) => { landQueued = resolve; });
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm-later' ? queuedStart : Promise.resolve({ status: 'ok' })));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      await act(async () => { utils.container.querySelector('.shiaijo-row__pick').click(); });
      await act(async () => {});
      await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
      const note = () => utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error');
      expect(note()?.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(mLater) }));
      await act(async () => { landQueued({ status: 'ok' }); });
      await act(async () => {});
      expect(note()?.textContent).toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(mLater) }));
    } finally {
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  // The one-start guard stays silent for a repeat advance to the match already
  // being started: that match is simply on its way.
  it('a repeat Finish + Start Next to the match already being started stays silent', async () => {
    const mRun = {
      id: 'm-run', compId: 'c1', compName: 'Cup', status: 'running', phase: 'pool', poolName: 'Pool A',
      court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'),
    };
    window.tournamentMatches = () => [mRun, openMatch];
    window.filterMatchesByCourt = (m) => m;
    const prevRevert = window.API.revertMatchToQueue;
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({});
    // Up next's own start hangs; the finish of the running bout lands.
    const onEditScore = vi.fn((_compId, matchId) => (matchId === 'm-open' ? new Promise(() => {}) : Promise.resolve({ status: 'ok' })));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      const upNextStart = [...utils.container.querySelector('.shiaijo-upnext__card').querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
      await act(async () => { upNextStart.click(); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-open']);
      await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: side('p1', 'Yamada') }); });
      expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-open', 'm-run']);
      expect(utils.container.querySelector('.shiaijo-upnext__card .shiaijo-upnext__error')).toBeNull();
    } finally {
      window.API.revertMatchToQueue = prevRevert;
    }
  });

  it('offers Reinstate for a reinstateable (kiken-injury) withdrawal', async () => {
    window.tournamentMatches = () => [barredMatch({ ineligibleSides: { b: 'kiken-injury' } }), openMatch];
    window.filterMatchesByCourt = (m) => m;
    const reinstateCompetitor = vi.fn().mockResolvedValue({});
    const prevR = window.API.reinstateCompetitor;
    window.API.reinstateCompetitor = reinstateCompetitor;
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament()); });
      const reinstateBtn = utils.getByTestId('barred-match-reinstate');
      expect(reinstateBtn.textContent).toBe('Reinstate Yama');
      await act(async () => { reinstateBtn.click(); });
      expect(reinstateCompetitor).toHaveBeenCalledWith('c1', 'y', '', expect.any(Object));
    } finally {
      window.API.reinstateCompetitor = prevR;
    }
  });

  // bc-cse: both sides barred on a POOL match (barredMatch's own phase) can
  // be recorded as drawn -- neither single-sided default-win action fits,
  // but the pool/league draw action does.
  it('both sides barred: the note, and the draw action (no single-sided action)', async () => {
    window.tournamentMatches = () => [barredMatch({ id: 'Pool A-2', ineligibleSides: { a: 'fusenpai', b: 'kiken-voluntary' } })];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    const notice = utils.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Both withdrew earlier: neither can fight this match.');
    expect(utils.queryByTestId('barred-match-record-fusensho')).toBeNull();
    expect(utils.queryByTestId('barred-match-reinstate')).toBeNull();
    expect(utils.getByTestId('barred-match-record-drawn').textContent).toBe('Record as drawn (neither can fight)');
  });

  // bc-cse: when EVERY scheduled match on the court is barred there is no
  // Up Next card at all (upNext skips a barred match on purpose), so
  // telling the operator to use it points at something not on screen. The
  // placeholder must name the real remedy instead.
  it('placeholder names the real remedy when every scheduled match is barred (no Up Next card)', async () => {
    window.tournamentMatches = () => [barredMatch()];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    expect(utils.container.querySelector('.shiaijo-upnext__card')).toBeNull();
    expect(utils.container.textContent).toContain(
      'Every scheduled match on this court is barred. Resolve a withdrawal in the queue to bring one back.'
    );
    expect(utils.container.textContent).not.toContain('Start the next match from the Up Next card');
  });

  // bc-cse: the ordinary placeholder still applies when there is nothing
  // scheduled for a reason OTHER than "everything is barred" -- here, a
  // pending placeholder final with no real Up Next yet (same fixture shape
  // as the "Later" row test above).
  it('placeholder keeps the ordinary copy when nothing is scheduled for an unrelated reason', async () => {
    const completedFeeder = {
      id: 'r2-m0', compId: 'c1', compName: 'Cup', status: 'completed',
      phase: 'bracket', matchNumber: 1, court: 'A',
      sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
      winner: { id: 'p1', name: 'Yamada' },
    };
    const pendingFinal = {
      id: 'r3-m0', compId: 'c1', compName: 'Cup', status: 'scheduled',
      phase: 'bracket', matchNumber: 3, court: 'A',
      sideA: { id: '', name: 'Winner of r2-m0' },
      sideB: { id: '', name: 'Winner of r2-m1' },
    };
    window.tournamentMatches = () => [completedFeeder, pendingFinal];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    expect(utils.container.textContent).toContain('Start the next match from the Up Next card');
    expect(utils.container.textContent).not.toContain('Every scheduled match on this court is barred');
  });

  // bc-cse: a barred queue row offers only its own one-tap resolution --
  // Call to court and Lineup, both meaningless for a match the server would
  // refuse to start, are hidden.
  it('a barred queue row hides Call to court and Lineup', async () => {
    const teamBarred = {
      id: 'm-team-barred', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:05', compKind: 'team', teamSize: 3,
      sideA: { id: 'team-a', name: 'Team A' }, sideB: { id: 'team-b', name: 'Team B' },
      ineligibleSides: { b: 'kiken-voluntary' },
    };
    window.tournamentMatches = () => [teamBarred, openMatch];
    window.filterMatchesByCourt = (m) => m;
    let utils;
    await act(async () => { utils = renderPage(makeMinimalTournament()); });
    const rows = [...utils.container.querySelectorAll('.shiaijo-qrow')];
    const row = rows.find((r) => r.textContent.includes('Team A'));
    expect(row).toBeTruthy();
    const buttonText = [...row.querySelectorAll('button')].map((b) => b.textContent);
    expect(buttonText).not.toContain('Lineup');
    expect(buttonText.some((t) => /call to court/i.test(t))).toBe(false);
  });
});
