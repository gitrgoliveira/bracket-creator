// told_taps.jsx, the one owner of what a board told as not applied (stage 6
// review 9, F4): the device's record, shared by its tabs.
import { describe, it, expect } from 'vitest';
import { createToldTaps, toldTapsKey, TOLD_TAPS_KEEP_MS, TOLD_TAPS_MAX } from '../told_taps.jsx';

function fakeStorage() {
    const data = {};
    let refuse = false;
    return {
        data,
        refuse(v) { refuse = v; },
        getItem: (k) => (k in data ? data[k] : null),
        setItem: (k, v) => { if (refuse) throw new Error('QuotaExceededError'); data[k] = String(v); },
        removeItem: (k) => { delete data[k]; },
        keys: () => Object.keys(data),
    };
}

describe('told taps', () => {
    it('is told once per device, and taken back once', () => {
        const storage = fakeStorage();
        const a = createToldTaps({ storage, tab: 'a' });
        const b = createToldTaps({ storage, tab: 'b' });
        expect(a.tell('c1', 'm1', 'x')).toBe(true);
        expect(b.tell('c1', 'm1', 'x'), 'another tab told it already').toBe(false);
        expect(b.told('c1', 'm1', 'x')).toBe(true);
        expect(b.takeBack('c1', 'm1', 'x')).toBe(true);
        expect(a.takeBack('c1', 'm1', 'x'), 'once').toBe(false);
        expect(a.told('c1', 'm1', 'x')).toBe(false);
    });

    it('tells the other tabs which taps were taken back', () => {
        const storage = fakeStorage();
        const a = createToldTaps({ storage, tab: 'a' });
        const b = createToldTaps({ storage, tab: 'b' });
        a.tell('c1', 'm1', 'x');
        a.tell('c1', 'm1', 'y');
        const key = toldTapsKey('c1', 'm1');
        const oldValue = storage.data[key];
        b.takeBack('c1', 'm1', 'y');
        expect(a.onStored(key, oldValue, storage.data[key])).toEqual([{ compID: 'c1', matchID: 'm1', id: 'y' }]);
        expect(a.onStored('other', null, '{}')).toEqual([]);
    });

    it('settles, prunes and forgets', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const a = createToldTaps({ storage, now: () => t, tab: 'a' });
        const b = createToldTaps({ storage, now: () => t, tab: 'b' });
        a.tell('c1', 'm1', 'x');
        a.tell('c1', 'm1', 'y');
        b.tell('c1', 'm1', 'z');
        a.settle('c1', 'm1', 'x');
        expect(a.told('c1', 'm1', 'x')).toBe(false);
        a.prune((id) => id === 'none');
        expect(a.told('c1', 'm1', 'y'), 'not queued here: pruned').toBe(false);
        expect(a.told('c1', 'm1', 'z'), 'another tab\'s stays').toBe(true);
        a.tell('c1', 'm2', 'w');
        a.forget();
        expect(a.told('c1', 'm2', 'w')).toBe(false);
        expect(b.told('c1', 'm1', 'z')).toBe(true);
        t += TOLD_TAPS_KEEP_MS;
        b.tell('c1', 'm1', 'v');
        expect(b.told('c1', 'm1', 'z'), 'kept a day at most').toBe(false);
    });

    it('keeps in memory what storage refuses', () => {
        const storage = fakeStorage();
        const a = createToldTaps({ storage, tab: 'a' });
        storage.refuse(true);
        expect(a.tell('c1', 'm1', 'x')).toBe(true);
        expect(a.tell('c1', 'm1', 'x')).toBe(false);
        expect(a.told('c1', 'm1', 'x')).toBe(true);
    });
});

// Stage 6 review 10, m1 and m7.
describe('told taps, the alert and the ceiling', () => {
    it('claims the applied-after-all alert once per device, and names the tab that told it', () => {
        const storage = fakeStorage();
        const a = createToldTaps({ storage, tab: 'a' });
        const b = createToldTaps({ storage, tab: 'b' });
        a.tell('c1', 'm1', 'x');
        expect(b.tellerOf('c1', 'm1', 'x')).toBe('a');
        expect(b.claimAlert('c1', 'm1', 'x'), 'not taken back yet').toBe(false);
        b.takeBack('c1', 'm1', 'x');
        expect(a.claimAlert('c1', 'm1', 'x')).toBe(true);
        expect(b.claimAlert('c1', 'm1', 'x'), 'once for the device').toBe(false);
    });

    it('tells another tab which entries went', () => {
        const storage = fakeStorage();
        const a = createToldTaps({ storage, tab: 'a' });
        const b = createToldTaps({ storage, tab: 'b' });
        a.tell('c1', 'm1', 'x');
        a.tell('c1', 'm1', 'y');
        const key = toldTapsKey('c1', 'm1');
        const oldValue = storage.data[key];
        b.settle('c1', 'm1', 'x');
        expect(a.goneOnStored(key, oldValue, storage.data[key])).toEqual([{ compID: 'c1', matchID: 'm1', id: 'x' }]);
    });

    it('plateaus: every record is swept by age, and the device keeps a fixed number in all', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const a = createToldTaps({ storage, now: () => t, tab: 'a' });
        const count = () => Object.keys(storage.data).reduce((n, k) => n + Object.keys(JSON.parse(storage.data[k])).length, 0);
        // A tap told and taken back on a new match every ten minutes for three
        // days: no match is ever written again.
        for (let i = 0; i < 3 * 24 * 6; i++) {
            a.tell('c1', `m${i}`, `x${i}`);
            a.takeBack('c1', `m${i}`, `x${i}`);
            t += 10 * 60 * 1000;
            a.beat();
        }
        expect(count(), 'only the last day is kept').toBeLessThanOrEqual(24 * 6 + 1);
        expect(Object.keys(storage.data).length).toBeLessThanOrEqual(24 * 6 + 1);
        // A burst within one minute is capped.
        for (let i = 0; i < TOLD_TAPS_MAX + 200; i++) a.tell('c2', 'm1', `y${i}`);
        a.sweep();
        expect(count()).toBeLessThanOrEqual(TOLD_TAPS_MAX);
    });
});