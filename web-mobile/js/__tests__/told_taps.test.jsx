// told_taps.jsx, the one owner of what a board told as not applied (stage 6
// review 9, F4): the device's record, shared by its tabs.
import { describe, it, expect } from 'vitest';
import { createToldTaps, toldTapsKey, TOLD_TAPS_KEEP_MS } from '../told_taps.jsx';

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
