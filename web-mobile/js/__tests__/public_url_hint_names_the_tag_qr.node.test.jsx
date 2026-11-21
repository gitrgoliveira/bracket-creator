// The Public URL field must say what leaving it blank costs (operator ruling
// 2026-09-22: "as long as the docs cover it and the configuration page is
// explicit on that behaviour, it's ok").
//
// The behaviour itself is settled and correct: no public URL, no QR on the
// printed tags. `playerTagQRPNG` (internal/helper/qr.go) returns nil bytes for
// an empty publicURL and `CreateTagsSheet` embeds nothing, which was measured
// on a real export -- the workbook carries 0 images with the field blank and
// one per competitor with it set.
//
// What was NOT explicit was the field the operator reads while deciding. It
// said "Leave blank to use the current browser address", which is true of the
// share links (linkBase falls back to window.location.origin) and false of the
// printed tag, whose QR is built SERVER-side from tourn.PublicURL with no such
// fallback. So the hint described a fallback that does not reach the one
// output you cannot correct after the fact: a printed sheet of tags.
//
// A SOURCE check, and read through readCode so the comment beside the hint --
// which necessarily talks about tags and QR codes -- cannot satisfy the
// assertion on its own (the trap documented in helpers/source.js).
//
// Deliberately NOT pinned to exact wording: what must survive is that the
// blank case is described in terms of the TAGS, not only the browser address.
// Rewording is free; dropping the consequence is not.
import { describe, it, expect } from 'vitest';
import { readCode } from './helpers/source.js';

const hintAfterPublicURL = () => {
  const code = readCode('admin_setup.jsx');
  const m = code.match(/Public URL<\/label>[\s\S]*?field__hint">([^<]*)</);
  return m ? m[1] : '';
};

describe('the Public URL hint is explicit about the printed tag', () => {
  it('the field has a hint at all', () => {
    expect(hintAfterPublicURL().trim().length).toBeGreaterThan(0);
  });

  it('names the QR code and the tag it is printed on', () => {
    const hint = hintAfterPublicURL();
    expect(hint).toMatch(/QR/);
    expect(hint).toMatch(/tag/i);
  });

  it('the sentence about leaving it BLANK is the one that names the tag', () => {
    // The regression this exists for. The old hint mentioned QR codes (in its
    // list of what the field is for) and then described the blank case purely
    // as "use the current browser address" -- so every word was true and the
    // reader still concluded the tags would be fine. Asserting "the hint
    // mentions QR somewhere" would have passed on that wording.
    const hint = hintAfterPublicURL();
    const blankSentence = hint
      .split(/(?<=\.)\s+/)
      .find((s) => /blank/i.test(s)) || '';
    expect(blankSentence, 'the hint says what leaving it blank does').not.toBe('');
    expect(blankSentence, 'and says it in terms of the printed tag').toMatch(/tag/i);
  });
});
