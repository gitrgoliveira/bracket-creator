// The app shows no emoji (operator decision: it must look the same on every
// device, and an emoji is drawn by each device's own emoji font). A glyph the
// UI needs is an Icon (icon.jsx), drawn by the app itself. This gate reads
// every top-level js/*.jsx module, comments blanked (check-helpers.mjs), and
// fails on any character with Emoji_Presentation, any character followed by
// U+FE0F (the variation selector that asks for the emoji form of a text
// symbol such as the warning sign), and any pictograph outside the Basic
// Multilingual Plane: those (the calendar U+1F5D3 among them) lack
// Emoji_Presentation yet phones draw them as emoji. Text-presentation symbols
// the app sets in its own fonts (the warning sign, check, cross, arrows,
// approx) stay text.
//
// One string is allowed: the document.title prefix in viewer_alerts.jsx. The
// browser's own chrome draws a tab title, so neither a page font nor an SVG
// can reach it.
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
import { stripComments } from '../../check-helpers.mjs';

const JS = resolve(__dirname, '..');

const ALLOWED = [
  { file: 'viewer_alerts.jsx', text: '"🔴 NOW: "' },
];

const EMOJI = /\p{Emoji_Presentation}|.️|(?=\p{Extended_Pictographic})[\u{10000}-\u{10FFFF}]/u;

function emojiHits(file, src) {
  let code = stripComments(src);
  for (const a of ALLOWED) {
    if (a.file === file) code = code.split(a.text).join(' '.repeat(a.text.length));
  }
  const hits = [];
  code.split('\n').forEach((line, i) => {
    const m = line.match(EMOJI);
    if (m) hits.push(`${file}:${i + 1}: ${JSON.stringify(m[0])} in ${line.trim().slice(0, 120)}`);
  });
  return hits;
}

describe('the app UI draws its own icons, never emoji', () => {
  it('the detector flags emoji and leaves text symbols alone', () => {
    for (const s of ['🏆', '⏳', '✅', '⚠️', '🗓', '🥋']) expect(EMOJI.test(s), s).toBe(true);
    for (const s of ['⚠', '✓', '✕', '≈', '₂', '←', '→', '▸', '●']) expect(EMOJI.test(s), s).toBe(false);
  });

  it('no non-test js/*.jsx module carries an emoji outside a comment', () => {
    const files = readdirSync(JS).filter((f) => f.endsWith('.jsx')).sort();
    expect(files.length).toBeGreaterThan(50);
    const hits = files.flatMap((f) => emojiHits(f, readFileSync(resolve(JS, f), 'utf8')));
    expect(hits, `Use an Icon (icon.jsx) instead:\n${hits.join('\n')}`).toEqual([]);
  });

  // A text symbol that stands in for an icon (a caret, a chevron, a star, a
  // house, a pencil, a heavy tick, a paging angle) is drawn by Icon too
  // (operator decision H1, round ship-jsxs-hallmark-1; Hallmark gate 30a). The
  // check, cross, warning sign and arrows are not listed: they stay text where
  // they ARE text (a pressed toggle's "✓ Fusensho", the arrow keys a hint
  // names), and each icon use of them was converted by hand.
  const ICON_ONLY = /[▶▸▾◂⌂★☆✎✔‹›]/u;
  it('no non-test js/*.jsx module types a symbol that only ever acts as an icon', () => {
    const files = readdirSync(JS).filter((f) => f.endsWith('.jsx')).sort();
    const hits = [];
    for (const f of files) {
      stripComments(readFileSync(resolve(JS, f), 'utf8')).split('\n').forEach((line, i) => {
        const m = line.match(ICON_ONLY);
        if (m) hits.push(`${f}:${i + 1}: ${JSON.stringify(m[0])} in ${line.trim().slice(0, 120)}`);
      });
    }
    expect(hits, `Draw it with Icon (icon.jsx) instead:\n${hits.join('\n')}`).toEqual([]);
  });

  // The cross cannot join ICON_ONLY: it is the multiplication sign where it
  // is text ("Overtime ×2", the encho counter's "×N"). A control whose WHOLE
  // content is a bare × is an icon, though (a close or clear button), and is
  // drawn by Icon name="x". Caught as `>×<` (or `>{"×"}<`) on one line, or a
  // line holding nothing but the cross, between a tag's open and close. A
  // cross that STARTS a text node and is followed by words ("× Remove this
  // bout") is the same icon set before a label, caught as `>× Word` on one
  // line or a line that begins with the cross and then a letter.
  const BARE_CROSS = />\s*(?:×|\{\s*["'`]×["'`]\s*\})\s*</u;
  const LEADING_CROSS = />\s*×\s*\p{L}/u;
  const LEADING_CROSS_LINE = /^×\s*\p{L}/u;
  const crossHit = (line) => BARE_CROSS.test(line)
    || LEADING_CROSS.test(line)
    || LEADING_CROSS_LINE.test(line.trim())
    || line.trim() === '×';
  it('the cross detector flags a cross used as an icon and leaves the multiplication sign alone', () => {
    for (const s of ['<button>×</button>', '<button>{"×"}</button>', '   ×', '<button>× Remove this bout</button>', '                    × Remove this bout']) {
      expect(crossHit(s), s).toBe(true);
    }
    for (const s of ['(E) Overtime ×{encounterEnchoCount}</span>', '<span>Overtime ×2</span>', '<span>×{n}</span>', '<Icon name="x" size={14} />Remove this bout']) {
      expect(crossHit(s), s).toBe(false);
    }
  });
  it('no non-test js/*.jsx module types a × as an icon (a control\'s whole content, or set before its label)', () => {
    const files = readdirSync(JS).filter((f) => f.endsWith('.jsx')).sort();
    const hits = [];
    for (const f of files) {
      stripComments(readFileSync(resolve(JS, f), 'utf8')).split('\n').forEach((line, i) => {
        if (crossHit(line)) {
          hits.push(`${f}:${i + 1}: "×" in ${line.trim().slice(0, 120)}`);
        }
      });
    }
    expect(hits, `Draw it with <Icon name="x" /> (icon.jsx) instead:\n${hits.join('\n')}`).toEqual([]);
  });

  it('each allow-listed string is still where the list says, so the list cannot go stale', () => {
    for (const a of ALLOWED) {
      expect(stripComments(readFileSync(resolve(JS, a.file), 'utf8')), a.file).toContain(a.text);
    }
  });
});
