// Tests for the pure competitionNextSteps helper (mp-a5d6).
// The function is unit-testable without mounting any component: it takes
// a plain competition object and returns a step array.
import { describe, it, expect } from 'vitest';
import { competitionNextSteps, overviewViewMode, overviewResultsSection, hasBracketRounds, latestCourtFinish, formatPausesRow, formatPerShiaijoRow } from '../admin_competition_overview.jsx';

// ---------------------------------------------------------------------------
// hasBracketRounds: never-nil LoadBracket contract
// ---------------------------------------------------------------------------
// Regression guard: internal/state/bracket.go's LoadBracket never returns nil
// - a competition with no bracket.json (every league) loads as
// &Bracket{Rounds: []}, a truthy object. A bare `!!bracket` check is therefore
// ALWAYS true and silently misroutes "View podium"/"Results" (via
// overviewResultsSection) to the empty AdminBracket view for any bracket-less
// format. Found live: clicking "View podium" on a completed league landed on
// the bracket tab's "Pick a match" empty state instead of league standings.
describe('hasBracketRounds; never-nil bracket contract', () => {
  it('is false for the never-nil empty-rounds default (no bracket.json)', () => {
    expect(hasBracketRounds({ rounds: [] })).toBe(false);
  });

  it('is false for a missing bracket', () => {
    expect(hasBracketRounds(null)).toBe(false);
    expect(hasBracketRounds(undefined)).toBe(false);
  });

  it('is true once the bracket has at least one real round', () => {
    expect(hasBracketRounds({ rounds: [[{ id: 'r1-m1' }]] })).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// overviewResultsSection: format → results/standings section
// ---------------------------------------------------------------------------
// Regression guard (Copilot review on PR #312): swiss competitions have no
// pools/bracket section, so a Results/podium CTA must route to "swiss", not the
// pools/bracket default: otherwise the operator lands on an empty/hidden view.
describe('overviewResultsSection: results routing', () => {
  it('routes swiss competitions to the "swiss" section regardless of bracket presence', () => {
    expect(overviewResultsSection('swiss', false)).toBe('swiss');
    expect(overviewResultsSection('swiss', true)).toBe('swiss');
  });

  it('routes to "bracket" when a bracket exists (non-swiss)', () => {
    expect(overviewResultsSection('knockout', true)).toBe('bracket');
    expect(overviewResultsSection('mixed', true)).toBe('bracket');
  });

  it('routes to "pools" when no bracket exists (non-swiss)', () => {
    expect(overviewResultsSection('mixed', false)).toBe('pools');
    expect(overviewResultsSection('league', false)).toBe('pools');
  });
});

// ---------------------------------------------------------------------------
// overviewViewMode: status → render-mode mapping
// ---------------------------------------------------------------------------
// Regression guard (mp-a5d6): the active phases are "pools" and "knockout",
// NOT a literal "running". An earlier version checked `status === "running"`,
// which is never true in production, so a running competition mis-rendered the
// "completed" card. These assertions lock the mapping against that drift.
describe('overviewViewMode: status mapping', () => {
  it('maps setup-ish statuses to "setup"', () => {
    expect(overviewViewMode('setup')).toBe('setup');
    expect(overviewViewMode('')).toBe('setup');
    expect(overviewViewMode(undefined)).toBe('setup');
    expect(overviewViewMode(null)).toBe('setup');
  });

  it('maps "draw-ready" to "draw-ready"', () => {
    expect(overviewViewMode('draw-ready')).toBe('draw-ready');
  });

  it('maps the running PHASES ("pools", "knockout") to "running"', () => {
    expect(overviewViewMode('pools')).toBe('running');
    expect(overviewViewMode('knockout')).toBe('running');
  });

  it('maps "completed" to "completed"', () => {
    expect(overviewViewMode('completed')).toBe('completed');
  });

  it('treats "invalid" and any unknown/future phase as "running" (keeps progress + scores widgets)', () => {
    expect(overviewViewMode('invalid')).toBe('running');
    expect(overviewViewMode('some-future-phase')).toBe('running');
  });

  it('never returns "completed" for an active phase (the original bug)', () => {
    expect(overviewViewMode('pools')).not.toBe('completed');
    expect(overviewViewMode('knockout')).not.toBe('completed');
  });
});

// ---------------------------------------------------------------------------
// Factories
// ---------------------------------------------------------------------------
function makeComp(overrides = {}) {
  return {
    id: 'comp-1',
    name: 'Shodan Open',
    status: 'setup',
    kind: 'individual',
    format: 'pools',
    players: [],
    courts: ['A'],
    ...overrides,
  };
}

let _playerSeq = 0;
function makePlayer(seed = null) {
  return { id: `p-${++_playerSeq}`, name: 'Player', seed };
}

// ---------------------------------------------------------------------------
// Basic shape
// ---------------------------------------------------------------------------
describe('competitionNextSteps: basic shape', () => {
  it('returns an array of step objects', () => {
    const steps = competitionNextSteps(makeComp());
    expect(Array.isArray(steps)).toBe(true);
    expect(steps.length).toBeGreaterThan(0);
  });

  it('every step has id, label, detail, state, section, cta', () => {
    const steps = competitionNextSteps(makeComp());
    for (const step of steps) {
      expect(typeof step.id).toBe('string');
      expect(typeof step.label).toBe('string');
      expect(typeof step.detail).toBe('string');
      expect(['done', 'active', 'pending']).toContain(step.state);
      // section is string or null
      expect(step.section === null || typeof step.section === 'string').toBe(true);
      // cta (active-step button label) is string or null
      expect(step.cta === null || typeof step.cta === 'string').toBe(true);
    }
  });

  it('every navigable step carries a cta label; non-navigable steps have cta null', () => {
    // Use a team comp so the lineups step is included in the navigable set.
    const steps = competitionNextSteps(makeComp({ kind: 'team', players: [] }));
    for (const step of steps) {
      if (step.section) {
        expect(typeof step.cta).toBe('string');
        expect(step.cta.endsWith('→')).toBe(true);
        // No clumsy double-verb copy like "Go to Review seeds & settings".
        expect(step.cta.startsWith('Go to ')).toBe(false);
      } else {
        expect(step.cta).toBeNull();
      }
    }
  });

  it('first step (create) is always done', () => {
    const steps = competitionNextSteps(makeComp());
    expect(steps[0].id).toBe('create');
    expect(steps[0].state).toBe('done');
  });

  it('last step (generate) is always pending or active, never done', () => {
    const steps = competitionNextSteps(makeComp());
    const last = steps[steps.length - 1];
    expect(last.id).toBe('generate');
    expect(last.state).not.toBe('done');
  });

  it('exactly one step is active', () => {
    const steps = competitionNextSteps(makeComp());
    const active = steps.filter(s => s.state === 'active');
    expect(active.length).toBe(1);
  });

  it('no step after the active step is done', () => {
    const steps = competitionNextSteps(makeComp());
    const activeIdx = steps.findIndex(s => s.state === 'active');
    for (let i = activeIdx + 1; i < steps.length; i++) {
      expect(steps[i].state).toBe('pending');
    }
  });
});

// ---------------------------------------------------------------------------
// Individual competition: setup states
// ---------------------------------------------------------------------------
// D26: participants.csv unreadable. The roster arrives empty, so a step that
// says "0 added, add participants" would point at a page that shows only the
// file's notice.
describe('competitionNextSteps: participants.csv unreadable', () => {
  const unreadable = { kind: 'corrupt-file', file: 'participants.csv', line: 0, column: 0, detail: 'could not be opened' };

  it('leaves out the Add participants step', () => {
    const steps = competitionNextSteps(makeComp({ players: [], dataIssues: [unreadable] }));
    expect(steps.find(s => s.id === 'participants')).toBeUndefined();
    expect(steps.some(s => /Add participants/.test(s.cta || ''))).toBe(false);
  });

  it('does not ask for participants on the generate step either', () => {
    const steps = competitionNextSteps(makeComp({ players: [], dataIssues: [unreadable] }));
    const gen = steps.find(s => s.id === 'generate');
    expect(gen.detail).not.toMatch(/Add at least 2/);
    expect(gen.detail).toBe('Repair the roster first: see the notice above');
  });

  // No seeding CTA leads the page while there is no roster: the generate step
  // takes `active` with no button (the repair is the banner's Reload), and the
  // settings step drops the seeds.
  it('leads with the repair, not a seeding call to action', () => {
    const steps = competitionNextSteps(makeComp({ players: [], dataIssues: [unreadable] }));
    const gen = steps.find(s => s.id === 'generate');
    const settings = steps.find(s => s.id === 'settings');
    expect(gen.state).toBe('active');
    expect(gen.cta).toBeNull();
    expect(settings.state).toBe('pending');
    expect(settings.label).toBe('Review settings');
    expect(settings.detail).not.toMatch(/seed/i);
  });

  it('quotes with curly marks in the create step', () => {
    const steps = competitionNextSteps(makeComp({ name: 'Open', players: [], dataIssues: [unreadable] }));
    expect(steps.find(s => s.id === 'create').detail).toBe('“Open” created');
  });

  it('keeps the step for a located fault on another file, and for an advisory note', () => {
    const other = { kind: 'corrupt-file', file: 'bracket.json', line: 2, column: 1, detail: 'x' };
    const advisory = { kind: 'missing-ids', file: 'participants.csv', detail: 'No id on file.' };
    expect(competitionNextSteps(makeComp({ dataIssues: [other, advisory] })).find(s => s.id === 'participants')).toBeTruthy();
  });
});

describe('competitionNextSteps: individual, 0 players', () => {
  it('participants step is active when 0 players', () => {
    const steps = competitionNextSteps(makeComp({ players: [] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.state).toBe('active');
  });

  it('participants step is active when 1 player', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer()] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.state).toBe('active');
  });

  it('detail mentions need at least 2 when < 2 players', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer()] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.detail).toMatch(/need at least 2/i);
  });

  it('participants step is done when >= 2 players', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer(), makePlayer()] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.state).toBe('done');
  });

  it('settings step becomes active after participants done', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer(), makePlayer()] }));
    const s = steps.find(s => s.id === 'settings');
    expect(s.state).toBe('active');
  });

  it('settings step detail mentions seed count when seeds present', () => {
    const players = [makePlayer(1), makePlayer(2), makePlayer()];
    const steps = competitionNextSteps(makeComp({ players }));
    const s = steps.find(st => st.id === 'settings');
    expect(s.detail).toMatch(/2 seed/i);
  });

  it('generate step is pending when players < 2', () => {
    const steps = competitionNextSteps(makeComp({ players: [] }));
    const g = steps.find(s => s.id === 'generate');
    expect(g.state).toBe('pending');
  });

  it('generate step detail mentions add participants when < 2 players', () => {
    const steps = competitionNextSteps(makeComp({ players: [] }));
    const g = steps.find(s => s.id === 'generate');
    expect(g.detail).toMatch(/at least 2/i);
  });

  it('generate step detail mentions header button when >= 2 players', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer(), makePlayer()] }));
    const g = steps.find(s => s.id === 'generate');
    expect(g.detail).toMatch(/header/i);
  });

  it('no lineups step for individual comps', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'individual' }));
    expect(steps.find(s => s.id === 'lineups')).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// Team competition
