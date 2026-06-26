import { describe, it, expect } from 'vitest';
import { buildCourtSlots, LOBBY_ROWS, LobbyMatchCell, LOBBY_COLORS } from '../display.jsx';
import { IndividualScore } from '../match_scoreboard.jsx';

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

// ── Depth-first vnode walker (same pattern as display_completed_state.test.jsx) ──
function findAll(node, pred, out = []) {
    if (!node || typeof node !== 'object') return out;
    if (Array.isArray(node)) { node.forEach(k => findAll(k, pred, out)); return out; }
    if (pred(node)) out.push(node);
    const kids = node.children || node.props?.children || [];
    [].concat(kids).forEach(k => findAll(k, pred, out));
    return out;
}
function treeStr(node) { return JSON.stringify(node); }

// Minimal slot factories for LobbyMatchCell rendering tests.
function makeRunningSlot(overrides = {}) {
    return {
        kind: 'running',
        match: {
            id: 'r1',
            sideA: { name: 'Aka Fighter' },
            sideB: { name: 'Shiro Fighter' },
            ipponsA: ['M'],
            ipponsB: [],
            hansokuA: 0,
            hansokuB: 0,
            ...overrides.match,
        },
        competition: { id: 'c1', name: 'Open', withZekkenName: false },
        isBracket: false,
        roundIndex: 0,
        totalRounds: 1,
        ...overrides,
    };
}

function makeScheduledSlot() {
    return {
        kind: 'scheduled',
        match: {
            id: 's1',
            sideA: { name: 'Aka Next' },
            sideB: { name: 'Shiro Next' },
            ipponsA: [],
            ipponsB: [],
            hansokuA: 0,
            hansokuB: 0,
        },
        competition: { id: 'c1', name: 'Open', withZekkenName: false },
        isBracket: false,
        roundIndex: 0,
        totalRounds: 1,
    };
}

// ── LOBBY_COLORS — no amber on next row ──────────────────────────────────────
describe('LOBBY_COLORS — amber removed from next row (mp-ulh9)', () => {
    it('the NEXT row background (schedBg) does not use the amber hex #fef3c7', () => {
        // NEXT shares the quiet queue background; its only distinct cue is the border.
        expect(LOBBY_COLORS.nextBg).toBeUndefined();
        expect(LOBBY_COLORS.schedBg).not.toContain('#fef3c7');
    });

    it('nextBorder does not use the amber-derived rgba(180,83,9', () => {
        expect(LOBBY_COLORS.nextBorder).not.toContain('180,83,9');
    });

    it('nowBg references the navy accent-soft token', () => {
        // Must reference --accent-soft or the navy fallback #e7eaf3.
        const val = LOBBY_COLORS.nowBg;
        expect(val.includes('--accent-soft') || val.includes('#e7eaf3') || val.includes('accent')).toBe(true);
    });

    it('nowBorder references the navy accent token', () => {
        const val = LOBBY_COLORS.nowBorder;
        expect(val.includes('--accent') || val.includes('#1d3557') || val.includes('accent')).toBe(true);
    });
});

