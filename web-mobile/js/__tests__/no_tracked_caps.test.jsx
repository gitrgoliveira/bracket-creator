// Labels, captions, table heads, tags and status words are sentence case (or
// lower case) and untracked (operator decision): nothing is set in capitals by
// the stylesheet, and no letter-spacing goes above 0.04em. This gate reads
// every top-level js/*.jsx module (comments blanked, check-helpers.mjs) for an
// inline style that uppercases or tracks, and both stylesheets for the same in
// CSS. Negative tracking (a large heading set tight) is not tracking and passes.
// A word typed in capitals is not caught here; write it in sentence case.
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { stripComments } from '../../check-helpers.mjs';

const JS = resolve(__dirname, '..');
const ROOT = resolve(JS, '..', '..');
const STYLESHEETS = ['web-mobile/css/styles.css', 'web/css/styles.css'];

// 0.04em is the most tracking a label may carry. A px value is read at the
// 16px root size (0.04em = 0.64px).
const MAX_EM = 0.04;
const MAX_PX = MAX_EM * 16;

function trackedTooWide(value) {
  const v = String(value).trim().replace(/^['"`]|['"`]$/g, '');
  const m = v.match(/^(-?\d*\.?\d+)\s*(em|rem|px)?$/);
  if (!m) return false; // normal, inherit, a variable: not a measured tracking
  const n = parseFloat(m[1]);
  if (m[2] === 'em' || m[2] === 'rem') return n > MAX_EM;
  return n > MAX_PX; // px, or a bare number, which Preact reads as px
}

const INLINE_UPPERCASE = /textTransform\s*:\s*['"`]uppercase['"`]/;
const INLINE_SPACING = /letterSpacing\s*:\s*(['"`][^'"`]*['"`]|-?\d*\.?\d+)/g;

function jsxHits(file, src) {
  const hits = [];
  stripComments(src).split('\n').forEach((line, i) => {
    const where = `${file}:${i + 1}: ${line.trim().slice(0, 120)}`;
    if (INLINE_UPPERCASE.test(line)) hits.push(`uppercase in ${where}`);
    for (const m of line.matchAll(INLINE_SPACING)) {
      if (trackedTooWide(m[1])) hits.push(`letterSpacing ${m[1]} in ${where}`);
    }
  });
  return hits;
}

function stripCssComments(src) {
  // Blank each comment character by character so line numbers stay true.
  return src.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));
}

const CSS_UPPERCASE = /text-transform\s*:\s*uppercase\b/i;
const CSS_SPACING = /letter-spacing\s*:\s*([^;}\n]+)/gi;

function cssHits(file, src) {
  const hits = [];
  stripCssComments(src).split('\n').forEach((line, i) => {
    const where = `${file}:${i + 1}: ${line.trim().slice(0, 120)}`;
    if (CSS_UPPERCASE.test(line)) hits.push(`text-transform: uppercase in ${where}`);
    for (const m of line.matchAll(CSS_SPACING)) {
      const value = m[1].replace(/!important/i, '').trim();
      if (trackedTooWide(value)) hits.push(`letter-spacing ${value} in ${where}`);
    }
  });
  return hits;
}

describe('labels are sentence case and untracked', () => {
  it('the detectors flag uppercasing and wide tracking, and leave tight or normal tracking alone', () => {
    expect(jsxHits('t.jsx', `<span style={{ textTransform: "uppercase" }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: "0.08em" }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: '0.05em' }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: 2 }}>x</span>`)).toHaveLength(1);
    for (const ok of [
      `<span style={{ letterSpacing: '0.04em' }}>x</span>`,
      `<span style={{ letterSpacing: '0.02em', textTransform: 'none' }}>x</span>`,
      `<h1 style={{ letterSpacing: "-0.02em" }}>x</h1>`,
      `<span style={{ letterSpacing: 'normal' }}>x</span>`,
    ]) expect(jsxHits('t.jsx', ok), ok).toEqual([]);

    expect(cssHits('t.css', '.a { text-transform: uppercase; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { letter-spacing: 0.14em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { letter-spacing: 0.05em !important; }')).toHaveLength(1);
    for (const ok of [
      '.a { letter-spacing: 0.04em; }',
      '.a { letter-spacing: -0.02em; }',
      '.a { letter-spacing: normal; text-transform: none; }',
      '/* letter-spacing: 0.2em; text-transform: uppercase */',
    ]) expect(cssHits('t.css', ok), ok).toEqual([]);
  });

  it('no non-test js/*.jsx module uppercases or tracks a label in an inline style', () => {
    const files = readdirSync(JS).filter((f) => f.endsWith('.jsx')).sort();
    expect(files.length).toBeGreaterThan(50);
    const hits = files.flatMap((f) => jsxHits(f, readFileSync(resolve(JS, f), 'utf8')));
    expect(hits, `Write the words in sentence case and drop the tracking:\n${hits.join('\n')}`).toEqual([]);
  });

  it('neither stylesheet uppercases or tracks a label', () => {
    const hits = STYLESHEETS.flatMap((f) => cssHits(f, readFileSync(resolve(ROOT, f), 'utf8')));
    expect(hits, `Write the words in sentence case and drop the tracking:\n${hits.join('\n')}`).toEqual([]);
  });
});
