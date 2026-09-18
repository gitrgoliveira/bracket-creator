// bc-dnst: a kachinuki row the SERVER appended beyond the team size (row 6+
// of a 5-person team) has no valid lineup position key at all -- a team's
// lineup only ever accepts "1".."teamSize" (or the five FIK names), so
// domain.TeamLineup.ValidatePositions 400s a PUT keyed "6" on a 5-person
// team. Before this fix, once such a row's name had already resolved once
// (e.g. from the server's own winner-stays bout log), re-picking a fighter
// there still routed through the inline lineup PUT (pickPlayer) rather than
// the free/manual path (pickManual) -- exactly the write that 400s.
//
// This mounts the real TeamScoreEditorModal (via the ScoreEditorModal
// dispatcher production uses) with a kachinuki encounter whose CURRENT bout
// is position 6 on a 5-person team, and whose AKA side already carries a
// resolved name (simulating the winner-stays bout log), then re-picks a
// DIFFERENT squad member there. It pins two things: putMatchLineup is never
// called, and the row's member-number label follows the picked member (not
// the row's stale one), which requires the picked member's id to be stored
// locally rather than derived from a name lookup that a blank/renamed
// member could never answer for.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';

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

const SQUAD_A = [
  { id: 'm-kept', index: 1, name: 'Kept Winner' },
  { id: 'm-blank', index: 7, name: '' },
  { id: 'm-fresh', index: 9, name: 'Fresh Fighter' },
];
const SQUAD_B = [{ id: 'm-shiro', index: 1, name: 'Shiro One' }];

let putMatchLineup;

beforeEach(() => {
  window.compMatches = () => [];
  window.compMatchesForCompetition = () => [];
  putMatchLineup = vi.fn();
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'comp1',
      config: { format: 'mixed', teamMatchType: 'kachinuki', naginata: false, players: [] },
    }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup,
    renameTeamMember: vi.fn().mockResolvedValue({ id: 'm-blank', index: 7, name: 'Ito' }),
    recordDecision: vi.fn(),
    fetchSquads: vi.fn().mockResolvedValue({ 'team-A': SQUAD_A, 'team-B': SQUAD_B }),
  };
});

// Bouts 1..5 are already fought (played), bout 6 -- beyond the 5-person
// team's own size -- is the CURRENT bout: AKA already carries a resolved
// name (the winner-stays bout log), SHIRO is still unresolved.
function kachinukiMatchBeyondTeamSize() {
  const playedBouts = [1, 2, 3, 4, 5].map((pos) => ({
    position: pos,
    // bc-dnst: bout 1's stored AKA text is an OLD spelling for the same
    // squad member ('m-kept', SQUAD_A's "Kept Winner"), simulating a rename
    // that happened after this bout was already fought. The read-only row
    // must show the squad's CURRENT name, never this frozen stored text.
    sideA: pos === 1 ? 'Old Winner Name' : `Aka ${pos}`,
    sideAMemberId: pos === 1 ? 'm-kept' : undefined,
    sideB: `Shiro ${pos}`,
    ipponsA: ['M'], ipponsB: [],
  }));
  return {
    id: 'm1',
    compId: 'comp1',
    status: 'running',
    phase: 'pool',
    poolName: 'Pool 1',
    court: 'A',
    compKind: 'team',
    teamSize: 5,
    compFormat: 'mixed',
    teamMatchType: 'kachinuki',
    sideA: { id: 'team-A', name: 'Team A', number: 'T5' },
    sideB: { id: 'team-B', name: 'Team B', number: 'T9' },
    subResults: [
      ...playedBouts,
      {
        position: 6,
        sideA: 'Kept Winner', sideB: '',
        sideAMemberId: 'm-kept',
        ipponsA: [], ipponsB: [],
      },
    ],
  };
}

async function renderEditor() {
  await act(async () => {
    render(
      <ScoreEditorModal
        match={kachinukiMatchBeyondTeamSize()}
        onClose={vi.fn()}
        onSubmit={vi.fn()}
        password="secret"
      />
    );
  });
}

