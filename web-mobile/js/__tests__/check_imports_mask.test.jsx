// stripComments (check-helpers.mjs) blanks comments, read by a real parser, for
// check-imports.mjs and the rule gates. An
// import it blanks by mistake is an import nobody checks, so the mask must read
// a regular-expression literal as code: a "/*" or a backtick inside one opens
// no comment or template.
import { describe, it, expect } from 'vitest';
import { stripComments } from '../../check-helpers.mjs';

const IMPORT = "import { X } from './y.jsx';";

describe('stripComments', () => {
  it('blanks line and block comments and keeps the line structure', () => {
    const src = `a(); // import { Z } from './z.jsx'\n/* b\nc */ ${IMPORT}\n`;
    const out = stripComments(src);
    expect(out).toHaveLength(src.length);
    expect(out.split('\n').length).toBe(src.split('\n').length);
    expect(out).toContain(IMPORT);
    expect(out).not.toContain('./z.jsx');
  });

  it('keeps a "//" or "/*" inside a string', () => {
    expect(stripComments(`const u = "http://x/*y";\n${IMPORT}`)).toContain('http://x/*y');
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
    const out = stripComments(src);
    expect(out).toContain(line);
    expect(out).toContain(IMPORT);
    expect(out).not.toContain('trailing');
  });

  it('still reads a division, a closing tag and JSX text as code', () => {
    const src = `const q = a / b; const w = <p>a/b</p>; const e = <br />;\n${IMPORT}`;
    expect(stripComments(src)).toBe(src);
  });

  it.each([
    ['after a postfix ++', 'a = i++ / 2; // note'],
    ['after a postfix --', 'a = x-- / 2 // hi'],
    ['after a property named like a keyword', 'a = obj.in / b; // c'],
  ])('reads a division %s as a division, so the comment after it is blanked', (_, line) => {
    const out = stripComments(`${line}\n${IMPORT}\n`);
    expect(out.split('\n')[0]).toBe(line.replace(/\/\/.*$/, (c) => ' '.repeat(c.length)));
    expect(out).toContain(IMPORT);
  });

  it('keeps a URL in JSX text but blanks a comment written straight after a colon', () => {
    const url = 'const l = <a>see https://x.test/a and http://y.test</a>;';
    expect(stripComments(url)).toBe(url);
    const out = stripComments(`switch (k) {\ncase 1:// import { Z } from './z.jsx'\ndefault:// note\n}\nx ? a ://b\n${IMPORT}`);
    expect(out).not.toContain('./z.jsx');
    expect(out).not.toContain('note');
    expect(out).not.toContain('//b');
    expect(out).toContain(IMPORT);
  });

  it('still reads a slash after a keyword as a regular expression', () => {
    const src = 'if (ok) return /[/*]/.test(s);\nthrow /`/;\n';
    expect(stripComments(`${src}${IMPORT}`)).toBe(`${src}${IMPORT}`);
  });

  it('leaves a "/" with no closing slash on its line to the normal scan', () => {
    const src = `f(/ not a regex\n${IMPORT}`;
    expect(stripComments(src)).toBe(src);
  });
  it('keeps every offset when the text before a comment is not ASCII', () => {
    const src = `const s = "é😀 日本"; // note\n${IMPORT}\n/* b */ x();`;
    const out = stripComments(src);
    expect(out).toHaveLength(src.length);
    expect(out.split('\n')[0]).toBe(`const s = "é😀 日本"; ${' '.repeat('// note'.length)}`);
    expect(out).toContain(IMPORT);
    expect(out).toContain('x();');
  });

  it('reads an apostrophe in JSX text, and a quote in a template, as text and not as a string opener', () => {
    const src = "const a = <p>Don't stop // not a comment</p>; // real\nconst b = `it's ${x} // inside`; /* real */\n";
    const out = stripComments(src);
    expect(out).toContain("<p>Don't stop // not a comment</p>");
    expect(out).toContain('`it\'s ${x} // inside`');
    expect(out).not.toContain('real');
  });

  it('blanks the comments of a stylesheet', () => {
    const css = 'a { color: red; } /* default win */\nb { content: "x"; }\n';
    const out = stripComments(css, 'css');
    expect(out).toHaveLength(css.length);
    expect(out).not.toContain('default');
    expect(out).toContain('b { content: "x"; }');
  });

  it('blanks the comments of a page: HTML comments, an inline script and a style, not a script with a src', () => {
    const html = [
      '<!-- one -->',
      '<script src="/a.js"></script>',
      '<script>',
      "  // two",
      "  const u = 'http://x.test'; /* three */",
      '</script>',
      '<style>/* four */ p { margin: 0 }</style>',
      '<p>text // kept</p>',
    ].join('\n');
    const out = stripComments(html, 'html');
    expect(out).toHaveLength(html.length);
    expect(out.split('\n').length).toBe(html.split('\n').length);
    for (const gone of ['one', 'two', 'three', 'four']) expect(out).not.toContain(gone);
    expect(out).toContain("const u = 'http://x.test';");
    expect(out).toContain('<script src="/a.js"></script>');
    expect(out).toContain('<p>text // kept</p>');
  });
});
