// bc-sseq FR8: a result another device recorded that the court's result
// replaced reads "Replaced by the court's result" in the match's History,
// whichever arrived first: moved out of the match when it was stored first
// (door "displaced"), or kept as it arrived after the court's (its own door).
// Each of the two results keeps its own line at the time its device made it.
// The reason is the server's history string (engine.HoldReasonCourtResult),
// pinned against the Go source so the two never drift apart.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import {
    historyEntryView, HISTORY_REASON_COURT_RESULT, COURT_RESULT_HISTORY_WORDS,
} from '../match_history_view.jsx';

const goSource = readFileSync(resolve(__dirname, '..', '..', '..', 'internal', 'engine', 'match_merge.go'), 'utf8');

const otherResult = { status: 'completed', winner: 'Sato', ipponsA: ['M', 'M'], ipponsB: [] };

describe('a result the court replaced, in the match history', () => {
    it('the reason string is the engine\'s HoldReasonCourtResult', () => {
        const m = goSource.match(/const HoldReasonCourtResult = "([^"]*)"/);
        expect(m, 'match_merge.go declares HoldReasonCourtResult').toBeTruthy();
        expect(HISTORY_REASON_COURT_RESULT).toBe(m[1]);
    });

    it('stored first, then moved out by the court\'s: a result moved to history, never a "later change"', () => {
        const v = historyEntryView({
            door: 'displaced', stamp: Date.UTC(2026, 10, 26, 9, 0, 5), changed: ['result'],
            outcomes: { result: 'held' }, held: { result: otherResult }, reason: HISTORY_REASON_COURT_RESULT,
        });
        expect(v.door).toBe('Result moved to history');
        expect(v.reason).toBe(COURT_RESULT_HISTORY_WORDS);
        expect(v.reason).toBe("Replaced by the court's result");
        expect(v.held.map((h) => h.text)).toEqual(['Moved to history: the result, Sato won']);
        expect(v.time).toMatch(/^\d\d:\d\d:\d\d$/);
    });

    it('arriving after the court\'s: the same words, kept as it came', () => {
        const v = historyEntryView({
            door: 'score', stamp: Date.UTC(2026, 10, 26, 9, 0, 5), changed: ['result', 'points'],
            outcomes: { result: 'held', points: 'held' }, held: { result: otherResult }, reason: HISTORY_REASON_COURT_RESULT,
        });
        expect(v.door).toBe('Score sheet saved');
        expect(v.reason).toBe("Replaced by the court's result");
        expect(v.held.map((h) => h.text)).toEqual(['Kept in history: the result, Sato won']);
    });

    it('any other displaced change keeps its words', () => {
        const v = historyEntryView({
            door: 'displaced', stamp: 1000, changed: ['points'], outcomes: { points: 'held' },
            held: { points: { ipponsA: ['K'], ipponsB: [] } }, reason: 'the match was already finished',
        });
        expect(v.door).toBe('Later change moved to history');
        expect(v.reason).toBe('Moved to history: the match was already finished');
    });

    it('a court result the built-on hold kept says why, in the server\'s words', () => {
        const m = goSource.match(/const HoldReasonResultBuiltOn = "([^"]*)"/);
        expect(m).toBeTruthy();
        const v = historyEntryView({
            door: 'score', stamp: 1000, changed: ['result'], outcomes: { result: 'held' },
            held: { result: otherResult }, reason: m[1],
        });
        expect(v.reason).toBe(`Not applied: ${m[1]}`);
    });
});
