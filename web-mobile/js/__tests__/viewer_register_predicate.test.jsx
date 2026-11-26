import { describe, it, expect } from 'vitest';
import { shouldShowRegister } from '../viewer.jsx';
import { shouldShowTeamNote } from '../viewer_home.jsx';

const selfRun    = { mode: 'self-run' };
const officiated = { mode: 'officiated' };

const indivSetup    = { kind: 'individual', status: 'setup' };
const indivNoStatus = { kind: 'individual', status: '' };
const indivPools    = { kind: 'individual', status: 'pools' };
const indivComplete = { kind: 'individual', status: 'completed' };
const teamSetup     = { kind: 'team', status: 'setup' };

describe('shouldShowRegister predicate (mp-e5j)', () => {
  it('shows: self-run + individual + setup status', () => {
    expect(shouldShowRegister(selfRun, indivSetup, true)).toBe(true);
  });

  it('shows: self-run + individual + empty status', () => {
    expect(shouldShowRegister(selfRun, indivNoStatus, true)).toBe(true);
  });

  it('hides: no handler (onRegister absent)', () => {
    expect(shouldShowRegister(selfRun, indivSetup, false)).toBe(false);
  });

  it('hides: officiated tournament', () => {
    expect(shouldShowRegister(officiated, indivSetup, true)).toBe(false);
  });

  it('hides: team competition', () => {
    expect(shouldShowRegister(selfRun, teamSetup, true)).toBe(false);
  });

  it('hides: a team size under another kind (Competition.IsTeam)', () => {
    expect(shouldShowRegister(selfRun, { kind: 'individual', teamSize: 3, status: 'setup' }, true)).toBe(false);
    expect(shouldShowRegister(selfRun, { kind: '', teamSize: 2, status: '' }, true)).toBe(false);
  });

  it('hides: competition past setup (pools)', () => {
    expect(shouldShowRegister(selfRun, indivPools, true)).toBe(false);
  });

  it('hides: competition complete', () => {
    expect(shouldShowRegister(selfRun, indivComplete, true)).toBe(false);
  });

  it('hides: null tournament', () => {
    expect(shouldShowRegister(null, indivSetup, true)).toBe(false);
  });
});

describe('shouldShowTeamNote predicate', () => {
  it('shows: self-run team competition in setup, by kind or by team size', () => {
    expect(shouldShowTeamNote(selfRun, teamSetup)).toBe(true);
    expect(shouldShowTeamNote(selfRun, { kind: 'individual', teamSize: 3, status: '' })).toBe(true);
  });

  it('hides: an individual competition', () => {
    expect(shouldShowTeamNote(selfRun, indivSetup)).toBe(false);
  });

  it('hides: a team competition past setup', () => {
    expect(shouldShowTeamNote(selfRun, { kind: 'team', status: 'pools' })).toBe(false);
  });

  it('hides: officiated tournament or no tournament', () => {
    expect(shouldShowTeamNote(officiated, teamSetup)).toBe(false);
    expect(shouldShowTeamNote(null, teamSetup)).toBe(false);
  });
});
