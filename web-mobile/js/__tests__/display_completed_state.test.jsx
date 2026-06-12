import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { TvDisplay } from '../display.jsx';

// mp-s99q: TvDisplay empty-state redesign tests.
// Covers the three empty-state sub-states, the IN PROGRESS wayfinding strip,
// and the "Scan for results" QR affordance.

// Stub window.renderQR so StreamingQR doesn't throw in the test env.
beforeEach(() => {
    if (typeof window !== 'undefined') {
        window.renderQR = () => {};
    }
});
afterEach(() => {
    if (typeof window !== 'undefined') {
        delete window.renderQR;
    }
});

// Depth-first vnode walker — same pattern as display_white_board.test.jsx.
function findAll(node, pred, out = []) {
    if (!node || typeof node !== 'object') return out;
    if (Array.isArray(node)) { node.forEach(k => findAll(k, pred, out)); return out; }
    if (pred(node)) out.push(node);
    const kids = node.children || node.props?.children || [];
    [].concat(kids).forEach(k => findAll(k, pred, out));
    return out;
}

function findFirst(node, pred) {
    const all = findAll(node, pred);
    return all.length > 0 ? all[0] : null;
}

// Walk the entire rendered tree to a flat JSON string for quick substring checks.
function treeStr(node) { return JSON.stringify(node); }

// Build a minimal tournament with declared courts.
function makeTournament(courts = ['A']) {
    return { name: 'Test Cup', courts };
}

// Build a competition with pool matches on the given court/status pairs.
function makeComp(id, matches) {
    return {
        id,
        name: `Comp ${id}`,
        kind: 'individual',
        teamSize: 0,
        poolMatches: matches,
        bracket: null,
    };
}

function makeMatch(id, court, status) {
    return { id, court, status, sideA: { name: `PlayerA-${id}` }, sideB: { name: `PlayerB-${id}` } };
}

// ─── allCompleted sub-state ───────────────────────────────────────────────────

describe('TvDisplay empty state — allCompleted', () => {
    it('shows "All matches completed" headline without "on Shiaijo A" suffix', () => {
        const comps = [makeComp('c1', [
            makeMatch('m1', 'A', 'completed'),
            makeMatch('m2', 'A', 'completed'),
        ])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('All matches completed');
        // Must NOT contain the old "on Shiaijo A" suffix.
        expect(str).not.toContain('on Shiaijo');
        // data-testid must be present on the headline container.
        const el = findFirst(tree, n => n?.props?.['data-testid'] === 'display-all-completed');
        expect(el).toBeTruthy();
    });

    it('renders a drawn SVG checkmark (polyline), NOT the raw ✓ Unicode glyph', () => {
        const comps = [makeComp('c1', [makeMatch('m1', 'A', 'completed')])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        // The SVG polyline must be present.
        expect(str).toContain('polyline');
        // The raw Unicode check glyph must NOT be present.
        expect(str).not.toContain('✓');
    });

    it('uses var(--ink-1) for headline color — not the old #9ca3af', () => {
        const comps = [makeComp('c1', [makeMatch('m1', 'A', 'completed')])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        // Old low-contrast gray must be absent from the headline.
        expect(str).not.toContain('"color":"#9ca3af"');
        // The ink-1 token must be used somewhere in the headline area.
        expect(str).toContain('--ink-1');
    });
});

// ─── Active-courts wayfinding strip ──────────────────────────────────────────

describe('TvDisplay empty state — IN PROGRESS wayfinding strip', () => {
    it('renders tvd-active-courts with chips for B and C when court A is completed and B/C have active matches', () => {
        const tournament = makeTournament(['A', 'B', 'C']);
        const comps = [
            makeComp('c1', [
                makeMatch('a1', 'A', 'completed'),
                makeMatch('b1', 'B', 'running'),
                makeMatch('c1', 'C', 'scheduled'),
            ]),
        ];
        // Need both sides to be real names so bracketSidesReady passes for B and C.
        const tree = TvDisplay({ court: 'A', tournament, competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('"data-testid":"tvd-active-courts"');
        // Chips for B and C must appear.
        expect(str).toContain('"data-court":"B"');
        expect(str).toContain('"data-court":"C"');
        // Current court A must NOT appear as a chip.
        expect(str).not.toContain('"data-court":"A"');
    });

    it('omits tvd-active-courts strip when no other courts have active matches', () => {
        const tournament = makeTournament(['A']);
        const comps = [makeComp('c1', [makeMatch('a1', 'A', 'completed')])];
        const tree = TvDisplay({ court: 'A', tournament, competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).not.toContain('"data-testid":"tvd-active-courts"');
    });

    it('omits tvd-active-courts strip when there are no other competitions', () => {
        const tournament = makeTournament(['A', 'B']);
        // Court B matches are all completed too.
        const comps = [makeComp('c1', [
            makeMatch('a1', 'A', 'completed'),
            makeMatch('b1', 'B', 'completed'),
        ])];
        const tree = TvDisplay({ court: 'A', tournament, competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).not.toContain('"data-testid":"tvd-active-courts"');
    });

    it('uses "IN PROGRESS" wording — never "LIVE" or "live"', () => {
        const tournament = makeTournament(['A', 'B']);
        const comps = [makeComp('c1', [
            makeMatch('a1', 'A', 'completed'),
            makeMatch('b1', 'B', 'running'),
        ])];
        const tree = TvDisplay({ court: 'A', tournament, competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('IN PROGRESS');
        expect(str).not.toContain('LIVE');
        expect(str).not.toContain('"live"');
    });
});

// ─── QR affordance ────────────────────────────────────────────────────────────

describe('TvDisplay empty state — "Scan for results" QR affordance', () => {
    it('renders "Scan for results" label in the empty state', () => {
        const comps = [makeComp('c1', [makeMatch('m1', 'A', 'completed')])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('Scan for results');
    });

    it('renders "Scan for results" in the no-matches sub-state too', () => {
        // No matches at all on court A.
        const comps = [makeComp('c1', [])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('Scan for results');
        const el = findFirst(tree, n => n?.props?.['data-testid'] === 'display-no-matches');
        expect(el).toBeTruthy();
    });

    it('renders "Scan for results" in the no-matches-at-all sub-state (empty comp)', () => {
        // No matches anywhere on court A — cleanest path through the noMatches branch.
        const comps = [makeComp('c1', [])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        const str = treeStr(tree);
        expect(str).toContain('Scan for results');
        expect(str).toContain('No matches scheduled');
    });
});

// ─── Headline text verification for all three sub-states ──────────────────────

describe('TvDisplay empty state — headline text per sub-state', () => {
    it('allCompleted: exact headline text (no court suffix)', () => {
        const comps = [makeComp('c1', [makeMatch('m1', 'A', 'completed')])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        expect(treeStr(tree)).toContain('All matches completed');
        expect(treeStr(tree)).not.toContain('on Shiaijo');
    });

    it('noMatches: exact headline text', () => {
        const comps = [makeComp('c1', [])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        expect(treeStr(tree)).toContain('No matches scheduled');
    });

    it('noMatches: headline "No matches scheduled" has no court suffix', () => {
        const comps = [makeComp('c1', [])];
        const tree = TvDisplay({ court: 'A', tournament: makeTournament(['A']), competitions: comps, connected: true });
        expect(treeStr(tree)).toContain('No matches scheduled');
        expect(treeStr(tree)).not.toContain('on Shiaijo');
    });
});
