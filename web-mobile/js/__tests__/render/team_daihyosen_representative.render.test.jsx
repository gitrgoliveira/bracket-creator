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

async function mount(match, { selfReport = false } = {}) {
  const onSubmit = vi.fn();
  const view = (m) => (
    <ScoreEditorModal match={m} onClose={vi.fn()} onSubmit={onSubmit} password="" selfReport={selfReport} />
  );
  let utils;
  await act(async () => { utils = render(view(match)); });
  return { ...utils, onSubmit, rerenderWith: async (m) => { await act(async () => { utils.rerender(view(m)); }); } };
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

describe('team daihyosen representative picker (bc-dhrp)', () => {
  it('R1: the daihyosen row renders a name box on each side, and never the team name in it', async () => {
    await mount(makeMatch({ subResults: [DH_EMPTY] }));
    await waitFor(() => expect(dhInputs().length).toBe(2));
    expect(dhInput('SHIRO').value).toBe('');
    expect(dhInput('AKA').value).toBe('');
  });

  it('R2: picking a member records its id on the -1 row, keeps the team names, and names bout:-1', async () => {
    const match = makeMatch({ subResults: [DH_EMPTY] });
    const { onSubmit } = await mount(match);
    await pickFromDh('SHIRO', 'Carol');
    expect(dhInput('SHIRO').value).toBe('Carol');
    await clickFinishTwice();
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const patch = onSubmit.mock.calls[0][0];
    const dh = dhEntryOf(patch);
    expect(dh.sideBMemberId).toBe('m1b');
    expect(dh.sideA).toBe('Team A');
    expect(dh.sideB).toBe('Team B');
    // The wire the server gets: the id rides the row and the write names its group.
    const wire = toBackendMatchResult(patch, match);
    const wireDh = wire.subResults.find((s) => s.position === -1);
    expect(wireDh.sideBMemberId).toBe('m1b');
    expect(wireDh.sideB).toBe('Team B');
    expect(wire.changed).toContain('bout:-1');
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
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
    expect(dh.ipponsB).toEqual(['M']);
    expect(dh.sideBMemberId).toBe('m1b');
    expect(dh.sideB).toBe('Team B');
  });

  it('R4: clearing a stored pick omits the id and names bout:-1 on the next write', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideBMemberId: 'm1b' }] });
    const { onSubmit } = await mount(match);
    // The stored pick shows as the member's own name, never the team's.
    expect(dhInput('SHIRO').value).toBe('Carol');
    const wrap = dhInput('SHIRO').closest('.lineup-name');
    const clear = within(wrap).getByRole('button', { name: 'Clear player' });
    await act(async () => { fireEvent.click(clear); });
    await clickFinishTwice();
    const patch = onSubmit.mock.calls[0][0];
    const dh = dhEntryOf(patch);
    expect('sideBMemberId' in dh).toBe(false);
    expect(dh.sideB).toBe('Team B');
    expect(toBackendMatchResult(patch, match).changed).toContain('bout:-1');
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
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
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
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
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
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
    expect(dh.sideBMemberId).toBe('m9');
    expect(dh.sideB).toBe('Team B');
  });

  it('R10: a side given another team drops the representative it picked, so the write stops naming the old team\'s member', async () => {
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { onSubmit, rerenderWith } = await mount(match);
    expect(dhInput('AKA').value).toBe('Alice');
    // A correction elsewhere seats team t3 on side A; the stored row still names Alice.
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    await act(async () => { await new Promise((r) => setTimeout(r, 0)); });
    expect(dhInput('AKA').value).toBe('');
    await clickFinishTwice();
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
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
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
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
    expect(onSubmit).toHaveBeenCalledTimes(1);
    const dh = dhEntryOf(onSubmit.mock.calls[0][0]);
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
    const match = makeMatch({ subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] });
    const { onSubmit, rerenderWith } = await mount(match);
    expect(dhInput('AKA').value).toBe('Alice');
    await rerenderWith(makeMatch({ sideA: { id: 't3', name: 'Team C' }, subResults: [{ ...DH_EMPTY, sideAMemberId: 'm1a' }] }));
    expect(dhInput('AKA').value).toBe('');
    // Past the autosave debounce: a clear that marked the sheet dirty would be written here.
    await act(async () => { await new Promise((r) => setTimeout(r, 500)); });
    expect(onSubmit).not.toHaveBeenCalled();
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
});
