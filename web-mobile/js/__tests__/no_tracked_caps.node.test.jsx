// Labels, captions, table heads, tags and status words are sentence case (or
// lower case) and untracked (operator decisions 2026-11-18, DESIGN.md "Labels
// and meta lines"): nothing is set in capitals or small caps by the
// stylesheet, and letter-spacing stays at 0.03em or less on text 15px or
// under, 0.04em or less above it. TV and lobby display type is read from
// across a hall and is exempt from the tracking limit (operator ruling
// 2026-11-20), never from the capitals rule. This gate reads every top-level
// js/*.jsx module (comments blanked, check-helpers.mjs) for an inline style
// that uppercases or tracks, and both stylesheets for the same in CSS.
// Negative tracking (a large heading set tight) is not tracking and passes.
// A word typed in capitals is not caught here; write it in sentence case.
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { stripComments } from '../../check-helpers.mjs';

const JS = resolve(__dirname, '..');
const ROOT = resolve(JS, '..', '..');
const STYLESHEETS = ['web-mobile/css/styles.css', 'web/css/styles.css'];

const SMALL_PX = 15;
const MAX_EM_SMALL = 0.03;
const MAX_EM_LARGE = 0.04;
const ROOT_PX = 16;

// The TV and lobby screens. Their type is sized by screen height (vh), which a
// px reading cannot measure, so they are named here rather than measured.
const TV_MODULES = new Set(['display_lobby.jsx', 'display_scoreboard.jsx']);
const TV_SELECTOR = /^\.(?:tvd|lobby)(?:[-_.:\s]|$)/;
const isTvSelectorList = (sel) => sel.split(',').every((s) => TV_SELECTOR.test(s.trim()));

