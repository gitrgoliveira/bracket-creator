// The backup page's audit fixes (bc-bkrs): an aria-disabled button stays
// readable, the page's controls reach the tap floor on a touch screen, and the
// breadcrumb above it is tappable and legible.
//
// jsdom lays nothing out, so this pins the rules in the stylesheet (the
// pattern of operator_tap_floor.test.jsx); the acceptance is the browser
// measurement on a phone.

import { describe, it, expect } from 'vitest';
import { cssBlock, readStylesheet } from './helpers/source.js';

const css = readStylesheet();

const block = (selector) => {
  const b = cssBlock(css, selector);
  expect(b, `rule ${selector} exists`).not.toBeNull();
  return b;
};

describe('backup page styles', () => {
  it('draws an aria-disabled button at full opacity with a muted fill, so its words stay readable', () => {
    const b = block('.btn[aria-disabled="true"]');
    expect(b).not.toMatch(/opacity/);
    expect(b).toMatch(/background: var\(--surface-2\)/);
    expect(b).toMatch(/color: var\(--ink-2\)/);
    // The hover shares the rule, so a primary button's hover fill never shows.
    expect(b).toMatch(/\.btn\[aria-disabled="true"\]:hover/);
    // A truly disabled button can be faded: it is not operable.
    expect(block('.btn:disabled')).toMatch(/opacity: 0\.6/);
  });

  it('floors the copy name box and the sync note on a touch screen', () => {
    expect(css).toMatch(
      /\.backup-page \.btn,\s*\.backup-scope \.input,\s*\.backup-copy-name,\s*\.backup-sync > summary \{ min-height: var\(--tap-floor\); \}/
    );
    expect(css).toMatch(/^\.backup-choice \{ min-height: max\(36px, var\(--tap-floor\)\); \}/m);
  });

  it('grows a crumb\'s tap area on a touch screen without moving the line, and inks its separator like the crumbs', () => {
    expect(css).toMatch(
      /@media \(pointer: coarse\) \{\s*\.crumbs button \{ padding: 15px 0; margin: -15px 0; \}\s*\}/
    );
    expect(block('.crumbs .sep')).toMatch(/color: var\(--ink-3\)/);
  });

  it('lets a card title be a heading without the heading\'s own margin', () => {
    expect(block('.card__title')).toMatch(/margin: 0;/);
  });
});
