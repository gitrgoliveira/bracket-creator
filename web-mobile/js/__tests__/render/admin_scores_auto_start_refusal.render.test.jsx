// On the Scores tab (AdminScoreEditor), finishing a match is plain Finish only.
// It never starts or advances to the next match, never calls a start API,
// passes onSubmitAndNext={null} with no onAfterDecision, and shows no refusal notice.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const side = (id, name) => ({ id, name });

const mk = (id, status, scheduledAt, a, b) => ({
  id, compId: 'c1', compName: 'Cup', status, court: 'A', scheduledAt, phase: 'pool', poolName: 'Pool A',
  sideA: side(`${id}-a`, a), sideB: side(`${id}-b`, b),
});
const M1 = mk('m-1', 'running', '09:00', 'Yamada', 'Tanaka');
const M2 = mk('m-2', 'scheduled', '09:05', 'Alice', 'Bob');
const M3 = mk('m-3', 'scheduled', '09:10', 'Carol', 'Dan');

let matches;
const probe = { props: null };

const STUBS = {
  ScoreEditorModal: (props) => { probe.props = props; return <div data-testid="score-editor" data-match={props.match.id} />; },
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (m) => m,
  tournamentMatches: () => [],
  compMatches: () => matches,
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordDecision: vi.fn(), startMatch: vi.fn() },
};

let restore, AdminScoreEditor, AdminScoreEditorPage;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  const mod = await import('../../admin_schedule_score_editor.jsx');
  AdminScoreEditor = mod.AdminScoreEditor;
  AdminScoreEditorPage = mod.AdminScoreEditorPage;
});
afterAll(() => restore());
beforeEach(() => { matches = [M1, M2, M3]; probe.props = null; });

const tournament = () => ({ competitions: [{ id: 'c1', name: 'Cup' }] });

function ui(onEditScore, showToast) {
  return (
    <AdminScoreEditor t={tournament()} onEditScore={onEditScore} onMoveCourt={null} password="pw" showToast={showToast} />
  );
}

async function mountAndOpenRunning(onEditScore, showToast) {
  let utils;
  await act(async () => { utils = render(ui(onEditScore, showToast)); });
  const scoreBtns = [...utils.container.querySelectorAll('button.test-score-open')];
  await act(async () => { fireEvent.click(scoreBtns[0]); });
  expect(probe.props.match.id).toBe('m-1');
  return utils;
}

describe('plain Finish on the Scores tab never starts or advances a match', () => {
  it('mounts with onSubmitAndNext null and no onAfterDecision', async () => {
    await mountAndOpenRunning(vi.fn(), vi.fn());
    expect(probe.props.onSubmitAndNext).toBeNull();
    expect(probe.props.onAfterDecision).toBeUndefined();
  });

  it('finishing calls onEditScore for the current match only, never calls start, and shows no refusal notice', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmit({ status: 'completed' }); });

    // Only m-1 is scored; m-2 is never started
    expect(onEditScore).toHaveBeenCalledTimes(1);
    expect(onEditScore.mock.calls[0][1]).toBe('m-1');
    expect(window.API.startMatch).not.toHaveBeenCalled();
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(0);
    expect(showToast).not.toHaveBeenCalled();
  });
});

describe('the Scores page hands its toast down', () => {
  it('AdminScoreEditorPage passes showToast to the editor', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    const showToast = vi.fn();
    let utils;
    await act(async () => {
      utils = render(
        <AdminScoreEditorPage
          tournament={tournament()} onBack={() => {}} onEditScore={onEditScore} onMoveCourt={null}
          onLogout={() => {}} onViewerMode={() => {}} password="pw" showToast={showToast}
        />,
      );
    });
    await act(async () => { fireEvent.click(utils.container.querySelector('button.test-score-open')); });
    expect(probe.props.match.id).toBe('m-1');
  });
});

describe('a completed team row marks the withdrawn team without any window global', () => {
  const saved = {};
  beforeEach(() => {
    saved.teamMatchMarks = window.teamMatchMarks;
    delete window.teamMatchMarks;
  });
  afterEach(() => { if (saved.teamMatchMarks) window.teamMatchMarks = saved.teamMatchMarks; });

  it('keeps Kiken beside the withdrawn team and the winner cue on the other', async () => {
    matches = [{
      ...mk('m-4', 'completed', '09:15', 'Hana', 'Ichi'),
      compKind: 'team', teamSize: 3, decision: 'kiken-voluntary', decisionBy: 'shiro',
      subResults: [{ position: 1, sideA: 'Hana-1', sideB: 'Ichi-1', winner: 'Hana-1' }],
      winner: side('m-4-a', 'Hana'),
    }];
    let utils;
    await act(async () => { utils = render(ui(vi.fn(), vi.fn())); });
    const row = [...utils.container.querySelectorAll('.score-edit-row')].find((r) => r.textContent.includes('Hana'));
    expect(row, 'the completed row is listed').toBeTruthy();
    expect(row.querySelector('[data-testid="team-summary-mark-shiro"]')?.textContent).toBe('Kiken');
    expect(row.querySelector('[data-testid="team-summary-mark-aka"]')).toBeNull();
    expect(row.querySelector('.score-edit-row__side--aka').classList.contains('score-edit-row__side--win')).toBe(true);
  });
});
