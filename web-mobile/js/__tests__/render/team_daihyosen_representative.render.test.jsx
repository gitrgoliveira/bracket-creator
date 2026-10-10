// bc-dhrp: the daihyosen (representative bout) row lets the operator pick each
// team's representative by member id. The row keeps the TEAM names in sideA/sideB
// (placeHt compares the winner against them); the picks ride sideAMemberId /
// sideBMemberId. These tests mount the REAL ScoreEditorModal dispatcher, the same
// route every production mount uses, as team_daihyosen_silence.render.test.jsx does.
import React from 'react';
import { render, act, fireEvent, screen, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { toBackendMatchResult } from '../../api_serializers.jsx';
import { FETCH_TIMEOUT_MS } from '../../write_result.jsx';
import { AUTOSAVE_DEBOUNCE_MS } from '../../admin_scoring_autosave.jsx';

const SQUADS = {
  t1: [{ id: 'm1a', name: 'Alice', index: 1 }, { id: 'm2a', name: 'Brenda', index: 2 }],
  t2: [{ id: 'm1b', name: 'Carol', index: 1 }, { id: 'm2b', name: 'Dana', index: 2 }],
};

const STUBBED_GLOBALS = {
  isHikiwake: (_type) => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: (_kind) => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    fetchSquads: vi.fn(),
    fetchTeamLineup: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    fetchLineupInForce: vi.fn().mockResolvedValue({ saved: false, positions: {} }),
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn().mockResolvedValue({}),
    recordDecision: vi.fn(),
    addTeamMember: vi.fn(),
    renameTeamMember: vi.fn(),
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
  // The real member resolver (resolveMemberIdsForPositions) is the one mint and
  // rename path a typed representative name takes; it is not re-stubbed here.
  await import('../../admin_lineup.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
});

afterAll(() => restoreGlobals());

beforeEach(() => {
  window.API.fetchSquads.mockReset().mockResolvedValue(SQUADS);
  window.API.fetchLineupInForce.mockReset().mockResolvedValue({ saved: false, positions: {} });
  window.API.recordScore.mockClear();
  window.API.addTeamMember.mockReset();
  window.API.renameTeamMember.mockReset();
  window.API.putMatchLineup.mockClear();
});

// Numbered bouts 1..3 are recorded as ties so Finish is allowed (bc-tmfn), as in
// the silence test. The daihyosen row is the one under test.
const FOUGHT_BOUTS = [1, 2, 3].map((position) => ({
  position, sideA: '', sideB: '', ipponsA: [], ipponsB: [], winner: '', decision: 'hikiwake',
}));

const DH_EMPTY = { position: -1, sideA: 'Team A', sideB: 'Team B', decision: 'daihyosen' };

function makeMatch(overrides = {}, { fought = true } = {}) {
  const { subResults = [], ...rest } = overrides;
  return {
    id: 'm-ko-1',
    compId: 'comp1',
    status: 'running',
    phase: 'knockout',
    court: 'A',
    compKind: 'team',
    teamSize: 3,
    sideA: { id: 't1', name: 'Team A' },
    sideB: { id: 't2', name: 'Team B' },
    ...rest,
    subResults: [...(fought ? FOUGHT_BOUTS : []), ...subResults],
  };
}

function dhEntryOf(patch) {
  return (patch.subResults || []).find((s) => s.position === -1);
}

async function mount(match, { selfReport = false, onClose = vi.fn(), prevMatch, nextMatch, onPrev, onNext } = {}) {
  const onSubmit = vi.fn();
  const view = (m) => (
    <ScoreEditorModal match={m} onClose={onClose} onSubmit={onSubmit} password="" selfReport={selfReport}
      prevMatch={prevMatch} nextMatch={nextMatch} onPrev={onPrev} onNext={onNext} />
  );
  let utils;
  await act(async () => { utils = render(view(match)); });
  return { ...utils, onSubmit, onClose, rerenderWith: async (m) => { await act(async () => { utils.rerender(view(m)); }); } };
}

// The daihyosen row's two name boxes, by their aria-label ("Daihyosen SHIRO player").
function dhInputs() {
  return screen.queryAllByLabelText(/^Daihyosen (SHIRO|AKA) player$/);
}
function dhInput(side) {
  return dhInputs().find((el) => el.getAttribute('aria-label').includes(side));
}

async function openDh(side) {
  const input = dhInput(side);
  await act(async () => { fireEvent.focus(input); fireEvent.click(input); });
}

async function pickFromDh(side, name) {
  await openDh(side);
  const opt = await screen.findByText(name, { selector: '.pmf__opt-name' });
  await act(async () => { fireEvent.click(opt.closest('button')); });
  // Let the pick's member wait and write settle.
  await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
}

async function clickFinishTwice() {
  // First tap arms the button; the second commits (buildPatch -> onSubmit).
  await act(async () => { fireEvent.click(screen.getByText('Finish')); });
  await act(async () => { fireEvent.click(screen.getByText('Tap again to finish')); });
}

// The FINISH write: the one onSubmit call whose patch completes the match. A pick
// arms the editor's autosave, a RUNNING write sent through the same onSubmit, and
// its debounce is real time: under load it can land between the pick and Finish.
// So a test that means the finish cannot read calls[0] or count every call.
function finishPatchOf(onSubmit) {
  const finishes = onSubmit.mock.calls.map(([patch]) => patch).filter((patch) => patch.status === 'completed');
  expect(finishes).toHaveLength(1);
  return finishes[0];
}

describe('team daihyosen representative picker (bc-dhrp)', () => {
  it('R1: the daihyosen row renders a name box on each side, and never the team name in it', async () => {
    await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await waitFor(() => expect(dhInputs().length).toBe(2));
    expect(dhInput('SHIRO').value).toBe('');
    expect(dhInput('AKA').value).toBe('');
  });

  it('R2: picking a member records its id on the -1 row, keeps the team names, and names repPicks', async () => {
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    await pickFromDh('SHIRO', 'Carol');
    expect(dhInput('SHIRO').value).toBe('Carol');
    await clickFinishTwice();
    const patch = finishPatchOf(onSubmit);
    const dh = dhEntryOf(patch);
    expect(dh.sideBMemberId).toBe('m1b');
    expect(dh.sideA).toBe('Team A');
    expect(dh.sideB).toBe('Team B');
    // The wire the server gets: the id rides the row and the write names its group.
    const wire = toBackendMatchResult(patch, match);
    const wireDh = wire.subResults.find((s) => s.position === -1);
    expect(wireDh.sideBMemberId).toBe('m1b');
    expect(wireDh.sideB).toBe('Team B');
    expect(wire.changed).toContain('repPicks');
    expect(wire.changed, 'a pick is not a change of the bout').not.toContain('bout:-1');
    // A pick is never a lineup write: "daihyosen" is not a lineup key.
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
  });

  it('R3: a point recorded elsewhere is adopted after the pick round-trips', async () => {
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit, rerenderWith } = await mount(match);
    await pickFromDh('SHIRO', 'Carol');
    // The server echoes the pick back (stamped after it), then another device scores a point.
    const echoed = makeMatch({
      modifiedAt: Date.now() + 60000,
      subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b' }],
    });
    await rerenderWith(echoed);
    const scored = makeMatch({
      modifiedAt: Date.now() + 120000,
      subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b', ipponsB: ['M'] }],
    });
    await rerenderWith(scored);
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.ipponsB).toEqual(['M']);
    expect(dh.sideBMemberId).toBe('m1b');
    expect(dh.sideB).toBe('Team B');
  });

  it('R4: clearing a stored pick omits the id and names repPicks on the next write', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b' }] });
    const { onSubmit } = await mount(match);
    // The stored pick shows as the member's own name, never the team's.
    expect(dhInput('SHIRO').value).toBe('Carol');
    const wrap = dhInput('SHIRO').closest('.lineup-name');
    const clear = within(wrap).getByRole('button', { name: 'Clear player' });
    await act(async () => { fireEvent.click(clear); });
    await clickFinishTwice();
    const patch = finishPatchOf(onSubmit);
    const dh = dhEntryOf(patch);
    expect('sideBMemberId' in dh).toBe(false);
    expect(dh.sideB).toBe('Team B');
    const cleared = toBackendMatchResult(patch, match).changed;
    expect(cleared).toContain('repPicks');
    expect(cleared).not.toContain('bout:-1');
  });

  it('R5: the representative roster includes a member already placed at a numbered position', async () => {
    // Carol is fielded at position 1 of the team's lineup for this match: a placed
    // member the numbered pickers filter out, and the representative picker must not.
    window.API.fetchLineupInForce.mockImplementation(async (_compId, teamId) => (teamId === 't2'
      ? { saved: true, positions: { 1: 'Carol' }, memberIds: { 1: 'm1b' }, sourceMatchId: 'm-ko-1' }
      : { saved: false, positions: {} }));
    const match = makeMatch({
      subResults: [
        { position: 1, sideA: 'Alice', sideB: 'Carol', sideAMemberId: 'm1a', sideBMemberId: 'm1b', ipponsA: [], ipponsB: [], decision: '' },
        DH_EMPTY,
      ],
    }, { fought: false });
    await mount(match);
    await openDh('SHIRO');
    expect(await screen.findByText('Carol', { selector: '.pmf__opt-name' })).toBeTruthy();
  });

  it('R6: a typed name matching no team member mints one through the member resolver and records its id on the row', async () => {
    window.API.addTeamMember.mockResolvedValue({ id: 'm9', name: 'Eve', index: 3 });
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.sideBMemberId).toBe('m9');
    expect(dh.sideB).toBe('Team B');
    expect(window.API.putMatchLineup).not.toHaveBeenCalled();
  });

  it('R7: under selfReport the representative picker is offered while the match runs', async () => {
    await mount(makeMatch({ subResults: [DH_EMPTY] }), { selfReport: true });
    await waitFor(() => expect(dhInputs().length).toBe(2));
  });

  it('R8: a name typed over a picked blank member renames that member and keeps its id', async () => {
    window.API.fetchSquads.mockResolvedValue({ ...SQUADS, t2: [...SQUADS.t2, { id: 'm3b', name: '', index: 3 }] });
    window.API.renameTeamMember.mockResolvedValue({ id: 'm3b', name: 'Frank', index: 3, modifiedAt: 5 });
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    await openDh('SHIRO');
    const blank = await screen.findByText('no name yet', { selector: '.pmf__opt-name' });
    await act(async () => { fireEvent.click(blank.closest('button')); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Frank' } }); });
    const add = await screen.findByText(/Add “Frank”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.renameTeamMember).toHaveBeenCalledWith('comp1', 't2', 'm3b', 'Frank', ''));
    expect(window.API.addTeamMember).not.toHaveBeenCalled();
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.sideBMemberId).toBe('m3b');
    expect(dh.sideB).toBe('Team B');
  });

  it('R9: a typed name with the team members unreadable goes on and says so in the row, as a lineup name does', async () => {
    window.API.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    window.API.addTeamMember.mockResolvedValue({ id: 'm9', name: 'Eve', index: 3 });
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    await waitFor(() => expect(screen.getByTestId('team-editor-lineup-warning').textContent).toContain('may have been added as a new team member'));
    expect(screen.getByTestId('team-editor-lineup-warning').getAttribute('data-tone')).toBe('warn');
    expect(screen.getByTestId('team-editor-lineup-warning').textContent).not.toContain('Lineup saved');
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.sideBMemberId).toBe('m9');
    expect(dh.sideB).toBe('Team B');
  });

  it('R10: a side given another team drops the representative it picked, so the write stops naming the old team\'s member', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { onSubmit, rerenderWith } = await mount(match);
    expect(dhInput('AKA').value).toBe('Alice');
    // A correction elsewhere seats team t3 on side A. The server's write clears the side's
    // stored pick in the same change, so the push that brings the new team has no pick.
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('');
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect('sideAMemberId' in dh, 'the old team\'s member is not sent').toBe(false);
    expect(dh.sideA, 'the row names the team now on the side').toBe('Team C');
  });

  it('R11: two typed names for one side: the newer pick wins even when the older name resolves last', async () => {
    let resolveEve;
    const eveAnswer = new Promise((resolve) => { resolveEve = resolve; });
    window.API.addTeamMember.mockImplementation((_c, _t, name) => (name === 'Eve'
      ? eveAnswer
      : Promise.resolve({ id: 'm-frank', name, index: 3 })));
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const addEve = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(addEve.closest('button')); });
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Frank' } }); });
    const addFrank = await screen.findByText(/Add “Frank”/);
    await act(async () => { fireEvent.click(addFrank.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Frank', ''));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    // The older name answers last.
    await act(async () => { resolveEve({ id: 'm-eve', name: 'Eve', index: 4 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('SHIRO').value).toBe('Frank');
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.sideBMemberId, 'the later pick, not the older name that resolved last').toBe('m-frank');
  });

  it('R12: Finish and Close wait while a typed representative is still being named, and the finishing write carries the pick', async () => {
    let answerEve;
    window.API.addTeamMember.mockImplementation(() => new Promise((resolve) => { answerEve = resolve; }));
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    // The member POST is still out. Finish or Close now would send or drop the pick
    // while the member may already exist on the server, so both wait for it.
    expect(screen.getByText('Finish').closest('button').disabled).toBe(true);
    expect(screen.getByText('✕ Close').closest('button').disabled).toBe(true);
    await act(async () => { answerEve({ id: 'm9', name: 'Eve', index: 3 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(screen.getByText('Finish').closest('button').disabled).toBe(false);
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect(dh.sideBMemberId).toBe('m9');
    expect(dh.sideB).toBe('Team B');
  });

  it('R13: a side given another team with no representative picked leaves the sheet clean: nothing is autosaved', async () => {
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit, rerenderWith } = await mount(match);
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    // Past the autosave debounce: a sheet the side change left dirty would be written here.
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(onSubmit).not.toHaveBeenCalled();
  });

  it('R14: on a read-only representative row an unpicked side shows its team name, and a picked side shows the representative', async () => {
    // selfReport on a completed match: the row offers no picker, so it is read-only.
    const match = makeMatch({ status: 'completed', subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b' }] });
    await mount(match, { selfReport: true });
    expect(screen.getByText('Carol', { selector: '.tsm-name__static' })).toBeTruthy();
    expect(screen.getByText('Team A', { selector: '.tsm-name__static' })).toBeTruthy();
    expect(screen.queryByText('-', { selector: '.tsm-name__static' })).toBeNull();
  });

  it('R15: a side given another team clears a stored pick and sends nothing: the clear is the server following, not an operator edit', async () => {
    window.confirmDialog.mockClear();
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { onSubmit, onClose, rerenderWith } = await mount(match);
    expect(dhInput('AKA').value).toBe('Alice');
    // The server's shape: the push that seats the new team has already cleared the side's pick.
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    expect(dhInput('AKA').value).toBe('');
    // Past the autosave debounce: a clear that marked the sheet dirty would be written here.
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(onSubmit).not.toHaveBeenCalled();
    // The board agrees with the server, so the sheet is not dirty: Close asks nothing.
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('R16: a side given another team leaves an armed Finish armed', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { rerenderWith } = await mount(match);
    await act(async () => { fireEvent.click(screen.getByText('Finish')); });
    expect(screen.getByText('Tap again to finish')).toBeTruthy();
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    expect(screen.getByText('Tap again to finish')).toBeTruthy();
  });

  it('R17: under selfReport the members-unavailable warning tells a participant to ask the organizer, not to open the Lineups page', async () => {
    window.API.fetchSquads.mockRejectedValue(new TypeError('Failed to fetch'));
    window.API.addTeamMember.mockResolvedValue({ id: 'm9', name: 'Eve', index: 3 });
    const match = makeMatch({ subResults: [DH_EMPTY] });
    await mount(match, { selfReport: true });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(screen.getByTestId('team-editor-lineup-warning').textContent).toContain('may have been added as a new team member'));
    const text = screen.getByTestId('team-editor-lineup-warning').textContent;
    expect(text).toContain('Ask the tournament organizer to check the team.');
    expect(text).not.toContain('Lineups page');
  });

  it('R18: Remove daihyosen is disabled while a typed representative is still being named, and offered again once it is', async () => {
    let answerEve;
    window.API.addTeamMember.mockImplementation(() => new Promise((resolve) => { answerEve = resolve; }));
    const match = makeMatch({ subResults: [DH_EMPTY] });
    await mount(match);
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    expect(screen.getByTestId('team-daihyosen-remove').disabled).toBe(true);
    await act(async () => { answerEve({ id: 'm9', name: 'Eve', index: 3 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(screen.getByTestId('team-daihyosen-remove').disabled).toBe(false);
  });

  it('R18b: Prev and Next are disabled, like Close, while a typed representative is still being named, and offered again once it is', async () => {
    let answerEve;
    window.API.addTeamMember.mockImplementation(() => new Promise((resolve) => { answerEve = resolve; }));
    const neighbour = makeMatch();
    const match = makeMatch({ subResults: [DH_EMPTY] });
    await mount(match, { prevMatch: neighbour, nextMatch: neighbour, onPrev: vi.fn(), onNext: vi.fn() });
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    // leaveEditor does nothing while the member POST is out, so the buttons must not look available.
    expect(screen.getByText('← Prev').closest('button').disabled).toBe(true);
    expect(screen.getByText('Next →').closest('button').disabled).toBe(true);
    await act(async () => { answerEve({ id: 'm9', name: 'Eve', index: 3 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(screen.getByText('← Prev').closest('button').disabled).toBe(false);
    expect(screen.getByText('Next →').closest('button').disabled).toBe(false);
  });

  it('R19: a completed match whose side was given another team clears the stored pick, and Close closes at once with no discard prompt', async () => {
    window.confirmDialog.mockClear();
    const match = makeMatch({ status: 'completed', subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { rerenderWith, onClose } = await mount(match);
    expect(dhInput('AKA').value).toBe('Alice');
    // A correction elsewhere seats team t3 on side A. The push that brings it has cleared the
    // side's pick; adopting that is the server following, not an edit.
    await rerenderWith(makeMatch({ status: 'completed', sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('');
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('R20: after a side is given another team, an operator pick on that side is an unsaved change, and Close asks first', async () => {
    window.confirmDialog.mockClear();
    window.confirmDialog.mockResolvedValueOnce(false);
    window.API.fetchSquads.mockResolvedValue({ ...SQUADS, t3: [{ id: 'm1c', name: 'Erin', index: 1 }] });
    const match = makeMatch({ status: 'completed', subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { rerenderWith, onClose } = await mount(match);
    await rerenderWith(makeMatch({ status: 'completed', sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    await pickFromDh('AKA', 'Erin');
    expect(dhInput('AKA').value).toBe('Erin');
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ message: 'Discard unsaved scoring changes?' }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('R21: a side given another team clears the stored pick without making the representative row a touched one: the next Finish leaves its scoreline unstated', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { onSubmit, rerenderWith } = await mount(match);
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect('sideAMemberId' in dh, 'the cleared pick is not sent').toBe(false);
    expect(dh.ipponsA, 'an untouched row states no scoreline, so a stored verdict is not overwritten').toBeUndefined();
    expect(dh.ipponsB).toBeUndefined();
  });

  it('R22: after a side is given another team, a pick the operator puts on that side and then clears is their own edit: Close asks first', async () => {
    window.confirmDialog.mockClear();
    window.confirmDialog.mockResolvedValueOnce(false);
    window.API.fetchSquads.mockResolvedValue({ ...SQUADS, t3: [{ id: 'm1c', name: 'Erin', index: 1 }] });
    const match = makeMatch({ status: 'completed', subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { rerenderWith, onClose } = await mount(match);
    await rerenderWith(makeMatch({ status: 'completed', sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    await pickFromDh('AKA', 'Erin');
    const wrap = dhInput('AKA').closest('.lineup-name');
    await act(async () => { fireEvent.click(within(wrap).getByRole('button', { name: 'Clear player' })); });
    expect(dhInput('AKA').value).toBe('');
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).toHaveBeenCalledWith(expect.objectContaining({ message: 'Discard unsaved scoring changes?' }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('R23: a representative typed while the members are still being read is not named once the editor has closed', async () => {
    // The member list never answers while the editor is open, so the typed name waits for it.
    window.API.fetchSquads.mockImplementation(() => new Promise(() => {}));
    window.API.addTeamMember.mockResolvedValue({ id: 'm9', name: 'Eve', index: 3 });
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { unmount } = await mount(match);
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    // The members wait's deadline is a setTimeout: faked from the Add tap, so the wait can end
    // after the editor has unmounted (a cancelled read never answers it, only the deadline does).
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] });
    try {
      await act(async () => { fireEvent.click(add.closest('button')); });
      await act(async () => { await Promise.resolve(); });
      expect(window.API.addTeamMember).not.toHaveBeenCalled();
      await act(async () => { unmount(); });
      await act(async () => {
        vi.advanceTimersByTime(FETCH_TIMEOUT_MS);
        for (let i = 0; i < 50; i += 1) await Promise.resolve();
      });
      expect(window.API.addTeamMember, 'the editor is gone, so no member is minted for nobody\'s pick').not.toHaveBeenCalled();
      expect(window.API.renameTeamMember).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  it('R24: a representative typed on a row that then leaves the sheet lands nothing, and a bout added again shows no pick from it', async () => {
    // The member POST is still out when another device removes the representative bout.
    let answerEve;
    window.API.addTeamMember.mockImplementation(() => new Promise((resolve) => { answerEve = resolve; }));
    const t0 = Date.now();
    const { onSubmit, rerenderWith } = await mount(makeMatch({ modifiedAt: t0, subResults: [DH_EMPTY] }));
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    // Another device removes the representative bout: the row is gone from the sheet.
    await rerenderWith(makeMatch({ modifiedAt: t0 + 1000 }));
    expect(dhInputs(), 'the representative row has left the sheet').toHaveLength(0);
    // The POST answers late. The pick belonged to a row that is gone, so it lands nothing.
    await act(async () => { answerEve({ id: 'm9', name: 'Eve', index: 3 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    // Past the autosave debounce: a pick that landed (and marked the sheet dirty) would be written here.
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(onSubmit, 'a pick on a row that is gone arms no autosave').not.toHaveBeenCalled();
    // The representative bout is added again (a fresh, empty row, stamped later).
    await rerenderWith(makeMatch({ modifiedAt: t0 + 2000, subResults: [DH_EMPTY] }));
    expect(dhInput('SHIRO').value, 'the new bout shows no pick from the old one').toBe('');
    await clickFinishTwice();
    const dh = dhEntryOf(finishPatchOf(onSubmit));
    expect('sideBMemberId' in dh, 'the old pick is not sent on the new bout').toBe(false);
  });

  it('R25: a representative typed before the bout was removed and added again lands nothing on the new bout, even if it answers after the add', async () => {
    let answerEve;
    window.API.addTeamMember.mockImplementation(() => new Promise((resolve) => { answerEve = resolve; }));
    const t0 = Date.now();
    const { onSubmit, rerenderWith } = await mount(makeMatch({ modifiedAt: t0, subResults: [DH_EMPTY] }));
    const input = dhInput('SHIRO');
    await act(async () => { fireEvent.focus(input); fireEvent.change(input, { target: { value: 'Eve' } }); });
    const add = await screen.findByText(/Add “Eve”/);
    await act(async () => { fireEvent.click(add.closest('button')); });
    await waitFor(() => expect(window.API.addTeamMember).toHaveBeenCalledWith('comp1', 't2', 'Eve', ''));
    await rerenderWith(makeMatch({ modifiedAt: t0 + 1000 }));
    await rerenderWith(makeMatch({ modifiedAt: t0 + 2000, subResults: [DH_EMPTY] }));
    // The POST answers only now, with the new representative bout already on the sheet.
    await act(async () => { answerEve({ id: 'm9', name: 'Eve', index: 3 }); });
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(dhInput('SHIRO').value, 'the answer of a pick typed on the removed bout names no one on the new one').toBe('');
    expect(onSubmit).not.toHaveBeenCalled();
  });

  // The representative row follows the server per PART: the score and each side's pick
  // are separate edits (bc-mrgc orders the picks apart from the bout row).
  const dhRow = () => [...document.querySelectorAll('.team-sub-match')].pop();
  const dhIppon = (color, letter) => [...dhRow().querySelectorAll(`.team-sub-match__side--${color} button.ipt-btn`)].find((b) => b.textContent === letter);
  const dhMarks = (color) => [...dhRow().querySelectorAll(`.tsm-center-pts--${color} button.editor-side__pt`)].map((b) => b.textContent.trim());
  const pastDebounce = () => act(async () => { await new Promise((r) => setTimeout(r, AUTOSAVE_DEBOUNCE_MS + 150)); });
  const lastSubmitted = (onSubmit) => {
    expect(onSubmit).toHaveBeenCalled();
    const calls = onSubmit.mock.calls;
    return calls[calls.length - 1][0];
  };

  it('R26: a pick made here does not hold another device\'s point on the bout, and a point struck here does not drop the pick', async () => {
    const { onSubmit, rerenderWith } = await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await pickFromDh('AKA', 'Alice');
    expect(dhInput('AKA').value).toBe('Alice');
    // Another device scores Shiro's point. The pick has not landed there, so the push carries none.
    await rerenderWith(makeMatch({ modifiedAt: Date.now() + 60000, subResults: [{ ...DH_EMPTY, ipponsB: ['M'] }] }));
    expect(dhMarks('shiro'), 'the point scored elsewhere is shown').toContain('M');
    expect(dhInput('AKA').value, 'the pick is still the operator\'s until the server holds it').toBe('Alice');
    // Strike Aka's point here: the write carries both points and the pick, and names both groups.
    await act(async () => { fireEvent.click(dhIppon('aka', 'M')); });
    await pastDebounce();
    const patch = lastSubmitted(onSubmit);
    const dh = dhEntryOf(patch);
    expect(dh.ipponsB, 'Shiro\'s point is not written over').toEqual(['M']);
    expect(dh.ipponsA).toEqual(['M']);
    expect(dh.sideAMemberId).toBe('m1a');
    expect(patch.changed).toContain('bout:-1');
    expect(patch.changed).toContain('repPicks');
  });

  it('R27: a pick adopted from another device and then cleared here is named in the next write', async () => {
    // The editor MUST mount before the pick exists (R4 mounts with it, so its mount-time
    // baseline already holds the pick and the clear is a change against it).
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit, rerenderWith } = await mount(match);
    await rerenderWith(makeMatch({ modifiedAt: Date.now() + 60000, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    expect(dhInput('AKA').value).toBe('Alice');
    const wrap = dhInput('AKA').closest('.lineup-name');
    await act(async () => { fireEvent.click(within(wrap).getByRole('button', { name: 'Clear player' })); });
    await clickFinishTwice();
    const patch = finishPatchOf(onSubmit);
    expect('sideAMemberId' in dhEntryOf(patch)).toBe(false);
    expect(toBackendMatchResult(patch, match).changed, 'the server keeps the other device\'s pick unless repPicks is named').toContain('repPicks');
  });

  it('R28: a pick adopted for one side does not hide the pick kept on the other: the next write still names repPicks and carries both', async () => {
    const { onSubmit, rerenderWith } = await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await pickFromDh('AKA', 'Alice');
    // Another device picks Carol for Shiro; Alice has not landed.
    await rerenderWith(makeMatch({ modifiedAt: Date.now() + 60000, subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b' }] }));
    expect(dhInput('SHIRO').value, 'the other side follows the server').toBe('Carol');
    expect(dhInput('AKA').value, 'the side the operator picked stays theirs').toBe('Alice');
    await clickFinishTwice();
    const patch = finishPatchOf(onSubmit);
    const dh = dhEntryOf(patch);
    expect(dh.sideAMemberId).toBe('m1a');
    expect(dh.sideBMemberId).toBe('m1b');
    expect(patch.changed).toContain('repPicks');
  });

  it('R29: a pick made here and not yet written is dropped when its side is given another team: nothing is sent for it, and the sheet is not dirty', async () => {
    window.confirmDialog.mockClear();
    const { onSubmit, onClose, rerenderWith } = await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await pickFromDh('AKA', 'Alice');
    expect(dhInput('AKA').value).toBe('Alice');
    // The server never received the pick, so the push that seats team t3 cannot clear it.
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('');
    // The pick armed an autosave that still fires; whatever it writes names no member of the old team.
    await pastDebounce();
    for (const [patch] of onSubmit.mock.calls) {
      expect('sideAMemberId' in (dhEntryOf(patch) || {}), 'the dropped pick is not sent').toBe(false);
      expect(patch.changed || [], 'the picks did not change').not.toContain('repPicks');
    }
    await act(async () => { fireEvent.click(screen.getByText('✕ Close')); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('R30: the dropped pick takes its edit stamp with it: a pick another device makes for the new team is followed even when its push is stamped before the dropped pick', async () => {
    window.API.fetchSquads.mockResolvedValue({ ...SQUADS, t3: [{ id: 'm1c', name: 'Erin', index: 1 }] });
    const { rerenderWith } = await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await pickFromDh('AKA', 'Alice');
    const pickedAt = Date.now();
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [DH_EMPTY] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('');
    // The correction that seated team t3 reached this device late: the other device's pick for t3
    // was made after it but before the pick dropped here, so its stamp is older than that pick's.
    await rerenderWith(makeMatch({ modifiedAt: pickedAt - 500, sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1c' }] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('Erin');
  });
});
