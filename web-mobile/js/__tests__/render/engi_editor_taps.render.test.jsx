// bc-evts stage 4: the engi editor scores a RUNNING match by taps. Each flag
// + or − is one flag.add / flag.remove event for its side, handed to the
// outbox (API.recordTap) in the tick it is made; nothing waits on it. The
// board is the server's match with this device's pending taps folded on, so
// it never counts a tap twice, and Finish is a rule-bound write carrying the
// copy the board showed. A match that is not running keeps a local draft,
// sent once by Save. The flag + and − are left unguarded against a double tap
// (CLAUDE.md "A double tap is two complete clicks"): two quick taps are two
// flags.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, afterAll, beforeEach, vi } from 'vitest';
import { installIndividualEditor, resetOutbox, API, engiBout, counts, btn } from '../helpers/engi_editor.js';
import { serverFold } from '../helpers/fake_outbox.js';
import { pointerTap } from '../helpers/tap_events.js';
import { TAP_BOUNCE_MS } from '../../tap_guard.jsx';

let ScoreEditorModal;
let restore;
let outbox;
beforeAll(async () => { ({ ScoreEditorModal, restore } = await installIndividualEditor()); });
afterAll(() => restore());
beforeEach(() => { outbox = resetOutbox(); });

const el = (match, props = {}) => <ScoreEditorModal match={match} onClose={vi.fn()} onSubmit={vi.fn()} password="pw" variant="inline" {...props} />;
const click = (r, testid) => act(async () => { fireEvent.click(btn(r.container, testid)); });
const tapEvents = () => API.recordTap.mock.calls.map((c) => c[2]);
const wait = (ms) => act(() => new Promise((res) => setTimeout(res, ms)));