// ---------------------------------------------------------------------------
describe('competitionNextSteps: team competition', () => {
  it('includes a lineups step for team comps', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team' }));
    expect(steps.find(s => s.id === 'lineups')).toBeDefined();
  });

  it('lineups step comes after settings and before generate', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team' }));
    const ids = steps.map(s => s.id);
    const settingsIdx = ids.indexOf('settings');
    const lineupsIdx = ids.indexOf('lineups');
    const generateIdx = ids.indexOf('generate');
    expect(settingsIdx).toBeGreaterThan(-1);
    expect(lineupsIdx).toBeGreaterThan(settingsIdx);
    expect(generateIdx).toBeGreaterThan(lineupsIdx);
  });

  it('lineups step points to "lineups" section', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team' }));
    const l = steps.find(s => s.id === 'lineups');
    expect(l.section).toBe('lineups');
  });

  it('participants label says "teams" for team comps', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team' }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.label).toMatch(/teams/i);
  });

  it('participants detail says "teams" for team comps when < 2', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team', players: [] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.detail).toMatch(/team/i);
  });

  it('exactly one active step for team comp with 0 players', () => {
    const steps = competitionNextSteps(makeComp({ kind: 'team', players: [] }));
    expect(steps.filter(s => s.state === 'active').length).toBe(1);
  });

  it('exactly one active step for team comp with >= 2 players', () => {
    const steps = competitionNextSteps(makeComp({
      kind: 'team',
      players: [makePlayer(), makePlayer(), makePlayer()],
    }));
    expect(steps.filter(s => s.state === 'active').length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------
describe('competitionNextSteps: edge cases', () => {
  it('handles null/undefined comp gracefully (returns array)', () => {
    // Should not throw: caller may pass a partially-loaded comp.
    expect(() => competitionNextSteps(null)).not.toThrow();
    expect(Array.isArray(competitionNextSteps(null))).toBe(true);
  });

  it('handles missing players array', () => {
    const steps = competitionNextSteps({ id: 'c1', name: 'X', kind: 'individual' });
    expect(Array.isArray(steps)).toBe(true);
    const p = steps.find(s => s.id === 'participants');
    expect(p).toBeDefined();
  });

  it('participants step detail mentions count', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer(), makePlayer(), makePlayer()] }));
    const p = steps.find(s => s.id === 'participants');
    expect(p.detail).toMatch(/3/);
  });

  it('all non-create steps have a section or are the generate step', () => {
    const steps = competitionNextSteps(makeComp({ players: [makePlayer(), makePlayer()] }));
    for (const step of steps) {
      if (step.id === 'create') continue;
      if (step.id === 'generate') {
        // generate has no section (points at page-head button)
        expect(step.section).toBeNull();
      } else {
        expect(typeof step.section).toBe('string');
      }
    }
  });
});

