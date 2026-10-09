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
});
