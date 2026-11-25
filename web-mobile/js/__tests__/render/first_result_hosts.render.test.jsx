// bc-drev (operator decision D4 b): every admin host asks before the first
// result of a drawn competition. The Pools (preview), Bracket (preview) and
// Scores tab of a competition, and the score editor page, all write through
// AdminApp's one editMatchScore and so attemptScoreWrite, which asks
// (first_result_confirm.jsx); AdminApp notes the competitions it holds (the
// tournament's list, then the open competition's detail) so the question knows
// which competitions await their first result. The barred-match notice's
// Record fusensho and Record as drawn call the API directly and ask on their
// own.
//
// AdminApp is mounted with each host view and a stub host, which hands back the
// onEditScore it was given: the real editMatchScore.
import React from 'react';
import { render, act, screen, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { resetFirstResultConfirm, FIRST_RESULT_CANCELLED, firstResultAsks } from '../../first_result_confirm.jsx';
import { wasToasted } from '../../write_result.jsx';
import { BarredMatchNotice } from '../../barred_match_notice.jsx';

const probe = { props: null };
const hostProbe = (testid) => (props) => { probe.props = props; return <div data-testid={testid} />; };

const STUBBED_GLOBALS = {
  // MODULE-EVAL-TIME: captured at import by admin.jsx.
  AdminScoreEditorPage: hostProbe('score-page'),
  AdminCompetition: hostProbe('competition-page'),
  confirmDialog: vi.fn(),
  API: {
    recordScore: vi.fn(),
    recordDecision: vi.fn(),
    subscribeToEvents: vi.fn(() => () => {}),
    fetchCompetitions: vi.fn().mockResolvedValue([]),
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
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
  resetFirstResultConfirm();
  window.API.recordScore = vi.fn().mockResolvedValue({ applied: true });
  window.API.recordDecision = vi.fn().mockResolvedValue({ applied: true });
  window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue(null);
  window.confirmDialog = vi.fn();
});
afterEach(() => cleanup());

const poolMatch = (id, status = 'scheduled') => ({
  id, compId: 'c2', status, phase: 'pool', poolName: 'Pool 1', court: 'A',
  sideA: { id: `${id}-a`, name: 'Suzuki' }, sideB: { id: `${id}-b`, name: 'Sato' },
});
const competition = (status, poolMatches = [poolMatch('Pool 1-0'), poolMatch('Pool 1-1')]) => ({
  id: 'c2', name: 'Women', status, poolMatches, bracket: null,
});

async function mountHost(view, competitions) {
  const showToast = vi.fn();
  await act(async () => {
    render(
      <AdminApp
        tournament={{ name: 'Cup', courts: ['A'], competitions }}
        onUpdate={vi.fn()} onLogout={vi.fn()} onViewerMode={vi.fn()} onPasswordChange={vi.fn()}
        tweaks={{}} password="pw" view={view} setView={vi.fn()}
        showToast={showToast} authConfig={{}}
      />,
    );
  });
  expect(probe.props?.onEditScore, 'the host was handed the real editMatchScore').toBeTypeOf('function');
  return { showToast, onEditScore: probe.props.onEditScore };
}

// Each admin host that records results, by the view AdminApp renders it under.
const HOSTS = [
  ['Pools (preview)', { kind: 'competition', id: 'c2', section: 'pools' }],
  ['Bracket (preview)', { kind: 'competition', id: 'c2', section: 'bracket' }],
  ['the Scores tab', { kind: 'competition', id: 'c2', section: 'scores' }],
  ['the score editor page', { kind: 'scoreEditor' }],
];

describe('every admin host asks before the first result of a drawn competition (bc-drev D4 b)', () => {
  for (const [host, view] of HOSTS) {
    it(`${host}: a Start asks, naming the competition and the match, and Not yet sends nothing`, async () => {
      const { showToast, onEditScore } = await mountHost(view, [competition('draw-ready')]);
      window.confirmDialog.mockResolvedValue(false);
      const m = poolMatch('Pool 1-0');

      let caught;
      await act(async () => {
        try { await onEditScore('c2', m.id, { status: 'running' }, m); } catch (e) { caught = e; }
      });

      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      const q = window.confirmDialog.mock.calls[0][0];
      expect(q.title).toBe('Start Women?');
      expect(q.message).toBe('Women has not started yet. Starting Pool 1 · Match 1 starts it. After that the draw is fixed: competitors can no longer be swapped, and the draw can no longer be discarded.');
      expect(q.confirmLabel).toBe('Start the competition');
      expect(q.cancelLabel).toBe('Not yet');
      expect(window.API.recordScore).not.toHaveBeenCalled();
      expect(caught?.firstResultCancelled).toBe(true);
      expect(caught?.message).toBe(FIRST_RESULT_CANCELLED);
      // The operator's own choice: toasted once, not as an error, and marked
      // so a host that shows it on a row does not toast it again.
      expect(showToast.mock.calls).toEqual([[FIRST_RESULT_CANCELLED]]);
      expect(wasToasted(caught)).toBe(true);
    });

    it(`${host}: agreed, the write goes out, and the next result asks nothing`, async () => {
      const { onEditScore } = await mountHost(view, [competition('draw-ready')]);
      window.confirmDialog.mockResolvedValue(true);
      const m = poolMatch('Pool 1-0');

      await act(async () => { await onEditScore('c2', m.id, { status: 'completed', ipponsA: ['M'] }, m); });
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(window.confirmDialog.mock.calls[0][0].message).toContain('Recording a result for Pool 1 · Match 1 starts it.');
      expect(window.API.recordScore).toHaveBeenCalledTimes(1);

      const m2 = poolMatch('Pool 1-1');
      await act(async () => { await onEditScore('c2', m2.id, { status: 'running' }, m2); });
      expect(window.confirmDialog).toHaveBeenCalledTimes(1);
      expect(window.API.recordScore).toHaveBeenCalledTimes(2);
    });
  }

  it('a competition already started asks nothing', async () => {
    const { onEditScore } = await mountHost({ kind: 'scoreEditor' }, [competition('pools')]);
    const m = poolMatch('Pool 1-0');
    await act(async () => { await onEditScore('c2', m.id, { status: 'running' }, m); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('a drawn competition with a result already recorded asks nothing', async () => {
    const { onEditScore } = await mountHost({ kind: 'scoreEditor' },
      [competition('draw-ready', [poolMatch('Pool 1-0', 'completed'), poolMatch('Pool 1-1')])]);
    const m = poolMatch('Pool 1-1');
    await act(async () => { await onEditScore('c2', m.id, { status: 'running' }, m); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  it('a write that leaves the match scheduled asks nothing', async () => {
    const { onEditScore } = await mountHost({ kind: 'scoreEditor' }, [competition('draw-ready')]);
    const m = poolMatch('Pool 1-0');
    await act(async () => { await onEditScore('c2', m.id, { status: 'scheduled' }, m); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(window.API.recordScore).toHaveBeenCalledTimes(1);
  });

  // The open competition's detail is noted too, after the tournament's list:
  // it is the fresher copy of that competition.
  it('the open competition is judged on its detail once it loads', async () => {
    window.API.fetchCompetitionDetails = vi.fn().mockResolvedValue({
      config: { id: 'c2', name: 'Women', status: 'draw-ready' },
      pools: [], poolMatches: [poolMatch('Pool 1-0', 'running')], standings: [], bracket: null,
    });
    await mountHost({ kind: 'competition', id: 'c2', section: 'pools' }, [competition('draw-ready')]);
    await act(async () => { for (let i = 0; i < 6; i++) await Promise.resolve(); });
    expect(window.API.fetchCompetitionDetails).toHaveBeenCalledWith('c2');
    // The detail shows a match running: the competition has started.
    expect(firstResultAsks('c2')).toBe(false);
  });

  it('a competition seen back in setup forgets the agreement, so a new draw asks again', async () => {
    const { onEditScore } = await mountHost({ kind: 'scoreEditor' }, [competition('draw-ready')]);
    window.confirmDialog.mockResolvedValue(true);
    const m = poolMatch('Pool 1-0');
    await act(async () => { await onEditScore('c2', m.id, { status: 'running' }, m); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    cleanup();

    // The draw was discarded, then generated again.
    await mountHost({ kind: 'scoreEditor' }, [competition('setup', [])]);
    cleanup();
    const again = await mountHost({ kind: 'scoreEditor' }, [competition('draw-ready')]);
    await act(async () => { await again.onEditScore('c2', m.id, { status: 'running' }, m); });
    expect(window.confirmDialog).toHaveBeenCalledTimes(2);
  });
});

// The barred-match notice records its decision through window.API directly,
// not through attemptScoreWrite, so it asks on its own.
describe('the barred-match notice asks before the first result of a drawn competition (bc-drev D4 b)', () => {
  const barred = (over = {}) => ({
    ...poolMatch('Pool 1-0'),
    ineligibleSides: { b: 'kiken-voluntary' },
    ...over,
  });

  async function mountNotice(match, competitions) {
    // What an admin host notes about the competitions it shows.
    await mountHost({ kind: 'scoreEditor' }, competitions);
    await act(async () => { render(<BarredMatchNotice match={match} password="pw" />); });
  }

  it('Record fusensho asks, and Not yet sends nothing and says so', async () => {
    await mountNotice(barred(), [competition('draw-ready')]);
    window.confirmDialog.mockResolvedValue(false);
    await act(async () => { fireEvent.click(screen.getByTestId('barred-match-record-fusensho')); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    const q = window.confirmDialog.mock.calls[0][0];
    expect(q.title).toBe('Start Women?');
    expect(q.message).toContain('Recording a result for Pool 1 · Match 1 · Sato vs Suzuki starts it.');
    expect(window.API.recordDecision).not.toHaveBeenCalled();
    expect(screen.getByTestId('barred-match-notice').textContent).toContain(FIRST_RESULT_CANCELLED);
    // Still offered: the decision can be recorded once the competition is to start.
    expect(screen.getByTestId('barred-match-record-fusensho').disabled).toBe(false);
  });

  it('Record fusensho, agreed, records the decision once', async () => {
    await mountNotice(barred(), [competition('draw-ready')]);
    window.confirmDialog.mockResolvedValue(true);
    await act(async () => { fireEvent.click(screen.getByTestId('barred-match-record-fusensho')); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
    expect(window.API.recordDecision.mock.calls[0][2]).toMatchObject({ decision: 'fusensho', decisionBy: 'shiro' });
    expect(firstResultAsks('c2')).toBe(false);
  });

  it('Record as drawn asks, and Not yet sends nothing', async () => {
    await mountNotice(barred({ ineligibleSides: { a: 'kiken-injury', b: 'fusenpai' } }), [competition('draw-ready')]);
    window.confirmDialog.mockResolvedValue(false);
    await act(async () => { fireEvent.click(screen.getByTestId('barred-match-record-drawn')); });

    expect(window.confirmDialog).toHaveBeenCalledTimes(1);
    expect(window.API.recordDecision).not.toHaveBeenCalled();
    expect(screen.getByTestId('barred-match-notice').textContent).toContain(FIRST_RESULT_CANCELLED);
  });

  it('a competition already started asks nothing', async () => {
    await mountNotice(barred(), [competition('pools')]);
    await act(async () => { fireEvent.click(screen.getByTestId('barred-match-record-fusensho')); });
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
  });
});
