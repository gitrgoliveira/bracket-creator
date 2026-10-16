// bc-evts stage 3: a running autosave is an outbox entry and answers pending
// at once. landedAutosave must clear the edit-time adopt stamp only once the
// server HOLDS the edit, i.e. on the SETTLED answer, never on the pending one,
// or a server copy that predates the edit is adopted over it meanwhile. A
// callback, never a wait: the submit's own promise is handed back untouched.
import { describe, it, expect, vi } from 'vitest';
import { landedAutosave } from '../admin_scoring_autosave.jsx';

async function flush() { for (let i = 0; i < 10; i++) await Promise.resolve(); }

function pendingAnswer() {
    let resolve, reject;
    const settled = new Promise((res, rej) => { resolve = res; reject = rej; });
    return { answer: { applied: true, pending: true, key: 'k', settled }, resolve, reject };
}

describe('landedAutosave on a pending answer', () => {
    it('calls onLanded only after `settled` resolves with a landed answer', async () => {
        const p = pendingAnswer();
        const onLanded = vi.fn();
        const onRefused = vi.fn();
        const submit = landedAutosave(() => Promise.resolve(p.answer), { current: 3 }, onLanded, onRefused);
        const got = submit({});
        await expect(got).resolves.toBe(p.answer);
        await flush();
        expect(onLanded).not.toHaveBeenCalled();
        p.resolve({ applied: true, status: 'running' });
        await flush();
        expect(onLanded).toHaveBeenCalledWith(3);
        expect(onRefused).not.toHaveBeenCalled();
    });

    it('calls onRefused when `settled` brings a superseded answer or a refusal', async () => {
        const held = pendingAnswer();
        const onLanded = vi.fn();
        const onRefused = vi.fn();
        landedAutosave(() => Promise.resolve(held.answer), { current: 1 }, onLanded, onRefused)({});
        held.resolve({ applied: false, reason: 'superseded' });
        await flush();
        expect(onRefused).toHaveBeenCalledWith(1);

        const refused = pendingAnswer();
        landedAutosave(() => Promise.resolve(refused.answer), { current: 2 }, onLanded, onRefused)({});
        refused.reject(new Error('court_busy'));
        await flush();
        expect(onRefused).toHaveBeenCalledWith(2);
        expect(onLanded).not.toHaveBeenCalled();
    });

    it('calls neither for a clock refusal or a discarded entry', async () => {
        const onLanded = vi.fn();
        const onRefused = vi.fn();
        const skew = pendingAnswer();
        landedAutosave(() => Promise.resolve(skew.answer), { current: 1 }, onLanded, onRefused)({});
        skew.resolve({ applied: false, reason: 'clock_skew' });
        const gone = pendingAnswer();
        landedAutosave(() => Promise.resolve(gone.answer), { current: 1 }, onLanded, onRefused)({});
        gone.resolve({ discarded: true });
        await flush();
        expect(onLanded).not.toHaveBeenCalled();
        expect(onRefused).not.toHaveBeenCalled();
    });
});
