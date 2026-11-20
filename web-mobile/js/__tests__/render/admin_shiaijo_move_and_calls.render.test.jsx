import { act, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { setupShiaijoConsole, mountCourt, courtSide, courtMatch } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

// P1-T6: the console's move confirm captures `shown: mm` when the dialog
// opens and hands `{ confirmed: true, shown: pendingMove.shown }` to
// `onMoveCourt`. A match this console just started leaves the queue,
// so no queue row offers it a court move.
describe('court move confirm snapshot (P1-T6)', () => {
  const rowOf = (c, text) => [...c.utils.container.querySelectorAll('.shiaijo-qrow')].find((r) => r.textContent.includes(text));
  const moveDialog = (c) => c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');

  it('a waiting match keeps the waiting words and passes { confirmed: true, shown } from when the dialog opened', async () => {
    const onMoveCourt = vi.fn().mockResolvedValue(true);
    const m1 = courtMatch('m1', 'scheduled', { modifiedAt: 100 });
    const m2 = courtMatch('m2', 'scheduled', { modifiedAt: 200 });
    const c = await mountCourt([m1, m2], { onMoveCourt });
    try {
      await act(async () => { within(rowOf(c, 'Aka m2')).getByTestId('move-to-B').click(); });
      const dialog = moveDialog(c);
      expect(dialog.querySelector('.shiaijo-move-confirm__title').textContent).toBe('Move to Shiaijo B?');
      expect(dialog.querySelector('.shiaijo-move-confirm__body').textContent)
        .toBe('Shiro m2 vs Aka m2 leaves Shiaijo A and goes last in the queue on Shiaijo B.');
      expect(within(dialog).getByRole('button', { name: 'Cancel' })).toBeTruthy();

      // Even if the feed changes m2 before Confirm is clicked, confirmMoveCourt
      // sends the snapshot captured when the dialog opened, never re-reading sorted.
      c.feed.current = [m1, { ...m2, status: 'running', modifiedAt: 900 }];
      await c.refresh();

      await act(async () => { within(moveDialog(c)).getByRole('button', { name: 'Move to Shiaijo B' }).click(); });
      expect(onMoveCourt).toHaveBeenCalledTimes(1);
      expect(onMoveCourt).toHaveBeenCalledWith('c1', 'm2', 'B', {
        confirmed: true,
        shown: expect.objectContaining({ id: 'm2', compId: 'c1', status: 'scheduled', modifiedAt: 200 }),
      });
    } finally { c.restore(); }
  });

  // Only a waiting row offers a court move on the console (showActions): a
  // finished row shows Correct alone, so the confirm never speaks for one.
  it('a finished row offers no court move', async () => {
    const m1 = courtMatch('m1', 'completed', { modifiedAt: 100, winner: 'Aka m1' });
    const m2 = courtMatch('m2', 'scheduled', { modifiedAt: 200 });
    const m3 = courtMatch('m3', 'scheduled', { modifiedAt: 300 });
    const c = await mountCourt([m1, m2, m3], { onMoveCourt: vi.fn() });
    try {
      const done = c.utils.container.querySelector('.shiaijo-qrow--complete');
      expect(done && done.textContent).toContain('Aka m1');
      expect(within(done).queryByTestId('move-to-B')).toBeNull();
      expect(within(rowOf(c, 'Aka m3')).getByTestId('move-to-B')).toBeTruthy();
    } finally { c.restore(); }
  });

  // Since commit d8390747, `filteredScheduled` filters out `isStartedSnapshot(m)`,
  // so a match this console just started leaves Up next and Upcoming as soon as
  // `startedFrom` is set. No queue row offers it a court move.
  it('a match this console just started leaves Up next and Upcoming, so no queue row offers it a court move', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true, pending: true, key: 'k-start', settled: new Promise(() => {}) });
    const onMoveCourt = vi.fn().mockResolvedValue(true);
    const m2 = courtMatch('m2', 'scheduled', { modifiedAt: 100 });
    const m3 = courtMatch('m3', 'scheduled', { modifiedAt: 100 });
    const c = await mountCourt([m2, m3], { onEditScore, onMoveCourt });
    try {
      expect(rowOf(c, 'Aka m3')).toBeTruthy();
      const startBtn = [...rowOf(c, 'Aka m3').querySelectorAll('button')].find((b) => /^start/i.test(b.textContent.trim()));
      await act(async () => { startBtn.click(); });
      await act(async () => {});
      expect(c.editorMatch()).toBe('m3');

      // The match leaves Up next and Upcoming: no queue row offers it a move
      expect(rowOf(c, 'Aka m3')).toBeFalsy();
      expect(onMoveCourt).not.toHaveBeenCalled();
    } finally {
      c.restore();
    }
  });
});