describe('a running engi match is scored by taps', () => {
  it('each + and − is one flag event for its side, and nothing else is written', async () => {
    const onSubmit = vi.fn();
    const r = render(el(engiBout(), { onSubmit }));
    await click(r, 'engi-shiro-inc');
    await click(r, 'engi-aka-inc');
    await click(r, 'engi-shiro-inc');
    await click(r, 'engi-shiro-dec');
    expect(tapEvents()).toEqual([
      { kind: 'flag.add', pos: 0, side: 'b' },
      { kind: 'flag.add', pos: 0, side: 'a' },
      { kind: 'flag.add', pos: 0, side: 'b' },
      { kind: 'flag.remove', pos: 0, side: 'b' },
    ]);
    for (const call of API.recordTap.mock.calls) {
      expect(call.slice(0, 2)).toEqual(['c1', 'm1']);
      expect(call[3]).toBe('pw');
      expect(call[4]).toMatchObject({ id: 'm1', scoreVersion: 3 });
    }
    expect(counts(r.container)).toEqual([1, 1]);
    expect(onSubmit).not.toHaveBeenCalled();
    expect(API.recordScore).not.toHaveBeenCalled();
  });

  it('the keys tap too: a and s add, Backspace takes the last side\'s flag off', async () => {
    const r = render(el(engiBout()));
    await act(async () => { fireEvent.keyDown(window, { key: 's' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'a' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Backspace' }); });
    expect(tapEvents().map((e) => `${e.kind}:${e.side}`)).toEqual(['flag.add:b', 'flag.add:a', 'flag.remove:a']);
    expect(counts(r.container)).toEqual([1, 0]);
  });

  it('a count stays inside 0..5: a key at a bound sends nothing', async () => {
    const r = render(el(engiBout({ flagsB: 5 })));
    expect(btn(r.container, 'engi-shiro-inc').disabled).toBe(true);
    expect(btn(r.container, 'engi-aka-dec').disabled).toBe(true);
    await act(async () => { fireEvent.keyDown(window, { key: 's' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'a' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Backspace' }); });
    await act(async () => { fireEvent.keyDown(window, { key: 'Backspace' }); });
    expect(tapEvents().map((e) => `${e.kind}:${e.side}`)).toEqual(['flag.add:a', 'flag.remove:a']);
    expect(counts(r.container)).toEqual([5, 0]);
  });

  it('a quick double tap on + is two flags', async () => {
    const r = render(el(engiBout()));
    await pointerTap(btn(r.container, 'engi-shiro-inc'));
    await pointerTap(btn(r.container, 'engi-shiro-inc'));
    expect(tapEvents()).toHaveLength(2);
    expect(counts(r.container)).toEqual([2, 0]);
  });

  it('the board is the server\'s copy with this device\'s pending taps on it', async () => {
    let server = engiBout({ flagsB: 2 });
    const r = render(el(server));
    await click(r, 'engi-shiro-inc');
    expect(counts(r.container)).toEqual([3, 0]);
    // Another device's Aka flag lands; its push reaches this one.
    server = serverFold(server, [{ kind: 'flag.add', pos: 0, side: 'a', id: 'devB.1', stamp: server.modifiedAt + 1 }], { engi: true }).match;
    await act(async () => { r.rerender(el(server)); });
    expect(counts(r.container), 'the other device\'s flag, with this one\'s pending flag on top').toEqual([3, 1]);
  });

  it('a push carrying the tap before its answer never counts it twice', async () => {
    let server = engiBout();
    const r = render(el(server));
    await click(r, 'engi-shiro-inc');
    // The server logged the tap (eventSeqs names its seq) and pushed the match
    // before the tap's own answer came back.
    server = serverFold(server, outbox.taps('c1', 'm1'), { engi: true }).match;
    expect(server.flagsB).toBe(1);
    await act(async () => { r.rerender(el(server)); });
    expect(counts(r.container)).toEqual([1, 0]);
    await act(async () => { outbox.ack('c1', 'm1', server, {}); });
    expect(counts(r.container)).toEqual([1, 0]);
  });

  it('a tap the server did not apply leaves the board on the server\'s count and says why', async () => {
    const r = render(el(engiBout({ flagsB: 1 })));
    await click(r, 'engi-shiro-inc');
    const [tap] = outbox.taps('c1', 'm1');
    await act(async () => {
      outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'matchClosed', reason: 'match_closed', message: 'The match is finished. Use Correct result.' } });
    });
    expect(counts(r.container)).toEqual([1, 0]);
    expect(r.container.querySelector('[data-testid="tap-not-applied-note"]').textContent).toBe('The match is finished. Use Correct result.');
  });

  it('a tap refused because the match is not running shows the server\'s sentence next to the board', async () => {
    const r = render(el(engiBout()));
    await click(r, 'engi-aka-inc');
    const [tap] = outbox.taps('c1', 'm1');
    const sentence = 'This match was sent back to the queue. Start it to score it.';
    await act(async () => { outbox.ack('c1', 'm1', null, { [tap.id]: { outcome: 'notStarted', reason: 'match_sent_back', message: sentence } }); });
    expect(r.container.querySelector('[data-testid="tap-refusal"]').textContent).toBe(sentence);
    // The next tap takes the sentence down.
    await click(r, 'engi-aka-inc');
    expect(r.container.querySelector('[data-testid="tap-refusal"]')).toBeNull();
  });

  it('Save sends the board\'s flags and the server copy the board showed', async () => {
    const onSubmit = vi.fn().mockReturnValue({ applied: true, pending: true, settled: new Promise(() => {}) });
    const r = render(el(engiBout({ flagsB: 2 }), { onSubmit }));
    await click(r, 'engi-shiro-inc');
    await pointerTap(btn(r.container, 'engi-submit'));
    await wait(TAP_BOUNCE_MS + 20);
    await pointerTap(btn(r.container, 'engi-submit'));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ flagsA: 0, flagsB: 3, status: 'completed', seenScoreVersion: 3 });
    expect(onSubmit.mock.calls[0][0].changed).toEqual(expect.arrayContaining(['flags', 'result']));
  });

  it('Save names the newest copy an answer handed back when the board took it', async () => {
    const onSubmit = vi.fn().mockReturnValue({ applied: true, pending: true, settled: new Promise(() => {}) });
    let server = engiBout({ flagsB: 2 });
    const r = render(el(server, { onSubmit }));
    await click(r, 'engi-shiro-inc');
    server = serverFold(server, outbox.taps('c1', 'm1'), { engi: true }).match;
    await act(async () => { outbox.ack('c1', 'm1', server, {}); });
    expect(server.scoreVersion).toBe(4);
    await pointerTap(btn(r.container, 'engi-submit'));
    await wait(TAP_BOUNCE_MS + 20);
    await pointerTap(btn(r.container, 'engi-submit'));
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ flagsB: 3, seenScoreVersion: 4 });
  });

  it('Close on a running match asks nothing and writes nothing: the taps are in the outbox', async () => {
    const onClose = vi.fn();
    const onSubmit = vi.fn();
    const r = render(el(engiBout(), { onClose, onSubmit, variant: 'modal' }));
    await click(r, 'engi-shiro-inc');
    await click(r, 'engi-close-btn');
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(window.confirmDialog).not.toHaveBeenCalled();
    expect(onSubmit).not.toHaveBeenCalled();
  });
});

describe('a match that is not running keeps a local draft', () => {
  it('a scheduled match sends no tap; Save sends the counts once', async () => {
    const onSubmit = vi.fn().mockReturnValue({ applied: true, pending: true, settled: new Promise(() => {}) });
    const r = render(el(engiBout({ status: 'scheduled' }), { onSubmit }));
    await click(r, 'engi-shiro-inc');
    await click(r, 'engi-shiro-inc');
    await click(r, 'engi-aka-inc');
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(counts(r.container)).toEqual([2, 1]);
    await pointerTap(btn(r.container, 'engi-submit'));
    await wait(TAP_BOUNCE_MS + 20);
    await pointerTap(btn(r.container, 'engi-submit'));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0]).toMatchObject({ flagsA: 1, flagsB: 2, status: 'completed' });
    expect(onSubmit.mock.calls[0][0].seenScoreVersion).toBeUndefined();
  });

  it('an untouched scheduled editor follows another device\'s count', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await act(async () => { r.rerender(el(engiBout({ status: 'scheduled', flagsA: 2, modifiedAt: 1 }))); });
    expect(counts(r.container)).toEqual([0, 2]);
  });

  // Ported from engi_editor_tap_back_kept (retired with the engi autosave): a
  // net-zero fiddle on a scheduled match holds nothing against another device.
  it('a scheduled fiddle back to the server\'s count still follows another device', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await click(r, 'engi-aka-inc');
    await click(r, 'engi-aka-dec');
    await act(async () => { r.rerender(el(engiBout({ status: 'scheduled', flagsA: 2, modifiedAt: 1 }))); });
    expect(counts(r.container)).toEqual([0, 2]);
  });

  it('a match finished elsewhere while open shows the server\'s result, and a correction is local', async () => {
    const r = render(el(engiBout()));
    await click(r, 'engi-shiro-inc');
    await act(async () => { outbox.ack('c1', 'm1', null, {}); });
    await act(async () => { r.rerender(el(engiBout({ status: 'completed', flagsA: 3, flagsB: 0, winner: { id: 'p1' }, scoreVersion: 6 }))); });
    expect(counts(r.container)).toEqual([0, 3]);
    API.recordTap.mockClear();
    await click(r, 'engi-shiro-inc');
    expect(API.recordTap).not.toHaveBeenCalled();
    expect(counts(r.container)).toEqual([1, 3]);
  });

  it('a draft made before the match starts gives way to the running board', async () => {
    const r = render(el(engiBout({ status: 'scheduled' })));
    await click(r, 'engi-aka-inc');
    expect(counts(r.container)).toEqual([0, 1]);
    await act(async () => { r.rerender(el(engiBout({ status: 'running', flagsB: 1, scoreVersion: 4 }))); });
    expect(counts(r.container), 'the board is the server\'s running match').toEqual([1, 0]);
    await click(r, 'engi-aka-inc');
    expect(tapEvents()).toEqual([{ kind: 'flag.add', pos: 0, side: 'a' }]);
    expect(counts(r.container)).toEqual([1, 1]);
  });
});
