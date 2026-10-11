// PR #463 round 13 (gap C, bc-aadv on the Scores tab): the Scores tab's own
// auto-advance (Finish + Start Next, and the start after a decision) used to
// swallow a refused start. A thrown 409 (eligibility, court_busy,
// already_ineligible) hit `catch (_startErr) {}`, a clock_skew refusal (HTTP 200
// {applied:false}) was never read and was taken for a start that landed, and
// nothing stopped a second start while one was out. The court console had all
// three (admin_shiaijo.jsx startMatch). Both now ask start_match.jsx.
//
// What the operator sees on a refusal: a toast (the moment's signal), a
// persistent notice on the REFUSED match's list row (role=status, the
// full-width slot BarredMatchNotice uses), and the editor open on that match in
// pre-match so they see which match and why.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll, beforeEach, afterEach } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';
import { CLOCK_SKEW_REASON_TEXT, startWhileStartingMessage, startWasBlockedByStartMessage, markToasted } from '../../write_result.jsx';
import { scoreRowMatchName } from '../../pool_ids.jsx';
import { START_SUPERSEDED_MESSAGE } from '../../start_match.jsx';

const side = (id, name) => ({ id, name });

const mk = (id, status, scheduledAt, a, b) => ({
  id, compId: 'c1', compName: 'Cup', status, court: 'A', scheduledAt, phase: 'pool', poolName: 'Pool A',
  sideA: side(`${id}-a`, a), sideB: side(`${id}-b`, b),
});
const M1 = mk('m-1', 'running', '09:00', 'Yamada', 'Tanaka');
const M2 = mk('m-2', 'scheduled', '09:05', 'Alice', 'Bob');
const M3 = mk('m-3', 'scheduled', '09:10', 'Carol', 'Dan');

// The matches the (stubbed) compMatches hands the page; a test swaps them to
// show a refused match leaving `scheduled`.
let matches;

// Captures the LATEST ScoreEditorModal props so a test can drive
// onSubmitAndNext / onAfterDecision directly, as the court console's suite does.
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
  API: { fetchCompetitionDetails: vi.fn().mockResolvedValue(null), recordDecision: vi.fn() },
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
  // Rows sort running first: the first Score button is m-1's.
  const scoreBtns = [...utils.container.querySelectorAll('button.test-score-open')];
  await act(async () => { fireEvent.click(scoreBtns[0]); });
  expect(probe.props.match.id).toBe('m-1');
  return utils;
}

const isStart = (call) => call[2] && call[2].startOnly === true;
const rowOf = (utils, name) => [...utils.container.querySelectorAll('.score-edit-row')].find((r) => r.textContent.includes(name));
const noticeIn = (row) => row && row.querySelector('[data-testid="start-refusal-notice"]');

const WITHDREW = 'Alice withdrew in Pool A · Match 2 and cannot fight again. Record the fusensho for Bob.';

