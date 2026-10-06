// bc-tmfd: the team sheet pins its running total and its actions.
//
// At the operator's iPad size the IV/PW band used to sit after every bout row
// and the footer actions below the fold. The team header and the band are now
// one sticky unit (.team-sheet-pin) ahead of the bouts, and the footer is the
// pinned dock. jsdom lays nothing out, so this pins the DOM ORDER and the
// stylesheet rules; the acceptance is the browser measurement at 1180x820.

import React from 'react';
import { render, act, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
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

async function mount({ teamSize = 5, teamMatchType = 'fixed', variant } = {}) {
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
  // One entry per selector of every rule, comments dropped (they carry braces),
  // wherever an @media block nests it. A selector's last compound is what the
  // rule styles, so a rule for something INSIDE a class is not a rule for it.
  const rules = [...css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)].flatMap(
    ([, selectors, decls]) => selectors.split(',').map((selector) => ({ selector: selector.trim(), decls })),
  );
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
    const bar = block('.editor-modal--team > .editor-modal__body > .team-sheet-pin:first-child');
    expect(bar).toMatch(/(?:^|[\s;])top:\s*calc\(-1 \* var\(--editor-body-pad-top\)\)/);
    expect(bar).toMatch(/margin-top:\s*calc\(-1 \* var\(--editor-body-pad-top\)\)/);
    expect(bar).toMatch(/padding-top:\s*var\(--editor-body-pad-top\)/);
  });
});
