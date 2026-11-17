import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, probe, mountCourt, courtSide, courtMatch } from '../helpers/shiaijo_console.js';
import { startWhileStartingMessage, markToasted, startWasBlockedByStartMessage } from '../../write_result.jsx';
import { scoreRowMatchName } from '../../pool_ids.jsx';
import { START_SUPERSEDED_MESSAGE } from '../../start_match.jsx';

setupShiaijoConsole();

// PR #463 batch 14, item 2: the queue row asks winnerSideLR and teamMatchMarks
// by ES import from the side_marks.jsx leaf, not off window. The old `?.` guard
// painted NOTHING when the global was missing: no winner cue and no Kiken mark,
// with no error to say why. This suite never publishes either global, which is
// the point: the row must not need them.
describe('a completed queue row marks its winner and the withdrawn team without any window global', () => {
  const saved = {};
  beforeEach(() => {
    saved.winnerSideLR = window.winnerSideLR;
    saved.teamMatchMarks = window.teamMatchMarks;
    delete window.winnerSideLR;
    delete window.teamMatchMarks;
  });
  afterEach(() => {
    if (saved.winnerSideLR) window.winnerSideLR = saved.winnerSideLR;
    if (saved.teamMatchMarks) window.teamMatchMarks = saved.teamMatchMarks;
  });
  const completedRow = (utils, name) => [...utils.container.querySelectorAll('.shiaijo-qrow--complete')]
    .find((r) => r.textContent.includes(name));

  it('a completed team row carries the winner cue on the winner and Kiken beside the withdrawn team', async () => {
    const c = await mountCourt([
      courtMatch('m1', 'completed', {
        compKind: 'team', teamSize: 3, subResults: [], modifiedAt: 1000,
        winner: courtSide('m1-a', 'Aka m1'), decision: 'kiken-voluntary', decisionBy: 'shiro',
      }),
      courtMatch('m2', 'scheduled'),
    ]);
    try {
      const row = completedRow(c.utils, 'Aka m1');
      expect(row, 'the completed row is listed').toBeTruthy();
      expect(row.querySelector('.shiaijo-qrow__side--aka').classList.contains('shiaijo-qrow__side--win')).toBe(true);
      expect(row.querySelector('.shiaijo-qrow__side--shiro').classList.contains('shiaijo-qrow__side--win')).toBe(false);
      expect(row.querySelector('[data-testid="team-summary-mark-shiro"]')?.textContent).toBe('Kiken');
      expect(row.querySelector('[data-testid="team-summary-mark-aka"]')).toBeNull();
    } finally { c.restore(); }
  });
});

