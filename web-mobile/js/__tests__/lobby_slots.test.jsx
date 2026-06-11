import { describe, it, expect } from 'vitest';
import { buildCourtSlots, LOBBY_ROWS } from '../display.jsx';

// Unit tests for buildCourtSlots — the slot-building logic that drives the
// cross-court table in LobbyDisplay (mp-1nf).
//
// Three scenarios:
//   1. Running match present → slot[0] = running, rest = upcoming queue
//   2. No running match → auto-promote first scheduled to slot[0] (kind: 'upnext')
//   3. Fewer than LOBBY_ROWS.length scheduled matches → tail slots are null

// Helper: build a minimal competition with poolMatches on a given court.
function makeComp(name, court, matches) {
    return {
        name,
        poolMatches: matches.map((m, i) => ({
            id: `m${i}`,
            court,
            status: m.status,
            sideA: m.sideA || { name: `Player A${i}` },
            sideB: m.sideB || { name: `Player B${i}` },
            queuePosition: m.queuePosition || i + 1,
            scheduledAt: m.scheduledAt || `09:${String(i * 8).padStart(2, '0')}`,
        })),
        bracket: { rounds: [] },
    };
}

describe('buildCourtSlots', () => {
    const TOTAL = LOBBY_ROWS.length; // expected slot array length

    it('returns LOBBY_ROWS.length slots', () => {
        const comps = [makeComp('Test', 'A', [
            { status: 'running' },
            { status: 'scheduled' },
        ])];
        const slots = buildCourtSlots(comps, 'A');
        expect(slots).toHaveLength(TOTAL);
    });

    it('running match present: slot[0] is running, remaining are scheduled', () => {
        const matches = [
            { status: 'running' },
            { status: 'scheduled' },
            { status: 'scheduled' },
            { status: 'scheduled' },
        ];
        const comps = [makeComp('Mens', 'A', matches)];
        const slots = buildCourtSlots(comps, 'A');

        expect(slots[0]).not.toBeNull();
        expect(slots[0].kind).toBe('running');
        expect(slots[1]).not.toBeNull();
        expect(slots[1].kind).toBe('scheduled');
        expect(slots[2]).not.toBeNull();
        expect(slots[2].kind).toBe('scheduled');
        expect(slots[3]).not.toBeNull();
        expect(slots[3].kind).toBe('scheduled');
        // Remaining slots should be null (only 3 upcoming, total = 6)
        for (let i = 4; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });

    it('no running match: auto-promotes first scheduled to slot[0] as upnext', () => {
        const matches = [
            { status: 'scheduled', sideA: { name: 'First A' }, sideB: { name: 'First B' } },
            { status: 'scheduled' },
            { status: 'scheduled' },
        ];
        const comps = [makeComp('Womens', 'B', matches)];
        const slots = buildCourtSlots(comps, 'B');

        expect(slots[0]).not.toBeNull();
        expect(slots[0].kind).toBe('upnext');
        expect(slots[0].match.sideA.name).toBe('First A');
        expect(slots[1]).not.toBeNull();
        expect(slots[1].kind).toBe('scheduled');
        expect(slots[2]).not.toBeNull();
        expect(slots[2].kind).toBe('scheduled');
        for (let i = 3; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });

    it('no running and no upcoming: all slots are null', () => {
        const comps = [makeComp('Empty', 'C', [])];
        const slots = buildCourtSlots(comps, 'C');

        for (let i = 0; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });

    it('fewer than TOTAL scheduled: tail slots are null (padding)', () => {
        const matches = [
            { status: 'running' },
            { status: 'scheduled' },
        ];
        const comps = [makeComp('Small', 'A', matches)];
        const slots = buildCourtSlots(comps, 'A');

        expect(slots[0].kind).toBe('running');
        expect(slots[1].kind).toBe('scheduled');
        for (let i = 2; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });

    it('wrong court returns all nulls', () => {
        const comps = [makeComp('Mens', 'A', [{ status: 'running' }])];
        const slots = buildCourtSlots(comps, 'Z');

        for (let i = 0; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });

    it('completed matches are excluded from slots', () => {
        const matches = [
            { status: 'completed' },
            { status: 'completed' },
            { status: 'scheduled' },
        ];
        const comps = [makeComp('Done', 'A', matches)];
        const slots = buildCourtSlots(comps, 'A');

        // No running → auto-promote. Only 1 scheduled match available.
        expect(slots[0]).not.toBeNull();
        expect(slots[0].kind).toBe('upnext');
        for (let i = 1; i < TOTAL; i++) {
            expect(slots[i]).toBeNull();
        }
    });
});
