// lineup_resolver.jsx: teamCarriesName is the ONE test of "this typed name is
// one a team member already carries", used by the lineup panel's save and its
// name box, so the two cannot drift apart.

import { describe, it, expect } from 'vitest';
import { teamCarriesName } from '../lineup_resolver.jsx';

const entries = [{ id: 'm1', name: 'Sano' }, { id: 'm2', name: '' }, null];

describe('teamCarriesName', () => {
  it('matches a member name ignoring case and surrounding spaces', () => {
    expect(teamCarriesName(entries, 'sano')).toBe(true);
    expect(teamCarriesName(entries, '  SANO ')).toBe(true);
  });

  it('does not match an unknown name', () => {
    expect(teamCarriesName(entries, 'Ito')).toBe(false);
  });

  it('never matches a blank name, so a nameless member is not taken', () => {
    expect(teamCarriesName(entries, '')).toBe(false);
    expect(teamCarriesName(entries, '   ')).toBe(false);
    expect(teamCarriesName(entries, undefined)).toBe(false);
  });

  it('tolerates a missing entry list', () => {
    expect(teamCarriesName(undefined, 'Sano')).toBe(false);
  });
});
