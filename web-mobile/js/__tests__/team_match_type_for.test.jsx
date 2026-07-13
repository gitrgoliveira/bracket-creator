// teamMatchTypeFor (pool_ids.jsx): resolves the competition-level team match
// format from either the flat viewer shape (c.teamMatchType) or the admin
// detail shape (c.config.teamMatchType). Callers suppress the format for
// supplementary rep bouts at the stamping site.

import { describe, it, expect } from 'vitest';
import { teamMatchTypeFor, teamMatchTypeHint } from '../pool_ids.jsx';

describe('teamMatchTypeFor', () => {
  it('reads the flat viewer shape', () => {
    expect(teamMatchTypeFor({ teamMatchType: "kachinuki" })).toBe("kachinuki");
  });

  it('reads the config-nested admin shape', () => {
    expect(teamMatchTypeFor({ config: { teamMatchType: "kachinuki" } })).toBe("kachinuki");
  });

  it('flat teamMatchType wins when both flat and nested are set', () => {
    expect(teamMatchTypeFor({ teamMatchType: "fixed", config: { teamMatchType: "kachinuki" } })).toBe("fixed");
  });

  it('returns "" for null comp', () => {
    expect(teamMatchTypeFor(null)).toBe("");
  });

  it('returns "" for undefined comp', () => {
    expect(teamMatchTypeFor(undefined)).toBe("");
  });

  it('returns "" for an individual comp with no teamMatchType field', () => {
    expect(teamMatchTypeFor({ id: "comp-1", kind: "individual" })).toBe("");
  });
});

describe('teamMatchTypeHint', () => {
  it('returns the winner-stays copy for kachinuki', () => {
    expect(teamMatchTypeHint(true)).toMatch(/winner of each bout stays/);
  });
  it('returns the fixed-order copy otherwise', () => {
    expect(teamMatchTypeHint(false)).toMatch(/scheduled up-front by position/);
  });
});
