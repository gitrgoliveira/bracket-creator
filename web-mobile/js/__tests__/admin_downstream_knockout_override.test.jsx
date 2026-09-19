// bc-kcdg: admin.jsx's attemptScoreWrite is the confirm+retry loop behind
// editMatchScore, the single chokepoint every score-editor host (bracket
// panel, pools, schedule score editor, per-court shiaijo) and both editor
// bodies (individual, team) route a score write through. Extracted (like
// mergeCompetitionsIntoTournament) as a pure-ish helper taking recordScore/
// confirmDialog as injected collaborators, so the contract is pinned here
// without rendering the whole admin SPA.

import { describe, it, expect, vi } from 'vitest';
import { attemptScoreWrite } from '../admin.jsx';

function downstreamError(fields = {}) {
  const e = new Error('Aoki Taro already played match m5.');
  e.downstreamKnockoutPlayed = {
    matchId: 'm1', blockingMatchId: 'm5', displaced: 'Aoki Taro', ...fields,
  };
  return e;
}

describe('attemptScoreWrite (bc-kcdg)', () => {
  it('passes a normal write straight through on success', async () => {
    const recordScore = vi.fn().mockResolvedValue({ id: 'm1', status: 'completed' });
    const confirmDialog = vi.fn();
    const result = { status: 'completed' };
    const res = await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result, password: 'pw', match: null,
    });
    expect(res).toEqual({ id: 'm1', status: 'completed' });
    expect(recordScore).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledWith('c1', 'm1', result, 'pw', null);
    // No refusal: the confirm dialog must never be shown for an ordinary
    // success or an ordinary (non-downstream) failure.
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('re-throws a plain (non-downstream) failure without ever prompting', async () => {
    const plainErr = new Error('Failed to record score');
    const recordScore = vi.fn().mockRejectedValue(plainErr);
    const confirmDialog = vi.fn();
    await expect(attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: {}, password: 'pw', match: null,
    })).rejects.toBe(plainErr);
    expect(confirmDialog).not.toHaveBeenCalled();
  });

  it('on a downstream refusal, prompts and, on confirm, retries with forceDownstreamReopen:true', async () => {
    const recordScore = vi.fn()
      .mockRejectedValueOnce(downstreamError())
      .mockResolvedValueOnce({ id: 'm1', status: 'completed' });
    const confirmDialog = vi.fn().mockResolvedValue(true);
    const originalResult = { status: 'completed' };

    const res = await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: originalResult, password: 'pw', match: null,
    });

    expect(res).toEqual({ id: 'm1', status: 'completed' });
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    // The dialog must name the blocking match and the displaced competitor
    // (the copy itself is pinned in write_result_downstream_knockout.test.jsx;
    // this just confirms attemptScoreWrite actually threads the refusal
    // through rather than showing a generic prompt).
    const dialogArg = confirmDialog.mock.calls[0][0];
    expect(dialogArg.message).toContain('m5');
    expect(dialogArg.message).toContain('Aoki Taro');

    // The retry must be the SAME patch plus the force flag, not a fresh one.
    expect(recordScore).toHaveBeenCalledTimes(2);
    const retryArgs = recordScore.mock.calls[1];
    expect(retryArgs[2]).toEqual({ status: 'completed', forceDownstreamReopen: true });
    // The original object must not be mutated in place.
    expect(originalResult).toEqual({ status: 'completed' });
  });

  it('on a downstream refusal, declining leaves the match unwritten and marks the error cancelled', async () => {
    const err = downstreamError();
    const recordScore = vi.fn().mockRejectedValue(err);
    const confirmDialog = vi.fn().mockResolvedValue(false);

    const caught = await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: { status: 'completed' }, password: 'pw', match: null,
    }).then(() => { throw new Error('expected a rejection'); }, (e) => e);

    expect(caught).toBe(err);
    expect(caught.downstreamKnockoutPlayedCancelled).toBe(true);
    // Only the one doomed attempt: no retry was ever sent.
    expect(recordScore).toHaveBeenCalledTimes(1);
  });

  it('does not loop the confirm dialog on a second refusal after the forced retry', async () => {
    // A genuine race: the forced retry is refused again. The guard on
    // result.forceDownstreamReopen must stop a second prompt and just
    // propagate the error like any other failure.
    const err2 = downstreamError({ blockingMatchId: 'm6' });
    const recordScore = vi.fn()
      .mockRejectedValueOnce(downstreamError())
      .mockRejectedValueOnce(err2);
    const confirmDialog = vi.fn().mockResolvedValue(true);

    const caught = await attemptScoreWrite({
      recordScore, confirmDialog, compId: 'c1', matchId: 'm1', result: { status: 'completed' }, password: 'pw', match: null,
    }).then(() => { throw new Error('expected a rejection'); }, (e) => e);

    expect(caught).toBe(err2);
    expect(caught.downstreamKnockoutPlayedCancelled).toBeUndefined();
    expect(confirmDialog).toHaveBeenCalledTimes(1);
    expect(recordScore).toHaveBeenCalledTimes(2);
  });
});