// ── LobbyMatchCell — NOW navy treatment (mp-ulh9) ────────────────────────────
// The row-label column already prints "Now" / "Next" / "#3"…, so no inline
// NOW dot/label is rendered in the cell itself; the navy bg+border carries
// the live signal.
describe('LobbyMatchCell — NOW row uses navy treatment, NEXT does not (mp-ulh9)', () => {
    it('NOW cell uses LOBBY_COLORS.nowBg (navy) and LOBBY_COLORS.nowBorder', () => {
        const tree = LobbyMatchCell({ slot: makeRunningSlot(), rowKind: 'now' });
        const str = treeStr(tree);
        // navy-soft bg via --accent-soft (or its fallback hex)
        expect(/--accent-soft|#e7eaf3/.test(str)).toBe(true);
        // navy border via --accent (or its fallback hex)
        expect(/--accent[^-]|#1d3557/.test(str)).toBe(true);
    });

    it('NEXT cell does NOT use the navy bg or border', () => {
        const tree = LobbyMatchCell({ slot: makeScheduledSlot(), rowKind: 'next' });
        const str = treeStr(tree);
        expect(str).not.toContain('--accent-soft');
        expect(str).not.toContain('#e7eaf3');
        expect(str).not.toContain('#1d3557');
    });
});

// ── visibleRows trimming — empty queue rows omitted ─────────────────────────
// When all slots >= 2 are null across all visible courts, only the Now and
// Next anchor rows should appear (slot < 2). Rows with slot >= 2 are included
// only when at least one court has a non-null entry at that index.
describe('visibleRows trimming — slot >= 2 rows omitted when all courts have null there', () => {
    // Replicate the filter logic from LobbyDisplay so we can unit-test it without
    // rendering the full component (consistent with the buildCourtSlots test pattern).
    function computeVisibleRows(courtSlots) {
        return LOBBY_ROWS.filter(row =>
            row.slot < 2 || courtSlots.some(slots => slots[row.slot] != null)
        );
    }

    it('only Now and Next rows render when all slot >= 2 entries are null', () => {
        // Two courts, each with only slot[0] filled (running) — slots 2–5 null.
        const compA = [makeComp('Open', 'A', [{ status: 'running' }])];
        const compB = [makeComp('Open', 'B', [{ status: 'running' }])];
        const courtSlots = [
            buildCourtSlots(compA, 'A'),
            buildCourtSlots(compB, 'B'),
        ];
        const rows = computeVisibleRows(courtSlots);
        expect(rows).toHaveLength(2);
        expect(rows[0].slot).toBe(0);
        expect(rows[1].slot).toBe(1);
    });

    it('includes a slot >= 2 row when at least one court has a non-null entry there', () => {
        // Court A: running + 3 scheduled → slots 0–3 filled, 4–5 null.
        // Court B: running only → slots 1–5 null.
        const compA = [makeComp('Open', 'A', [
            { status: 'running' },
            { status: 'scheduled' },
            { status: 'scheduled' },
            { status: 'scheduled' },
        ])];
        const compB = [makeComp('Open', 'B', [{ status: 'running' }])];
        const courtSlots = [
            buildCourtSlots(compA, 'A'),
            buildCourtSlots(compB, 'B'),
        ];
        const rows = computeVisibleRows(courtSlots);
        // Slots 0, 1, 2, 3 should be present; 4 and 5 are null on both courts.
        expect(rows.map(r => r.slot)).toEqual([0, 1, 2, 3]);
    });

    it('always includes Now and Next even when all courts are completely empty', () => {
        const courtSlots = [
            buildCourtSlots([makeComp('Empty', 'A', [])], 'A'),
            buildCourtSlots([makeComp('Empty', 'B', [])], 'B'),
        ];
        const rows = computeVisibleRows(courtSlots);
        expect(rows).toHaveLength(2);
        expect(rows[0].label).toBe('Now');
        expect(rows[1].label).toBe('Next');
    });
});

// ── LobbyMatchCell — hansoku foul marks (mp-0ky7) ───────────────────────────
// ── LobbyMatchCell — delegates the matchup body to IndividualScore ────────────
// The cell renders one IndividualScore row (same shared component the
// per-court board and viewer card use). Attribution is positional — Shiro
// ippons next to Shiro name, Aka ippons next to Aka name, hansoku ▲ on the
// outer edge of the offending side — instead of a centred dash-separated
// numeric string. No more hand-rolled lobby-only score rendering, no "0"
// placeholder for empty ippons (kendo isn't numeric), no separate
// lobby-foul-mark testids.
describe('LobbyMatchCell — delegates body to IndividualScore (mp-0ky7 / score reuse)', () => {
    const findIndiv = tree => findAll(tree, n => n?.type === IndividualScore);

    it('renders exactly one IndividualScore vnode for a running slot', () => {
        const slot = makeRunningSlot({ match: { ipponsA: ['M'], ipponsB: [], hansokuA: 0, hansokuB: 1 } });
        const tree = LobbyMatchCell({ slot, rowKind: 'now' });
        expect(findIndiv(tree).length).toBe(1);
    });

    it('renders an IndividualScore vnode for a scheduled slot too (no separate "vs" branch in the cell)', () => {
        const tree = LobbyMatchCell({ slot: makeScheduledSlot(), rowKind: 'next' });
        expect(findIndiv(tree).length).toBe(1);
    });

    it('passes match.ipponsA / .ipponsB through; no hand-rolled "0" placeholder in the tree', () => {
        const slot = makeRunningSlot({ match: { ipponsA: ['M'], ipponsB: [] } });
        const tree = LobbyMatchCell({ slot, rowKind: 'now' });
        const indiv = findIndiv(tree)[0];
        expect(indiv.props.match.ipponsA).toEqual(['M']);
        expect(indiv.props.match.ipponsB).toEqual([]);
        // No literal "0" string-child anywhere in the cell (the prior
        // "0 - M" fallback is gone). Whitelist the empty-array
        // serialisation "[]" and any numeric "0" inside style values.
        const str = treeStr(tree);
        expect(str).not.toMatch(/,"0"|>"0"/);
    });

    it('does NOT render the dropped lobby-only foul testids; hansoku is owned by IndividualScore', () => {
        const slot = makeRunningSlot({ match: { hansokuB: 1, hansokuA: 0, ipponsA: [], ipponsB: [] } });
        const tree = LobbyMatchCell({ slot, rowKind: 'now' });
        const str = treeStr(tree);
        // The lobby-prefixed testids from the removed hand-rolled path must
        // not appear anywhere. The canonical foul-mark-b/-a testids ship
        // inside IndividualScore's rendered output (covered by
        // match_scoreboard.test.jsx) and aren't visible here because the
        // component vnode isn't expanded by these snapshot-style tests.
        expect(str).not.toContain('lobby-foul-mark-');
        // The hansoku count IS forwarded through the match prop so the
        // shared component can render the ▲ on the offending side.
        const indiv = findIndiv(tree)[0];
        expect(indiv.props.match.hansokuB).toBe(1);
        expect(indiv.props.match.hansokuA).toBe(0);
    });

    it('passes withZekkenName from the competition through to IndividualScore', () => {
        const slot = makeRunningSlot({ competition: { id: 'c1', name: 'Open', withZekkenName: true } });
        const tree = LobbyMatchCell({ slot, rowKind: 'now' });
        const indiv = findIndiv(tree)[0];
        expect(indiv.props.withZekkenName).toBe(true);
    });

    it('renders showNames so the IndividualScore prints competitor names (lobby cells have no separate name row)', () => {
        const slot = makeRunningSlot();
        const tree = LobbyMatchCell({ slot, rowKind: 'now' });
        const indiv = findIndiv(tree)[0];
        expect(indiv.props.showNames).toBe(true);
    });

    it('comp-meta span is shrinkable (flex:1 + minWidth:0) so long text ellipsizes beside the decision suffix', () => {
        const tree = LobbyMatchCell({ slot: makeRunningSlot(), rowKind: 'now' });
        const meta = findAll(tree, n => n?.type === 'span' && n.props?.style?.textOverflow === 'ellipsis')[0];
        expect(meta).toBeTruthy();
        expect(meta.props.style.flex).toBe(1);
        expect(meta.props.style.minWidth).toBe(0);
    });
});
