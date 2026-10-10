// PR #463 batch 14, item 10: editMatchScore (admin.jsx), the chokepoint every
// score write goes through, toasts the error it throws and MARKS it
// (write_result.jsx markToasted), so the two hosts that catch it to show it on a
// card or a row (the court console's Start, the Scores tab's automatic start) can
// ask wasToasted and not toast the same sentence a second time. Those hosts' own
// suites mock onEditScore, so this is the test that the mark is really put on
// what the real editMatchScore throws, on BOTH branches it toasts.
//
// AdminApp is mounted with the Scores view and a stub page, which hands back the
// onEditScore it was given: the real editMatchScore.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED, wasToasted } from '../../write_result.jsx';

const probe = { props: null };

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminScoreEditorPage: (props) => { probe.props = props; return <div data-testid="score-page" />; },
  confirmDialog: vi.fn(),
  API: {
    recordScore: vi.fn(),
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
  },
};

let restore;
let AdminApp;

beforeAll(async () => {
  restore = installWindowStubs(STUBBED_GLOBALS);
  await import('../../admin.jsx');
  AdminApp = window.AdminApp;
});
afterAll(() => restore());
beforeEach(() => {
  probe.props = null;
  window.API.recordScore.mockReset();
  window.confirmDialog.mockReset();
});

async function mountScoresView() {
  const showToast = vi.fn();
  await act(async () => {
    render(
      <AdminApp
        tournament={{ name: 'Cup', courts: ['A'], competitions: [] }}
        onUpdate={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={{ kind: 'scoreEditor' }} setView={vi.fn()}
        showToast={showToast} authConfig={{}}
      />,
    );
  });
  expect(probe.props?.onEditScore, 'the page was handed the real editMatchScore').toBeTypeOf('function');
  return { showToast, onEditScore: probe.props.onEditScore };
}

describe('editMatchScore marks every error it toasts', () => {
  it('a thrown refusal is toasted once, by it, and carries the mark', async () => {
    const { showToast, onEditScore } = await mountScoresView();
    const refusal = new Error('Kato is fighting on shiaijo B');
    window.API.recordScore.mockRejectedValue(refusal);

    let caught;
    await act(async () => {
      try { await onEditScore('c1', 'm1', { status: 'running' }, { id: 'm1' }); } catch (e) { caught = e; }
    });

    expect(caught).toBe(refusal);
    expect(showToast.mock.calls).toEqual([['Kato is fighting on shiaijo B', 'error']]);
    expect(wasToasted(caught)).toBe(true);
  });

  it('a declined downstream override is toasted once, with its own words, and carries the mark', async () => {
    const { showToast, onEditScore } = await mountScoresView();
    const refusal = Object.assign(new Error('a later match was played'), {
      downstreamKnockoutPlayed: { blockingMatchId: 'm9', blockingMatches: [], displaced: [] },
    });
    window.API.recordScore.mockRejectedValue(refusal);
    window.confirmDialog.mockResolvedValue(false);

    let caught;
    await act(async () => {
      try { await onEditScore('c1', 'm1', { status: 'completed' }, { id: 'm1' }); } catch (e) { caught = e; }
    });

    expect(caught).toBe(refusal);
    expect(showToast.mock.calls).toEqual([[DOWNSTREAM_KNOCKOUT_PLAYED_CANCELLED]]);
    expect(wasToasted(caught)).toBe(true);
  });
});
