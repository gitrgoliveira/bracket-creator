// own_status.jsx, the ONE owner of a score editor's own status writes and its
// kept draft (stage 5 review 4): the rules no render can isolate.
import { describe, it, expect } from 'vitest';
import { createOwnStatus, OWN_START, OWN_REOPEN, OWN_REP, LANDED, REFUSED, HELD, DISCARDED } from '../own_status.jsx';

const DRAFT = [{ m: 0 }, { m: 1 }];

describe('own_status', () => {
  it('an answer acts only while its token is the current one', () => {
    const o = createOwnStatus('scheduled', 1);
    const t1 = o.begin(OWN_START, { ipponsB: ['M'] });
    const t2 = o.begin(OWN_START, { ipponsB: ['K'] });
    o.answer(t1, HELD, 'stale');
    expect(o.view().override, 'the newer Start still shows').toEqual({ ipponsB: ['K'] });
    expect(o.view().refusal).toBe('');
    o.answer(t2, REFUSED, 'Turned down.');
    expect(o.view().override).toBeNull();
    expect(o.view().refusal).toBe('Turned down.');
    expect(o.view().startOffered).toBe(true);
  });

  it('an own Start shows the match running until the feed does, and Start is not offered meanwhile, a restamp included', () => {
    const o = createOwnStatus('scheduled', 1);
    o.begin(OWN_START, {});
    expect(o.view()).toMatchObject({ shownStatus: 'running', startOffered: false });
    o.observe('scheduled', 2);
    expect(o.view()).toMatchObject({ shownStatus: 'running', startOffered: false });
  });

  it('a draft kept when another device moves the match, scheduled or completed; never for the own write', () => {
    for (const from of ['scheduled', 'completed']) {
      const o = createOwnStatus(from, 1);
      o.note(...DRAFT);
      o.observe('running', 2);
      expect(o.view().kept).toEqual({ base: DRAFT[0], from });
    }
    const own = createOwnStatus('completed', 1);
    own.note(...DRAFT);
    const t = own.begin(OWN_REOPEN);
    own.answer(t, LANDED);
    own.observe('running', 2);
    expect(own.view().kept).toBeNull();
  });

  it('the feed moving before the own write\'s answer waits on that answer: turned down, the draft is kept', () => {
    const o = createOwnStatus('scheduled', 1);
    o.note(...DRAFT);
    const t = o.begin(OWN_START, {});
    o.observe('running', 2);
    expect(o.view().kept).toBeNull();
    o.answer(t, DISCARDED);
    expect(o.view().kept).toEqual({ base: DRAFT[0], from: 'scheduled' });
    expect(o.view().refusal, 'no refusal note over a running match').toBe('');
  });

  it('a kept draft the match leaves running under is dropped: the note, and the reset', () => {
    const o = createOwnStatus('scheduled', 1);
    o.note(...DRAFT);
    o.observe('running', 2);
    const seq = o.view().resetSeq;
    o.observe('completed', 3);
    expect(o.view()).toMatchObject({ kept: null, droppedShown: true, resetSeq: seq + 1 });
  });

  it('an own Start turned down while nothing moved keeps the draft on the board: no reset', () => {
    const o = createOwnStatus('scheduled', 1);
    o.note(...DRAFT);
    const t = o.begin(OWN_START, {});
    o.answer(t, HELD, 'Not started.');
    expect(o.view()).toMatchObject({ shownStatus: 'scheduled', resetSeq: 0, kept: null, refusal: 'Not started.' });
  });

  // Stage 5 review 6: a representative-bout change on a running match was
  // begun as an own status write, so a tap echo restamping the match, or a
  // finish elsewhere, read as the match leaving under a draft typed before the
  // Start, and its refusal said "Your unsaved entries were dropped".
  it('a write begun while the match shows running moves no status: nothing it meets is a dropped draft', () => {
    for (const next of ['running', 'completed']) {
      for (const outcome of [REFUSED, HELD, DISCARDED, LANDED]) {
        const o = createOwnStatus('scheduled', 1);
        o.note(...DRAFT);
        const start = o.begin(OWN_START, {});
        o.answer(start, LANDED);
        o.observe('running', 2);
        const rep = o.begin(OWN_REP);
        expect(o.view().pendingKind, 'no own status write from running').toBeNull();
        o.observe(next, 3);
        o.answer(rep, outcome);
        expect(o.view(), `${next} then ${outcome}`).toMatchObject({ droppedShown: false, kept: null });
      }
    }
  });

  it('an own write that runs the match takes the draft it carried with it: none surfaces later', () => {
    for (const shownFirst of [false, true]) {
      const o = createOwnStatus('scheduled', 1);
      o.note(...DRAFT);
      const start = o.begin(OWN_START, {});
      if (shownFirst) { o.observe('running', 2); o.answer(start, LANDED); } else { o.answer(start, LANDED); o.observe('running', 2); }
      // Finished elsewhere; a reopen begun before the editor notes a draft
      // again, then a correction elsewhere, and the reopen turned down.
      o.observe('completed', 3);
      const reopen = o.begin(OWN_REOPEN);
      o.observe('completed', 4);
      o.answer(reopen, REFUSED);
      expect(o.view().droppedShown, `the Start's draft is no draft of the finished match (feed first ${shownFirst})`).toBe(false);
    }
  });
});
