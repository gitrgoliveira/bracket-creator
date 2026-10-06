// bc-tmfd: the team sheet pins its running total and its actions.
//
// At the operator's iPad size the IV/PW band used to sit after every bout row
// and the footer actions below the fold. The team header and the band are now
// one sticky unit (.team-sheet-pin) ahead of the bouts, and the footer is the
// pinned dock. jsdom lays nothing out, so this pins the DOM ORDER and the
// stylesheet rules; the acceptance is the browser measurement at 1180x820.

import React from 'react';
import { render, act, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { cssBlock, readStylesheet } from '../helpers/source.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

const ONE_MARK = [{ position: 1, sideA: 'A1', sideB: 'B1', ipponsA: [], ipponsB: ['M'] }];

async function mount({ teamSize = 5, teamMatchType = 'fixed', variant, unreadable = false } = {}) {
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
    id: 'comp1',
    config: { format: 'knockout', teamMatchType, naginata: false, players: [] },
  });
  let utils;
  await act(async () => {
    utils = render(
      <ScoreEditorModal
        match={{
          id: 'm1',
          compId: 'comp1',
          status: 'running',
          phase: 'bracket',
          court: 'A',
          compKind: 'team',
          teamSize,
          compFormat: 'knockout',
          teamMatchType,
          round: 'Semi-final',
          matchNumber: 1,
          sideA: { id: 'team-A', name: 'Team A' },
          sideB: { id: 'team-B', name: 'Team B' },
          subResults: ONE_MARK,
          ...(unreadable ? { subResultsUnreadable: true } : {}),
        }}
        onClose={vi.fn()}
        onSubmit={vi.fn().mockResolvedValue(undefined)}
        password=""
        {...(variant ? { variant } : {})}
      />
    );
  });
  return utils;
}

const follows = (a, b) => Boolean(a.compareDocumentPosition(b) & Node.DOCUMENT_POSITION_FOLLOWING);
const PIN = '.team-sheet-pin';
const HINT = '[data-testid="team-scoring-clear-hint"]';

function expectPinnedOrder(container, { band }) {
  const pin = container.querySelector(PIN);
  expect(pin, 'the pinned wrapper renders').not.toBeNull();
  expect(pin.querySelector('.sb-match'), 'the team header is in the wrapper').not.toBeNull();
  if (band) {
    expect(pin.querySelector('[data-testid="team-summary-result"]'), 'the band is in the wrapper').not.toBeNull();
    expect(pin.querySelector('.team-summary')).not.toBeNull();
    expect(follows(pin.querySelector('.sb-match'), pin.querySelector('.team-summary'))).toBe(true);
  } else {
    expect(pin.querySelector('.team-summary'), 'no band in kachinuki bout mode').toBeNull();
  }
  const firstBout = container.querySelector('.team-sub-match');
  expect(firstBout).not.toBeNull();
  expect(follows(pin, firstBout), 'the wrapper precedes the first bout').toBe(true);
  expect(pin.contains(firstBout)).toBe(false);

  const hint = container.querySelector(HINT);
  expect(hint, 'the clear-a-mark hint renders').not.toBeNull();
  expect(pin.contains(hint), 'the hint stays out of the wrapper').toBe(false);
  expect(follows(pin, hint)).toBe(true);
  expect(follows(hint, firstBout)).toBe(true);

  // The band, when it renders at all, no longer follows the bouts.
  const band_ = container.querySelector('.team-summary');
  if (band_) expect(follows(firstBout, band_)).toBe(false);
}

