// bc-sbq: the shiaijo console's Send back to queue clears the whole bout log,
// and its confirm said what would be lost from the court feed alone, which
// lags the editor: a point just struck read "nothing will be lost", and a
// reopened team encounter lost every bout it had fought. Both editors now
// report their own board through onBoardChange, and the console asks it as
// well as the feed (admin_shiaijo.render.test.jsx pins that half). This pins
// the editors' half: what each reports, and that it follows the board.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  resolveRoundIndex: () => 0,
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

const lastReport = (spy) => spy.mock.calls[spy.mock.calls.length - 1][0];

describe('the editors report their board to the host (bc-sbq)', () => {
  it('the individual editor reports the point the moment it is struck', async () => {
    const onBoardChange = vi.fn();
    let view;
    await act(async () => {
      view = render(<ScoreEditorModal
        match={{
          id: 'm1', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A',
          sideA: { id: 'p1', name: 'Yamada' }, sideB: { id: 'p2', name: 'Tanaka' },
          ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0,
        }}
        onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onBoardChange={onBoardChange} password="" />);
    });
    expect(lastReport(onBoardChange)).toEqual({ compId: 'c1', matchId: 'm1', points: 0, fouls: 0, overtime: false, draw: false, bouts: 0 });

    const men = [...view.container.querySelectorAll('button')].find((b) => b.textContent === 'M');
    await act(async () => { fireEvent.click(men); });
    expect(lastReport(onBoardChange)).toMatchObject({ compId: 'c1', matchId: 'm1', points: 1 });
  });

  // Review finding: engi flags are entered with no autosave, so only the
  // board holds them until the match is finished.
  it('the engi editor reports the flags on its board', async () => {
    const onBoardChange = vi.fn();
    await act(async () => {
      render(<ScoreEditorModal
        match={{
          id: 'm3', compId: 'c1', status: 'running', phase: 'pool', poolName: 'Pool A', court: 'A', compEngi: true,
          sideA: { id: 'p1', name: 'Yamada - Sato' }, sideB: { id: 'p2', name: 'Tanaka - Ito' },
          flagsA: 2, flagsB: 1,
        }}
        onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onBoardChange={onBoardChange} password="" />);
    });
    expect(lastReport(onBoardChange)).toMatchObject({ compId: 'c1', matchId: 'm3', flags: 3 });
  });

  it('the team editor reports how many bouts carry a result', async () => {
    const onBoardChange = vi.fn();
    await act(async () => {
      render(<ScoreEditorModal
        match={{
          id: 'm2', compId: 'c1', status: 'running', phase: 'bracket', round: 'Final', court: 'A',
          compKind: 'team', teamSize: 3,
          sideA: { id: 'team-kyoto', name: 'Kyoto' }, sideB: { id: 'team-osaka', name: 'Osaka' },
          subResults: [
            { position: 1, sideA: '', sideB: '', ipponsA: ['M'], ipponsB: [], winner: 'Kyoto', decision: '' },
            { position: 2, sideA: '', sideB: '', ipponsA: [], ipponsB: ['K'], winner: 'Osaka', decision: '' },
          ],
        }}
        onClose={vi.fn()} onSubmit={vi.fn().mockResolvedValue(undefined)} onBoardChange={onBoardChange} password="" />);
    });
    expect(lastReport(onBoardChange)).toMatchObject({ compId: 'c1', matchId: 'm2', bouts: 2 });
  });
});
