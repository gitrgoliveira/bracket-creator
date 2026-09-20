// bc-sccl, operator decision 2026-09-20: on the three operator surfaces that
// show a matchup as two cells, the side is carried by a TINTED CELL, not by a
// SHIRO/AKA text badge.
//
// DESIGN.md §4 offers three treatments for a dense row -- a tinted cell, an
// always-on coloured header, or a filled badge -- and these surfaces used the
// badge, the smallest of the three, on the densest lists in the app. Moving
// them to the tint also makes them agree with PoolNumberedMatchRow
// (viewer_standings.jsx), which renders tinted and badge-less on the very same
// court-console screen, and it gives the competitor number chip its side colour
// indirectly: the chip is color: inherit (bc-lbty) and now sits on a tinted
// ground.
//
// The badge was also the side's only TEXT. §4 requires that colour never be the
// only signal, so two things replace it: the Shiro hatch (a non-colour cue) and
// an sr-only label on the scores list. The two court-console surfaces already
// carry aria-label on the cell, so they get no sr-only span -- it would
// double-announce.
//
// Removed markup and deleted CSS leave no failing test behind on their own,
// which is why this file exists (same reason as running_state_static.test.jsx).
// These are SOURCE and STYLESHEET assertions: all three surfaces mount over the
// API or inside a court console, so rendering them here would exercise their
// fetch harnesses rather than the treatment. A regression puts the badge back
// or drops the fill class, and that is what these catch.

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { dirname, resolve } from 'path';
import { fileURLToPath } from 'url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const read = (p) => readFileSync(resolve(__dirname, '..', p), 'utf8');
const css = readFileSync(resolve(__dirname, '..', '..', 'css', 'styles.css'), 'utf8');

// The declarations of one top-level rule, by exact selector.
const block = (selector) => {
  const start = css.indexOf(`\n${selector} {`);
  expect(start, `rule ${selector} exists`).toBeGreaterThan(-1);
  return css.slice(start, css.indexOf('}', start));
};

// Every surface that was converted, with the side classes it must now apply.
const SURFACES = [
  ['admin_schedule_score_editor.jsx', 'score-edit-row__side'],
  ['admin_shiaijo.jsx', 'shiaijo-qrow__side'],
  ['admin_shiaijo.jsx', 'shiaijo-sides__side'],
];

describe('the side is carried by a tinted cell', () => {
  it('defines the fill once, Shiro hatched and Aka flat red-soft', () => {
    // The hatch is the non-colour cue that lets the text badge go, so it is
    // pinned as a gradient rather than just "some background".
    expect(block('.side-fill--shiro')).toMatch(/repeating-linear-gradient\(-45deg/);
    expect(block('.side-fill--shiro')).toContain('var(--shiro-hatch)');
    expect(block('.side-fill--aka')).toContain('background: var(--red-soft)');
  });

  for (const [file, base] of SURFACES) {
    it(`${base} applies both fill classes`, () => {
      const src = read(file);
      expect(src).toContain(`${base}--shiro side-fill--shiro`);
      expect(src).toContain(`${base}--aka side-fill--aka`);
    });
  }

  it('renders no SHIRO/AKA text badge, and its styling is gone', () => {
    for (const [file] of SURFACES) {
      expect(read(file), `${file} still renders a badge`).not.toMatch(/se-color-badge/);
    }
    // The class had no other user, so leaving the rules behind would be dead CSS.
    expect(css).not.toMatch(/se-color-badge/);
  });

  it('replaces the badge TEXT with an sr-only label on the scores list', () => {
    // Only there: the two court-console cells already carry aria-label, and a
    // second label would be announced twice.
    const scores = read('admin_schedule_score_editor.jsx');
    expect(scores).toMatch(/<span className="sr-only">Shiro: <\/span>/);
    expect(scores).toMatch(/<span className="sr-only">Aka: <\/span>/);
    expect(read('admin_shiaijo.jsx')).not.toMatch(/sr-only">(Shiro|Aka)/);
  });
});

// The tint darkens what sits on it. --ink-3 secondary text measures 4.83:1 on a
// white card but 4.09:1 on --red-soft, under the 4.5 floor for this 11px line,
// so both tinted surfaces move their dojo line to --ink-2 (8.69:1). Pinned
// because a later "tidy the greys" pass would otherwise silently reintroduce a
// contrast failure.
describe('text on a tinted cell keeps its contrast', () => {
  it('darkens the dojo line to --ink-2 on both tinted surfaces', () => {
    expect(block('.score-edit-row__side .dojo')).toContain('color: var(--ink-2)');
    expect(css).toMatch(/\.shiaijo-sides__side \.dojo \{[^}]*color: var\(--ink-2\)/);
  });
});

// Two more surfaces converted in the same pass (operator decision 2026-09-20).
// The compact schedule rows STACK their two sides, so they take the treatment
// .vsched-item__side--* already gives the stacked public schedule rows rather
// than the left/right form used above. The engi card needed no new fill at all:
// it already carried one, and only the badge came off.
describe('the stacked and engi surfaces carry the side the same way', () => {
  it('tints both compact schedule rows and drops the A/S squares', () => {
    // \s* because this declaration wraps the angle onto the next line.
    expect(block('.tw-match__name--shiro')).toMatch(/repeating-linear-gradient\(\s*-45deg/);
    expect(block('.tw-match__name--aka')).toContain('background: var(--red-soft)');
    for (const f of ['admin_schedule_page.jsx', 'viewer_schedule.jsx']) {
      const src = read(f);
      expect(src, `${f} still renders an A/S square`).not.toMatch(/tw-match__badge/);
      expect(src).toContain('tw-match__name--shiro');
      expect(src).toContain('tw-match__name--aka');
      expect(src).toMatch(/<span className="sr-only">Shiro: <\/span>/);
      expect(src).toMatch(/<span className="sr-only">Aka: <\/span>/);
    }
    expect(css, 'the A/S square styling is dead CSS now').not.toMatch(/tw-match__badge/);
  });

  it('drops the engi badge, whose card was already tinted', () => {
    const src = read('admin_scoring_engi.jsx');
    expect(src).not.toMatch(/engi-side__badge/);
    expect(src).toMatch(/<span className="sr-only">Shiro: <\/span>/);
    expect(src).toMatch(/<span className="sr-only">Aka: <\/span>/);
    expect(css).not.toMatch(/engi-side__badge/);
    // The fill it relies on must survive: nothing else names the side there.
    expect(block('.engi-side--aka')).toContain('background: var(--red-soft)');
    expect(block('.engi-side--shiro')).toMatch(/repeating-linear-gradient/);
  });
});
