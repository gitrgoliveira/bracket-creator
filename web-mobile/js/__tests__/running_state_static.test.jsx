import { describe, it, expect } from 'vitest';
import { cssBlock, readStylesheet } from './helpers/source.js';

// bc-csst, operator ruling 2026-09-19 (DESIGN.md Principle 3): motion means a
// warning or an expected operator action, never "this is normal and ongoing".
// The running-match dot and the viewer's admin pill lost their pulses on that
// ruling. A removed animation leaves no failing test behind on its own, so
// this one reads the stylesheet and pins the removal, the same way
// team_editor_vacancy_not_flagged pins a removed warning.

const css = readStylesheet();

const block = (selector) => {
  const b = cssBlock(css, selector);
  expect(b, `rule ${selector} exists`).not.toBeNull();
  return b;
};

describe('ordinary ongoing state does not pulse', () => {
  it('has no running-state or admin-pill keyframes left', () => {
    expect(css).not.toMatch(/@keyframes\s+pulse\b/);
    expect(css).not.toMatch(/@keyframes\s+admin-pill-pulse\b/);
  });

  it('keeps the running dot static: navy fill and soft ring, no animation', () => {
    const dot = block('.dot--running');
    expect(dot).toContain('background: var(--accent)');
    expect(dot).toContain('box-shadow: 0 0 0 4px var(--accent-soft)');
    expect(dot).not.toMatch(/animation/);
  });

  it('keeps the prominent admin pill static', () => {
    expect(block('.viewer__head--hero .viewer__admin-pill--prominent')).not.toMatch(/animation/);
  });
});