describe('bc-dnst: kachinuki row beyond teamSize routes name picks off the lineup PUT', () => {
  it('re-picking the AKA side never calls putMatchLineup', async () => {
    await renderEditor();

    const akaInput = document.querySelector('.team-sub-match__side--aka input');
    expect(akaInput, 'expected the AKA side to render a typeable name picker').toBeTruthy();

    await act(async () => { fireEvent.focus(akaInput); });

    const options = Array.from(document.querySelectorAll('.team-sub-match__side--aka .pmf__option'));
    const freshOption = options.find((b) => b.textContent.includes('Fresh Fighter'));
    expect(freshOption, 'expected "Fresh Fighter" to be offered from the squad').toBeTruthy();

    await act(async () => { fireEvent.mouseDown(freshOption); });

    expect(putMatchLineup).not.toHaveBeenCalled();

    // The row's member-number label must follow the PICKED member ("T5.9"),
    // not stay on the row's previous one ("T5.1") -- this requires the
    // picked member's id to have been stored and read back directly, since
    // a name-based lookup alone could not tell the two apart if a rename
    // ever made them share a name.
    const label = document.querySelector(
      '.team-sub-match__side--aka [data-testid="team-sub-match-member-label-aka"]'
    );
    expect(label, 'expected a member-number label on the AKA side').toBeTruthy();
    expect(label.textContent).toBe('T5.9');
  });

  // bc-dnst: a name typed over a picked, still-blank member names THAT
  // member: the row keeps the picked id (its number label stays "T5.7") and
  // the squad member is renamed through the API, exactly as the fixed-order
  // path does through the resolver. Without this the typed name dropped the
  // id and the row fell back to a name lookup a blank member can never win.
  it('typing a name over a picked blank member keeps its id and renames the member', async () => {
    await renderEditor();
    const akaInput = document.querySelector('.team-sub-match__side--aka input');
    await act(async () => { fireEvent.focus(akaInput); });
    const blankOption = Array.from(document.querySelectorAll('.team-sub-match__side--aka .pmf__option'))
      .find((b) => b.textContent.includes('T5.7'));
    expect(blankOption, 'expected the blank slot T5.7 to be offered').toBeTruthy();
    await act(async () => { fireEvent.mouseDown(blankOption); });
    // The current bout's AKA side is the one that renders an input (fought
    // rows are read-only), so locate it from the input, not the first row.
    const akaSide = () => document.querySelector('.team-sub-match__side--aka input').closest('.team-sub-match__side--aka');
    expect(akaSide().textContent).toContain('T5.7');

    const akaInput2 = document.querySelector('.team-sub-match__side--aka input');
    await act(async () => {
      fireEvent.focus(akaInput2);
      fireEvent.change(akaInput2, { target: { value: 'Ito' } });
      fireEvent.keyDown(akaInput2, { key: 'Enter' });
    });
    await act(async () => { await Promise.resolve(); });

    expect(putMatchLineup).not.toHaveBeenCalled();
    expect(window.API.renameTeamMember.mock.calls).toEqual([['comp1', 'team-A', 'm-blank', 'Ito', 'secret']]);
    expect(akaSide().textContent).toContain('T5.7');
  });

  // bc-dnst: a rename reaches a bout already fought (renderReadOnlyBout,
  // admin_scoring_team.jsx). Bout 1's stored AKA text ('Old Winner Name')
  // is frozen on the SubMatchResult, but its sideAMemberId ('m-kept')
  // resolves to SQUAD_A's "Kept Winner" (the member's CURRENT name), so
  // the read-only row must display that instead.
  it('a played bout shows the squad member\'s CURRENT name, not the frozen stored spelling', async () => {
    await renderEditor();
    const row = document.querySelector('[data-testid="kachinuki-done-bout-0"]');
    expect(row, 'expected the read-only row for bout 1').toBeTruthy();
    const akaStatic = row.querySelector('.team-sub-match__side--aka .tsm-name__static');
    expect(akaStatic.textContent).toBe('Kept Winner');
    expect(row.textContent).not.toContain('Old Winner Name');
    // Display-only: this substitution must never reach the wire. buildPatch
    // (and the playerNamesForBout it and squadLabelFor both read) is
    // untouched by this change -- only the rendered text swaps, via a
    // reassignment local to renderReadOnlyBout's own return block -- so a
    // save from this screen still carries whatever stored/resolved name
    // playerNamesForBout would have returned before this fix.
  });
});
