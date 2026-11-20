import { act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { setupShiaijoConsole, probe, outbox, makeMinimalTournament, renderPage, mountCourt, courtMatch } from '../helpers/shiaijo_console.js';

setupShiaijoConsole();

// bc-sbq (operator ruling 2026-09-26): a match sent back to the queue keeps
// its score, so Send back to queue is always offered, its confirm says the
// score is kept, and picking another bout sends a scored one back rather than
// refusing to switch (a mistaken switch is undone with one tap on its Start).
describe('Send back to queue keeps the score and is always offered (bc-sbq)', () => {
  const tapSendBack = async (c) => {
    await act(async () => { c.utils.getByRole('button', { name: /send back to queue/i }).click(); });
    return c.utils.container.querySelector('.shiaijo-move-confirm[role="dialog"]');
  };

  it('is offered on a team match with a fought bout, and says the score is kept', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', {
      compKind: 'team', teamSize: 3, reopenPending: true,
      subResults: [
        { position: 1, sideA: 'A1', sideB: 'B1', ipponsA: ['M'], ipponsB: [], winner: 'A1' },
        { position: 2, sideA: 'A2', sideB: 'B2', ipponsA: [], ipponsB: [] },
      ],
    })]);
    try {
      expect(c.editorMatch()).toBe('m1');
      const dialog = await tapSendBack(c);
      expect(dialog.textContent).toContain('The score already saved is kept');
      expect(dialog.textContent).not.toMatch(/discard|lost/i);
    } finally { c.restore(); }
  });

  it('sends a scored bout back when confirmed', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'], hansokuB: 1 })]);
    try {
      await tapSendBack(c);
      const confirm = [...c.utils.container.querySelectorAll('.shiaijo-move-confirm button')]
        .find((b) => /send back to queue/i.test(b.textContent));
      await act(async () => { confirm.click(); });
      expect(window.API.revertMatchToQueue).toHaveBeenCalledWith('c1', 'm1', '', expect.objectContaining({ onRefusal: expect.any(Function) }));
    } finally { c.restore(); }
  });

  // Stage 6 review 7 (operator rule: "never blocks the UI", "screens display
  // what the operator changes"): a send back the outbox only holds (offline)
  // shows at once, as a finish does: the bout leaves the live slot and the
  // editor and shows queued; a refused or discarded one puts it back.
  describe('a send back still queued shows at once', () => {
    beforeEach(() => { outbox.clear(); });
    const pendingSendBack = () => {
      let settle;
      const settled = new Promise((resolve, reject) => { settle = { resolve, reject }; });
      settled.catch(() => {});
      return { res: { applied: true, pending: true, key: 'k', settled }, settle };
    };
    // The send back door, as the outbox holds it (M4): the move is the
    // outbox's, read by the console, not the page's.
    const sendBackDoor = (res) => vi.fn().mockImplementation((c, m) => Promise.resolve(
      outbox.hold(c, m, { status: 'scheduled', kind: 'revert', payload: {} }, res)));
    const confirmSendBack = async (c) => {
      await tapSendBack(c);
      const confirm = [...c.utils.container.querySelectorAll('.shiaijo-move-confirm button')]
        .find((b) => /send back to queue/i.test(b.textContent));
      await act(async () => { confirm.click(); });
    };
    const upNextText = (c) => {
      const card = c.utils.container.querySelector('.shiaijo-upnext__card');
      return card ? card.textContent : '';
    };
    for (const outcome of ['refused', 'discarded']) {
      it(`leaves the live slot at once offline, and comes back when ${outcome}`, async () => {
        const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'], modifiedAt: 100 })]);
        const { res, settle } = pendingSendBack();
        window.API.revertMatchToQueue = sendBackDoor(res);
        try {
          expect(c.editorMatch()).toBe('m1');
          await confirmSendBack(c);
          expect(c.editorMatch(), 'the editor no longer shows the bout').toBeNull();
          expect(upNextText(c), 'the bout shows queued').toContain('Aka m1');
          expect(c.utils.queryByRole('button', { name: /^send back to queue$/i })).toBeNull();
          // The feed still says running (nothing reached the server): no change.
          await c.refresh();
          expect(c.editorMatch()).toBeNull();
          await act(async () => {
            if (outcome === 'refused') settle.reject(new Error('Not sent back: the match had finished'));
            else settle.resolve({ discarded: true });
          });
          expect(c.editorMatch(), 'put back on the live slot').toBe('m1');
          expect(upNextText(c)).not.toContain('Aka m1');
        } finally { c.restore(); }
      });
    }
    it('stays queued once landed until the feed shows it, and ends there', async () => {
      const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'], modifiedAt: 100 }), courtMatch('m2', 'scheduled')]);
      const { res, settle } = pendingSendBack();
      window.API.revertMatchToQueue = sendBackDoor(res);
      try {
        await confirmSendBack(c);
        await act(async () => { settle.resolve({ applied: true, modifiedAt: 200 }); });
        expect(c.editorMatch()).toBeNull();
        // The feed shows the send back: the queued copy is the feed's own.
        c.feed.current = [courtMatch('m1', 'scheduled', { ipponsA: ['M'], modifiedAt: 200 }), courtMatch('m2', 'scheduled')];
        await c.refresh();
        expect(c.editorMatch()).toBeNull();
        // A later change made elsewhere (started again on another device) shows.
        c.feed.current = [courtMatch('m1', 'running', { ipponsA: ['M'], modifiedAt: 300 }), courtMatch('m2', 'scheduled')];
        await c.refresh();
        expect(c.editorMatch()).toBe('m1');
      } finally { c.restore(); }
    });

    // Stage 6 review 8 (M4): the send back is read from the outbox, never kept
    // in the page, so the console mounted again (the operator went back to
    // the court, or reloaded) still shows the bout queued, with no editor,
    // while the outbox holds it; and once the outbox lets it go unanswered
    // (refused), the bout is live again there too.
    it('a send back still queued: the console mounted again shows the bout queued', async () => {
      const feed = [courtMatch('m1', 'running', { ipponsA: ['M'], modifiedAt: 100 })];
      const c = await mountCourt(feed);
      const { res, settle } = pendingSendBack();
      window.API.revertMatchToQueue = sendBackDoor(res);
      try {
        await confirmSendBack(c);
        expect(c.editorMatch()).toBeNull();
      } finally { c.restore(); }
      c.utils.unmount();
      const again = await mountCourt(feed);
      try {
        expect(again.editorMatch(), 'no editor on the bout sent back').toBeNull();
        expect(upNextText(again), 'the bout shows queued').toContain('Aka m1');
        expect(again.utils.queryByRole('button', { name: /^send back to queue$/i })).toBeNull();
        await act(async () => { settle.reject(new Error('Not sent back: the match had finished')); });
        expect(again.editorMatch(), 'refused: live again').toBe('m1');
      } finally { again.restore(); }
    });
  });

  it('picking another bout sends a scored one back to the queue and starts the pick', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'] }), courtMatch('m2', 'scheduled')]);
    try {
      const startNext = c.utils.getAllByRole('button').find((b) => /^start/i.test(b.textContent.trim()));
      expect(startNext, 'the Up Next card offers Start for m2').toBeTruthy();
      await act(async () => { startNext.click(); });
      expect(window.API.revertMatchToQueue.mock.calls[0].slice(0, 3)).toEqual(['c1', 'm1', '']);
      expect(c.showToast).not.toHaveBeenCalledWith('Finish or correct the bout in progress first', 'error');
    } finally { c.restore(); }
  });

  // bc-evts review 1, minor 7: a send back names what the console showed
  // (`seen`, the match's scoreVersion), as every rule-bound write does.
  it('Send back to queue says what the console showed', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'], scoreVersion: 5 })]);
    try {
      await tapSendBack(c);
      const confirm = [...c.utils.container.querySelectorAll('.shiaijo-move-confirm button')]
        .find((b) => /send back to queue/i.test(b.textContent));
      await act(async () => { confirm.click(); });
      expect(window.API.revertMatchToQueue.mock.calls.at(-1)[3]).toEqual(expect.objectContaining({ seen: 5 }));
    } finally { c.restore(); }
  });

  it('the send back of a pick says what the console showed', async () => {
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'], scoreVersion: 6 }), courtMatch('m2', 'scheduled')]);
    try {
      const startNext = c.utils.getAllByRole('button').find((b) => /^start/i.test(b.textContent.trim()));
      await act(async () => { startNext.click(); });
      expect(window.API.revertMatchToQueue.mock.calls.at(-1)[3]).toEqual(expect.objectContaining({ seen: 6 }));
    } finally { c.restore(); }
  });

  // bc-evts (the governing rule): the send back is the outbox's and nothing
  // waits for its answer. The pick's start is queued at once, after it
  // (`after`: the send back's queue key), so the outbox sends the start only
  // once the send back has settled, and the console moves on at once.
  it('the pick does not wait for the send back: the start is queued after it at once', async () => {
    const onEditScore = vi.fn().mockResolvedValue({ applied: true, pending: true, key: 'k-start', settled: new Promise(() => {}) });
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'] }), courtMatch('m2', 'scheduled')], { onEditScore });
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({ applied: true, pending: true, key: 'k-back', settled: new Promise(() => {}) });
    try {
      const startNext = c.utils.getAllByRole('button').find((b) => /^start/i.test(b.textContent.trim()));
      await act(async () => { startNext.click(); });
      await act(async () => {});
      expect(window.API.revertMatchToQueue).toHaveBeenCalledTimes(1);
      // The send back's answer never came; the start went to the outbox anyway,
      // naming the send back it follows.
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(onEditScore.mock.calls[0][1]).toBe('m2');
      expect(onEditScore.mock.calls[0][2].after).toBe('k-back');
    } finally { c.restore(); }
  });

  // Z1 (stage 3 review 12): the pick pins the panel on the match it started;
  // a start the server refuses afterwards takes the pin back, so the console
  // shows what the server holds and not a match that never started.
  it('a pick whose start is refused later does not keep the panel on the refused match', async () => {
    probe.props = null;
    let refuse;
    const settled = new Promise((r) => { refuse = r; });
    const onEditScore = vi.fn().mockResolvedValue({ applied: true, pending: true, key: 'k-start', settled });
    const c = await mountCourt([courtMatch('m1', 'running', { ipponsA: ['M'] }), courtMatch('m2', 'scheduled')], { onEditScore });
    window.API.revertMatchToQueue = vi.fn().mockResolvedValue({ applied: true, pending: true, key: 'k-back', settled: new Promise(() => {}) });
    try {
      const startNext = c.utils.getAllByRole('button').find((b) => /^start/i.test(b.textContent.trim()));
      await act(async () => { startNext.click(); });
      await act(async () => {});
      expect(probe.props.match?.id).toBe('m2');
      await act(async () => { refuse({ applied: false, reason: 'clock_skew' }); await settled; });
      await act(async () => {});
      // The editor no longer shows the refused match (m1, whose send back is
      // still queued, reads queued: stage 6 review 7, so the court shows no
      // live bout and the editor is gone).
      expect(c.editorMatch()).not.toBe('m2');
    } finally { c.restore(); }
  });
});

