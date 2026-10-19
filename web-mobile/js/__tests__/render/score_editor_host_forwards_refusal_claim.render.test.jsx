// A score-editor host hands the editor's opts on to its write (bc-evts stage
// 6): the editor's own Start passes `{ onRefusal }`, its claim on a refusal it
// says itself (StartRefusedNotice), and a host that dropped it left the
// outbox to tell the same refusal again in a toast. Pinned on the Scores tab,
// the host the double report was seen on.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const M2 = {
  id: 'm-2', compId: 'c1', compName: 'Cup', status: 'scheduled', court: 'A', scheduledAt: '09:05', phase: 'pool', poolName: 'Pool A',
  sideA: { id: 'a', name: 'Alice' }, sideB: { id: 'b', name: 'Bob' },
};
const probe = { props: null };
const STUBS = {
  ScoreEditorModal: (props) => { probe.props = props; return <div data-testid="score-editor" />; },
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (m) => m,
  tournamentMatches: () => [],
  compMatches: () => [M2],
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordDecision: vi.fn() },
};

let restore, AdminScoreEditor;
beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  ({ AdminScoreEditor } = await import('../../admin_schedule_score_editor.jsx'));
});
afterAll(() => restore());

describe('the Scores tab hands the editor\'s opts on to the write', () => {
  it('the onRefusal the editor passes reaches onEditScore', async () => {
    const onEditScore = vi.fn(async () => ({ applied: true }));
    let utils;
    await act(async () => {
      utils = render(<AdminScoreEditor t={{ competitions: [{ id: 'c1', name: 'Cup' }] }} onEditScore={onEditScore} onMoveCourt={null} password="pw" showToast={vi.fn()} />);
    });
    await act(async () => { fireEvent.click(utils.container.querySelector('button.test-score-open')); });
    const onRefusal = vi.fn(() => true);
    await act(async () => { await probe.props.onSubmit({ status: 'running' }, { onRefusal }); });
    expect(onEditScore).toHaveBeenCalledTimes(1);
    expect(onEditScore.mock.calls[0][4], 'the editor\'s claim reaches the write').toMatchObject({ onRefusal });
  });
});
