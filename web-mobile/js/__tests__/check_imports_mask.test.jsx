// maskComments (check-helpers.mjs) blanks comments for check-imports.mjs. An
// import it blanks by mistake is an import nobody checks, so the mask must read
// a regular-expression literal as code: a "/*" or a backtick inside one opens
// no comment or template.
import { describe, it, expect } from 'vitest';
import { maskComments } from '../../check-helpers.mjs';

const IMPORT = "import { X } from './y.jsx';";

describe('maskComments', () => {
  it('blanks line and block comments and keeps the line structure', () => {
    const src = `a(); // import { Z } from './z.jsx'\n/* b\nc */ ${IMPORT}\n`;
    const out = maskComments(src);
    expect(out).toHaveLength(src.length);
    expect(out.split('\n').length).toBe(src.split('\n').length);
    expect(out).toContain(IMPORT);
    expect(out).not.toContain('./z.jsx');
  });

  it('keeps a "//" or "/*" inside a string', () => {
    expect(maskComments(`const u = "http://x/*y";\n${IMPORT}`)).toContain('http://x/*y');
  });

  it.each([
    ['a class holding "/*"', 'const r = /[/*]/;'],
    ['a "/*" after an escaped slash', 'const r = /a\\/*b/;'],
    ['a backtick', 'const r = /`/;'],
    ['a quote', "const r = /'/;"],
    ['after return', 'function f(s) { return /[/*]/.test(s); }'],
    ['after an arrow', 'const f = (s) => /[/*]/.test(s);'],
    ['inside a call', 'x.replace(/[/*]/g, "");'],
  ])('does not let a regular expression holding %s hide the import after it', (_, line) => {
    const src = `${line}\n${IMPORT}\n/* trailing */\n`;
    const out = maskComments(src);
    expect(out).toContain(line);
    expect(out).toContain(IMPORT);
    expect(out).not.toContain('trailing');
  });

  it('still reads a division, a closing tag and JSX text as code', () => {
    const src = `const q = a / b; const w = <p>a/b</p>; const e = <br />;\n${IMPORT}`;
    expect(maskComments(src)).toBe(src);
  });

  it('leaves a "/" with no closing slash on its line to the normal scan', () => {
    const src = `f(/ not a regex\n${IMPORT}`;
    expect(maskComments(src)).toBe(src);
  });
});
