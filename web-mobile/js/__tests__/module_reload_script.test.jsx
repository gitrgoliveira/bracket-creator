// bc-evts stage 3 review 4, G6 (pinned here by review 5, H7): the inline
// bcModuleFailed script in index.html, which every /dist/ module tag calls on
// error, reloads the page once per build directory per tab, never while the
// page knows it is offline, and never when sessionStorage refuses (a tab that
// cannot note the reload would loop). The Go test pins that the tags carry the
// attribute; this one runs the script itself, as index.html ships it.
import { describe, it, expect, vi } from 'vitest';
import fs from 'fs';
import path from 'path';

const INDEX = path.resolve(__dirname, '../../index.html');

function inlineScript() {
    const html = fs.readFileSync(INDEX, 'utf8');
    // The bare `<script>` tags index.html ships, found by a plain search (it is
    // this repo's own file, not markup to filter).
    const blocks = [];
    for (let at = html.indexOf('<script>'); at !== -1; at = html.indexOf('<script>', at)) {
        const end = html.indexOf('</script>', at);
        if (end === -1) break;
        blocks.push(html.slice(at + '<script>'.length, end));
        at = end;
    }
    const found = blocks.filter((b) => b.includes('window.bcModuleFailed'));
    expect(found, 'index.html defines bcModuleFailed in one inline script').toHaveLength(1);
    return found[0];
}

// Runs the script against a page of our own: its window (sessionStorage and
// location) and its navigator, so the reload is observable.
function page({ onLine = true, storage } = {}) {
    const store = new Map();
    const sessionStorage = storage || {
        getItem: (k) => (store.has(k) ? store.get(k) : null),
        setItem: (k, v) => { store.set(k, String(v)); },
    };
    const win = { sessionStorage, location: { reload: vi.fn() } };
    // eslint-disable-next-line no-new-func
    new Function('window', 'navigator', inlineScript())(win, { onLine });
    expect(typeof win.bcModuleFailed).toBe('function');
    return { win, store };
}

const module = (dir, file = 'api_client.js') => ({ src: `http://court.local/dist/${dir}/${file}` });

describe('bcModuleFailed (index.html)', () => {
    it('reloads once per build directory', () => {
        const { win, store } = page();
        win.bcModuleFailed(module('abc123'));
        expect(win.location.reload).toHaveBeenCalledTimes(1);
        // Another module of the same build failing after the reload: no loop.
        win.bcModuleFailed(module('abc123', 'data.jsx'));
        win.bcModuleFailed(module('abc123'));
        expect(win.location.reload).toHaveBeenCalledTimes(1);
        expect([...store.keys()]).toEqual(['bc_module_reload_for:http://court.local/dist/abc123/']);
        // The page a newer build serves names another directory: once more.
        win.bcModuleFailed(module('def456'));
        expect(win.location.reload).toHaveBeenCalledTimes(2);
    });

    it('does not reload, nor note a reload, while the page knows it is offline', () => {
        const { win, store } = page({ onLine: false });
        win.bcModuleFailed(module('abc123'));
        expect(win.location.reload).not.toHaveBeenCalled();
        expect(store.size).toBe(0);
    });

    it('does not reload when sessionStorage refuses, and throws nothing', () => {
        const refusing = {
            getItem: () => { throw new Error('SecurityError'); },
            setItem: () => { throw new Error('SecurityError'); },
        };
        const { win } = page({ storage: refusing });
        expect(() => win.bcModuleFailed(module('abc123'))).not.toThrow();
        expect(win.location.reload).not.toHaveBeenCalled();
    });

    it('does not reload when sessionStorage takes the read but refuses the note', () => {
        const full = { getItem: () => null, setItem: () => { throw new Error('QuotaExceededError'); } };
        const { win } = page({ storage: full });
        expect(() => win.bcModuleFailed(module('abc123'))).not.toThrow();
        expect(win.location.reload).not.toHaveBeenCalled();
    });

    it('does not reload when the window has no sessionStorage at all', () => {
        const win = { location: { reload: vi.fn() } };
        Object.defineProperty(win, 'sessionStorage', { get: () => { throw new Error('SecurityError'); } });
        // eslint-disable-next-line no-new-func
        new Function('window', 'navigator', inlineScript())(win, { onLine: true });
        expect(() => win.bcModuleFailed(module('abc123'))).not.toThrow();
        expect(win.location.reload).not.toHaveBeenCalled();
    });
});