// The px size a font-size or font shorthand sets, or null when it cannot be
// read as px (vh, %, a variable, inherited): such text counts as small.
function fontPx(value) {
  const m = String(value ?? '').match(/(?:^|\s|['"`])(\d*\.?\d+)(px|rem)?(?=[\s/'"`]|$)/);
  if (!m) return null;
  const n = parseFloat(m[1]);
  if (m[2] === 'rem') return n * ROOT_PX;
  if (m[2] === 'px' || /^\d*\.?\d+$/.test(String(value).trim())) return n; // a bare number is px to Preact
  return null;
}

function trackedTooWide(value, sizePx = null) {
  const v = String(value).trim().replace(/^['"`]|['"`]$/g, '');
  const m = v.match(/^(-?\d*\.?\d+)\s*(em|rem|px)?$/);
  if (!m) return false; // normal, inherit, a variable: not a measured tracking
  const n = parseFloat(m[1]);
  const size = sizePx ?? SMALL_PX;
  const em = m[2] === 'em' ? n : m[2] === 'rem' ? (n * ROOT_PX) / size : n / size;
  const max = sizePx != null && sizePx > SMALL_PX ? MAX_EM_LARGE : MAX_EM_SMALL;
  return em > max + 1e-9;
}

const INLINE_UPPERCASE = /textTransform\s*:\s*['"`]uppercase['"`]/;
const INLINE_SMALL_CAPS = /fontVariant(?:Caps)?\s*:\s*['"`][^'"`]*small-caps/;
const INLINE_SPACING = /letterSpacing\s*:\s*(['"`][^'"`]*['"`]|-?\d*\.?\d+)/g;
const INLINE_SIZE = /fontSize\s*:\s*(['"`][^'"`]*['"`]|\d*\.?\d+)/;

function jsxHits(file, src) {
  const hits = [];
  const tv = TV_MODULES.has(file);
  stripComments(src).split('\n').forEach((line, i) => {
    const where = `${file}:${i + 1}: ${line.trim().slice(0, 120)}`;
    if (INLINE_UPPERCASE.test(line)) hits.push(`uppercase in ${where}`);
    if (INLINE_SMALL_CAPS.test(line)) hits.push(`small caps in ${where}`);
    if (tv) return;
    const size = line.match(INLINE_SIZE);
    const sizePx = size ? fontPx(size[1]) : null;
    for (const m of line.matchAll(INLINE_SPACING)) {
      if (trackedTooWide(m[1], sizePx)) hits.push(`letterSpacing ${m[1]} in ${where}`);
    }
  });
  return hits;
}

function stripCssComments(src) {
  // Blank each comment character by character so line numbers stay true.
  return src.replace(/\/\*[\s\S]*?\*\//g, (c) => c.replace(/[^\n]/g, ' '));
}

const CSS_UPPERCASE = /text-transform\s*:\s*uppercase\b/i;
const CSS_SMALL_CAPS = /font-variant(?:-caps)?\s*:[^;}]*small-caps/i;
const CSS_RULE = /([^{}]+)\{([^{}]*)\}/g; // an innermost rule: its selector and body
const CSS_SPACING = /letter-spacing\s*:\s*([^;}\n]+)/gi;
const CSS_SIZE = /(?:^|[;\s])font(?:-size)?\s*:\s*([^;}]+)/i;

function cssHits(file, src) {
  const hits = [];
  const css = stripCssComments(src);
  const lineAt = (index) => css.slice(0, index).split('\n').length;
  const where = (index) => `${file}:${lineAt(index)}: ${css.split('\n')[lineAt(index) - 1].trim().slice(0, 120)}`;
  css.split('\n').forEach((line, i) => {
    const at = `${file}:${i + 1}: ${line.trim().slice(0, 120)}`;
    if (CSS_UPPERCASE.test(line)) hits.push(`text-transform: uppercase in ${at}`);
    if (CSS_SMALL_CAPS.test(line)) hits.push(`small caps in ${at}`);
  });
  for (const rule of css.matchAll(CSS_RULE)) {
    if (isTvSelectorList(rule[1])) continue;
    const body = rule[2];
    const bodyAt = rule.index + rule[1].length + 1;
    const size = body.match(CSS_SIZE);
    const sizePx = size ? fontPx(size[1]) : null;
    for (const m of body.matchAll(CSS_SPACING)) {
      const value = m[1].replace(/!important/i, '').trim();
      if (trackedTooWide(value, sizePx)) hits.push(`letter-spacing ${value} in ${where(bodyAt + m.index)}`);
    }
  }
  return hits;
}

describe('labels are sentence case and untracked', () => {
  it('the detectors flag capitals and wide tracking, and leave tight or normal tracking alone', () => {
    expect(jsxHits('t.jsx', `<span style={{ textTransform: "uppercase" }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ fontVariant: 'small-caps' }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: "0.08em" }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: '0.035em' }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ fontSize: '12px', letterSpacing: '0.04em' }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ fontSize: 18, letterSpacing: '0.05em' }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('t.jsx', `<span style={{ letterSpacing: 2 }}>x</span>`)).toHaveLength(1);
    expect(jsxHits('display_lobby.jsx', `<span style={{ textTransform: 'uppercase' }}>x</span>`)).toHaveLength(1);
    for (const ok of [
      `<span style={{ letterSpacing: '0.03em' }}>x</span>`,
      `<span style={{ fontSize: 18, letterSpacing: '0.04em' }}>x</span>`,
      `<span style={{ letterSpacing: '0.02em', textTransform: 'none' }}>x</span>`,
      `<h1 style={{ letterSpacing: "-0.02em" }}>x</h1>`,
      `<span style={{ letterSpacing: 'normal' }}>x</span>`,
    ]) expect(jsxHits('t.jsx', ok), ok).toEqual([]);
    expect(jsxHits('display_lobby.jsx', `<div style={{ fontSize: '2.6vh', letterSpacing: '0.14em' }}>x</div>`)).toEqual([]);
    expect(jsxHits('display_scoreboard.jsx', `<div style={{ letterSpacing: '0.16em' }}>x</div>`)).toEqual([]);

    expect(cssHits('t.css', '.a { text-transform: uppercase; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { font-variant-caps: all-small-caps; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { letter-spacing: 0.14em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { letter-spacing: 0.04em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { font-size: 12px; letter-spacing: 0.035em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { font: 800 13px/1 var(--f); letter-spacing: 0.04em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a { font-size: 18px; letter-spacing: 0.05em !important; }')).toHaveLength(1);
    expect(cssHits('t.css', '.tvd-a, .badge { letter-spacing: 0.14em; }')).toHaveLength(1);
    expect(cssHits('t.css', '.tvd-a { text-transform: uppercase; }')).toHaveLength(1);
    expect(cssHits('t.css', '.a {\n  font-size: 12px;\n  letter-spacing: 0.05em;\n}')[0]).toMatch(/^letter-spacing 0\.05em in t\.css:3:/);
    for (const ok of [
      '.a { letter-spacing: 0.03em; }',
      '.a { font-size: 9px; letter-spacing: 0.03em; }',
      '.a { font-size: 24px; letter-spacing: 0.04em; }',
      '.a { font-size: 1.5rem; letter-spacing: 0.04em; }',
      '.a { letter-spacing: -0.02em; }',
      '.a { letter-spacing: normal; text-transform: none; }',
      '.a { font-variant-numeric: tabular-nums; }',
      '/* letter-spacing: 0.2em; text-transform: uppercase */',
      '.tvd-side__lab { font-size: 2.6vh; letter-spacing: 0.14em; }',
      '.lobby-cell .msb-vs { letter-spacing: 0.1em; }',
      '@media (min-width: 900px) { .tvd-upnext-note { letter-spacing: 0.16em; } }',
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
