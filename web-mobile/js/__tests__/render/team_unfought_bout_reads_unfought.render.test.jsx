// An unfought bout of a fixed-order team encounter reads as unfought on every
// surface while the encounter runs, and only a Tie tap makes it a draw (the
// tie-marking rule: an empty row is not a tie).
//
// The bug this pins: the team editor's running autosave once wrote every bout
// with no winner as decision "hikiwake", untouched ones included. Every surface
// reads a stored "hikiwake" as a draw, so after the first point the TV board
// and the public match card showed X on every bout still to come, and the
// score sheet, opened again, showed them as "✓ Tie (hikiwake)", where a Tie
// tap then turned the draw OFF. buildPatch writes such a bout with decision ""
// (admin_scoring_team.jsx, subBoutHasBeenPlayed), and since bc-evts stage 5 a
// running match's points are taps the server folds the same way (an unplayed
// bout's decision stays "", engine.writeBoutRow).
//
// The test goes the whole way round: the sheet scores the first point (the
// encounter holds no rows yet; the tap makes its bout's row on the server,
// engine.createsMissingRow), the server folds the tap, and the sheet (opened again), the TV
// board and the public match card all render that stored match. So reverting the
// write rule reddens the display assertions too, not only the wire one
// (team_finish_refuses_unfought_bouts.render.test.jsx pins the wire alone).

import React from 'react';
import { render, act, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { makeFakeOutbox } from '../helpers/fake_outbox.js';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { serverFold } from '../helpers/fake_outbox.js';

let writes;

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordScore: vi.fn(),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    notePendingEdit: vi.fn(),
  },
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal, MatchDetailCard, TvDisplay;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../bracket.jsx');
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  ({ MatchDetailCard } = await import('../../viewer_match.jsx'));
  ({ TvDisplay } = await import('../../display.jsx'));
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  writes = [];
  window.API.recordScore.mockReset();
  window.API.recordScore.mockImplementation((_c, _m, patch) => {
    writes.push(patch);
    return Promise.resolve(undefined);
  });
});

afterEach(() => cleanup());

const MATCH = {
  id: 'Pool A-0', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
  compKind: 'team', teamSize: 3,
  sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
  subResults: [],
};

const click = (el) => act(async () => { fireEvent.click(el); });
const sheetRow = (i) => [...document.querySelectorAll('.team-sub-match')][i];
const sheetPoint = (i, side, letter) => [...sheetRow(i).querySelectorAll(`.team-sub-match__side--${side} button.ipt-btn`)]
  .find((b) => b.textContent === letter);
const tieButtons = () => [...document.querySelectorAll('[data-testid="scoring-modal-tie-button"]')];

async function mountSheet(match) {
  let view;
  const onSubmit = (p) => window.API.recordScore('c1', match.id, p, '', match);
  await act(async () => {
    view = render(<ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={onSubmit} password="" />);
  });
  return view;
}

// The sheet scores the encounter's first point: the encounter holds no bout
// rows, and the point is a tap alone, which makes its bout's row on the server;
// the server folds it, and that is the match it stores.
const taps = () => window.API.recordTap.mock.calls.map((c) => c[2]);
async function storedAfterFirstPoint() {
  const view = await mountSheet(MATCH);
  await click(sheetPoint(0, 'shiro', 'M'));
  await act(async () => { view.unmount(); });
  expect(writes, 'no score write ahead of the tap').toHaveLength(0);
  const sent = window.API.pendingTaps('c1', MATCH.id);
  expect(taps()).toEqual([{ kind: 'ippon.add', pos: 1, side: 'b', letter: 'M' }]);
  cleanup();
  writes = [];
  return serverFold(MATCH, sent, { team: true, teamSize: 3 }).match;
}

const numbered = (subs) => subs.filter((s) => s.position > 0);

describe('an unfought bout of a running team encounter', () => {
  it('is written with no result, not as a draw', async () => {
    const stored = await storedAfterFirstPoint();
    const rows = numbered(stored.subResults);
    expect(rows.map((s) => s.position), 'only the bout tapped has a row').toEqual([1]);
    expect(rows[0].ipponsB).toEqual(['M']);
    expect(rows[0].decision).toBe('');
  });

  it('reads unfought on the score sheet opened again, and a Tie tap marks it a draw', async () => {
    const stored = await storedAfterFirstPoint();
    await mountSheet(stored);
    // No bout reads as a draw: no X in any row's centre, no Tie switched on.
    expect(document.querySelector('.tsm-draw')).toBeNull();
    expect(tieButtons().length).toBeGreaterThan(0);
    for (const b of tieButtons()) expect(b.textContent).toBe('Tie (hikiwake)');

    // A Tie tap on bout 2 marks it a draw, on screen and on the wire, and
    // leaves bout 3 unfought.
    expect(document.querySelectorAll('.team-sub-match')).toHaveLength(3);
    const tie = sheetRow(1).querySelector('[data-testid="scoring-modal-tie-button"]');
    expect(tie).not.toBeNull();
    await click(tie);
    expect(document.querySelector('.tsm-draw')?.textContent).toBe('X');
    expect(document.body.textContent).toContain('✓ Tie (hikiwake)');
    expect(taps().slice(-1)).toEqual([{ kind: 'bout.decision', pos: 2, value: 'hikiwake' }]);
    expect(writes, 'the tie is a tap, not a write').toHaveLength(0);
    cleanup();
  });

  it('reads unfought on the public match card: no X in any bout centre', async () => {
    const stored = await storedAfterFirstPoint();
    let container;
    await act(async () => { ({ container } = render(<MatchDetailCard match={stored} escapeToClose={false} />)); });
    const centres = [...container.querySelectorAll('[data-testid="team-scoreboard"] .msb-vs')].map((c) => c.textContent);
    expect(centres.length).toBeGreaterThanOrEqual(3);
    expect(centres).not.toContain('X');
  });

  it('reads unfought on the TV board: no X in any bout centre', async () => {
    const stored = await storedAfterFirstPoint();
    const comp = {
      id: 'c1', name: 'Cup', format: 'mixed', withZekkenName: false, kind: 'team', teamSize: 3,
      poolMatches: [stored], bracket: { rounds: [] },
    };
    let container;
    await act(async () => { ({ container } = render(<TvDisplay court="A" tournament={{ name: 'Cup' }} competitions={[comp]} />)); });
    const board = container.querySelector('[data-testid="team-scoreboard"]');
    expect(board).not.toBeNull();
    const centres = [...board.querySelectorAll('.msb-vs')].map((c) => c.textContent);
    expect(centres.length).toBeGreaterThanOrEqual(3);
    expect(centres).not.toContain('X');
  });
});

// bc-evts stage 5: a running team match is scored by taps handed to the outbox
// (API.recordTap); each test gets a fresh fake of that surface, whose pending
// taps the board folds on. Registered last, so it lands on the API the hooks
// above set up.
beforeEach(() => { if (window.API) Object.assign(window.API, makeFakeOutbox().api); });
