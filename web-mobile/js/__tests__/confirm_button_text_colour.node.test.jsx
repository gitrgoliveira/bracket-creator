import { describe, it, expect } from 'vitest';
import { cssBlock, readStylesheet } from './helpers/source.js';

// The armed "tap again to confirm" state (.btn--confirm) was written for
// buttons that also carry .btn--primary, which sets color: var(--accent-fg).
// A plain .btn with no .btn--primary alongside it -- e.g. the kachinuki End
// match button (admin_scoring_team.jsx, data-testid
// "kachinuki-end-match-button"), className={`btn ${endArmed ?
// "btn--confirm" : ""}`} -- gets the default dark ink on the rule's navy
// (--accent-strong) fill, which is unreadable (confirmed in a real browser
// screenshot). .btn--confirm must own its own readable text colour so it
// never depends on a sibling class.
describe('.btn--confirm owns a readable text colour', () => {
  it('declares color: var(--accent-fg) on the armed state', () => {
    const css = readStylesheet();
    const rule = cssBlock(css, '.btn--confirm');
    expect(rule, 'rule .btn--confirm exists').not.toBeNull();
    expect(rule).toContain('color: var(--accent-fg)');
  });
});
