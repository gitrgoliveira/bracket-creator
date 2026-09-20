import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

// bc-csst, operator ruling 2026-09-19 (DESIGN.md Principle 3): motion means a
// warning or an expected operator action, never "this is normal and ongoing".
// The running-match dot and the viewer's admin pill lost their pulses on that
// ruling. A removed animation leaves no failing test behind on its own, so
// this one reads the stylesheet and pins the removal, the same way
// team_editor_vacancy_not_flagged pins a removed warning.

const __dirname = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(resolve(__dirname, '..', '..', 'css', 'styles.css'), 'utf8');

// The declarations of one top-level rule, by exact selector.
const block = (selector) => {
  const start = css.indexOf(`\n${selector} {`);
  expect(start, `rule ${selector} exists`).toBeGreaterThan(-1);
  return css.slice(start, css.indexOf('}', start));
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