// bc-obox A1: a court call is an outbox entry. The send answers at once with
// the id the device made and a `settled` promise; the console keeps that id,
// so a withdrawal can name the call before it has landed, and a refusal is
// told once by the outbox, never by a catch-and-log here.
describe('Court calls through the outbox (bc-obox)', () => {
  const CALL = 'Announce this match to spectators and competitors';
  const callAnswer = (id) => {
    let settle, refuse;
    const settled = new Promise((resolve, reject) => { settle = resolve; refuse = reject; });
    return { answer: { applied: true, pending: true, key: `k-${id}`, settled, id }, settle, refuse };
  };
  const withdrawnAnswer = () => ({ applied: true, pending: true, key: 'k-del', settled: Promise.resolve(true) });
  const callOf = (c) => c.utils.getAllByTitle(CALL)[0];
  const tapCall = async (c) => { await act(async () => { callOf(c).click(); }); await act(async () => {}); };
  let prev;
  beforeEach(() => {
    prev = { send: window.API.sendAnnouncement, del: window.API.deleteAnnouncement };
    window.API.deleteAnnouncement = vi.fn(async () => withdrawnAnswer());
  });
  afterEach(() => {
    window.API.sendAnnouncement = prev.send;
    window.API.deleteAnnouncement = prev.del;
  });

  it('withdraws the call by the id the send answered, before the send has landed, and tells nothing itself', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const call = callAnswer('ann-1');
    window.API.sendAnnouncement = vi.fn(async () => call.answer);
    const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
    try {
      await tapCall(c);
      expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(1);
      expect(c.showToast).toHaveBeenCalledWith(expect.stringMatching(/^Called /));
      expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

      // The match is started elsewhere while the call is still unsent.
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
      const [id, , opts] = window.API.deleteAnnouncement.mock.calls[0];
      expect(id).toBe('ann-1');
      expect(opts.message).toMatch(/^Now calling /);
      expect(warn).not.toHaveBeenCalled();
    } finally {
      warn.mockRestore();
      c.restore();
    }
  });

  it('a withdrawal the outbox refuses is not caught or logged here', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    const refused = Promise.reject(new Error('refused'));
    refused.catch(() => {});
    window.API.deleteAnnouncement = vi.fn(async () => ({ applied: true, pending: true, key: 'k', settled: refused }));
    window.API.sendAnnouncement = vi.fn(async () => callAnswer('ann-1').answer);
    const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
    try {
      await tapCall(c);
      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
      expect(warn.mock.calls.filter((a) => String(a[0]).includes('withdraw'))).toEqual([]);
    } finally {
      warn.mockRestore();
      c.restore();
    }
  });

  it('a call whose text changed takes the earlier one down only once the new one has landed', async () => {
    const first = callAnswer('ann-1');
    const second = callAnswer('ann-2');
    const answers = [first, second];
    window.API.sendAnnouncement = vi.fn(async () => answers.shift().answer);
    const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
    try {
      await tapCall(c);
      first.settle({ id: 'ann-1' });
      // A side's name resolved: the same match, a different text.
      c.feed.current = [courtMatch('m1', 'scheduled', { sideB: courtSide('m1-b', 'Shiro renamed') }), courtMatch('m2', 'scheduled')];
      await c.refresh();
      await tapCall(c);
      expect(window.API.sendAnnouncement).toHaveBeenCalledTimes(2);
      expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

      second.settle({ id: 'ann-2' });
      await act(async () => {});
      expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
      expect(window.API.deleteAnnouncement.mock.calls[0][0]).toBe('ann-1');
    } finally {
      c.restore();
    }
  });

  it('a refused new call leaves the earlier one up, and starting the match then withdraws that one', async () => {
    const first = callAnswer('ann-1');
    const second = callAnswer('ann-2');
    const answers = [first, second];
    window.API.sendAnnouncement = vi.fn(async () => answers.shift().answer);
    const c = await mountCourt([courtMatch('m1', 'scheduled'), courtMatch('m2', 'scheduled')]);
    try {
      await tapCall(c);
      first.settle({ id: 'ann-1' });
      c.feed.current = [courtMatch('m1', 'scheduled', { sideB: courtSide('m1-b', 'Shiro renamed') }), courtMatch('m2', 'scheduled')];
      await c.refresh();
      await tapCall(c);
      second.refuse(new Error('refused'));
      await act(async () => {});
      expect(window.API.deleteAnnouncement).not.toHaveBeenCalled();

      c.feed.current = [courtMatch('m1', 'running'), courtMatch('m2', 'scheduled')];
      await c.refresh();
      expect(window.API.deleteAnnouncement).toHaveBeenCalledTimes(1);
      expect(window.API.deleteAnnouncement.mock.calls[0][0]).toBe('ann-1');
    } finally {
      c.restore();
    }
  });
});
