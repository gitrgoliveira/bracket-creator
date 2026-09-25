// bc-cse: a scheduled match a competitor is barred from (withdrew earlier)
// cannot be fought. The Scores tab's own auto-advance (Finish + Start Next's
// nextActiveMatch) must skip it exactly as the court console's does, and its
// row shows the note plus the one-tap default-win action instead of relying
// on the operator to open the editor to discover why Start would fail.
//
// Prev/Next (chained navigation) still REACHES a barred match on purpose:
// opening one directly is how the operator resolves it (ineligible_match.jsx
// header). Only the AUTO-PICK is scoped here.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

const side = (id, name) => ({ id, name });

// bc-tmfn fixture convention: Yama withdrew (kiken-voluntary) on side B, so
// the default win is awaited for Umi (side A).
const RUNNING = { id: 'm-run', compId: 'c1', status: 'running', court: 'A', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka') };
const BARRED = {
  id: 'm-barred', compId: 'c1', status: 'scheduled', court: 'A', scheduledAt: '09:05',
  sideA: side('u', 'Umi'), sideB: side('y', 'Yama'), ineligibleSides: { b: 'kiken-voluntary' },
};
const OPEN = { id: 'm-open', compId: 'c1', status: 'scheduled', court: 'A', scheduledAt: '09:10', sideA: side('s', 'Sato'), sideB: side('k', 'Kato') };

// Captures the LATEST ScoreEditorModal props so a test can drive
// onSubmitAndNext directly, mirroring admin_shiaijo.render.test.jsx's probe.
const probe = { props: null };

const STUBS = {
  ScoreEditorModal: (props) => { probe.props = props; return <div data-testid="score-editor" />; },
  AdminTopbar: ({ children }) => <div>{children}</div>,
  Breadcrumbs: () => null,
  CourtPicker: () => null,
  getScoreBtnClass: () => 'test-score-open',
  matchScoreStr: () => '',
  filterMatchesByCourt: (matches) => matches,
  tournamentMatches: () => [],
  compMatches: () => [RUNNING, BARRED, OPEN],
  startPatch: () => ({ status: 'running', winner: null }),
  confirmDialog: vi.fn().mockResolvedValue(true),
  pluralize: (n, s, p) => `${n} ${n === 1 ? s : (p || s + 's')}`,
  API: {
    fetchCompetitionDetails: vi.fn().mockResolvedValue(null),
    recordDecision: vi.fn().mockResolvedValue({ applied: true }),
  },
};

let restore, AdminScoreEditor;

beforeAll(async () => {
  window.scrollTo = vi.fn();
  restore = installWindowStubs(STUBS);
  const mod = await import('../../admin_schedule_score_editor.jsx');
  AdminScoreEditor = mod.AdminScoreEditor;
});
afterAll(() => restore());

function mount(onEditScore = vi.fn()) {
  return render(
    <AdminScoreEditor
      t={{ competitions: [{ id: 'c1', name: 'Cup' }] }}
      onEditScore={onEditScore}
      onMoveCourt={null}
      password="pw"
      showToast={vi.fn()}
    />
  );
}

describe('the Scores tab and a barred match (bc-cse)', () => {
  it("the barred match's row shows the note and calls recordDecision with the fusensho body", async () => {
    let utils;
    await act(async () => { utils = mount(); });
    const notice = utils.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('Yama withdrew: record the default win.');
    const btn = utils.getByTestId('barred-match-default-win');
    expect(btn.textContent).toBe('Record default win for Umi');

    await act(async () => { btn.click(); });
    expect(window.API.recordDecision).toHaveBeenCalledWith('c1', 'm-barred', {
      decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Yama withdrawn',
    }, 'pw');
  });

  it('nextActiveMatch (Finish + Start Next) skips the barred match and advances to the next fightable one', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    let utils;
    await act(async () => { utils = mount(onEditScore); });

    // Open the running match: rows sort running first.
    const scoreBtns = [...utils.container.querySelectorAll('button.test-score-open')];
    await act(async () => { fireEvent.click(scoreBtns[0]); });
    expect(probe.props.match.id).toBe('m-run');

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(onEditScore).toHaveBeenCalledTimes(2);
    expect(onEditScore.mock.calls[0][1]).toBe('m-run');
    // m-barred is skipped; m-open is started instead.
    expect(onEditScore.mock.calls[1][1]).toBe('m-open');
  });

  // bc-cse: notLandedBanner, not writeWasSuperseded alone -- a clock_skew
  // refusal must read its own copy, never the superseded one (which sends
  // the operator to check a result that does not exist and tells them not
  // to do the one thing, re-entering, that would actually save it).
  it('a clock_skew refusal shows the clock copy, not the superseded one, and stays tappable', async () => {
    window.API.recordDecision = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew' });
    let utils;
    await act(async () => { utils = mount(); });
    const btn = utils.getByTestId('barred-match-default-win');
    await act(async () => { btn.click(); });

    const notice = utils.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain("this device's clock was out of step with the server");
    expect(notice.textContent).not.toContain('a newer result for this match is already recorded');
    // Not landed: the operator can retry once the clock has resynced.
    expect(btn.disabled).toBe(false);
    expect(btn.textContent).toBe('Record default win for Umi');
  });

  // bc-cse: after a landed (or queued -- see below) write the button must
  // lock, or a second tap before the SSE refetch repaints this row sends a
  // second /decision.
  it('locks the button after a successful record so a second tap sends only one /decision', async () => {
    window.API.recordDecision = vi.fn().mockResolvedValue({ applied: true });
    let utils;
    await act(async () => { utils = mount(); });
    const btn = utils.getByTestId('barred-match-default-win');
    await act(async () => { btn.click(); });

    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toBe('Recorded');
    await act(async () => { btn.click(); });
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
  });

  // bc-cse: a QUEUED write (offline court) is not yet stored but WILL land,
  // so the button locks the same way a landed write does -- a second tap
  // would double-enqueue -- while the notice says it is not saved yet.
  it('a queued write locks the button too and says it is not saved yet', async () => {
    window.API.recordDecision = vi.fn().mockResolvedValue({ queued: true });
    let utils;
    await act(async () => { utils = mount(); });
    const btn = utils.getByTestId('barred-match-default-win');
    await act(async () => { btn.click(); });

    expect(btn.disabled).toBe(true);
    const notice = utils.getByTestId('barred-match-notice');
    expect(notice.textContent).toContain('queued');
    await act(async () => { btn.click(); });
    expect(window.API.recordDecision).toHaveBeenCalledTimes(1);
  });
});