describe('team editor: the header and the result band are one pinned unit ahead of the bouts', () => {
  it('inline, five-person team match: header and band in the wrapper, which precedes the bouts', async () => {
    const { container } = await mount({ variant: 'inline' });
    expect(container.querySelector('.scoring-panel--team')).not.toBeNull();
    expectPinnedOrder(container, { band: true });
    const foot = container.querySelector('.editor-modal__foot--nav');
    expect(foot.querySelector('.score-nav'), '.score-nav is in the footer').not.toBeNull();
    expect(container.querySelector(PIN).contains(foot)).toBe(false);
  });

  it('the head, with its sync pill and correction pill, stays out of the wrapper', async () => {
    const { container } = await mount({ variant: 'inline' });
    expect(container.querySelector(PIN).contains(container.querySelector('.editor-modal__head'))).toBe(false);
  });

  it('inline, kachinuki bout mode: the wrapper holds the header only and the footer keeps its actions', async () => {
    const { container } = await mount({ variant: 'inline', teamMatchType: 'kachinuki' });
    expectPinnedOrder(container, { band: false });
    const foot = container.querySelector('.editor-modal__foot--nav');
    const inFoot = within(foot);
    expect(inFoot.getByRole('button', { name: 'Record bout' })).not.toBeNull();
    expect(inFoot.getByRole('button', { name: /End match/ })).not.toBeNull();
    expect(screen.queryByTestId('team-summary-result')).toBeNull();
  });

  it('overlay, compact (five-person): same order', async () => {
    const { container } = await mount();
    expect(container.querySelector('.editor-modal--team.editor-modal--compact')).not.toBeNull();
    expectPinnedOrder(container, { band: true });
    expect(container.querySelector('.editor-modal__foot--nav .score-nav')).not.toBeNull();
  });

  it('overlay, roomy (team of six): same order, ahead of the bout list', async () => {
    const { container } = await mount({ teamSize: 6 });
    expect(container.querySelector('.editor-modal--team')).not.toBeNull();
    expect(container.querySelector('.editor-modal--compact')).toBeNull();
    expectPinnedOrder(container, { band: true });
    const scroll = container.querySelector('.team-bouts-scroll');
    expect(follows(container.querySelector(PIN), scroll)).toBe(true);
    expect(container.querySelector(PIN).contains(scroll)).toBe(false);
  });
});

const css = readStylesheet();
const block = (selector) => {
  const b = cssBlock(css, selector);
  expect(b, `rule ${selector} exists`).not.toBeNull();
  return b;
};
// One entry per selector of every rule, comments dropped (they carry braces),
// wherever an @media block nests it. A selector's last compound is what the
// rule styles, so a rule for something INSIDE a class is not a rule for it. A
// list splits at its top-level commas only: the commas inside :is(...) and
// [...] belong to one selector.
const splitSelectors = (list) => {
  const out = [];
  let depth = 0;
  let current = '';
  for (const ch of list) {
    if (ch === '(' || ch === '[') depth += 1;
    if (ch === ')' || ch === ']') depth -= 1;
    if (ch === ',' && depth === 0) {
      out.push(current.trim());
      current = '';
    } else {
      current += ch;
    }
  }
  if (current.trim()) out.push(current.trim());
  return out;
};
const rules = [...css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)].flatMap(
  ([, selectors, decls]) => splitSelectors(selectors).map((selector) => ({ selector, decls })),
);

