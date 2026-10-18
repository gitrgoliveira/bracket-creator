// landed_taps.jsx (stage 6 review 9, F6): a tab in the background, whose
// timers the browser slows or stops, is still alive: a sign-out in another
// tab never takes its entries, and no other tab tells its taps, while it is
// hidden and within LANDED_TAB_HIDDEN_LIVE_MS of its last beat. And a sweep
// never writes back a match record over an entry another tab noted while the
// sweep was reading.
import { describe, it, expect } from 'vitest';
import {
    createLandedTaps, landedTapsKey, LANDED_TAB_LIVE_MS, LANDED_TAB_HIDDEN_LIVE_MS, LANDED_TAPS_KEEP_MS,
} from '../landed_taps.jsx';

function fakeStorage() {
    const data = {};
    let afterGet = null;
    return {
        data,
        afterGet(fn) { afterGet = fn; },
        getItem: (k) => {
            const v = k in data ? data[k] : null;
            if (afterGet) {
                const fn = afterGet;
                if (fn(k)) afterGet = null;
            }
            return v;
        },
        setItem: (k, v) => { data[k] = String(v); },
        removeItem: (k) => { delete data[k]; },
        keys: () => Object.keys(data),
    };
}

const rec = (id) => ({ id, where: 'Pool A · Match 1', kind: 'ippon.add', side: 'a', pos: 1, letter: 'M' });

describe('a tab in the background', () => {
    it('keeps its taps through another tab\'s sign-out', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        b.note('c1', 'm1', [rec('x'), rec('y')]);
        // b goes to the background: its timers stop.
        b.beat({ hidden: true });
        t += LANDED_TAB_LIVE_MS * 3;
        a.beat();
        a.forget();
        expect(b.has('c1', 'm1', 'y'), 'a sign-out in a leaves b\'s taps').toBe(true);
        // b comes back and tells its own.
        b.beat({ hidden: false });
        expect(b.claim('c1', 'm1', ['y']).map((r) => r.id)).toEqual(['y']);
    });

    // Stage 6 review 10, M1: hiddenness protects a tab's record from a sign-out
    // and the sweep, never its telling. Tap in tab b, switch to tab a (b hidden,
    // its timers stopped), another device's Finish turns the tap away: a tells
    // it at once, and b never tells it again.
    it('has its taps told by a visible tab, once', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        b.note('c1', 'm1', [rec('x')]);
        b.beat({ hidden: true });
        t += 1_000;
        a.beat({ hidden: false });
        expect(a.claim('c1', 'm1', ['x']).map((r) => r.id), 'the visible tab tells the hidden tab\'s tap').toEqual(['x']);
        b.beat({ hidden: false });
        expect(b.claim('c1', 'm1', ['x']), 'told once per device').toEqual([]);
    });

    it('leaves a gone tab\'s taps to the visible tab, not to a hidden one', () => {
        const storage = fakeStorage();
        const now = () => 1_000;
        const z = createLandedTaps({ storage, now, tab: 'z' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        const b = createLandedTaps({ storage, now, tab: 'b' });
        z.note('c1', 'm1', [rec('x')]);
        z.leave();
        a.beat({ hidden: true });
        b.beat({ hidden: false });
        expect(a.claim('c1', 'm1', ['x']), 'a hidden tab leaves it to the visible one').toEqual([]);
        expect(b.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    it('a live visible tab keeps telling its own taps', () => {
        const storage = fakeStorage();
        const now = () => 1_000;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        b.note('c1', 'm1', [rec('x')]);
        a.beat({ hidden: false });
        expect(a.claim('c1', 'm1', ['x']), 'b is visible and alive: its own').toEqual([]);
        expect(b.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    // m6: a tab opened in the background beats as hidden from its first beat.
    it('starts hidden when its page is hidden as it is made', () => {
        const storage = fakeStorage();
        const desc = Object.getOwnPropertyDescriptor(document, 'visibilityState');
        Object.defineProperty(document, 'visibilityState', { configurable: true, get: () => 'hidden' });
        try {
            createLandedTaps({ storage, now: () => 1_000, tab: 'b' });
        } finally {
            if (desc) Object.defineProperty(document, 'visibilityState', desc);
            else delete document.visibilityState;
        }
        const tabs = JSON.parse(storage.data.bc_landed_tabs_v1);
        expect(tabs.b).toEqual({ at: 1_000, hidden: true });
    });

    it('is taken as gone once hidden past LANDED_TAB_HIDDEN_LIVE_MS', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        b.note('c1', 'm1', [rec('x')]);
        b.beat({ hidden: true });
        t += LANDED_TAB_HIDDEN_LIVE_MS + 1;
        a.beat();
        expect(a.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    it('a visible tab whose beat stopped is still gone after LANDED_TAB_LIVE_MS', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const b = createLandedTaps({ storage, now, tab: 'b' });
        const a = createLandedTaps({ storage, now, tab: 'a' });
        b.note('c1', 'm1', [rec('x')]);
        t += LANDED_TAB_LIVE_MS + 1;
        a.beat();
        expect(a.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });
});

describe('the sweep', () => {
    it('keeps an entry another tab noted while it was reading', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const a = createLandedTaps({ storage, now, tab: 'a' });
        a.note('c1', 'm1', [rec('old')]);
        t += LANDED_TAPS_KEEP_MS + 1;
        const key = landedTapsKey('c1', 'm1');
        // Another tab notes a tap of the match just after the sweep read it.
        storage.afterGet((k) => {
            if (k !== key) return false;
            const obj = JSON.parse(storage.data[key]);
            obj.fresh = { ...rec('fresh'), at: t, tab: 'b' };
            delete obj.fresh.id;
            storage.data[key] = JSON.stringify(obj);
            return true;
        });
        a.sweep();
        const after = JSON.parse(storage.data[key] || '{}');
        expect(Object.keys(after), 'the old entry swept, the new one kept').toEqual(['fresh']);
    });
});
