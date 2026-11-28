// bc-drev: the operator's agreement to start a competition (first_result_confirm.jsx)
// was given for the draw as it stood. A draw generated again, swapped or discarded
// on another device is announced by a draw_generated or draw_discarded push, and
// that push makes the next first result ask again, on a surface that never lists
// the competition in setup (the court console).
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let API;
let confirmMod;
let es;
let _orig;

beforeEach(async () => {
    _orig = { fetch: global.fetch, ES: global.EventSource };
    global.EventSource = class FakeES { constructor() { es = this; this.close = () => {}; } };
    global.EventSource.OPEN = 1;
    global.fetch = vi.fn(() => Promise.reject(new TypeError('offline')));
    vi.resetModules();
    API = (await import('../api_client.jsx')).API;
    confirmMod = await import('../first_result_confirm.jsx');
});

afterEach(() => {
    vi.resetModules();
    global.fetch = _orig.fetch;
    global.EventSource = _orig.ES;
});

const drawn = (id) => ({
    id, name: `Comp ${id}`, status: 'draw-ready', bracket: null,
    poolMatches: [{ id: 'Pool A-0', status: 'scheduled', sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' } }],
});

async function agreeFor(id) {
    await confirmMod.confirmFirstResult({ confirmDialog: vi.fn().mockResolvedValue(true), compId: id });
}

const push = (type, competitionId) => es.onmessage({ data: JSON.stringify({ type, data: { competitionId } }), lastEventId: '' });

describe('an agreement to start the competition', () => {
    it.each(['draw_generated', 'draw_discarded'])('is forgotten for that competition on a %s push', async (type) => {
        const off = API.subscribeToEvents(() => {});
        confirmMod.noteFirstResultCompetitions([drawn('c1'), drawn('c2')]);
        await agreeFor('c1');
        await agreeFor('c2');
        expect(confirmMod.firstResultAsks('c1')).toBe(false);

        push(type, 'c1');

        expect(confirmMod.firstResultAsks('c1')).toBe(true);
        expect(confirmMod.firstResultAsks('c2'), 'another competition keeps its agreement').toBe(false);
        off();
    });
});