describe('Finish + Start Next on the Scores tab says why the next match did not start', () => {
  it('a thrown refusal is noted on the refused match\'s row, toasted, and the editor lands on it in pre-match', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) throw new Error(WITHDREW);
      return { status: 'ok' };
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the refused match shows why on its own row').toBeTruthy();
    expect(note.textContent).toBe(WITHDREW);
    expect(note.getAttribute('role')).toBe('status');
    // No other row carries it.
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(1);
    expect(showToast).toHaveBeenCalledWith(WITHDREW, 'error');
    // The operator lands on the refused match, not started.
    expect(probe.props.match.id).toBe('m-2');
    expect(probe.props.match.status).toBe('scheduled');
  });

  // editMatchScore (admin.jsx) toasts what it throws and marks the error; the host
  // that catches it must not toast the same sentence again, which replaced the
  // single-slot toast and restarted its timer. The row still says it. The case
  // above is the unmarked one: a host whose onEditScore does not toast does.
  it('a thrown refusal onEditScore already toasted is not toasted again, and the row still says it', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) throw markToasted(new Error(WITHDREW));
      return { status: 'ok' };
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    expect(showToast).not.toHaveBeenCalled();
    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the refused match still says why on its own row').toBeTruthy();
    expect(note.textContent).toBe(WITHDREW);
    expect(probe.props.match.id).toBe('m-2');
  });

  it('a clock_skew refusal (HTTP 200 applied:false) is a refused start, not a landed one', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => (id === 'm-2' && patch.startOnly
      ? { applied: false, reason: 'clock_skew' }
      : { status: 'ok' }));
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    const sentence = `Could not start: ${CLOCK_SKEW_REASON_TEXT}. The clock has been resynced; try again.`;
    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the clock refusal is shown, not treated as a start that landed').toBeTruthy();
    expect(note.textContent).toBe(sentence);
    expect(showToast).toHaveBeenCalledWith(sentence, 'error');
    expect(probe.props.match.id).toBe('m-2');
  });

  it('a start that lands raises no notice and no toast', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ status: 'ok' });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-1', 'm-2']);
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(0);
    expect(showToast).not.toHaveBeenCalled();
    expect(probe.props.match.id).toBe('m-2');
  });

  it('a queued start lands on reconnect, so it is not a refusal', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => (id === 'm-2' && patch.startOnly ? { queued: true } : { status: 'ok' }));
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(0);
    expect(showToast).not.toHaveBeenCalled();
  });

  // PR #463 round 18 (this REPLACES the round-17 pin that asserted "no notice and
  // no toast", which encoded the opposite rule). A superseded start that holds
  // the result group (HTTP 200 applied:false, reason superseded, heldGroups
  // ['result']) wrote nothing: a different result is stored. It is a REFUSED start, through the same path a thrown or
  // clock_skew refusal takes: the sentence is stored on the refused match's row,
  // toasted once, and the editor lands on that match in pre-match.
  it('a superseded start is a refused start: its notice on the next match\'s row, one toast, the editor on that match', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => (id === 'm-2' && patch.startOnly
      ? { applied: false, reason: 'superseded', heldGroups: ['result'] }
      : { status: 'ok' }));
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-1', 'm-2']);
    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the refused match says why on its own row').toBeTruthy();
    expect(note.textContent).toBe(START_SUPERSEDED_MESSAGE);
    expect(note.getAttribute('role')).toBe('status');
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(1);
    expect(showToast.mock.calls).toEqual([[START_SUPERSEDED_MESSAGE, 'error']]);
    expect(probe.props.match.id).toBe('m-2');
    expect(probe.props.match.status).toBe('scheduled');
  });

  // PR #463 round 19 (S1). The same answer WITHOUT heldGroups is an echo-hold:
  // another device's start of m-2 landed first, so m-2 IS running. That is a start
  // that went out, treated as a landed one ('a start that lands raises no notice
  // and no toast' above): no notice, no toast, the editor on the next match.
  it('a superseded start with no held group is an echo of a start that landed: no notice, no toast', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => (id === 'm-2' && patch.startOnly
      ? { applied: false, reason: 'superseded' }
      : { status: 'ok' }));
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    expect(onEditScore.mock.calls.map((c) => c[1])).toEqual(['m-1', 'm-2']);
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(0);
    expect(showToast).not.toHaveBeenCalled();
    expect(probe.props.match.id).toBe('m-2');
  });
});

