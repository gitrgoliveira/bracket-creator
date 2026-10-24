// landed_taps.jsx and told_taps.jsx (stage 6 review 11, B): the tab that
// noted a tap tells it only while its page is shown, or when no tab is
// shown; otherwise the shown tab with the lowest id does. A hidden owner tab
// (a background desktop tab still gets the push) never takes the notice from
// the tab the operator looks at. And a claim is a marker written and read
// back before telling, so a tab whose claim another tab's landed over does
// not tell as well.
import { describe, it, expect } from 'vitest';
import { createLandedTaps, landedTapsKey } from '../landed_taps.jsx';
import { createToldTaps, toldTapsKey } from '../told_taps.jsx';

function fakeStorage() {
    const data = {};
    let afterSet = null;
    // Runs once a write of the record has landed (a removal too): the other
    // tab's step that lands before this tab reads the record back.
    const after = (k) => {
        if (!afterSet) return;
        const fn = afterSet;
        afterSet = null;
        if (!fn(k)) afterSet = fn;
    };
    return {
        data,
        afterSet(fn) { afterSet = fn; },
        getItem: (k) => (k in data ? data[k] : null),
        setItem: (k, v) => { data[k] = String(v); after(k); },
        removeItem: (k) => { delete data[k]; after(k); },
        keys: () => Object.keys(data),
    };
}

// A view of `storage` whose reads of `key` answer `snapshot` once: a tab
// whose copy of the record is behind (it read before the other tab wrote).
function staleOnce(storage, key, snapshot) {
    let stale = true;
    return {
        ...storage,
        getItem: (k) => {
            if (k === key && stale) { stale = false; return snapshot; }
            return storage.getItem(k);
        },
        setItem: (k, v) => storage.setItem(k, v),
        removeItem: (k) => storage.removeItem(k),
        keys: () => storage.keys(),
    };
}

const rec = (id) => ({ id, where: 'Pool A · Match 1', kind: 'ippon.add', side: 'a', pos: 1, letter: 'M' });

describe('who tells a landed tap', () => {
    it('a hidden owner leaves its tap to the shown tab', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const a = createLandedTaps({ storage, now, tab: 'a', hidden: false });
        const b = createLandedTaps({ storage, now, tab: 'b', hidden: false });
        b.note('c1', 'm1', [rec('x')]);
        // b goes to the background but still hears the push.
        b.beat({ hidden: true });
        t += 500;
        a.beat({ hidden: false });
        expect(b.tellsFor('b'), 'the hidden owner does not tell').toBe(false);
        expect(a.tellsFor('b'), 'the shown tab tells for it').toBe(true);
        expect(b.claim('c1', 'm1', ['x']), 'the hidden owner claims nothing').toEqual([]);
        expect(a.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    it('a hidden owner tells its own tap when no tab is shown', () => {
        const storage = fakeStorage();
        let t = 1_000;
        const now = () => t;
        const a = createLandedTaps({ storage, now, tab: 'a', hidden: true });
        const b = createLandedTaps({ storage, now, tab: 'b', hidden: true });
        b.note('c1', 'm1', [rec('x')]);
        t += 500;
        a.beat({ hidden: true });
        b.beat({ hidden: true });
        expect(a.claim('c1', 'm1', ['x'])).toEqual([]);
        expect(b.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    it('the owner shown tells its own tap, never another shown tab', () => {
        const storage = fakeStorage();
        const now = () => 1_000;
        const a = createLandedTaps({ storage, now, tab: 'a', hidden: false });
        const b = createLandedTaps({ storage, now, tab: 'b', hidden: false });
        b.note('c1', 'm1', [rec('x')]);
        a.beat();
        b.beat();
        expect(a.claim('c1', 'm1', ['x'])).toEqual([]);
        expect(b.claim('c1', 'm1', ['x']).map((r) => r.id)).toEqual(['x']);
    });

    it('two shown tabs racing a claim tell the tap once', () => {
        const storage = fakeStorage();
        const now = () => 1_000;
        const key = landedTapsKey('c1', 'm1');
        const a = createLandedTaps({ storage, now, tab: 'a', hidden: false });
        const owner = createLandedTaps({ storage, now, tab: 'b', hidden: false });
        owner.note('c1', 'm1', [rec('x')]);
        owner.leave();
        a.beat();
        // Tab c read the record before a wrote it, and its view of the tabs
        // misses a (a heartbeat it has not read yet): it too takes itself for
        // the teller. Its whole claim lands between a's write and a's read back.
        const snapshot = storage.data[key];
        const tabsKey = 'bc_landed_tabs_v1';
        let stale = false;
        const cView = {
            ...storage,
            getItem: (k) => {
                if (k === tabsKey) {
                    const tabs = JSON.parse(storage.getItem(k) || '{}');
                    delete tabs.a;
                    return JSON.stringify(tabs);
                }
                if (k === key && stale) { stale = false; return snapshot; }
                return storage.getItem(k);
            },
        };
        const c = createLandedTaps({ storage: cView, now, tab: 'c', hidden: false });
        const told = [];
        storage.afterSet((k) => {
            if (k !== key) return false;
            stale = true;
            for (const r of c.claim('c1', 'm1', ['x'])) told.push(['c', r.id]);
            return true;
        });
        for (const r of a.claim('c1', 'm1', ['x'])) told.push(['a', r.id]);
        expect(told.length, `told once, not ${JSON.stringify(told)}`).toBe(1);
    });
});

describe('who says a tap was applied after all', () => {
    it('two tabs racing the alert claim raise it once', () => {
        const storage = fakeStorage();
        const now = () => 1_000;
        const key = toldTapsKey('c1', 'm1');
        const told = createToldTaps({ storage, now, tab: 'b' });
        told.tell('c1', 'm1', 'x');
        told.takeBack('c1', 'm1', 'x');
        const snapshot = storage.data[key];
        const raised = [];
        storage.afterSet((k) => {
            if (k !== key) return false;
            const c = createToldTaps({ storage: staleOnce(storage, key, snapshot), now, tab: 'c' });
            if (c.claimAlert('c1', 'm1', 'x')) raised.push('c');
            return true;
        });
        const a = createToldTaps({ storage, now, tab: 'a' });
        if (a.claimAlert('c1', 'm1', 'x')) raised.push('a');
        expect(raised.length, `raised once, not ${JSON.stringify(raised)}`).toBe(1);
    });
});