// bc-strt: the console opens a match its Up next card started while the court
// list still says scheduled. It tells the editor so (`started`), for the
// scheduled snapshot the start was made from and no other, so a point struck
// before the refetch saves at once and a later send back to the queue is not
// mistaken for a start.
describe('the editor is told a match the console started is running (bc-strt)', () => {
  it('passes started for the snapshot it started, and only that one', async () => {
    // The probe is module-wide: drop a previous test's editor, or the refused
    // start below (which mounts no editor) would read that one's props.
    probe.props = null;
    const side = (id, name) => ({ id, name });
    const m1 = {
      id: 'm1', compId: 'c1', compName: 'Cup', status: 'scheduled', phase: 'pool', poolName: 'Pool A',
      court: 'A', scheduledAt: '09:00', sideA: side('p1', 'Yamada'), sideB: side('p2', 'Tanaka'), modifiedAt: 100,
    };
    const m2 = { ...m1, id: 'm2', scheduledAt: '09:05', sideA: side('p3', 'Sato'), sideB: side('p4', 'Kato') };
    let current = [m1, m2];
    window.tournamentMatches = () => current;
    const prevFetch = window.API.fetchCourtMatches;
    const prevSub = window.API.subscribeToEvents;
    window.API.fetchCourtMatches = vi.fn().mockImplementation(() => Promise.resolve([{ id: 'c1', name: 'Cup' }]));
    window.API.subscribeToEvents = () => () => {};
    let startResult = { applied: false, reason: 'clock_skew' };
    const onEditScore = vi.fn().mockImplementation(() => Promise.resolve(startResult));
    try {
      let utils;
      await act(async () => { utils = renderPage(makeMinimalTournament(), 'A', { onEditScore }); });
      const start = async () => {
        const card = utils.container.querySelector('.shiaijo-upnext__card');
        const btn = [...card.querySelectorAll('button')].find((b) => /start match/i.test(b.textContent));
        await act(async () => { btn.click(); });
      };
      const refresh = async () => { await act(async () => { utils.getByRole('button', { name: /refresh/i }).click(); }); };

      // A refused start is no start.
      await start();
      expect(onEditScore).toHaveBeenCalledTimes(1);
      expect(probe.props?.started || false).toBe(false);

      startResult = { applied: true };
      await start();
      expect(probe.props.match.id).toBe('m1');
      expect(probe.props.match.status, 'the list has not caught up yet').toBe('scheduled');
      expect(probe.props.started).toBe(true);

      // The list catches up; later the match goes back to the queue, which
      // stamps it. That scheduled snapshot was not started.
      current = [{ ...m1, status: 'running', modifiedAt: 200 }, m2];
      await refresh();
      current = [{ ...m1, modifiedAt: 300 }, m2];
      await refresh();
      expect(probe.props.match.id, 'the pick still holds the panel').toBe('m1');
      expect(probe.props.match.status).toBe('scheduled');
      expect(probe.props.started).toBe(false);
    } finally {
      window.API.fetchCourtMatches = prevFetch;
      window.API.subscribeToEvents = prevSub;
    }
  });
});
