// Tests for the pure competitionNextSteps helper (mp-a5d6).
// The function is unit-testable without mounting any component — it takes
// a plain competition object and returns a step array.
import { describe, it, expect } from 'vitest';
import { competitionNextSteps, overviewViewMode } from '../admin_competition_overview.jsx';

// ---------------------------------------------------------------------------
// overviewViewMode — status → render-mode mapping
// ---------------------------------------------------------------------------
// Regression guard (mp-a5d6): the active phases are "pools" and "playoffs",
// NOT a literal "running". An earlier version checked `status === "running"`,
// which is never true in production, so a running competition mis-rendered the
// "completed" card. These assertions lock the mapping against that drift.
describe('overviewViewMode — status mapping', () => {
  it('maps setup-ish statuses to "setup"', () => {
    expect(overviewViewMode('setup')).toBe('setup');
    expect(overviewViewMode('')).toBe('setup');
    expect(overviewViewMode(undefined)).toBe('setup');
    expect(overviewViewMode(null)).toBe('setup');
  });

  it('maps "draw-ready" to "draw-ready"', () => {
    expect(overviewViewMode('draw-ready')).toBe('draw-ready');
  });

  it('maps the running PHASES ("pools", "playoffs") to "running"', () => {
    expect(overviewViewMode('pools')).toBe('running');
    expect(overviewViewMode('playoffs')).toBe('running');
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
    expect(overviewViewMode('playoffs')).not.toBe('completed');
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

function makePlayer(seed = null) {
  return { id: `p-${Math.random()}`, name: 'Player', seed };
}

// ---------------------------------------------------------------------------
// Basic shape
// ---------------------------------------------------------------------------
describe('competitionNextSteps — basic shape', () => {
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
      expect(['done', 'active', 'todo']).toContain(step.state);
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

  it('last step (generate) is always todo or active, never done', () => {
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
      expect(steps[i].state).toBe('todo');
    }
  });
});

// ---------------------------------------------------------------------------
// Individual competition — setup states
// ---------------------------------------------------------------------------
describe('competitionNextSteps — individual, 0 players', () => {
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

  it('generate step is todo when players < 2', () => {
    const steps = competitionNextSteps(makeComp({ players: [] }));
    const g = steps.find(s => s.id === 'generate');
    expect(g.state).toBe('todo');
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
describe('competitionNextSteps — team competition', () => {
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
describe('competitionNextSteps — edge cases', () => {
  it('handles null/undefined comp gracefully (returns array)', () => {
    // Should not throw — caller may pass a partially-loaded comp.
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
// draw-ready / running / completed: pure helper is only for setup state, but
// it should still be callable with any comp without throwing.
// ---------------------------------------------------------------------------
describe('competitionNextSteps — non-setup statuses', () => {
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