describe('one start at a time on the Scores tab', () => {
  it('a Finish + Start Next while another match is still being started is refused, and says so on the refused match', async () => {
    let releaseStart;
    const onEditScore = vi.fn((_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) return new Promise((resolve) => { releaseStart = resolve; });
      return Promise.resolve({ status: 'ok' });
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    // First advance: m-1 finishes, the editor lands on m-2 and its start hangs.
    await act(async () => { probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(probe.props.match.id).toBe('m-2');
    expect(onEditScore.mock.calls.filter(isStart).map((c) => c[1])).toEqual(['m-2']);

    // Second advance, from the editor now on m-2: its finish goes out, but m-3's
    // start is refused because m-2's is still out.
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });

    const sentence = startWhileStartingMessage({ label: scoreRowMatchName(M2) });
    expect(onEditScore.mock.calls.filter(isStart).map((c) => c[1]), 'no second start went out').toEqual(['m-2']);
    const note = noticeIn(rowOf(utils, 'Carol'));
    expect(note, 'the refused match says why').toBeTruthy();
    expect(note.textContent).toBe(sentence);
    expect(showToast).toHaveBeenCalledWith(sentence, 'error');
    expect(probe.props.match.id).toBe('m-3');

    // That sentence describes the start still out. When it lands the court has a
    // running bout, so m-3 is not started on its own, and nothing else would say
    // it was asked for and never started: the row keeps the refusal, in the past
    // tense (bc-aadv, item 4).
    await act(async () => { releaseStart({ status: 'ok' }); });
    const kept = noticeIn(rowOf(utils, 'Carol'));
    expect(kept, 'the refusal does not vanish when the start lands').toBeTruthy();
    expect(kept.textContent).toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(M2) }));
    expect(kept.getAttribute('role')).toBe('status');
    expect(onEditScore.mock.calls.filter(isStart).map((c) => c[1]), 'and m-3 was not started on its own').toEqual(['m-2']);

    // It is bound to m-3 like any stored refusal here: it goes when m-3 leaves
    // scheduled in the live data.
    matches = [M1, M2, { ...M3, status: 'running' }];
    await act(async () => { utils.rerender(ui(onEditScore, showToast)); });
    expect(noticeIn(rowOf(utils, 'Carol'))).toBeFalsy();
  });
});

describe('a start that was waited for and then refused', () => {
  // The same rule as the court console's: a start that does not land leaves the
  // court free, so the waiting match gets no past-tense sentence, and the failed
  // start's own refusal (stored for it before the wait ends) is what stays.
  it('keeps its own refusal on its row, and the waiting row shows nothing', async () => {
    let failStart;
    const onEditScore = vi.fn((_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) return new Promise((_resolve, reject) => { failStart = reject; });
      return Promise.resolve({ status: 'ok' });
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { probe.props.onSubmitAndNext({ status: 'completed' }); });
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(noticeIn(rowOf(utils, 'Carol'))?.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(M2) }));

    await act(async () => { failStart(new Error(WITHDREW)); });
    expect(noticeIn(rowOf(utils, 'Alice'))?.textContent, 'the failed start keeps its own refusal').toBe(WITHDREW);
    expect(noticeIn(rowOf(utils, 'Carol')), 'the court is free: no past-tense notice').toBeFalsy();
  });

  // PR #463 round 18: a SUPERSEDED start is a start that wrote nothing, so it
  // ends like the thrown one above: the court is free, "was being started" would
  // be false, and its own refusal (the superseded sentence) is stored for it,
  // which replaces the refusal that waited on it (refuseStart keeps a single
  // slot and clears `waitingOn`, so the finally has nothing left to rewrite).
  it('a start that is superseded keeps its own refusal on its row, and the waiting row gets no past-tense notice', async () => {
    let releaseStart;
    const onEditScore = vi.fn((_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) return new Promise((resolve) => { releaseStart = resolve; });
      return Promise.resolve({ status: 'ok' });
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { probe.props.onSubmitAndNext({ status: 'completed' }); });
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(noticeIn(rowOf(utils, 'Carol'))?.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(M2) }));

    await act(async () => { releaseStart({ applied: false, reason: 'superseded', heldGroups: ['result'] }); });
    expect(noticeIn(rowOf(utils, 'Alice'))?.textContent, 'the superseded start keeps its own refusal').toBe(START_SUPERSEDED_MESSAGE);
    expect(noticeIn(rowOf(utils, 'Carol')), 'nothing was started: no past-tense notice on the waiting row').toBeFalsy();
    expect(utils.container.textContent).not.toContain('was being started');
    expect(onEditScore.mock.calls.filter(isStart).map((c) => c[1]), 'and m-3 was not started on its own').toEqual(['m-2']);
    expect(showToast.mock.calls.map((c) => c[0])).toEqual([startWhileStartingMessage({ label: scoreRowMatchName(M2) }), START_SUPERSEDED_MESSAGE]);
    expect(probe.props.match.id, 'the editor lands on the match that did not start').toBe('m-2');
  });

  // S1: the echo-hold is a start that went out, so the refusal that waited on it
  // is still accurate and is worded in the past tense, as for a landed start.
  it('a start held as an echo went out, so the waiting row gets the past-tense notice', async () => {
    let releaseStart;
    const onEditScore = vi.fn((_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) return new Promise((resolve) => { releaseStart = resolve; });
      return Promise.resolve({ status: 'ok' });
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { probe.props.onSubmitAndNext({ status: 'completed' }); });
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(noticeIn(rowOf(utils, 'Carol'))?.textContent).toBe(startWhileStartingMessage({ label: scoreRowMatchName(M2) }));

    await act(async () => { releaseStart({ applied: false, reason: 'superseded' }); });
    expect(noticeIn(rowOf(utils, 'Carol'))?.textContent, 'the waiting refusal was accurate: it is rewritten, not cleared')
      .toBe(startWasBlockedByStartMessage({ label: scoreRowMatchName(M2) }));
    expect(noticeIn(rowOf(utils, 'Alice')), 'm-2 started: it has no refusal of its own').toBeFalsy();
    expect(utils.container.textContent).not.toContain(START_SUPERSEDED_MESSAGE);
    expect(showToast.mock.calls.map((c) => c[0])).toEqual([startWhileStartingMessage({ label: scoreRowMatchName(M2) })]);
  });
});

