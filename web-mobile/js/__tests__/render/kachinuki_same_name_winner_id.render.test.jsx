// bc-pnum: two opposing fighters may legally share a display name, and a
// kachinuki bout row records its winner as a NAME. When both sides hold the
// same name that name cannot say who won, and the comparison's case order
// silently handed every such bout to Aka -- a coin flip written into the
// standings (IV), which decide the encounter.
//
// Operator ruling: "this should only use the IDs". The only channel that can
// carry the answer is the SIDE the operator picked, stamped as that side's
// squad member id at score time. The server cannot recover it afterwards:
// state.SubMatchResult.ResolveMemberWinnerID derives the id from the row's
// names and rightly refuses exactly this case.
//
// So this test pins the PRODUCER, mounting the real TeamScoreEditorModal
// through the same ScoreEditorModal dispatcher production uses, scoring a
// point and ending the encounter. It then runs the captured patch through
// the real toBackendMatchResult, because a value the serializer drops is
// worth nothing: an earlier attempt at this same defect shipped green and
// changed nothing, and that is the failure this second assertion exists to
// catch.
import React from 'react';
import { render, act, fireEvent, screen } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
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
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

const originals = {};
let ScoreEditorModal;

beforeAll(async () => {
  for (const [k, v] of Object.entries(STUBBED_GLOBALS)) {
    originals[k] = { had: k in window, value: window[k] };
    window[k] = v;
  }
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => {
  for (const [k, orig] of Object.entries(originals)) {
    if (orig.had) window[k] = orig.value;
    else delete window[k];
  }
});

beforeEach(() => {
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
    fetchSquads: vi.fn().mockResolvedValue({}),
  };
});

// A RUNNING kachinuki encounter whose current bout pairs two fighters called
// "Yamada" -- one per team, which the roster rules allow. Each side carries
// its own squad member id, stamped when the pairing was appended.
const AKA_MEMBER = 'm-aka-1';
const SHIRO_MEMBER = 'm-shiro-1';

function sameNameKachinukiMatch(overrides = {}) {
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A' },
    sideB: { id: 'team-B', name: 'Team B' },
    subResults: [
      {
        position: 1,
        sideA: 'Yamada', sideB: 'Yamada',
        sideAMemberId: AKA_MEMBER, sideBMemberId: SHIRO_MEMBER,
        ipponsA: [], ipponsB: [],
      },
    ],
    ...overrides,
  };
}

async function renderEditor(onSubmit) {
  await act(async () => {
    render(
      <ScoreEditorModal
        match={sameNameKachinukiMatch()}
        onClose={vi.fn()}
        onSubmit={onSubmit}
        password="secret"
      />
    );
  });
}

// The two ippon button groups render in board order: shiro (side B) left,
// aka (side A) right, per the FIK sheet. Index 0 is therefore shiro's "M".
function ipponButtons(side) {
  const groups = document.querySelectorAll('.team-sub-match__btns');
  const group = side === 'shiro' ? groups[0] : groups[1];
  return Array.from(group.querySelectorAll('button'));
}

async function scoreAndEnd(side) {
  const submitted = [];
  const onSubmit = vi.fn().mockImplementation(async (patch) => { submitted.push(patch); });
  await renderEditor(onSubmit);
  const m = ipponButtons(side).find((b) => b.textContent.trim() === 'M');
  expect(m, 'expected an M button for ' + side).toBeTruthy();
  await act(async () => { fireEvent.click(m); });
  // End match is a two-tap confirm.
  await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-end-match-button')); });
  await act(async () => { fireEvent.click(screen.getByTestId('kachinuki-end-match-button')); });
  expect(onSubmit).toHaveBeenCalled();
  return submitted[submitted.length - 1];
}

describe('bc-pnum: a same-name kachinuki bout records the winner by member id', () => {
  it('stamps the SHIRO member id when shiro scores, not the aka-first name order', async () => {
    const patch = await scoreAndEnd('shiro');
    const bout = (patch.subResults || []).find((s) => s.position === 1);
    expect(bout, 'the bout must reach the wire').toBeTruthy();
    // The scoreline proves which side the click landed on, so a swapped
    // button order fails here rather than silently inverting the assertion
    // below.
    expect(bout.ipponsB).toEqual(['M']);
    expect(bout.ipponsA).toEqual([]);
    expect(bout.winner).toBe('Yamada');
    expect(bout.winnerMemberId).toBe(SHIRO_MEMBER);
    // And it survives the real serializer, which is where the previous
    // attempt at this defect died.
    const wire = toBackendMatchResult(patch);
    expect(wire.subResults.find((s) => s.position === 1).winnerMemberId).toBe(SHIRO_MEMBER);
  });

  it('stamps the AKA member id when aka scores', async () => {
    const patch = await scoreAndEnd('aka');
    const bout = (patch.subResults || []).find((s) => s.position === 1);
    expect(bout.ipponsA).toEqual(['M']);
    expect(bout.ipponsB).toEqual([]);
    expect(bout.winnerMemberId).toBe(AKA_MEMBER);
  });
});
