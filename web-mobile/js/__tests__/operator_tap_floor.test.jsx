// bc-tp44: the operator's score-editor and lineup-panel controls reach the
// 44px floor under a coarse pointer, through --tap-floor in CLASSES (0px on a
// fine pointer, so a laptop keeps its density), never an inline style.
//
// jsdom lays nothing out, so this pins the rules in the stylesheet (the
// pattern of viewer_watchlist_panel.test.jsx); the acceptance is the browser
// measurement at 1180x820 with a coarse pointer.

import { describe, it, expect } from 'vitest';
import { cssBlock, readStylesheet } from './helpers/source.js';

const css = readStylesheet();

const block = (selector) => {
  const b = cssBlock(css, selector);
  expect(b, `rule ${selector} exists`).not.toBeNull();
  return b;
};

// Every `@media (pointer: coarse) { ... }` body, found by brace depth.
function coarseBlocks(source) {
  const out = [];
  const opener = '@media (pointer: coarse) {';
  let from = 0;
  for (;;) {
    const at = source.indexOf(opener, from);
    if (at < 0) return out;
    let depth = 1;
    let i = at + opener.length;
    while (i < source.length && depth > 0) {
      if (source[i] === '{') depth += 1;
      else if (source[i] === '}') depth -= 1;
      i += 1;
    }
    out.push(source.slice(at + opener.length, i - 1));
    from = i;
  }
}
const coarseRule = (selector) => {
  const esc = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  for (const body of coarseBlocks(css)) {
    const m = new RegExp(`(?:^|\\s)${esc}\\s*\\{([^}]*)\\}`).exec(body);
    if (m) return m[1];
  }
  return null;
};

describe('operator tap floors (bc-tp44)', () => {
  it('floors the encho stepper buttons on both axes', () => {
    const b = block('.encho-row__btn');
    expect(b).toMatch(/min-width: max\(28px, var\(--tap-floor\)\)/);
    expect(b).toMatch(/min-height: max\(28px, var\(--tap-floor\)\)/);
  });

  it('floors the encho label, which is the checkbox\'s tap target', () => {
    expect(block('.encho-row__label')).toMatch(/min-height: var\(--tap-floor\)/);
    const box = coarseRule('.encho-row__label input[type="checkbox"]');
    expect(box, 'coarse checkbox rule').not.toBeNull();
    expect(box).toMatch(/width: 20px/);
    expect(box).toMatch(/height: 20px/);
  });

  it('floors the lineup name bar and lets the input fill it', () => {
    const bar = block('.lineup-name__bar');
    expect(bar).toMatch(/min-height: max\(36px, var\(--tap-floor\)\)/);
    expect(bar).toMatch(/padding: 0 6px/);
    expect(bar).toMatch(/align-items: stretch/);
    const input = block('.lineup-name__bar .pmf__input');
    expect(input).toMatch(/align-self: stretch/);
    // The input is the tap target: it floors its own height (border-box, so
    // exactly the floor) rather than relying on the bar, whose floor counts
    // its borders and left an empty box's input 2px short.
    expect(input).toMatch(/min-height: var\(--tap-floor\)/);
    expect(block('.lineup-name__bar .lineup-name__clear')).toMatch(/align-self: center/);
  });

  it('floors the lineup panel Rename link under a coarse pointer, above the .btn floors', () => {
    expect(block('.btn.lineup-rename-btn')).toMatch(/min-height: 0/);
    const coarse = coarseRule('.btn.lineup-rename-btn');
    expect(coarse, 'coarse rename rule').not.toBeNull();
    expect(coarse).toMatch(/min-height: var\(--tap-floor\)/);
    expect(coarse).toMatch(/min-width: var\(--tap-floor\)/);
  });

  it('floors the collapse caret button on both axes without widening the gutter', () => {
    const coarse = coarseRule('.tsm-caret-btn');
    expect(coarse, 'coarse caret rule').not.toBeNull();
    expect(coarse).toMatch(/min-height: var\(--tap-floor\)/);
    expect(coarse).toMatch(/min-width: var\(--tap-floor\)/);
    expect(coarse).toMatch(/margin: -10px -5px/);
  });
});
