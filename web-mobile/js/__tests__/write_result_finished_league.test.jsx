// The finished-league warning (design item 3, operator decision D2: warn
// first, then save). attemptScoreWrite turns the 409 league_finished refusal
// of a correction into the confirm and, on Change it anyway, resends the same
// write with the request-only confirmFinished: true. It is asked on every host:
// the court console reaches attemptScoreWrite through editMatchScore with
// allowCourtForce false, so the warning is never gated on it. The copy is the
// e2e fixture's (scripts/e2e/fixtures/league.mjs TEXT.finishedLeague and
// TEXT.changeAnyway), pinned here word for word.

import { describe, it, expect, vi } from 'vitest';
import { attemptScoreWrite, leagueFinishedRefusal, leagueFinishedConfirm, LEAGUE_FINISHED_WARNING } from '../write_result.jsx';

const FIXTURE_WARNING = 'This league has finished. Changing this result can change the final table and podium. If it leaves a new tie at a podium place, the league goes back to match play to settle it.';

function leagueFinishedError(removedCourts = []) {
    return Object.assign(new Error('This league has finished.'), {
        code: 'league_finished', leagueFinished: { removedCourts },
    });
}

// An outbox door: answers pending at once and keeps the onRefusal the
// write was handed, so a test can bring the server's refusal back later.
function outboxDoor() {
    const calls = [];
    const door = vi.fn((compId, matchId, result, password, match, opts) => {
        const call = { result, opts };
        call.settled = new Promise((resolve, reject) => { call.resolve = resolve; call.reject = reject; });
        call.settled.catch(() => {});
        calls.push(call);
        return Promise.resolve({ applied: true, pending: true, key: `k${calls.length}`, settled: call.settled });
    });
    door.calls = calls;
    // The server refuses call i: the outbox offers the refusal to its claimer.
    door.refuse = (i, err) => {
        const claimed = calls[i].opts && calls[i].opts.onRefusal && calls[i].opts.onRefusal(err);
        calls[i].reject(err);
        return claimed;
    };
    return door;
}

async function settle() {
    for (let i = 0; i < 20; i++) await Promise.resolve();
}

const correction = { status: 'completed', winner: { id: 'p2', name: 'Bob' }, correctionReason: 'typo' };
const match = { id: 'Pool A-0', compId: 'c1', status: 'completed', court: 'A' };

describe('leagueFinishedConfirm', () => {
    it('says what the change can do, in the fixture\'s words, with Change it anyway and Cancel', () => {
        expect(LEAGUE_FINISHED_WARNING).toBe(FIXTURE_WARNING);
        const c = leagueFinishedConfirm({ removedCourts: [] });
        expect(c.message).toBe(FIXTURE_WARNING);
        expect(c.confirmLabel).toBe('Change it anyway');
        expect(c.cancelLabel).toBe('Cancel');
    });

    it('names a shiaijo the tournament no longer has', () => {
        expect(leagueFinishedConfirm({ removedCourts: ['C'] }).message)
            .toBe(`${FIXTURE_WARNING} The tournament no longer has Shiaijo C: if the league goes back to match play, its matches there show as unassigned until they are given a court.`);
        expect(leagueFinishedConfirm({ removedCourts: ['C', 'D', 'E'] }).message)
            .toContain('The tournament no longer has Shiaijo C, Shiaijo D and Shiaijo E:');
    });

    it('carries no em-dash and never says mat', () => {
        const all = leagueFinishedConfirm({ removedCourts: ['C', 'D'] }).message;
        expect(all).not.toMatch(/—/);
        expect(all).not.toMatch(/\bmats?\b/i);
    });

    it('leagueFinishedRefusal reads the parsed refusal and nothing else', () => {
        expect(leagueFinishedRefusal(leagueFinishedError(['C']))).toEqual({ removedCourts: ['C'] });
        expect(leagueFinishedRefusal(Object.assign(new Error('x'), { code: 'court_busy' }))).toBeNull();
        expect(leagueFinishedRefusal(null)).toBeNull();
    });
});

describe('attemptScoreWrite and a finished league', () => {
    // The court console's onEditScore is admin.jsx's editMatchScore, which
    // passes allowCourtForce false for the console: the warning still shows.
    it('asks, and on Change it anyway resends the same correction with confirmFinished (the court console\'s path)', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(true);
        const onForced = vi.fn();
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: correction,
            password: 'pw', match, allowCourtForce: false, onForced,
        });
        expect(door).toHaveBeenCalledTimes(1);
        expect(door.calls[0].result.confirmFinished).toBeUndefined();

        expect(door.refuse(0, leagueFinishedError())).toBe(true);
        await settle();

        expect(confirmDialog).toHaveBeenCalledTimes(1);
        const asked = confirmDialog.mock.calls[0][0];
        expect(asked.message).toBe(FIXTURE_WARNING);
        expect(asked.confirmLabel).toBe('Change it anyway');
        expect(door).toHaveBeenCalledTimes(2);
        expect(door.calls[1].result).toEqual({ ...correction, confirmFinished: true });
        expect(onForced).toHaveBeenCalledTimes(1);
    });

    it('Cancel resends nothing and hands the refusal, marked, to the caller', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(false);
        const onRefusal = vi.fn(() => true);
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: correction,
            password: 'pw', match, onRefusal,
        });
        const err = leagueFinishedError();
        door.refuse(0, err);
        await settle();

        expect(door).toHaveBeenCalledTimes(1);
        expect(onRefusal).toHaveBeenCalledWith(err);
        expect(err.leagueFinishedCancelled).toBe(true);
    });

    it('a resend refused again is not asked about a second time', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(true);
        const onRefusal = vi.fn(() => true);
        await attemptScoreWrite({
            recordScore: door, confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: correction,
            password: 'pw', match, onRefusal,
        });
        door.refuse(0, leagueFinishedError());
        await settle();
        const again = leagueFinishedError();
        door.refuse(1, again);
        await settle();

        expect(confirmDialog).toHaveBeenCalledTimes(1);
        expect(door).toHaveBeenCalledTimes(2);
        expect(onRefusal).toHaveBeenCalledWith(again);
    });

    it('a write that throws its refusal directly gets the same confirm, awaited', async () => {
        const forced = { id: 'Pool A-0', status: 'completed' };
        const recordScore = vi.fn()
            .mockRejectedValueOnce(leagueFinishedError(['C']))
            .mockResolvedValueOnce(forced);
        const confirmDialog = vi.fn().mockResolvedValue(true);
        const out = await attemptScoreWrite({
            recordScore, confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: correction, password: 'pw', match,
        });
        expect(out).toBe(forced);
        expect(confirmDialog.mock.calls[0][0].message).toContain('Shiaijo C');
        expect(recordScore.mock.calls[1][2].confirmFinished).toBe(true);
    });

    it('a decision correction (the decision editor\'s collaborator) is resent with confirmFinished in its body', async () => {
        const door = outboxDoor();
        const confirmDialog = vi.fn().mockResolvedValue(true);
        const body = { decision: 'kiken-injury', decisionBy: 'shiro' };
        await attemptScoreWrite({
            recordScore: (cId, mId, result, pwd, _m, o) => door(cId, mId, result, pwd, null, o),
            confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: body, password: 'pw',
        });
        door.refuse(0, leagueFinishedError());
        await settle();
        expect(door.calls[1].result).toEqual({ ...body, confirmFinished: true });
    });
});
