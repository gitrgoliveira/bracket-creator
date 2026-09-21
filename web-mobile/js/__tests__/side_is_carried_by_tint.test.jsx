// bc-sccl, operator decision 2026-09-20: on the three operator surfaces that
// show a matchup as two cells, the side is carried by a TINTED CELL, not by a
// SHIRO/AKA text badge.
//
// DESIGN.md §4 used to offer three treatments for a dense row -- a tinted cell,
// an always-on coloured header, or a filled badge -- and these surfaces used the
// badge, the smallest of the three, on the densest lists in the app (the badge
// option left §4 with the badges themselves). Moving
// them to the tint also makes them agree with PoolNumberedMatchRow
// (viewer_standings.jsx), which renders tinted and badge-less on the very same
// court-console screen, and it gives the competitor number chip its side colour
// indirectly: the chip is color: inherit (bc-lbty) and now sits on a tinted
// ground.
//
// The badge was also the side's only TEXT. §4 requires that colour never be the
// only signal, so two things replace it: the Shiro hatch (a non-colour cue) and
// an sr-only label on EVERY converted surface.
//
// That last part was wrong in the first cut of this file, which exempted the two
// court-console surfaces "because the cell already carries an aria-label". It
// does, and that label names nothing: the cell is a bare div, so its role is
// generic, and ARIA prohibits author naming on it. The exemption was asserted
// here as `not.toMatch(/sr-only/)`, so the gate held the gap open. Both now
// carry the span and it is pinned positively.
//
// Removed markup and deleted CSS leave no failing test behind on their own,
// which is why this file exists (same reason as running_state_static.test.jsx).
// These are SOURCE and STYLESHEET assertions. The surfaces that have a mount
// harness are pinned in their own render suites (admin_scoring_modal.render,
// admin_scoring_engi.render, admin_shiaijo.test); what a source sweep catches
// that a fixture cannot is the badge coming back or the fill class dropping
// out of ANY module, including one no fixture mounts. A regression puts the
// badge back or drops the fill class, and that is what these catch.

import { describe, it, expect } from 'vitest';
// Shared with the sibling suite: readCode strips comments, because every
// removal site explains itself in one and a raw read matches its own
// explanation.
import { readSource as read, readCode as codeOf, cssBlock, readStylesheet, modules, renderedLiteral } from './helpers/source.js';

const css = readStylesheet();