describe('the start after a decision says why too', () => {
  it('onAfterDecision lands on the refused match with the notice', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) throw new Error(WITHDREW);
      return { status: 'ok' };
    });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onAfterDecision({ winner: side('m-1-a', 'Yamada') }); });

    expect(onEditScore).toHaveBeenCalledTimes(1);
    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the decision path shows the refusal too').toBeTruthy();
    expect(note.textContent).toBe(WITHDREW);
    expect(showToast).toHaveBeenCalledWith(WITHDREW, 'error');
    // Today it left the operator where they were; the console lands them on the match.
    expect(probe.props.match.id).toBe('m-2');
  });

  it('a clock_skew refusal after a decision is a refusal as well', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: false, reason: 'clock_skew' });
    const utils = await mountAndOpenRunning(onEditScore, vi.fn());

    await act(async () => { await probe.props.onAfterDecision({ winner: side('m-1-a', 'Yamada') }); });

    expect(noticeIn(rowOf(utils, 'Alice')).textContent).toContain(CLOCK_SKEW_REASON_TEXT);
    expect(probe.props.match.id).toBe('m-2');
  });

  // See the superseded case above (round 18: it replaces the round-17 pin that
  // asserted no notice and no toast). refuseStart opens the refused match, so
  // onAfterDecision lands on m-2 either way; what changed is that the operator is
  // told why it is still scheduled.
  it('a superseded start after a decision lands on the next match with its notice and one toast', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: false, reason: 'superseded', heldGroups: ['result'] });
    const showToast = vi.fn();
    const utils = await mountAndOpenRunning(onEditScore, showToast);

    await act(async () => { await probe.props.onAfterDecision({ winner: side('m-1-a', 'Yamada') }); });

    expect(onEditScore).toHaveBeenCalledTimes(1);
    const note = noticeIn(rowOf(utils, 'Alice'));
    expect(note, 'the decision path shows the refusal too').toBeTruthy();
    expect(note.textContent).toBe(START_SUPERSEDED_MESSAGE);
    expect(utils.container.querySelectorAll('[data-testid="start-refusal-notice"]')).toHaveLength(1);
    expect(showToast.mock.calls).toEqual([[START_SUPERSEDED_MESSAGE, 'error']]);
    expect(probe.props.match.id).toBe('m-2');
  });
});