// PR #463 batch 14, item 10: a refusal the host's onEditScore already toasted
// (editMatchScore, admin.jsx, marks every error it toasts) is not toasted a second
// time by the console's Start, which replaced the single-slot toast and restarted
// its timer. The card or row still says it. An error nobody marked is the console's
// to toast (a mocked onEditScore that does not toast, a clock_skew answer).
describe('the console toasts a thrown Start refusal once', () => {
  const SENTENCE = 'Kato is fighting on shiaijo B';
  const startButton = (el) => [...el.querySelectorAll('button')].find((b) => /^start/i.test(b.textContent.trim()));
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const mountRefused = (error) => mountCourt(
    [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')],
    { onEditScore: vi.fn().mockRejectedValue(error) },
  );

  it('the Up next card: a marked refusal toasts nothing here and shows on the card', async () => {
    const c = await mountRefused(markToasted(new Error(SENTENCE)));
    try {
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      await act(async () => { startButton(card).click(); });
      await act(async () => {});
      expect(c.showToast).not.toHaveBeenCalled();
      expect(card.querySelector('.shiaijo-upnext__error')?.textContent).toBe(SENTENCE);
    } finally { c.restore(); }
  });

  it('a queue row: a marked refusal toasts nothing here and shows on that row', async () => {
    const c = await mountRefused(markToasted(new Error(SENTENCE)));
    try {
      await act(async () => { startButton(rowOf(c, 'Aka m2')).click(); });
      await act(async () => {});
      expect(c.showToast).not.toHaveBeenCalled();
      expect(rowOf(c, 'Aka m2').querySelector('.shiaijo-upnext__error')?.textContent).toBe(SENTENCE);
    } finally { c.restore(); }
  });

  it('an error nobody marked is toasted once by the console, on the card and on a row', async () => {
    const c = await mountRefused(new Error(SENTENCE));
    try {
      await act(async () => { startButton(c.utils.container.querySelector('.shiaijo-upnext__card')).click(); });
      await act(async () => {});
      expect(c.showToast.mock.calls).toEqual([[SENTENCE, 'error']]);
      c.showToast.mockClear();
      await act(async () => { startButton(rowOf(c, 'Aka m2')).click(); });
      await act(async () => {});
      expect(c.showToast.mock.calls).toEqual([[SENTENCE, 'error']]);
    } finally { c.restore(); }
  });
});

// PR #463 round 18: a Start the server answered SUPERSEDED (HTTP 200
// {applied:false, reason:'superseded'}: a newer change to the match is stored,
// nothing of the start was written) is a REFUSED start on the console, through
// the path a thrown or clock_skew refusal takes: the sentence is stored on the
// refused match (its Up next card or its queue row, shown while it is still
// scheduled), toasted once, and the panel is not pinned on a match the server
// did not start. Before this it said nothing at all.
describe('the console reports a superseded Start that holds its result as a refused one', () => {
  const startButton = (el) => [...el.querySelectorAll('button')].find((b) => /^start/i.test(b.textContent.trim()));
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const mountSuperseded = () => mountCourt(
    [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')],
    { onEditScore: vi.fn().mockResolvedValue({ applied: false, reason: 'superseded', heldGroups: ['result'] }) },
  );

  it('the Up next card: the superseded sentence on the card, one toast, the panel not pinned', async () => {
    const c = await mountSuperseded();
    try {
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      await act(async () => { startButton(card).click(); });
      await act(async () => {});
      expect(card.querySelector('.shiaijo-upnext__error[role="alert"]')?.textContent).toBe(START_SUPERSEDED_MESSAGE);
      expect(c.showToast.mock.calls).toEqual([[START_SUPERSEDED_MESSAGE, 'error']]);
      expect(c.editorMatch(), 'nothing was started: the panel is not pinned on m1').toBeNull();
    } finally { c.restore(); }
  });

  it('a queue row: the superseded sentence on that row, one toast, the panel not pinned', async () => {
    const c = await mountSuperseded();
    try {
      await act(async () => { startButton(rowOf(c, 'Aka m2')).click(); });
      await act(async () => {});
      expect(rowOf(c, 'Aka m2').querySelector('.shiaijo-upnext__error[role="alert"]')?.textContent).toBe(START_SUPERSEDED_MESSAGE);
      expect(c.showToast.mock.calls).toEqual([[START_SUPERSEDED_MESSAGE, 'error']]);
      expect(c.editorMatch()).toBeNull();
    } finally { c.restore(); }
  });

  // The notice is bound to the match like every stored Start refusal: when the
  // other device's change that superseded the start leaves the match running (it
  // started there), the notice drops (startRefusalStands) and the toast was the
  // only thing the operator saw.
  it('the notice goes when the match is no longer scheduled in the live data', async () => {
    const c = await mountSuperseded();
    try {
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      await act(async () => { startButton(card).click(); });
      await act(async () => {});
      expect(c.utils.container.textContent).toContain(START_SUPERSEDED_MESSAGE);
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      await act(async () => {});
      expect(c.utils.container.textContent).not.toContain(START_SUPERSEDED_MESSAGE);
    } finally { c.restore(); }
  });

  // PR #463 round 19 (S1). A superseded start WITHOUT heldGroups is an echo-hold:
  // another device's start of this match landed first with a later stamp, so the
  // match IS running. It is a start that went out, like a landed one: no sentence,
  // no toast, and the panel pins on the match. (A PICK's echo is tests i and j in
  // the past-tense describe above.)
  describe('an echo-held superseded Start (no heldGroups) is a start that went out', () => {
    const mountEcho = () => mountCourt(
      [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')],
      { onEditScore: vi.fn().mockResolvedValue({ applied: false, reason: 'superseded' }) },
    );

    it('the Up next card: no sentence, no toast, the panel pinned on the match', async () => {
      const c = await mountEcho();
      try {
        const card = c.utils.container.querySelector('.shiaijo-upnext__card');
        await act(async () => { startButton(card).click(); });
        await act(async () => {});
        expect(card.querySelector('.shiaijo-upnext__error'), 'no refusal sentence on the card').toBeNull();
        expect(c.utils.container.textContent).not.toContain('Not started');
        expect(c.showToast, 'no toast').not.toHaveBeenCalled();
        expect(c.editorMatch(), 'the start went out: the panel is pinned on m1').toBe('m1');
      } finally { c.restore(); }
    });

    it('a queue row: no sentence, no toast, the panel pinned on the match', async () => {
      const c = await mountEcho();
      try {
        await act(async () => { startButton(rowOf(c, 'Aka m2')).click(); });
        await act(async () => {});
        expect(rowOf(c, 'Aka m2'), 'the started bout left the queue (stage 6 review 1)').toBeUndefined();
        expect(c.utils.container.querySelector('.shiaijo-upnext__error'), 'no refusal sentence').toBeNull();
        expect(c.utils.container.textContent).not.toContain('Not started');
        expect(c.showToast, 'no toast').not.toHaveBeenCalled();
        expect(c.editorMatch(), 'the start went out: the panel is pinned on m2').toBe('m2');
      } finally { c.restore(); }
    });
  });
});

// PR #463 batch 14, item 4 (bc-aadv): a stored Start refusal belongs to the match
// it was refused for. It stays on that match's card or queue row for as long as
// the match is still scheduled on this court, and goes when the match leaves
// scheduled, when a NON-EMPTY court list no longer holds it, when eligibility
// moves in its competition (the adapted test above) or when the operator starts
// another match. Up next changing by itself no longer drops it, and a Finish +
// Start Next refusal is no different from a tap's.
describe('a stored Start refusal is bound to the refused match', () => {
  const REFUSAL = 'Kato is fighting on shiaijo B';
  const refusing = () => vi.fn().mockRejectedValue(new Error(REFUSAL));
  const three = () => [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')];
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const card = (c) => c.utils.container.querySelector('.shiaijo-upnext__card');
  const startIn = (el) => [...el.querySelectorAll('button')].find((b) => /^start/i.test(b.textContent.trim()));
  const noteIn = (el) => el?.querySelector('.shiaijo-upnext__error')?.textContent ?? null;
  const refuseOnRow = async (c, text) => {
    await act(async () => { startIn(rowOf(c, text)).click(); });
    await act(async () => {});
  };

  it('stays on its queue row when Up next moves on to another match while it is still scheduled', async () => {
    const c = await mountCourt(three(), { onEditScore: refusing() });
    try {
      await refuseOnRow(c, 'Aka m3');
      expect(noteIn(rowOf(c, 'Aka m3'))).toBe(REFUSAL);
      // m1 starts elsewhere: Up next is now m2, and m3 is still waiting here.
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')];
      await c.refresh();
      expect(card(c).textContent).toContain('Aka m2');
      expect(noteIn(card(c)), 'the refusal is about m3, not about Up next').toBeNull();
      expect(noteIn(rowOf(c, 'Aka m3'))).toBe(REFUSAL);
    } finally { c.restore(); }
  });

  it('goes when the refused match leaves scheduled, and does not come back when it is sent back to the queue', async () => {
    const c = await mountCourt(three(), { onEditScore: refusing() });
    try {
      await refuseOnRow(c, 'Aka m3');
      expect(noteIn(rowOf(c, 'Aka m3'))).toBe(REFUSAL);
      c.feed.current = [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'running')];
      await c.refresh();
      c.feed.current = three();
      await c.refresh();
      expect(rowOf(c, 'Aka m3'), 'm3 is back in the queue').toBeTruthy();
      expect(noteIn(rowOf(c, 'Aka m3')), 'a stale refusal never revives').toBeNull();
    } finally { c.restore(); }
  });

  it('goes when a non-empty court list no longer holds the match (moved), and does not come back', async () => {
    const c = await mountCourt(three(), { onEditScore: refusing() });
    try {
      await refuseOnRow(c, 'Aka m3');
      c.feed.current = [courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      c.feed.current = three();
      await c.refresh();
      expect(noteIn(rowOf(c, 'Aka m3')), 'moved away and back is a new situation').toBeNull();
    } finally { c.restore(); }
  });

  it('stays across a refetch that answers an empty list and then the same list again', async () => {
    const c = await mountCourt(three(), { onEditScore: refusing() });
    try {
      await refuseOnRow(c, 'Aka m3');
      // The list is empty while a feed loads; a transient empty answer says
      // nothing about the match, so the refusal is left where it is.
      c.feed.current = [];
      await c.refresh();
      c.feed.current = three();
      await c.refresh();
      expect(noteIn(rowOf(c, 'Aka m3'))).toBe(REFUSAL);
    } finally { c.restore(); }
  });

  it('is bound the same way when it is a Finish + Start Next that was refused', async () => {
    // m2 is Up next after m1 finishes, and its start is refused. No `advance`
    // option is left to pass: the refusal outlives a change of Up next exactly
    // as a tap's does, while m2 is still scheduled.
    const onEditScore = vi.fn(async (_c, id) => { if (id === 'm2') throw new Error(REFUSAL); return { applied: true }; });
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')], { onEditScore });
    try {
      await act(async () => { await probe.props.onSubmitAndNext({ status: 'completed', winner: courtSide('m1-a', 'Aka m1') }); });
      expect(noteIn(card(c))).toBe(REFUSAL);
      // The refetch: m1 done, and m3 is now scheduled ahead of m2.
      c.feed.current = [courtMatch('m1', 'completed'), courtMatch('m2', 'scheduled', { scheduledAt: '09:09' }), courtMatch('m3', 'scheduled')];
      await c.refresh();
      expect(card(c).textContent).toContain('Aka m3');
      expect(noteIn(card(c))).toBeNull();
      expect(noteIn(rowOf(c, 'Aka m2'))).toBe(REFUSAL);
      // And it goes with the match, as a tap's does.
      c.feed.current = [courtMatch('m1', 'completed'), courtMatch('m2', 'running'), courtMatch('m3', 'scheduled')];
      await c.refresh();
      c.feed.current = [courtMatch('m1', 'completed'), courtMatch('m2', 'scheduled', { scheduledAt: '09:09' }), courtMatch('m3', 'scheduled')];
      await c.refresh();
      expect(noteIn(rowOf(c, 'Aka m2'))).toBeNull();
    } finally { c.restore(); }
  });

  // (ii) An advance refused because another start was still out says so while
  // that start is out. When it lands, the court has a running bout, so the
  // refused match is NOT started on its own (one start at a time), and nothing
  // else would tell the operator it was asked for and never started: the refusal
  // is kept, in the past tense, on the refused match.
  it('an advance refused for a start still out keeps a past-tense notice on the refused match once that start lands', async () => {
    let landLater;
    const onEditScore = vi.fn((_c, id) => (id === 'm3'
      ? new Promise((r) => { landLater = r; })
      : Promise.resolve({ applied: true })));
    const c = await mountCourt([courtMatch('m1', 'running'), courtMatch('m2', 'scheduled'), courtMatch('m3', 'scheduled')], { onEditScore });
    try {
      // The editor holds this closure while its finish is out; m3's start is
      // begun from the queue meanwhile and hangs.
      const advance = probe.props.onSubmitAndNext;
      await act(async () => { startIn(rowOf(c, 'Aka m3')).click(); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((x) => x[1])).toEqual(['m3']);
      await act(async () => { await advance({ status: 'completed', winner: courtSide('m1-a', 'Aka m1') }); });
      // m2 is Up next and was asked for while m3's start was out: refused, present tense.
      const label = scoreRowMatchName(courtMatch('m3', 'scheduled'));
      expect(noteIn(card(c))).toBe(startWhileStartingMessage({ label }));
      expect(onEditScore.mock.calls.map((x) => x[1])).not.toContain('m2');

      await act(async () => { landLater({ applied: true }); });
      await act(async () => {});
      expect(noteIn(card(c)), 'the refusal does not vanish when the start lands').toBe(startWasBlockedByStartMessage({ label }));
      expect(onEditScore.mock.calls.map((x) => x[1]), 'and m2 was not started on its own').not.toContain('m2');

      // A Start on it clears the sentence and sends the start.
      await act(async () => { startIn(card(c)).click(); });
      await act(async () => {});
      expect(onEditScore.mock.calls.map((x) => x[1])).toContain('m2');
      expect(noteIn(card(c))).toBeNull();
    } finally { c.restore(); }
  });
});

// PR #463 batch 14, item 11: the console imports startPatch from start_match.jsx
// (the one owner of how a Start is written) instead of wrapping window.startPatch,
// which threw "admin_schedule.jsx not loaded" when the global was absent. This
// suite publishes none (it used to publish a bare vi.fn()); the test removes any
// another file left behind, so the console cannot be reading it.
describe('the console writes a Start without any window.startPatch', () => {
  it('Start on the Up next card sends the real start patch', async () => {
    const saved = window.startPatch;
    delete window.startPatch;
    const onEditScore = vi.fn().mockResolvedValue({ applied: true });
    const c = await mountCourt([courtMatch('m1', 'scheduled')], { onEditScore });
    try {
      const start = [...c.utils.container.querySelector('.shiaijo-upnext__card').querySelectorAll('button')]
        .find((b) => /^start/i.test(b.textContent.trim()));
      await act(async () => { start.click(); });
      await act(async () => {});
      expect(onEditScore).toHaveBeenCalledTimes(1);
      const patch = onEditScore.mock.calls[0][2];
      expect(patch?.status).toBe('running');
      expect(patch?.startOnly).toBe(true);
      expect(c.showToast, 'no "factory unavailable" toast').not.toHaveBeenCalled();
    } finally {
      c.restore();
      if (saved) window.startPatch = saved;
    }
  });
});
