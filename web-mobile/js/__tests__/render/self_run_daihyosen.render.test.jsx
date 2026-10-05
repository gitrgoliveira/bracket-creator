// bc-dhas (operator decision: "Participants run it"): on a self-run tournament
// the public score sheet runs the representative bout (daihyosen) of a tied
// knockout team match like any bout. A competitor adds it, scores it, finishes
// the match on the winner it decides, and removes one added by mistake, with
// no organiser password: the public page sends an empty one. A hantei stays
// the organiser's, so the public editor offers none, on the representative
// bout or on an individual match.
//
// Mounted through the real door, MatchViewerModal and "Report result". The
// flows below pin what the public editor sends; the server's side of the
// ruling is pinned by internal/mobileapp/self_run_daihyosen_test.go.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';
import { toBackendMatchResult } from '../../api_serializers.jsx';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
  API: {},
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let MatchViewerModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_lineup.jsx');
  await import('../../admin_scoring_modal.jsx');
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
});

afterAll(() => restoreGlobals());

const AKA = { id: 'team-A', name: 'Kodokan', number: 'T1' };
const SHIRO = { id: 'team-B', name: 'Mumeishi', number: 'T2' };
// Three bouts fought and tied: one win each and a draw.
const FOUGHT = [
  { position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kodokan', decision: '' },
  { position: 2, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'], winner: 'Mumeishi', decision: '' },
  { position: 3, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake' },
];
const REP_BOUT = { position: -1, sideA: 'Kodokan', sideB: 'Mumeishi', ipponsA: [], ipponsB: [], winner: '', decision: 'daihyosen' };

const knockoutTeamMatch = (subResults = FOUGHT) => ({
  id: 'm1', compId: 'c1', compName: 'Teams', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
  compKind: 'team', teamSize: 3, compFormat: 'knockout', teamMatchType: 'fixed',
  sideA: AKA, sideB: SHIRO, subResults,
});

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(window, 'alert').mockImplementation(() => {});
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({ id: 'c1', config: { format: 'knockout', players: [] } }),
    fetchSquads: vi.fn(),
    fetchLineupInForce: vi.fn(async () => null),
    putMatchLineup: vi.fn(),
    // Every write lands, as the server now answers the public page.
    recordScore: vi.fn(async (_c, _id, patch) => ({ status: patch.status })),
    recordDaihyosen: vi.fn(async () => ({ ...knockoutTeamMatch(), subResults: [...FOUGHT, REP_BOUT] })),
    removeDaihyosen: vi.fn(async () => ({ ...knockoutTeamMatch(), subResults: FOUGHT })),
    hasPendingTerminalWrite: () => false,
    notePendingEdit: () => () => {},
  };
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

async function settle(times = 8) {
  for (let i = 0; i < times; i++) {
    await act(async () => { await Promise.resolve(); });
  }
}

async function openEditor(match, onClose = vi.fn(), squads = {}) {
  await act(async () => {
    render(<MatchViewerModal match={match} onClose={onClose} tournament={{ mode: 'self-run', competitions: [{ id: 'c1', squads }] }} compId="c1" />);
  });
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
  await settle();
  return onClose;
}

const repBoutRow = () => [...document.querySelectorAll('.team-sub-match')][FOUGHT.length];
const ipponButton = (row, color, letter) =>
  [...row.querySelectorAll(`.team-sub-match__side--${color} button.ipt-btn`)].find((b) => b.textContent === letter);
const markSlot = (row, color, letter) =>
  [...row.querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].find((b) => b.textContent === letter);

// What the last score write put on the wire, through the real serializer.
function lastWire() {
  const [, , patch, , match] = window.API.recordScore.mock.calls.at(-1);
  return toBackendMatchResult(patch, match);
}
const repBoutOf = (wire) => (wire.subResults || []).find((s) => s.position === -1);

async function addRepBout() {
  await act(async () => { fireEvent.click(screen.getByTestId('scoring-modal-daihyosen-button')); });
  await settle();
}

describe('a participant runs the representative bout of a tied knockout team match (bc-dhas)', () => {
  it('adds it, scores it and finishes the match on the winner it decides', async () => {
    const onClose = await openEditor(knockoutTeamMatch());
    await addRepBout();

    expect(window.API.recordDaihyosen).toHaveBeenCalledWith('c1', 'm1', '', 0);
    expect(repBoutRow(), 'the representative bout is on the sheet').toBeTruthy();
    expect(screen.getByTestId('team-daihyosen-remove'), 'Remove is offered while it is unscored').toBeTruthy();
    expect(screen.queryByTestId('team-daihyosen-hantei-arm'), 'no hantei is offered').toBeNull();
    expect(screen.queryByText(/hantei/i)).toBeNull();

    await act(async () => { fireEvent.click(ipponButton(repBoutRow(), 'aka', 'M')); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    const scored = repBoutOf(lastWire());
    expect(scored).toMatchObject({ decision: 'daihyosen', ipponsA: ['M'], ipponsB: [] });
    expect(screen.queryByTestId('team-daihyosen-hantei-row'), 'nothing is left to offer once it is scored').toBeNull();

    await act(async () => { fireEvent.click(screen.getByText('Finish')); });
    await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
    await settle();
    const finished = lastWire();
    expect(finished).toMatchObject({ status: 'completed', winner: 'Kodokan', decision: '' });
    expect(repBoutOf(finished)).toMatchObject({ decision: 'daihyosen', winner: 'Kodokan', ipponsA: ['M'], ipponsB: [] });
    expect(window.API.recordScore.mock.calls.every(([, , , password]) => password === '')).toBe(true);
    expect(window.alert).not.toHaveBeenCalled();
    expect(onClose, 'the finished match closes the editor').toHaveBeenCalled();
  });

  // Another device removed the representative bout this sheet still shows. A
  // finish rests on that bout, so the server refuses it (409 no_daihyosen)
  // with a sentence, which the public page shows as it is, keeping the sheet.
  it('a finish refused because the bout was removed elsewhere shows the server\'s sentence', async () => {
    const sentence = "This match's representative bout was removed on another device. Check the scores and finish again.";
    window.API.recordScore = vi.fn(async (_c, _id, patch) => {
      if (patch.status === 'completed') throw new Error(sentence);
      return { status: patch.status };
    });
    // The page logs the refusal as well as showing it.
    const logError = vi.spyOn(console, 'error').mockImplementation(() => {});
    try {
      const onClose = await openEditor(knockoutTeamMatch([...FOUGHT, { ...REP_BOUT, ipponsA: ['M'], winner: 'Kodokan' }]));

      await act(async () => { fireEvent.click(screen.getByText('Finish')); });
      await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
      await settle();

      expect(window.alert).toHaveBeenCalledWith(sentence);
      expect(onClose, 'the sheet stays open').not.toHaveBeenCalled();
    } finally {
      logError.mockRestore();
    }
  });

  // The server refuses a second representative bout (daihyosen_exists): one
  // added on another device a moment before would otherwise sit beside it.
  it('says so when the bout was added on another device first', async () => {
    window.API.recordDaihyosen = vi.fn(async () => { throw new Error('daihyosen_exists'); });
    await openEditor(knockoutTeamMatch());
    await addRepBout();

    expect(document.querySelector('[data-testid="team-editor-error"]').textContent)
      .toContain('This match already has a representative bout');
    expect(window.alert).not.toHaveBeenCalled();
  });

  // The server refuses a participant's add or remove on a match that is not
  // running (409; result_finalized once it has finished), so the sheet offers
  // them only while it runs, following the live match as it changes.
  it('offers Add and Remove only while the match is running', async () => {
    const tournament = { mode: 'self-run', competitions: [{ id: 'c1', squads: {} }] };
    const at = (status, subResults) => ({ ...knockoutTeamMatch(subResults), status });
    let view;
    await act(async () => {
      view = render(<MatchViewerModal match={at('scheduled', FOUGHT)} onClose={vi.fn()} tournament={tournament} compId="c1" />);
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    await settle();
    const rerender = async (match) => {
      await act(async () => {
        view.rerender(<MatchViewerModal match={match} onClose={vi.fn()} tournament={tournament} compId="c1" />);
      });
      await settle();
    };
    expect(screen.queryByTestId('scoring-modal-daihyosen-button'), 'not before the match starts').toBeNull();

    await rerender(at('running', FOUGHT));
    expect(screen.getByTestId('scoring-modal-daihyosen-button'), 'while it runs').toBeTruthy();

    await rerender(at('running', [...FOUGHT, REP_BOUT]));
    expect(screen.getByTestId('team-daihyosen-remove'), 'while it runs').toBeTruthy();

    // The organiser ends it meanwhile (a withdrawal keeps the unscored bout).
    await rerender({ ...at('completed', [...FOUGHT, REP_BOUT]), winner: AKA, decision: 'kiken-voluntary', decisionBy: 'shiro' });
    expect(screen.queryByTestId('team-daihyosen-remove'), 'not once it has finished').toBeNull();
    expect(screen.queryByTestId('team-daihyosen-hantei-row')).toBeNull();
  });

  it('removes it right after a scoring tap: the save made first lands, then the remove', async () => {
    await openEditor(knockoutTeamMatch());
    await addRepBout();
    window.API.recordScore.mockClear();

    // A point struck and taken back off: the edit is still owed when Remove is tapped.
    await act(async () => { fireEvent.click(ipponButton(repBoutRow(), 'aka', 'M')); });
    await act(async () => { fireEvent.click(markSlot(repBoutRow(), 'aka', 'M')); });
    await act(async () => { fireEvent.click(screen.getByTestId('team-daihyosen-remove')); });
    await settle();

    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    // The row is back to what the server holds, so the editor has nothing to
    // say about its points and may leave them out; it still sends the row.
    const saved = repBoutOf(lastWire());
    expect(saved, 'the save made first carries the representative bout').toMatchObject({ decision: 'daihyosen' });
    expect([...(saved.ipponsA || []), ...(saved.ipponsB || [])]).toEqual([]);
    expect(window.API.removeDaihyosen).toHaveBeenCalledWith('c1', 'm1', '', 0);
    expect(window.alert).not.toHaveBeenCalled();
    expect(document.querySelector('[data-testid="team-editor-error"]'), 'no refusal is shown').toBeNull();
    expect(repBoutRow(), 'the row is gone').toBeFalsy();
    expect(screen.getByTestId('scoring-modal-daihyosen-button'), 'and it can be added again').toBeTruthy();
  });
});

// The organiser's verdict on the representative bout, as the page receives it
// once recorded (normalizeMatch derives decidedByHantei from the mark).
const DECIDED = { ...REP_BOUT, winner: 'Kodokan', ipponsA: ['Ht'], decidedByHantei: true };
// The same sentence the server refuses a participant's change with, read from
// the fixture both sides are pinned to.
const DECIDED_NOTE = JSON.parse(readFileSync(
  resolve(__dirname, '..', '..', '..', '..', 'internal', 'mobileapp', 'testdata', 'rep_bout_hantei_messages.json'),
  'utf8',
)).recorded;
const enabledControls = (row) => [...row.querySelectorAll('button')].filter((b) => !b.disabled).map((b) => b.textContent);
const named = (p, names) => names.map((name, i) => ({ id: `${p}${i + 1}`, index: i + 1, name }));

describe('a hantei stays the organiser\'s on the public score sheet (bc-dhas)', () => {
  it('a representative bout the judges decided shows its verdict and offers nothing to change', async () => {
    // Team members on hand, so a fighter picker would have names to offer.
    await openEditor(knockoutTeamMatch([...FOUGHT, DECIDED]), vi.fn(), {
      'team-A': named('a', ['Ren Abe', 'Kai Mori', 'Yui Sato']),
      'team-B': named('b', ['Mei Ito', 'Sho Ueda', 'Rin Ota']),
    });

    const chip = screen.getByTestId('team-daihyosen-ht-aka');
    expect(chip.disabled, 'the Ht mark is shown, not offered for undo').toBe(true);
    expect(enabledControls(repBoutRow()), 'no scoring control on the bout').toEqual([]);
    expect(repBoutRow().querySelectorAll('input, select'), 'no fighter picker on the bout').toHaveLength(0);
    expect(screen.getByTestId('team-daihyosen-decided-note').textContent).toBe(DECIDED_NOTE);
    expect(screen.queryByTestId('team-daihyosen-remove')).toBeNull();
    expect(screen.queryByText('Decide by hantei…')).toBeNull();
    expect(screen.queryByTestId('team-daihyosen-hantei-cancel')).toBeNull();
    expect(screen.queryByTestId('scoring-modal-encho-pill'), 'nor its overtime').toBeNull();
    await act(async () => { fireEvent.click(chip); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).not.toHaveBeenCalled();
  });

  // The server takes a participant's write only when it sends the recorded
  // verdict back as it is (internal/mobileapp/self_run_daihyosen_test.go,
  // "sending it back as it is", pins the same row shape).
  it('takes up the organiser\'s verdict while the sheet is open, and a later tap elsewhere saves it back', async () => {
    const tournament = { mode: 'self-run', competitions: [{ id: 'c1', squads: {} }] };
    let view;
    await act(async () => {
      view = render(<MatchViewerModal match={knockoutTeamMatch([...FOUGHT, REP_BOUT])} onClose={vi.fn()} tournament={tournament} compId="c1" />);
    });
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Report result' })); });
    await settle();
    expect(screen.getByTestId('team-daihyosen-remove'), 'the unscored bout can still be removed').toBeTruthy();
    expect(screen.getByTestId('scoring-modal-encho-pill'), 'and fought on in overtime').toBeTruthy();

    // The organiser records the hantei on another device; the page's live row follows.
    await act(async () => {
      view.rerender(<MatchViewerModal match={knockoutTeamMatch([...FOUGHT, DECIDED])} onClose={vi.fn()} tournament={tournament} compId="c1" />);
    });
    await settle();
    expect(screen.getByTestId('team-daihyosen-decided-note').textContent).toBe(DECIDED_NOTE);
    expect(enabledControls(repBoutRow()), 'no scoring control on the bout').toEqual([]);
    expect(screen.queryByTestId('scoring-modal-encho-pill'), 'nor its overtime').toBeNull();

    const bout1 = [...document.querySelectorAll('.team-sub-match')][0];
    await act(async () => { fireEvent.click(bout1.querySelector('[aria-label="Add a SHIRO foul"]')); });
    await act(async () => { vi.advanceTimersByTime(AUTOSAVE_DEBOUNCE_MS + 50); });
    await settle();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
    expect(repBoutOf(lastWire()), 'the verdict goes back as it was recorded').toMatchObject({
      sideA: 'Kodokan', sideB: 'Mumeishi', winner: 'Kodokan', ipponsA: ['Ht'], ipponsB: [], decision: 'daihyosen',
    });
    expect(window.alert).not.toHaveBeenCalled();
  });

  it('a tied individual match offers no hantei, and points the participant at the organizer', async () => {
    await openEditor({
      id: 'm2', compId: 'c1', compName: 'Individuals', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
      compFormat: 'knockout', sideA: { id: 'p-a', name: 'Aoki' }, sideB: { id: 'p-b', name: 'Baba' },
      ipponsA: ['M'], ipponsB: ['K'],
    });
    expect(screen.queryByTestId('scoring-modal-hantei-row')).toBeNull();
    expect(screen.queryByText('Decide by hantei…')).toBeNull();
    expect(screen.getByText('Needs a winner').closest('button').title)
      .toBe('Needs a winner: fight encho, then ask the organizer for a hantei if still tied.');
  });
});