describe('the refusal notice goes when it stops being true', () => {
  async function refused() {
    const onEditScore = vi.fn(async (_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) throw new Error(WITHDREW);
      return { status: 'ok' };
    });
    const utils = await mountAndOpenRunning(onEditScore, vi.fn());
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(noticeIn(rowOf(utils, 'Alice'))).toBeTruthy();
    return { utils, onEditScore };
  }

  it('when the refused match leaves scheduled in the live data', async () => {
    const { utils, onEditScore } = await refused();
    matches = [M1, { ...M2, status: 'running' }, M3];
    await act(async () => { utils.rerender(ui(onEditScore, vi.fn())); });
    expect(noticeIn(rowOf(utils, 'Alice'))).toBeFalsy();
  });

  // PR #463 round 15 (F): the court console keeps a refusal across an empty list
  // (a list that is empty while it loads says nothing about the match), and the
  // Scores tab asks the same rule (start_match.jsx startRefusalStands).
  it('but not across an empty list: it says nothing about the match', async () => {
    const { utils, onEditScore } = await refused();
    matches = [];
    await act(async () => { utils.rerender(ui(onEditScore, vi.fn())); });
    matches = [M1, M2, M3];
    await act(async () => { utils.rerender(ui(onEditScore, vi.fn())); });
    expect(noticeIn(rowOf(utils, 'Alice')), 'the same refusal is still there').toBeTruthy();
    expect(noticeIn(rowOf(utils, 'Alice')).textContent).toBe(WITHDREW);
  });

  it('when a list that still holds matches no longer holds the refused one', async () => {
    const { utils, onEditScore } = await refused();
    matches = [M1, M3];
    await act(async () => { utils.rerender(ui(onEditScore, vi.fn())); });
    matches = [M1, M2, M3];
    await act(async () => { utils.rerender(ui(onEditScore, vi.fn())); });
    expect(noticeIn(rowOf(utils, 'Alice')), 'moved away and back is a new situation').toBeFalsy();
  });

  it('when the operator opens another match', async () => {
    const { utils } = await refused();
    const carolScore = rowOf(utils, 'Carol').querySelector('button.test-score-open');
    await act(async () => { fireEvent.click(carolScore); });
    expect(probe.props.match.id).toBe('m-3');
    expect(noticeIn(rowOf(utils, 'Alice'))).toBeFalsy();
  });

  it('but not when the editor is only closed: the row is where it stays visible', async () => {
    const { utils } = await refused();
    await act(async () => { probe.props.onClose(); });
    expect(utils.container.querySelector('[data-testid="score-editor"]')).toBeNull();
    expect(noticeIn(rowOf(utils, 'Alice'))).toBeTruthy();
  });
});

describe('the Scores page hands its toast down', () => {
  // AdminScoreEditor took a showToast prop that no host passed, so the toast
  // this fix adds would have reached nobody: the page must hand its own down.
  it('AdminScoreEditorPage passes showToast to the editor', async () => {
    const onEditScore = vi.fn(async (_c, id, patch) => {
      if (id === 'm-2' && patch.startOnly) throw new Error(WITHDREW);
      return { status: 'ok' };
    });
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
    await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed' }); });
    expect(showToast).toHaveBeenCalledWith(WITHDREW, 'error');
  });
});

// PR #463 batch 14, item 2: the Scores tab's completed team row asks
// teamMatchMarks by ES import from the side_marks.jsx leaf. It used to read
// window.teamMatchMarks and, with the global missing, painted no mark and no
// error. This suite never publishes the global, which is the point.
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
      // This surface's own team-row signal is a non-empty subResults (a bout
      // fought before the withdrawal), the default teamMatchMarks applies.
      compKind: 'team', teamSize: 3, decision: 'kiken-voluntary', decisionBy: 'shiro',
      subResults: [{ position: 1, sideA: 'Hana-1', sideB: 'Ichi-1', winner: 'Hana-1' }],
      winner: side('m-4-a', 'Hana'),
    }];
    let utils;
    await act(async () => { utils = render(ui(vi.fn(), vi.fn())); });
    const row = rowOf(utils, 'Hana');
    expect(row, 'the completed row is listed').toBeTruthy();
    expect(row.querySelector('[data-testid="team-summary-mark-shiro"]')?.textContent).toBe('Kiken');
    expect(row.querySelector('[data-testid="team-summary-mark-aka"]')).toBeNull();
    expect(row.querySelector('.score-edit-row__side--aka').classList.contains('score-edit-row__side--win')).toBe(true);
  });
});
