// bc-dhas: the public self-run team score editor shows each fighter's member
// number ("T2.1") the way the admin editor does. The editor used to load the
// team members from GET /api/competitions/:id/team-members, which self-run
// mode keeps behind the organiser password, so on the public page the call
// was refused and every row lost its number. The self-run host now hands the
// editor the members from the viewer payload it already holds, and the
// editor asks the route only when no host did.
//
// Mounts the real team editor through MatchViewerModal and "Report result",
// the way a participant reaches it on the public page.
import React from 'react';
import { render, act, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const STUBBED_GLOBALS = {
  isHikiwake: () => false,
  arraysEqual: (a, b) => a.length === b.length && a.every((v, i) => v === b[i]),
  isKikenDecision: () => false,
  isTextEntry: () => false,
  isInteractiveTarget: () => false,
  confirmDialog: vi.fn().mockResolvedValue(true),
  API: {},
  AdminLineupHelpers: { rosterFor: vi.fn().mockReturnValue([]) },
  compMatches: () => [],
  compMatchesForCompetition: () => [],
  Term: ({ children }) => <span>{children}</span>,
  GlossaryHint: ({ name }) => <span title={name} />,
};

let restoreGlobals;
let ScoreEditorModal;
let MatchViewerModal;

beforeAll(async () => {
  restoreGlobals = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin_scoring_modal.jsx');
  ScoreEditorModal = window.ScoreEditorModal;
  ({ MatchViewerModal } = await import('../../viewer_match.jsx'));
});

afterAll(() => restoreGlobals());

const members = (prefix, names) => names.map((name, i) => ({ id: `${prefix}-m${i + 1}`, index: i + 1, name }));
const MEMBERS = {
  'team-A': members('a', ['Ren Abe', 'Kai Mori', 'Yui Sato', 'Rin Ota', 'Sho Ueda']),
  'team-B': members('b', ['Mei Ito', 'Sora Kato', 'Aoi Endo', 'Hana Ito', 'Jun Oda']),
};

let fetchSquads;

beforeEach(() => {
  // What the server answers a caller without the organiser password.
  fetchSquads = vi.fn().mockRejectedValue(Object.assign(new Error('unauthorized'), { status: 401 }));
  window.API = {
    fetchCompetitionDetails: vi.fn().mockResolvedValue({
      id: 'c1',
      config: { format: 'mixed', teamMatchType: 'fixed', naginata: false, players: [] },
    }),
    fetchSquads,
    recordScore: vi.fn().mockResolvedValue(undefined),
    recordDaihyosen: vi.fn(),
    removeDaihyosen: vi.fn(),
    putMatchLineup: vi.fn(),
    recordDecision: vi.fn(),
  };
});

function teamMatch() {
  return {
    id: 'm1', compId: 'c1', compName: 'Teams', status: 'running',
    phase: 'pool', poolName: 'Pool 1', court: 'A',
    compKind: 'team', teamSize: 5, compFormat: 'mixed', teamMatchType: 'fixed',
    sideA: { id: 'team-A', name: 'Kodokan', number: 'T1' }, // Aka
    sideB: { id: 'team-B', name: 'Mumeishi', number: 'T2' }, // Shiro
  };
}

const selfRun = (squads) => ({ mode: 'self-run', competitions: [{ id: 'c1', name: 'Teams', squads }] });

const labels = (color) =>
  [...document.querySelectorAll(`[data-testid="team-sub-match-member-label-${color}"]`)].map((n) => n.textContent);

async function openSelfRunEditor(tournament) {
  let view;
  await act(async () => {
    view = render(<MatchViewerModal match={teamMatch()} onClose={vi.fn()} tournament={tournament} compId="c1" />);
  });
  await act(async () => {
    fireEvent.click(screen.getByRole('button', { name: 'Report result' }));
  });
  return view;
}

describe('the self-run team editor takes its team members from the page (bc-dhas)', () => {
  it('shows every row\'s member number without asking the password-gated route', async () => {
    await openSelfRunEditor(selfRun(MEMBERS));
    await waitFor(() => expect(labels('shiro')).toEqual(['T2.1', 'T2.2', 'T2.3', 'T2.4', 'T2.5']));
    expect(labels('aka')).toEqual(['T1.1', 'T1.2', 'T1.3', 'T1.4', 'T1.5']);
    expect(fetchSquads).not.toHaveBeenCalled();
  });

  it('follows the members the page holds when a refetch brings new ones', async () => {
    const view = await openSelfRunEditor(selfRun({ 'team-A': MEMBERS['team-A'] }));
    await waitFor(() => expect(labels('aka')).toHaveLength(5));
    expect(labels('shiro')).toEqual([]);

    await act(async () => {
      view.rerender(<MatchViewerModal match={teamMatch()} onClose={vi.fn()} tournament={selfRun(MEMBERS)} compId="c1" />);
    });
    await waitFor(() => expect(labels('shiro')).toEqual(['T2.1', 'T2.2', 'T2.3', 'T2.4', 'T2.5']));
    expect(fetchSquads).not.toHaveBeenCalled();
  });

  it('a page without the competition in its list still does not ask the route', async () => {
    await openSelfRunEditor({ mode: 'self-run' });
    await act(async () => { await Promise.resolve(); });
    expect(labels('shiro')).toEqual([]);
    expect(fetchSquads).not.toHaveBeenCalled();
  });

  // Every admin host passes no members, and the editor loads them itself with
  // the operator's password, as before.
  it('an admin host still loads them from the route', async () => {
    fetchSquads.mockResolvedValue(MEMBERS);
    await act(async () => {
      render(<ScoreEditorModal match={teamMatch()} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" />);
    });
    await waitFor(() => expect(labels('shiro')).toEqual(['T2.1', 'T2.2', 'T2.3', 'T2.4', 'T2.5']));
    expect(fetchSquads).toHaveBeenCalledWith('c1', 'pw');
  });
});