const block = (selector) => {
  const b = cssBlock(css, selector);
  expect(b, `rule ${selector} exists`).not.toBeNull();
  return b;
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

  it('keeps the Shiro hatch in the compact score-editor host', () => {
    // The individual editor is ALWAYS compact (admin_scoring_individual.jsx
    // 1210/1215) and every kachinuki editor is, so this is the host where the
    // hatch has to hold. A `background:` shorthand there silently reset the
    // background-image the base rule paints, which is how the editors shipped
    // the badge removal without the one non-colour cue it relies on.
    const b = block('.editor-modal--compact .sb-side--shiro');
    expect(b).toMatch(/repeating-linear-gradient\(\s*-45deg/);
    expect(b).toContain('var(--shiro-hatch)');
    expect(b, 'the shorthand resets background-image').not.toMatch(/^\s*background:/m);
  });

  // Pins the PRIMITIVE, not the spelling. This used to require the exact
  // substring `<base>--shiro side-fill--shiro`, so it pinned two class names
  // being adjacent in that order inside one literal: reordering them, or
  // moving the fill into the SideCell that now emits it, failed a test whose
  // message is about the tint being applied.
  for (const [file, base] of SURFACES) {
    it(`${base} renders both sides through SideCell`, () => {
      const src = read(file);
      expect(src).toMatch(/<SideCell side="shiro"/);
      expect(src).toMatch(/<SideCell side="aka"/);
      expect(src, `${base} keeps its own geometry class`).toContain(`${base}--shiro`);
      expect(src).toContain(`${base}--aka`);
      // The fill arrives FROM the primitive on these surfaces, so it must not
      // also be hand-typed -- two sources for one fact is what this removes.
      expect(src, 'the fill class is the primitive\'s job here')
        .not.toMatch(/side-fill--(shiro|aka)/);
    });
  }

  it('renders no SHIRO/AKA text badge, and its styling is gone', () => {
    for (const [file] of SURFACES) {
      expect(read(file), `${file} still renders a badge`).not.toMatch(/se-color-badge/);
    }
    // The class had no other user, so leaving the rules behind would be dead CSS.
    expect(css).not.toMatch(/se-color-badge/);
  });

  // .bc-color-badge was the watchlist card's side badge and the last one left
  // in the tree. bc-wlhc replaced that card with tinted rows, so it lost its
  // only renderer -- but it was the one badge this sweep did not name, so its
  // ~24 lines survived while every sibling's were deleted, under a comment
  // saying "Used by viewer_watchlist.jsx only" and a CLAUDE.md sentence calling
  // it the ruling's standing exception. Dead CSS plus a doc telling the next
  // agent to keep it.
  it('leaves no .bc-color-badge behind either, in markup or in the sheet', () => {
    for (const f of ['viewer_watchlist.jsx', 'bracket.jsx', 'viewer_match.jsx']) {
      expect(read(f), `${f} still renders the watchlist side badge`).not.toMatch(/bc-color-badge/);
    }
    expect(css, 'the rules outlived their only renderer').not.toMatch(/bc-color-badge/);
  });

  // The label is no longer hand-typed anywhere: SideCell emits it with the
  // tint, so a surface cannot ship one without the other. That is the whole
  // point of the primitive -- two surfaces shipped the tint with the side
  // announced to nobody, each having reached for an aria-label the element's
  // role does not permit.
  it('gets its side label from the primitive on every converted surface', () => {
    const CONVERTED = [
      'admin_schedule_score_editor.jsx', 'admin_shiaijo.jsx', 'admin_scoring_engi.jsx',
      'admin_schedule_page.jsx', 'viewer_schedule.jsx',
    ];
    for (const file of CONVERTED) {
      const src = read(file);
      expect(src, `${file} does not use SideCell`).toMatch(/<SideCell side="shiro"/);
      expect(src, `${file} does not use SideCell`).toMatch(/<SideCell side="aka"/);
    }
    // The surfaces that paint their own tint take the label alone. Pinned
    // POSITIVELY too: the sweep below only forbids a hand-typed label, and
    // passes just as happily when there is no label at all, so without this
    // both viewer_match.jsx rows could drop their span and the public schedule
    // would ship colour-only under a green suite.
    const LABEL_ONLY = [
      ['admin_scoring_individual.jsx', /<SideLabel side=\{s\.color\} \/>/],
      ['admin_scoring_team.jsx', /<SideLabel side=\{s\.color\} \/>/],
      ['viewer_match.jsx', /<SideLabel side="shiro" \/>/],
      ['viewer_match.jsx', /<SideLabel side="aka" \/>/],
      ['viewer_standings.jsx', /<SideLabel side="shiro" \/>/],
      ['viewer_standings.jsx', /<SideLabel side="aka" \/>/],
    ];
    for (const [file, re] of LABEL_ONLY) {
      expect(read(file), `${file} lost its SideLabel`).toMatch(re);
    }
    // The lists above pin the surfaces this change edited. This pins the
    // RULE: no module may hand-type a side label, so a NEW surface cannot ship
    // one either. The enumerated form passed for every file it did not name,
    // which is how viewer_standings.jsx kept "Shiro: " and viewer_match.jsx
    // kept "Shiro:" -- the exact two spellings side_cell.jsx cites as its
    // reason to exist -- through the commit that introduced the owner. Only
    // the owner is excluded, by name; there is no other holdout.
    const HOLDOUTS = ['side_cell.jsx'];
    const handTyped = modules().filter(
      (f) => !HOLDOUTS.includes(f) && /sr-only">\s*(Shiro|Aka)/.test(codeOf(f))
    );
    expect(handTyped, 'the side label has one emitter: SideLabel').toEqual([]);
    // The CLASS half of the same rule, repo-wide rather than for the three
    // SURFACES above: the fill class is spelled by the owner alone. Exactly
    // one module may take it without the label, the watchlist hero, because
    // it names the side in VISIBLE text (CLAUDE.md's size exception); it must
    // do so through sideFillClass and still render that word.
    const handTypedClass = modules().filter((f) => f !== 'side_cell.jsx' && /side-fill--/.test(codeOf(f)));
    expect(handTypedClass, 'the fill class has one owner: side_cell.jsx').toEqual([]);
    const classCallers = modules().filter((f) => f !== 'side_cell.jsx' && /sideFillClass\(/.test(codeOf(f)));
    expect(classCallers, 'sideFillClass has one sanctioned caller').toEqual(['viewer_watchlist.jsx']);
    expect(codeOf('viewer_watchlist.jsx'), 'the hero must still name the side visibly').toMatch(/wl-hero__side-lbl/);
    expect(modules().length, 'the sweep must actually find the modules').toBeGreaterThan(50);
    // ...and the primitive really emits it, so the loop above is not vacuous.
    expect(codeOf('side_cell.jsx')).toMatch(/<span className="sr-only">\{sideWord\(side\)\}: <\/span>/);
    // ...unconditionally. The surfaces cannot opt out because there is no
    // opt-out: a default-true `label` prop shipped once with zero callers,
    // which is an untested switch on the guarantee this file exists to make.
    expect(codeOf('side_cell.jsx'), 'the label opt-out is back').not.toMatch(/label\s*=\s*true/);
    // The inert channel is gone rather than left beside the real one: an
    // aria-label on these role=generic cells was never announced.
    expect(read('admin_shiaijo.jsx')).not.toMatch(/shiaijo-(qrow|sides)__side[^>]*aria-label/);
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
    // These rows take the SHARED fill at the --mid pitch. They used to carry a
    // byte-identical private copy of that gradient, added by the same change
    // that introduced the modifier -- the seventh copy the .side-fill--*
    // comment says nobody adds. Nothing else paints .tw-match__name's
    // background, so collapsing it moved no pixel; that is why this asserts on
    // the modifier rather than on a per-surface class.
    // \s* because this declaration wraps the angle onto the next line.
    expect(block('.side-fill--shiro.side-fill--mid')).toMatch(/repeating-linear-gradient\(\s*-45deg/);
    expect(block('.side-fill--aka')).toContain('background: var(--red-soft)');
    expect(css, 'the private copy is back').not.toMatch(/\.tw-match__name--(shiro|aka)\s*\{/);
    for (const f of ['admin_schedule_page.jsx', 'viewer_schedule.jsx']) {
      const src = read(f);
      expect(src, `${f} still renders an A/S square`).not.toMatch(/tw-match__badge/);
      expect(src).toMatch(/<SideCell side="shiro" density="mid"/);
      expect(src).toMatch(/<SideCell side="aka" density="mid"/);
    }
    expect(css, 'the A/S square styling is dead CSS now').not.toMatch(/tw-match__badge/);
  });

  it('drops the engi badge, whose card was already tinted', () => {
    const src = read('admin_scoring_engi.jsx');
    expect(src).not.toMatch(/engi-side__badge/);
    // Its own fill too (7/8px pitch), so fill={false} and the label from the
    // primitive. The literal this used to pin moved into side_cell.jsx.
    expect(src).toMatch(/<SideCell side="shiro" fill=\{false\}/);
    expect(src).toMatch(/<SideCell side="aka" fill=\{false\}/);
    expect(css).not.toMatch(/engi-side__badge/);
    // The fill it relies on must survive: nothing else names the side there.
    expect(block('.engi-side--aka')).toContain('background: var(--red-soft)');
    expect(block('.engi-side--shiro')).toMatch(/repeating-linear-gradient/);
  });
});

// The engi editor showed NO competitor number at all until 2026-09-20 (spotted
// by the operator reviewing the tint change above). An engi PAIR is one
// participant with one number, so the chip rides the FIRST member's line, which
// is what every other engi surface does (viewer_standings.jsx,
// viewer_competition.jsx). Without it this was the one scoring surface where an
// operator could not match the card in front of them to the bout sheet.
describe('the engi editor shows the competitor number', () => {
  const src = () => read('admin_scoring_engi.jsx');

  // Only the OWNER is pinned at the source. Where the chip lands, and that the
  // pair's second member gets none, is asserted on the rendered editor in
  // render/admin_scoring_engi.render.test.jsx: this used to pin the two JSX
  // lines character for character, which reddened on a neutral rewrite
  // (`m.sideB && m.sideB.number`) and stayed green if the whole side was
  // wrapped in `{false && ...}`.
  it('renders each side through NumberedName, the one owner of the chip rule', () => {
    expect(src()).toMatch(/import \{ NumberedName \} from '\.\/numbered_name\.jsx'/);
    expect(src()).toMatch(/<NumberedName side="shiro"/);
    expect(src()).toMatch(/<NumberedName side="aka"/);
  });
});

// The TV board carries the side WITHOUT a heading (operator ruling 2026-09-20).
// It has no tint to rely on, so the two cues that survive across a hall do the
// work instead: POSITION (Shiro left, Aka right, fixed by kendo convention) and
// COLOUR (each name and its IV/PW in --ink-1 vs --red). The SHIRO/AKA words
// were a third statement of the same fact on the surface with the least
// vertical space. No sr-only replacement: a projector/OBS board has no
// screen-reader audience, unlike the operator consoles that did take one.
describe('the TV board names no side', () => {
  const src = () => read('display_scoreboard.jsx');

  it('renders no SHIRO/AKA heading', () => {
    expect(src()).not.toMatch(/TermD name="(shiro|aka)"/);
    expect(codeOf('display_scoreboard.jsx')).not.toMatch(renderedLiteral('SHIRO|AKA'));
  });

  it('still separates the sides by colour, which is now load-bearing', () => {
    // If these two ever became the same colour the board would carry the side
    // by position alone, which is what the heading used to back up.
    expect(src()).toMatch(/color: "var\(--ink-1\)"[^}]*\}\}>\{repShiro/);
    expect(src()).toMatch(/color: "var\(--red\)"[^}]*\}\}>\{repAka/);
  });
});
