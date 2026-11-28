// bc-drev (operator decision D4 b): the first result of a drawn competition asks
// whether to start the competition, before anything is sent. The owner is
// first_result_confirm.jsx; attemptScoreWrite (write_result.jsx) asks it for
// every editor's start, finish and decision on every host.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
    competitionAwaitsFirstResult, noteFirstResultCompetitions, writeRecordsResult, firstResultAsks,
    firstResultConfirm, confirmFirstResult, resetFirstResultConfirm, FIRST_RESULT_CANCELLED,
} from '../first_result_confirm.jsx';
import { attemptScoreWrite } from '../write_result.jsx';

const EMPTY = { id: '', name: '' };
const side = (id, name) => ({ id, name });
const poolMatch = (status = 'scheduled') => ({ id: 'Pool A-0', status, sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka') });
const drawn = (over = {}) => ({ id: 'c1', name: 'Men individual', status: 'draw-ready', poolMatches: [poolMatch()], bracket: null, ...over });

beforeEach(() => resetFirstResultConfirm());

describe('competitionAwaitsFirstResult', () => {
    it('is true for a drawn competition with nothing recorded', () => {
        expect(competitionAwaitsFirstResult(drawn())).toBe(true);
    });
    it('is false once a pool match is running or completed', () => {
        expect(competitionAwaitsFirstResult(drawn({ poolMatches: [poolMatch('running')] }))).toBe(false);
        expect(competitionAwaitsFirstResult(drawn({ poolMatches: [poolMatch('completed')] }))).toBe(false);
    });
    it('is false for a competition that is not draw-ready', () => {
        expect(competitionAwaitsFirstResult(drawn({ status: 'pools' }))).toBe(false);
        expect(competitionAwaitsFirstResult(drawn({ status: 'setup' }))).toBe(false);
        expect(competitionAwaitsFirstResult(null)).toBe(false);
    });
    it('does not count a bye the knockout draw resolved', () => {
        const bye = { id: 'r0-m0', status: 'completed', sideA: side('p1', 'Yamada'), sideB: EMPTY };
        expect(competitionAwaitsFirstResult(drawn({ poolMatches: [], bracket: { rounds: [[bye]] } }))).toBe(true);
    });
});

describe('writeRecordsResult', () => {
    it('is true for a start, a finish and a decision', () => {
        expect(writeRecordsResult({ status: 'running' })).toBe(true);
        expect(writeRecordsResult({ status: 'completed' })).toBe(true);
        expect(writeRecordsResult({ decision: 'fusensho', decisionBy: 'a' })).toBe(true);
    });
    it('is false for a write that leaves the match scheduled', () => {
        expect(writeRecordsResult({ status: 'scheduled' })).toBe(false);
        expect(writeRecordsResult(null)).toBe(false);
    });
});

describe('the question', () => {
    it('names the competition, the match and what follows', () => {
        const q = firstResultConfirm({ name: 'Men individual', label: 'Pool A · Match 1', starts: true });
        expect(q.title).toBe('Start Men individual?');
        expect(q.message).toBe('Men individual has not started yet. Starting Pool A · Match 1 starts it. After that the draw is fixed: competitors can no longer be swapped, and the draw can no longer be discarded.');
        expect(q.confirmLabel).toBe('Start the competition');
        expect(q.cancelLabel).toBe('Not yet');
        expect(firstResultConfirm({ name: 'Men individual', label: 'Pool A · Match 1' }).message)
            .toContain('Recording a result for Pool A · Match 1 starts it.');
    });

    it('is asked only about a competition noted as drawn with nothing recorded', async () => {
        const confirmDialog = vi.fn().mockResolvedValue(true);
        expect(await confirmFirstResult({ confirmDialog, compId: 'c1' })).toBe(true);
        noteFirstResultCompetitions([drawn({ status: 'pools' })]);
        expect(await confirmFirstResult({ confirmDialog, compId: 'c1' })).toBe(true);
        expect(confirmDialog).not.toHaveBeenCalled();
    });

    it('is asked once per competition: agreed, it is not asked again', async () => {
        noteFirstResultCompetitions([drawn()]);
        const confirmDialog = vi.fn().mockResolvedValue(true);
        expect(await confirmFirstResult({ confirmDialog, compId: 'c1', label: 'Pool A · Match 1', starts: true })).toBe(true);
        expect(confirmDialog).toHaveBeenCalledTimes(1);
        expect(confirmDialog.mock.calls[0][0].title).toBe('Start Men individual?');
        // The court still shows the competition drawn (the start is queued).
        noteFirstResultCompetitions([drawn()]);
        expect(firstResultAsks('c1')).toBe(false);
        expect(await confirmFirstResult({ confirmDialog, compId: 'c1' })).toBe(true);
        expect(confirmDialog).toHaveBeenCalledTimes(1);
    });

    it('kept back, it answers false and asks again next time', async () => {
        noteFirstResultCompetitions([drawn()]);
        const confirmDialog = vi.fn().mockResolvedValue(false);
        expect(await confirmFirstResult({ confirmDialog, compId: 'c1' })).toBe(false);
        expect(firstResultAsks('c1')).toBe(true);
    });

    it('a dialog that fails to open keeps the write back', async () => {
        noteFirstResultCompetitions([drawn()]);
        expect(await confirmFirstResult({ confirmDialog: vi.fn().mockRejectedValue(new Error('x')), compId: 'c1' })).toBe(false);
        expect(await confirmFirstResult({ confirmDialog: undefined, compId: 'c1' })).toBe(false);
    });

    it('an agreement is forgotten when the draw is discarded, so a new draw asks again', async () => {
        noteFirstResultCompetitions([drawn()]);
        await confirmFirstResult({ confirmDialog: vi.fn().mockResolvedValue(true), compId: 'c1' });
        noteFirstResultCompetitions([drawn({ status: 'setup', poolMatches: [] })]);
        noteFirstResultCompetitions([drawn()]);
        expect(firstResultAsks('c1')).toBe(true);
    });
});

describe('attemptScoreWrite asks before the first result (every host)', () => {
    const write = (recordScore, confirmDialog, result) => attemptScoreWrite({
        recordScore, confirmDialog, compId: 'c1', matchId: 'Pool A-0', result, password: 'pw', label: 'Pool A · Match 1',
    });

    it('kept back, nothing is sent and the error says why', async () => {
        noteFirstResultCompetitions([drawn()]);
        const recordScore = vi.fn().mockResolvedValue({ applied: true });
        const confirmDialog = vi.fn().mockResolvedValue(false);
        const err = await write(recordScore, confirmDialog, { status: 'running' }).catch((e) => e);
        expect(err.firstResultCancelled).toBe(true);
        expect(err.message).toBe(FIRST_RESULT_CANCELLED);
        expect(recordScore).not.toHaveBeenCalled();
        expect(confirmDialog.mock.calls[0][0].message).toContain('Starting Pool A · Match 1 starts it.');
    });

    it('agreed, the write is sent once, and a later write asks nothing', async () => {
        noteFirstResultCompetitions([drawn()]);
        const recordScore = vi.fn().mockResolvedValue({ applied: true });
        const confirmDialog = vi.fn().mockResolvedValue(true);
        await write(recordScore, confirmDialog, { status: 'running' });
        await write(recordScore, confirmDialog, { status: 'completed' });
        expect(confirmDialog).toHaveBeenCalledTimes(1);
        expect(recordScore).toHaveBeenCalledTimes(2);
        expect(recordScore.mock.calls[0][2]).toEqual({ status: 'running' });
    });

    it('names the match by matchName, the label with the pairing, when the host gives one', async () => {
        noteFirstResultCompetitions([drawn()]);
        const confirmDialog = vi.fn().mockResolvedValue(false);
        await attemptScoreWrite({
            recordScore: vi.fn(), confirmDialog, compId: 'c1', matchId: 'Pool A-0', result: { status: 'running' }, password: 'pw',
            label: 'Pool A · Match 1', matchName: 'Pool A · Match 1 · Kato vs Sato',
        }).catch(() => {});
        expect(confirmDialog.mock.calls[0][0].message).toContain('Starting Pool A · Match 1 · Kato vs Sato starts it.');
    });

    it('a decision asks too, worded as a result', async () => {
        noteFirstResultCompetitions([drawn()]);
        const recordScore = vi.fn().mockResolvedValue({ applied: true });
        const confirmDialog = vi.fn().mockResolvedValue(false);
        const err = await write(recordScore, confirmDialog, { decision: 'fusensho', decisionBy: 'a' }).catch((e) => e);
        expect(err.firstResultCancelled).toBe(true);
        expect(confirmDialog.mock.calls[0][0].message).toContain('Recording a result for Pool A · Match 1 starts it.');
        expect(recordScore).not.toHaveBeenCalled();
    });

    it('a write that asks nothing still reaches recordScore in the same tick', () => {
        noteFirstResultCompetitions([drawn({ status: 'pools' })]);
        const recordScore = vi.fn(() => new Promise(() => {}));
        write(recordScore, vi.fn(), { status: 'running' });
        expect(recordScore).toHaveBeenCalledTimes(1);
    });

    it('asks nothing for a scheduled write, or for a competition already started', async () => {
        noteFirstResultCompetitions([drawn()]);
        const recordScore = vi.fn().mockResolvedValue({ applied: true });
        const confirmDialog = vi.fn().mockResolvedValue(false);
        await write(recordScore, confirmDialog, { status: 'scheduled' });
        noteFirstResultCompetitions([drawn({ poolMatches: [poolMatch('running')] })]);
        await write(recordScore, confirmDialog, { status: 'completed' });
        expect(confirmDialog).not.toHaveBeenCalled();
        expect(recordScore).toHaveBeenCalledTimes(2);
    });
});