// ---------------------------------------------------------------------------
// Court blockers (spec 007 R9): with a shiaijo allocation the draw cannot
// halve down, the header's "Generate draw" button is DISABLED, so the step
// that told the operator to press it was pointing at a dead control.
// ---------------------------------------------------------------------------
describe('competitionNextSteps: generate step under a court block', () => {
  const twoPlayers = () => [makePlayer(), makePlayer()];
  const generateStep = (c, courts) => competitionNextSteps(c, courts).find(s => s.id === 'generate');

  it('points at the header button when nothing blocks the draw', () => {
    const step = generateStep(makeComp({ players: twoPlayers(), courts: ['A', 'B'] }), ['A', 'B', 'C']);
    expect(step.detail).toBe('Use the “Generate draw” button in the header above');
    expect(step.section).toBeNull();
    expect(step.cta).toBeNull();
  });

  it('names the blocker and routes to Settings instead', () => {
    const step = generateStep(makeComp({ players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(step.detail).not.toContain('Use the “Generate draw” button');
    expect(step.detail).toContain('3 shiaijo cannot be paired down to a single bracket');
    expect(step.detail).toContain('Reassign shiaijo in Settings.');
    expect(step.section).toBe('settings');
    expect(step.cta).toBe('Reassign shiaijo →');
  });

  it('still asks for participants first when there are too few', () => {
    // The participant shortfall is the earlier blocker and stays the message:
    // a court fix would not make the draw possible.
    const step = generateStep(makeComp({ players: [], courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(step.detail).toBe('Add at least 2 participants first');
    expect(step.section).toBeNull();
  });

  it('leaves league alone: the count rule does not govern its courts', () => {
    const step = generateStep(makeComp({ format: 'league', players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(step.detail).toBe('Use the “Generate draw” button in the header above');
  });

  it('applies the count rule without a tournament court list', () => {
    // The overview can render before the tournament fetch settles; the count
    // half of the rule needs no venue data.
    const step = generateStep(makeComp({ players: twoPlayers(), courts: ['A', 'B', 'C'] }), undefined);
    expect(step.detail).toContain('3 shiaijo cannot be paired down to a single bracket');
  });

  it('names only counts the venue can supply', () => {
    // The detail line renders alone, with no venue-aware hint beneath it, so
    // a 3-shiaijo tournament must not be told to "use 2 or 4".
    const step = generateStep(makeComp({ players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(step.detail).toContain('This tournament has 3, so this competition can use 1 or 2');
    expect(step.detail).not.toContain('4');
  });

  // The CTA authored on this step could never render: only an `active` step
  // draws its button, and "Review seeds & settings" is emitted non-done
  // forever (there is no "seeds reviewed" signal), so it took `active` on
  // every render and the generate step was always `pending`.
  it('is the ACTIVE step while it is blocked, so its CTA can render', () => {
    const step = generateStep(makeComp({ players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(step.cta).toBe('Reassign shiaijo →');
    expect(step.section).toBe('settings');
    expect(step.state).toBe('active');
  });

  it('demotes the never-completing settings step while the draw is blocked', () => {
    // Otherwise two buttons on the same card both navigate to Settings.
    const steps = competitionNextSteps(makeComp({ players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(steps.find(s => s.id === 'settings').state).toBe('pending');
    expect(steps.filter(s => s.state === 'active').map(s => s.id)).toEqual(['generate']);
  });

  it('leaves the ordinary checklist alone when nothing blocks the draw', () => {
    const steps = competitionNextSteps(makeComp({ players: twoPlayers(), courts: ['A', 'B'] }), ['A', 'B', 'C']);
    expect(steps.filter(s => s.state === 'active').map(s => s.id)).toEqual(['settings']);
    expect(steps.find(s => s.id === 'generate').state).toBe('pending');
  });

  it('does not jump the queue while participants are still missing', () => {
    // A court fix would not make the draw possible yet, and the generate step
    // carries no CTA in that state, so promoting it would show a dead row.
    const steps = competitionNextSteps(makeComp({ players: [], courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(steps.filter(s => s.state === 'active').map(s => s.id)).toEqual(['participants']);
  });

  it('still promotes the blocker on a team competition, past the lineups step', () => {
    const steps = competitionNextSteps(
      makeComp({ kind: 'team', players: twoPlayers(), courts: ['A', 'B', 'C'] }), ['A', 'B', 'C']);
    expect(steps.map(s => s.id)).toContain('lineups');
    expect(steps.filter(s => s.state === 'active').map(s => s.id)).toEqual(['generate']);
  });
});

// ---------------------------------------------------------------------------
// draw-ready / running / completed: pure helper is only for setup state, but
// it should still be callable with any comp without throwing.
// ---------------------------------------------------------------------------
describe('competitionNextSteps: non-setup statuses', () => {
  it('does not throw for draw-ready status', () => {
    expect(() => competitionNextSteps(makeComp({ status: 'draw-ready', players: [makePlayer(), makePlayer()] }))).not.toThrow();
  });

  it('does not throw for running status', () => {
    expect(() => competitionNextSteps(makeComp({ status: 'running', players: [makePlayer(), makePlayer()] }))).not.toThrow();
  });

  it('does not throw for completed status', () => {
    expect(() => competitionNextSteps(makeComp({ status: 'completed', players: [makePlayer(), makePlayer()] }))).not.toThrow();
  });
});

describe('Overview estimate helpers (p2-overview)', () => {
  it('latestCourtFinish picks the latest court end and flags a finish past midnight', () => {
    expect(latestCourtFinish({ A: '13:10', B: '13:38' }, '09:00')).toEqual({ clock: '13:38', nextDay: false });
    // A court end later than StartTime + total estimate still wins.
    expect(latestCourtFinish({ A: '11:00', B: '16:45' }, '09:00')).toEqual({ clock: '16:45', nextDay: false });
    // An end earlier than the start crossed midnight.
    expect(latestCourtFinish({ A: '23:30', B: '00:40' }, '20:00')).toEqual({ clock: '00:40', nextDay: true });
    // The server may report a clock past 24:00.
    expect(latestCourtFinish({ A: '24:40' }, '20:00')).toEqual({ clock: '00:40', nextDay: true });
    expect(latestCourtFinish({}, '09:00')).toBeNull();
    expect(latestCourtFinish(undefined, '09:00')).toBeNull();
  });

  it('formatPerShiaijoRow labels by the competition shiaijo, says about, and names an idle one', () => {
    const est = { perCourtMinutes: [229, 221] };
    expect(formatPerShiaijoRow(est, ['A', 'B'])).toBe('A about 3h 49m · B about 3h 41m');
    expect(formatPerShiaijoRow({ perCourtMinutes: [55, 0] }, ['C', 'D'])).toBe('C about 55m · D not used');
    expect(formatPerShiaijoRow({ perCourtMinutes: [] }, ['A'])).toBeNull();
    expect(formatPerShiaijoRow(null, ['A'])).toBeNull();
  });

  it('formatPausesRow names the day pauses, says none, and lists an unused pause', () => {
    const c = { date: '05-06-2026' };
    const t = {
      date: '05-06-2026',
      durationDays: 1,
      pauses: [
        { name: 'Opening ceremony', start: '09:30', length: '30m', day: 0 },
        { name: 'Lunch', start: '12:30', length: '1h', day: 0 },
        { name: 'Closing ceremony', start: '', length: '30m', day: 0 },
      ],
    };
    expect(formatPausesRow(t, c)).toBe(
      'Opening ceremony 09:30 to 10:00, Lunch 12:30 to 13:30 · Closing ceremony: not used, no start time',
    );
    expect(formatPausesRow({ ...t, pauses: [] }, c)).toBe('No pauses that day');
    // A pause on another day is not that day's pause.
    const twoDay = { date: '05-06-2026', durationDays: 2, pauses: [{ name: 'Lunch', start: '12:30', length: '1h', day: 2 }] };
    expect(formatPausesRow(twoDay, c)).toBe('No pauses that day');
    expect(formatPausesRow(twoDay, { date: '06-06-2026' })).toBe('Lunch 12:30 to 13:30');
  });
});
