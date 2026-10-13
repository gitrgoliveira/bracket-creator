// Operator ruling 2026-10-13: "Self run mode has no check-in." checkInApplies
// (competition_shape.jsx) is the ONE client owner of whether a competition's
// check-in setting counts; the watchlist roster and the admin dashboard ask it.
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from './helpers/stub_globals.js';
import { collectText } from './helpers/vdom.js';
import { checkInApplies, SELF_RUN_NO_CHECK_IN } from '../competition_shape.jsx';
import { buildRoster } from '../viewer.jsx';

const SELF_RUN = { mode: 'self-run' };
const OFFICIATED = { mode: 'officiated' };

describe('checkInApplies', () => {
  it('is the competition setting in an officiated tournament', () => {
    expect(checkInApplies(OFFICIATED, true)).toBe(true);
    expect(checkInApplies(OFFICIATED, false)).toBe(false);
  });
  it('is the competition setting when the tournament has no mode or is unknown', () => {
    expect(checkInApplies({}, true)).toBe(true);
    expect(checkInApplies(null, true)).toBe(true);
    expect(checkInApplies(undefined, false)).toBe(false);
  });
  it('is false in a self-run tournament whatever the stored setting is', () => {
    expect(checkInApplies(SELF_RUN, true)).toBe(false);
    expect(checkInApplies(SELF_RUN, false)).toBe(false);
  });
  it('words the refusal as the server does', () => {
    expect(SELF_RUN_NO_CHECK_IN).toBe('Self-run mode has no check-in.');
  });
});

describe('buildRoster in a self-run tournament', () => {
  const comps = [{ id: 'c1', checkInEnabled: true, players: [{ id: 'p1', name: 'Alice', checkedIn: true }] }];
  it('never marks anyone checked in', () => {
    expect(buildRoster(comps, SELF_RUN)[0].checkedIn).toBe(false);
  });
  it('keeps the mark in an officiated tournament and when no tournament is given', () => {
    expect(buildRoster(comps, OFFICIATED)[0].checkedIn).toBe(true);
    expect(buildRoster(comps)[0].checkedIn).toBe(true);
  });
});

describe('AdminDashboard Registration desk card', () => {
  let AdminDashboard;
  let restoreGlobals;
  beforeAll(async () => {
    restoreGlobals = installWindowStubs({
      pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || `${s}s`)}`,
      formatLabelShort: (f) => f,
      formatDate: (d) => d,
      formatAdminHeaderSub: () => 'header',
      competitionKindLabel: () => 'Individual',
      courtCount: () => 1,
      StatusBadge: function StatusBadge() { return null; },
    });
    await import('../admin_shell.jsx');
    AdminDashboard = window.AdminDashboard;
  });
  afterAll(() => restoreGlobals());

  const dashboardText = (mode) => collectText(AdminDashboard({
    tournament: {
      name: 'Cup', courts: ['A'], mode,
      competitions: [{ id: 'c1', name: 'Men', kind: 'individual', status: 'setup', players: [], courts: ['A'] }],
    },
    onOpenRegistration: vi.fn(),
  }));

  it('is offered in an officiated tournament', () => {
    expect(dashboardText('officiated')).toContain('Registration desk');
  });
  it('is not offered in a self-run tournament', () => {
    expect(dashboardText('self-run')).not.toContain('Registration desk');
  });
});
