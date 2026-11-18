// The outbox modules share ONE store (api_client_store.jsx) and import no module
// above them: their static import graph has no cycle, a call a lower module
// makes into a higher one goes through `late`, which every owner provides at
// load, and no module grows past the size the split was made for.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readdirSync, readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const JS_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const STORE = path.join(JS_DIR, 'api_client_store.jsx');
const OUTBOX = readdirSync(JS_DIR).filter((f) => /^api_client(_\w+)?\.jsx$/.test(f)).map((f) => f.replace(/\.jsx$/, ''));
const importsOf = (mod) => {
    const src = readFileSync(path.join(JS_DIR, `${mod}.jsx`), 'utf8');
    return [...src.matchAll(/^(?:import|export)\b[^;]*?\bfrom\s*'\.\/(api_client(?:_\w+)?)\.jsx'/gms)].map((m) => m[1]);
};

describe('the outbox modules and their store', () => {
    it('import one another in no cycle, and the store imports only leaves', () => {
        const graph = Object.fromEntries(OUTBOX.map((m) => [m, importsOf(m)]));
        for (const dep of graph.api_client_store) expect(graph[dep], `the store imports ${dep}, which imports more`).toEqual([]);
        const state = {};
        const path_ = [];
        const visit = (m) => {
            if (state[m] === 'done') return;
            expect(state[m], `an import cycle through ${[...path_, m].join(' -> ')}`).not.toBe('open');
            state[m] = 'open';
            path_.push(m);
            for (const next of graph[m]) visit(next);
            path_.pop();
            state[m] = 'done';
        };
        for (const m of OUTBOX) visit(m);
    });

    it('keeps every module under about 2,500 lines', () => {
        for (const m of OUTBOX) {
            const lines = readFileSync(path.join(JS_DIR, `${m}.jsx`), 'utf8').split('\n').length;
            expect(lines, `${m}.jsx has ${lines} lines`).toBeLessThanOrEqual(2500);
        }
    });
});

describe('late and provide', () => {
    beforeEach(() => { vi.resetModules(); });

    it('throws naming the call when a module that provides it has not loaded', async () => {
        const { late } = await import(/* @vite-ignore */ `${STORE}?alone`);
        expect(() => late._rerunFlushSoon()).toThrow(/late\._rerunFlushSoon was called before the module that provides it loaded/);
    });

    it('refuses a name the store does not list', async () => {
        const { provide, late } = await import(/* @vite-ignore */ `${STORE}?typo`);
        expect(() => provide({ _rerunFlushSon: () => {} })).toThrow(/not a listed call/);
        const fn = () => 'ran';
        provide({ _rerunFlushSoon: fn });
        expect(late._rerunFlushSoon()).toBe('ran');
    });

    it('is fully provided once the outbox has loaded', async () => {
        await import('../api_client.jsx');
        const { late } = await import('../api_client_store.jsx');
        for (const [name, fn] of Object.entries(late)) {
            expect(String(fn), `late.${name} was never provided`).not.toContain('was called before the module that provides it loaded');
        }
    });
});