describe('the stylesheet pins the two bars in the inline team panel (bc-tmfd)', () => {
  it('clips the panel instead of making it a scroll container, so both bars stick to the page', () => {
    expect(block('.scoring-panel--team')).toMatch(/overflow:\s*clip/);
    expect(block('.scoring-panel--team .editor-modal__body')).toMatch(/overflow:\s*visible/);
  });

  it('makes the team footer a bottom dock, after the static rule it overrides', () => {
    const dock = block('.scoring-panel--team .editor-modal__foot--nav');
    expect(dock).toMatch(/position:\s*sticky/);
    expect(dock).toMatch(/bottom:\s*0/);
    expect(dock).toMatch(/z-index:\s*20\b/);
    expect(dock).toMatch(/max-height:\s*45vh/);
    const staticAt = css.indexOf('\n.scoring-panel .editor-modal__foot--nav {');
    expect(staticAt).toBeGreaterThan(-1);
    expect(css.indexOf('\n.scoring-panel--team .editor-modal__foot--nav {')).toBeGreaterThan(staticAt);
  });

  it('keeps the dock under the topbar (30) and above the pin (9) and the name list (8)', () => {
    const z = (sel) => Number(/z-index:\s*(\d+)/.exec(block(sel))[1]);
    expect(z('.scoring-panel--team .editor-modal__foot--nav')).toBeLessThan(z('.topbar-stack'));
    expect(z('.scoring-panel--team .editor-modal__foot--nav')).toBeGreaterThan(z('.team-sheet-pin'));
    expect(z('.editor-modal__body .lineup-name__dropdown')).toBeLessThan(z('.team-sheet-pin'));
  });

  it('stacks the name list below the pinned header inside a score editor, and keeps 60 elsewhere', () => {
    expect(block('.editor-modal__body .lineup-name__dropdown')).toMatch(/z-index:\s*8\b/);
    expect(block('.lineup-name__dropdown')).toMatch(/z-index:\s*60/);
  });

  it('leaves the name list height to LineupNameInput: no CSS max-height on it', () => {
    expect(block('.lineup-name__dropdown')).not.toMatch(/max-height/);
  });

  it('keeps the action row visible at the dock bottom while the content above scrolls', () => {
    const nav = block('.scoring-panel--team .editor-modal__foot--nav .score-nav');
    expect(nav).toMatch(/position:\s*sticky/);
    expect(nav).toMatch(/bottom:\s*0/);
    expect(nav).toMatch(/background:\s*var\(--surface\)/);
  });

  it('falls back to overflow: visible where overflow: clip is unsupported, so the bars still pin', () => {
    expect(css).toMatch(/@supports not \(overflow: clip\)\s*\{\s*\.scoring-panel--team\s*\{\s*overflow:\s*visible;/);
  });

  it('pins the header unit at the top, and the inline scope sits it under the shell topbar', () => {
    const pin = block('.team-sheet-pin');
    expect(pin).toMatch(/position:\s*sticky/);
    expect(pin).toMatch(/top:\s*0/);
    expect(pin).toMatch(/z-index:\s*9\b/);
    expect(block('.scoring-panel--team .team-sheet-pin')).toMatch(/top:\s*var\(--topbar-stack-h/);
  });

  it('leaves the individual and engi inline panels alone: no sticky or clip on the shared panel rules', () => {
    const shared = block('.scoring-panel');
    expect(shared).toMatch(/overflow:\s*hidden/);
    expect(block('.scoring-panel .editor-modal__foot--nav')).toMatch(/position:\s*static/);
  });
});

// The overlay's bout rows flow in .editor-modal__body, a modal's one scroll
// region, under the pinned bar. A rule that hid that body's overflow and made
// the bout list a scroll area of its own left the list only the height the
// pinned bar, a knockout's tie-breaker panel and the rows below the bouts did
// not take: 8px for a team of six at 1180x820, so no bout could be seen. jsdom
// lays nothing out, so these read the rules; the acceptance is the browser
// measurement.
describe('the stylesheet keeps the overlay bouts in the body, under the pinned bar', () => {
  const styling = (cls) =>
    rules.filter(({ selector }) => new RegExp(`\\.${cls}(?![\\w-])[^\\s>+~]*$`).test(selector));
  // Anchored on the property name, so text-overflow is not read as overflow.
  const CLIPS = /(?:^|[\s;])overflow(?:-y)?:[^;]*\b(?:hidden|clip)\b/;
  const OWN_OVERFLOW = /(?:^|[\s;])overflow(?:-y)?:[^;]*\b(?:auto|scroll|hidden|clip)\b/;

  it('leaves the body the scroll region: no rule on an editor body hides its overflow', () => {
    expect(block('.editor-modal__body')).toMatch(/overflow-y:\s*auto/);
    const bodies = styling('editor-modal__body');
    expect(bodies.map((r) => r.selector), 'the sweep finds the body rules').toContain('.editor-modal__body');
    for (const { selector, decls } of bodies) {
      expect(decls, `${selector} hides the body overflow`).not.toMatch(CLIPS);
    }
  });

  it('never makes the bout list a scroll region of its own, which would leave it only the height that remains', () => {
    const lists = styling('team-bouts-scroll');
    expect(lists.length, 'the sweep finds a rule for the wrapper itself, or it proves nothing').toBeGreaterThan(0);
    for (const { selector, decls } of lists) {
      expect(decls, `${selector} gives the bout list its own overflow`).not.toMatch(OWN_OVERFLOW);
    }
  });

  it('names the body top padding in both densities and pads with it, so the bar can cover exactly that much', () => {
    for (const selector of ['.editor-modal__body', '.editor-modal--compact .editor-modal__body']) {
      const body = block(selector);
      expect(body, `${selector} names the top padding`).toMatch(/--editor-body-pad-top:\s*\d+px/);
      expect(body, `${selector} pads with it`).toMatch(/padding:\s*var\(--editor-body-pad-top\)\s/);
    }
  });

  it('sticks the overlay bar over the body padding: top and margin-top pull it up by that much, padding-top keeps its content in place', () => {
    const bar = block('.editor-modal--team > .editor-modal__body > .team-sheet-pin');
    expect(bar).toMatch(/(?:^|[\s;])top:\s*calc\(-1 \* var\(--editor-body-pad-top\)\)/);
    expect(bar).toMatch(/margin-top:\s*calc\(-1 \* var\(--editor-body-pad-top\)\)/);
    expect(bar).toMatch(/padding-top:\s*var\(--editor-body-pad-top\)/);
  });

  // The note shown for a match whose stored data cannot be read is the one thing that
  // can come before the bar in the body. The bar is then the body's second child, and a
  // rule that only reached a first child left it stuck below the body's top padding, so a
  // bout scrolled past showed in a strip above the team names.
  it('reaches the bar when the unreadable-data note comes first, so the strip above the team names is covered then as well', async () => {
    const { container } = await mount({ unreadable: true });
    const body = container.querySelector('.editor-modal--team > .editor-modal__body');
    const children = Array.from(body.children);
    const pin = body.querySelector('.team-sheet-pin');
    expect(children[0].classList.contains('data-issue--editor'), 'the note opens the body').toBe(true);
    expect(children[1], 'and the bar follows it').toBe(pin);
    const pullingUp = rules.filter(({ decls }) => /margin-top:\s*calc\(-1 \* var\(--editor-body-pad-top\)\)/.test(decls));
    expect(pullingUp.length, 'the rule that pulls the bar over the body padding exists').toBeGreaterThan(0);
    for (const { selector } of pullingUp) {
      expect(pin.matches(selector), `${selector} reaches the bar that follows the note`).toBe(true);
    }
  });

  // The bar's box starts that far above its content (the padding it covers), so what
  // comes before it must leave that much clear, or the box would hide the bottom edge of
  // the note. The note carries a bottom margin of its own and the body a gap, in each
  // density; this is the premise the rule above rests on.
  it('leaves the note room above the bar: its bottom margin and the body gap clear the padding the bar covers, in both densities', () => {
    const px = (decls, prop) => Number(new RegExp(`(?:^|[\\s;])${prop}:\\s*(\\d+)px`).exec(decls)?.[1]);
    // The bottom of a margin shorthand: the third value when there are three or four, else the first.
    const marginBottom = (decls) => {
      const values = /(?:^|[\s;])margin:\s*([^;}]+)/.exec(decls)?.[1].trim().split(/\s+/) ?? [];
      return parseFloat(values.length >= 3 ? values[2] : values[0]);
    };
    const noteMargin = marginBottom(block('.data-issue--editor'));
    expect(noteMargin, 'the note has a bottom margin the sweep can read').toBeGreaterThan(0);
    for (const selector of ['.editor-modal__body', '.editor-modal--compact .editor-modal__body']) {
      const body = block(selector);
      const pad = px(body, '--editor-body-pad-top');
      const gap = px(body, 'gap');
      expect(gap, `${selector} has a gap the sweep can read`).toBeGreaterThan(0);
      expect(noteMargin + gap, `${selector}: ${pad}px is covered above the bar, and the note's ${noteMargin}px margin plus the ${gap}px gap must leave it clear`).toBeGreaterThanOrEqual(pad);
    }
  });
});

// A scroll that brings a control only just into view (a keyboard Tab onto it,
// scrollIntoView) leaves it exactly under whichever bar it came from: measured on
// the court console, "Remove this bout" scrolled to the bottom edge had the dock's
// hint text across its centre, so a tap on it landed on the dock. The sheet's own
// content carries scroll margin sized to the bars to keep such a target clear of
// them, and CSS cannot read an element's height, so the sheet publishes the
// heights while it is mounted (published_height.jsx, the mechanism AdminTopbar's
// --topbar-stack-h uses too): on the element whose scroll they affect. The inline
// hosts scroll the DOCUMENT under both bars; an overlay scrolls its body under the
// pinned header alone, its footer sitting outside the scrolling body and covering
// nothing. jsdom lays nothing out, so the heights are stubbed; the acceptance is
// the browser.
describe('the team sheet publishes the heights of the bars the page scrolls under', () => {
  const PIN_H = '--team-pin-h';
  const DOCK_H = '--team-dock-h';
  const ROOT = document.documentElement;
  const DOCK = '.editor-modal__foot--nav';
  const realRect = Element.prototype.getBoundingClientRect;
  const realRO = globalThis.ResizeObserver;
  let heights;
  let observers;

  beforeEach(() => {
    heights = { 'team-sheet-pin': 88, 'editor-modal__foot--nav': 140 };
    observers = [];
    Element.prototype.getBoundingClientRect = function () {
      const cls = Object.keys(heights).find((c) => this.classList && this.classList.contains(c));
      return cls ? { top: 0, bottom: heights[cls], height: heights[cls], left: 0, right: 0, width: 0, x: 0, y: 0 } : realRect.call(this);
    };
    globalThis.ResizeObserver = class {
      constructor(cb) { this.cb = cb; this.observed = []; this.disconnected = false; observers.push(this); }
      observe(el) { this.observed.push(el); }
      disconnect() { this.disconnected = true; }
    };
  });

  afterEach(() => {
    Element.prototype.getBoundingClientRect = realRect;
    globalThis.ResizeObserver = realRO;
    ROOT.style.removeProperty(PIN_H);
    ROOT.style.removeProperty(DOCK_H);
  });

  const rootValue = (prop) => ROOT.style.getPropertyValue(prop);
  const valueOn = (el, prop) => el.style.getPropertyValue(prop);
  const watching = (el) => observers.find((o) => o.observed.includes(el));

  it('inline: publishes both heights on the document root as measured, before any observer fires', async () => {
    const { container } = await mount({ variant: 'inline' });
    expect(rootValue(PIN_H)).toBe('88px');
    expect(rootValue(DOCK_H)).toBe('140px');
    expect(watching(container.querySelector(PIN)), 'the header unit is observed').toBeDefined();
    expect(watching(container.querySelector(DOCK)), 'and so is the dock').toBeDefined();
    // The page scrolls, not the panel: nothing is published on the panel's body.
    const body = container.querySelector('.editor-modal__body');
    expect(valueOn(body, PIN_H)).toBe('');
    expect(valueOn(body, DOCK_H)).toBe('');
  });

  it('inline: follows each bar as it grows and shrinks (a correction prompt opens in the dock)', async () => {
    const { container } = await mount({ variant: 'inline' });
    heights['editor-modal__foot--nav'] = 210;
    act(() => { watching(container.querySelector(DOCK)).cb([]); });
    expect(rootValue(DOCK_H)).toBe('210px');
    expect(rootValue(PIN_H), 'the header unit is unchanged').toBe('88px');
    heights['team-sheet-pin'] = 64;
    act(() => { watching(container.querySelector(PIN)).cb([]); });
    expect(rootValue(PIN_H)).toBe('64px');
    expect(rootValue(DOCK_H)).toBe('210px');
  });

  it('inline: removes both from the root and stops observing when the sheet unmounts', async () => {
    const { container, unmount } = await mount({ variant: 'inline' });
    const pinObserver = watching(container.querySelector(PIN));
    const dockObserver = watching(container.querySelector(DOCK));
    unmount();
    expect(rootValue(PIN_H)).toBe('');
    expect(rootValue(DOCK_H)).toBe('');
    expect(pinObserver.disconnected).toBe(true);
    expect(dockObserver.disconnected).toBe(true);
  });

  it('overlay: publishes the header unit on the scrolling body, and nothing on the root or for the footer', async () => {
    const { container } = await mount();
    const body = container.querySelector('.editor-modal--team > .editor-modal__body');
    expect(valueOn(body, PIN_H)).toBe('88px');
    expect(rootValue(PIN_H), 'the document is not scrolled under the overlay bar').toBe('');
    expect(rootValue(DOCK_H)).toBe('');
    expect(valueOn(body, DOCK_H), 'the footer sits outside the scrolling body and covers nothing').toBe('');
    expect(watching(container.querySelector(DOCK)), 'so it is not measured').toBeUndefined();
  });

  it('overlay: follows the header unit, and removes it when the sheet unmounts', async () => {
    const { container, unmount } = await mount();
    const body = container.querySelector('.editor-modal--team > .editor-modal__body');
    const observer = watching(container.querySelector(PIN));
    heights['team-sheet-pin'] = 120;
    act(() => { observer.cb([]); });
    expect(valueOn(body, PIN_H)).toBe('120px');
    unmount();
    expect(valueOn(body, PIN_H)).toBe('');
    expect(observer.disconnected).toBe(true);
  });
});

// The clearance is margin on the sheet's CONTENT, never padding on the scroller.
// Padding claims a strip of the viewport, and a bar's own controls (the dock's
// Finish, a button of the topbar) lie inside it, so focusing one scrolled the page
// by about 300px for nothing (measured at 1180x820 with the root padded by the
// bars). Margin on the content's controls leaves those alone. jsdom lays nothing
// out, so these read the rules and run their selectors against the mounted sheets;
// the acceptance is the browser.
describe('the stylesheet keeps the sheet\'s own content clear of the bars with scroll margin', () => {
  const BAR_HEIGHT = /--(?:topbar-stack|team-pin|team-dock)-h/;
  const declares = (decls, prop) => new RegExp(`(?:^|[\\s;])${prop}:`).test(decls);
  const margins = () => rules.filter((r) => declares(r.decls, 'scroll-margin-top') && BAR_HEIGHT.test(r.decls));
  const inlineRule = () => margins().find((r) => r.selector.startsWith('.scoring-panel--team > .editor-modal__body'));
  const overlayRule = () => margins().find((r) => r.selector.startsWith('.editor-modal--team > .editor-modal__body'));
  const reach = (el) => ({
    inline: Boolean(inlineRule()) && el.matches(inlineRule().selector),
    overlay: Boolean(overlayRule()) && el.matches(overlayRule().selector),
  });
  const CONTROLS = 'button, a[href], input, select, textarea, summary, [tabindex]';
  const controlsIn = (container, scope) => Array.from(container.querySelectorAll(scope)).flatMap((el) => (
    el.matches(CONTROLS) ? [el] : Array.from(el.querySelectorAll(CONTROLS))
  ));
  const named = (el) => el.outerHTML.slice(0, 90);

  it('pads no scroll by a bar height: a bar\'s own control would sit inside the padded strip', () => {
    const padding = rules.filter((r) => declares(r.decls, 'scroll-padding(?:-[a-z]+)?') && BAR_HEIGHT.test(r.decls));
    expect(padding.map((r) => r.selector)).toEqual([]);
  });

  it('inline hosts: the content clears the topbar and the pinned header above it and the dock below it', () => {
    const rule = inlineRule();
    expect(rule, 'a rule for the inline body exists').toBeDefined();
    expect(rule.decls).toMatch(/scroll-margin-top:\s*calc\(var\(--topbar-stack-h,\s*0px\)\s*\+\s*var\(--team-pin-h,\s*0px\)\)/);
    expect(rule.decls).toMatch(/scroll-margin-bottom:\s*var\(--team-dock-h,\s*0px\)/);
  });

  it('overlay hosts: the content clears the pinned header alone, and nothing at the foot', () => {
    const rule = overlayRule();
    expect(rule, 'a rule for the overlay body exists').toBeDefined();
    expect(rule.decls).toMatch(/scroll-margin-top:\s*var\(--team-pin-h,\s*0px\)/);
    expect(rule.decls, 'the footer sits outside the scrolling body and covers nothing').not.toMatch(/scroll-margin-bottom/);
  });

  it('inline sheet: every control in the body is reached by the inline rule alone, and none in the pin, the dock or the head', async () => {
    const { container } = await mount({ variant: 'inline', teamMatchType: 'kachinuki' });
    const content = controlsIn(container, '.editor-modal__body').filter((el) => !el.closest(PIN));
    expect(content.length, 'the sheet has content controls').toBeGreaterThan(0);
    expect(container.querySelector('[data-testid="kachinuki-add-bout-button"]'), 'the button below the bouts is one of them').not.toBeNull();
    for (const el of content) expect(reach(el), named(el)).toEqual({ inline: true, overlay: false });

    const bars = [...controlsIn(container, '.editor-modal__foot--nav'), ...controlsIn(container, '.editor-modal__head')];
    expect(bars.length, 'the dock and the head have controls').toBeGreaterThan(0);
    // The pinned header holds no control today; one added to it must not be reached either.
    const probe = container.querySelector(PIN).appendChild(document.createElement('button'));
    for (const el of [...bars, probe]) expect(reach(el), named(el)).toEqual({ inline: false, overlay: false });
  });

  // The inline sheet's head (its title row and Close) scrolls with the page above
  // the pinned header, so only the topbar can cover it: its controls clear the
  // topbar alone. The overlay's head sits outside its scrolling body.
  const headRule = () => margins().find((r) => r.selector.startsWith('.scoring-panel--team > .editor-modal__head'));
  it('inline sheet: the head\'s controls clear the topbar, and nothing else is reached by that rule', async () => {
    const rule = headRule();
    expect(rule, 'a rule for the inline head exists').toBeDefined();
    expect(rule.decls).toMatch(/scroll-margin-top:\s*var\(--topbar-stack-h,\s*0px\)/);
    expect(rule.decls, 'the head is above the dock, which never covers it').not.toMatch(/scroll-margin-bottom/);
    const { container } = await mount({ variant: 'inline', teamMatchType: 'kachinuki' });
    const head = controlsIn(container, '.editor-modal__head');
    expect(head.length, 'the head has controls').toBeGreaterThan(0);
    for (const el of head) expect(el.matches(rule.selector), named(el)).toBe(true);
    const others = [...controlsIn(container, '.editor-modal__body'), ...controlsIn(container, '.editor-modal__foot--nav')];
    for (const el of others) expect(el.matches(rule.selector), named(el)).toBe(false);
  });

  it('overlay sheet: every control in the body is reached by the overlay rule alone, and none in the pin, the dock or the head', async () => {
    const { container } = await mount({ teamMatchType: 'kachinuki' });
    const content = controlsIn(container, '.editor-modal__body').filter((el) => !el.closest(PIN));
    expect(content.length, 'the sheet has content controls').toBeGreaterThan(0);
    for (const el of content) expect(reach(el), named(el)).toEqual({ inline: false, overlay: true });

    const bars = [...controlsIn(container, '.editor-modal__foot--nav'), ...controlsIn(container, '.editor-modal__head')];
    expect(bars.length, 'the dock and the head have controls').toBeGreaterThan(0);
    const probe = container.querySelector(PIN).appendChild(document.createElement('button'));
    for (const el of [...bars, probe]) expect(reach(el), named(el)).toEqual({ inline: false, overlay: false });
  });
});
