// Every character the UI writes is drawn by a font the app ships, so a page
// looks the same on every device (operator decision 2026-11-18: "ship the
// fonts: same everywhere"). The shipped faces are Archivo (its Latin file, plus
// any Archivo face listed with a unicode-range), JetBrains Mono, and "BC
// Symbols", the symbol subsets whose unicode-range lists live in
// web-mobile/css/styles.css. A Latin letter or a symbol (Unicode script Latin
// or Common) outside them would be drawn by whatever font the device has, so it
// fails here; add it to a subset (and to its unicode-range) or write it another
// way. Emoji are not judged here (they are drawn by Icon, which the emoji gate
// pins), and neither are other scripts: a name typed in Japanese is the
// device's to draw.
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve, relative } from 'node:path';
import { walk, stripComments } from '../../check-helpers.mjs';

const ROOT = resolve(__dirname, '../../..');
const MOBILE_CSS = resolve(ROOT, 'web-mobile/css/styles.css');
const CLI_CSS = resolve(ROOT, 'web/css/styles.css');

// The code points web-mobile/vendor/fonts/archivo/archivo-latin.woff2 holds
// (its cmap, read with fontTools: TTFont(path).getBestCmap()). Read it again
// if that file is ever replaced.
const ARCHIVO_LATIN = 'U+000D, U+0020-007E, U+00A0-00FF, U+2013-2014, U+2018-201A, U+201C-201D, U+2022, U+2026, U+20AC';

function parseRanges(text) {
  return text.split(',').map((r) => r.trim()).filter(Boolean).map((r) => {
    const [a, b] = r.replace(/^U\+/i, '').split('-');
    return [parseInt(a, 16), parseInt(b || a, 16)];
  });
}

// The unicode-range of every @font-face of the named families.
function faceRanges(css, families) {
  const out = [];
  for (const m of css.matchAll(/@font-face\s*{([^}]*)}/g)) {
    const fam = /font-family:\s*"([^"]+)"/.exec(m[1]);
    const range = /unicode-range:\s*([^;]+);/.exec(m[1]);
    if (fam && families.includes(fam[1]) && range) out.push(...parseRanges(range[1]));
  }
  return out;
}

function covered(ranges, cp) {
  return ranges.some(([a, b]) => cp >= a && cp <= b);
}

const JUDGED = /[\p{Script=Latin}\p{Script=Common}]/u;
// Common-script marks that belong to Japanese, Chinese or Korean text (the
// Katakana long-vowel mark ー in a sample name): another script's, not judged.
const CJK = /[\p{Script_Extensions=Hiragana}\p{Script_Extensions=Katakana}\p{Script_Extensions=Han}\p{Script_Extensions=Hangul}]/u;
// Emoji (drawn as pictures by default) are the emoji gate's; a symbol drawn as
// text by default (⚠ ⏱ ★, or a bare ⚙ or 🗓) is judged here, so it must be in
// a shipped font.
const EMOJI = /\p{Emoji_Presentation}/u;

function uncoveredIn(file, kind, ranges) {
  const lines = stripComments(readFileSync(file, 'utf8'), kind).split('\n');
  const hits = [];
  lines.forEach((line, i) => {
    for (const ch of line) {
      const cp = ch.codePointAt(0);
      if (cp < 0x80 || !JUDGED.test(ch) || CJK.test(ch) || EMOJI.test(ch) || cp === 0xfe0f) continue;
      if (!covered(ranges, cp)) hits.push(`${relative(ROOT, file)}:${i + 1} U+${cp.toString(16).toUpperCase().padStart(4, '0')} ${ch}`);
    }
  });
  return hits;
}

describe('shipped fonts cover what the UI writes', () => {
  const css = stripComments(readFileSync(MOBILE_CSS, 'utf8'), 'css');
  const ranges = [...parseRanges(ARCHIVO_LATIN), ...faceRanges(css, ['BC Symbols', 'Archivo'])];

  it('the tournament app writes no character outside them', () => {
    const files = [...walk(resolve(ROOT, 'web-mobile/js'))];
    expect(files.length).toBeGreaterThan(50);
    const hits = files.flatMap((f) => uncoveredIn(f, 'js', ranges));
    hits.push(...uncoveredIn(resolve(ROOT, 'web-mobile/index.html'), 'html', ranges));
    expect(hits).toEqual([]);
  });

  it('the CLI web UI writes no character outside them', () => {
    const files = [...walk(resolve(ROOT, 'web/js'))];
    const hits = files.flatMap((f) => uncoveredIn(f, 'js', ranges));
    hits.push(...uncoveredIn(resolve(ROOT, 'web/index.html'), 'html', ranges));
    expect(hits).toEqual([]);
  });

  // A stylesheet writes text through `content:`, as a character or as an
  // escape ("\2197"); both are drawn by a font like any other text.
  function contentCharsIn(file) {
    const text = stripComments(readFileSync(file, 'utf8'), 'css');
    const hits = [];
    for (const m of text.matchAll(/content:\s*(["'])((?:\\.|(?!\1).)*)\1/g)) {
      const decoded = m[2].replace(/\\([0-9a-fA-F]{1,6})\s?/g, (_, hex) => String.fromCodePoint(parseInt(hex, 16)));
      for (const ch of decoded) {
        const cp = ch.codePointAt(0);
        if (cp < 0x80 || !JUDGED.test(ch) || EMOJI.test(ch)) continue;
        if (!covered(ranges, cp)) hits.push(`${relative(ROOT, file)} content U+${cp.toString(16).toUpperCase().padStart(4, '0')} ${ch}`);
      }
    }
    return hits;
  }

  it('neither stylesheet writes a character outside them through content:', () => {
    expect([...contentCharsIn(MOBILE_CSS), ...contentCharsIn(CLI_CSS)]).toEqual([]);
  });

  it('reads an escaped content: character', () => {
    // A self-check on the escape decoding: U+2600 is in no subset.
    const css = '.x::after { content: "\\2600"; }';
    const decoded = /content:\s*"([^"]*)"/.exec(css)[1].replace(/\\([0-9a-fA-F]{1,6})\s?/g, (_, h) => String.fromCodePoint(parseInt(h, 16)));
    expect(decoded).toBe('☀');
    expect(covered(ranges, decoded.codePointAt(0))).toBe(false);
  });

  it('the CLI stylesheet lists the same symbol ranges as the app', () => {
    const cli = stripComments(readFileSync(CLI_CSS, 'utf8'), 'css');
    const key = (rs) => rs.map(([a, b]) => `${a}-${b}`).sort();
    expect(key(faceRanges(cli, ['BC Symbols', 'Archivo']))).toEqual(key(faceRanges(css, ['BC Symbols', 'Archivo'])));
  });

  it('judges a symbol the fonts do not hold', () => {
    // A self-check: U+2600 (☀) is in no subset, so the scan reports it.
    expect(covered(ranges, 0x2600)).toBe(false);
    expect(covered(ranges, 0x2192)).toBe(true);
    expect(covered(ranges, 0x2082)).toBe(true);
  });
});
