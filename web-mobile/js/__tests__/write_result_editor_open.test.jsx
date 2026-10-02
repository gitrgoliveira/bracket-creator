// bc-plcl: whether a score editor's host keeps the editor open after a write.
// One rule every host asks (the Pools tab, the Scores tab, the bracket panel);
// the Pools tab once closed on every write, Start match and each autosaved
// point included.

import { describe, it, expect } from 'vitest';
import { writeKeepsEditorOpen, writeWasRefused, writeRetryable } from '../write_result.jsx';

describe('writeKeepsEditorOpen', () => {
  const running = { status: 'running', winner: null };
  const finish = { status: 'completed', winner: { id: 'p1', name: 'Yamada' } };

  it('a running write with no winner leaves the bout live on the board', () => {
    expect(writeKeepsEditorOpen(running, { status: 'running' })).toBe(true);
  });

  it('a running write with a winner is not a start', () => {
    expect(writeKeepsEditorOpen({ status: 'running', winner: finish.winner }, { status: 'running' })).toBe(false);
  });

  it('a saved finish is the editor\'s job done', () => {
    expect(writeKeepsEditorOpen(finish, { applied: true })).toBe(false);
  });

  it.each([
    ['queued', { queued: true }],
    ['superseded', { applied: false, reason: 'superseded' }],
    ['refused for the clock', { applied: false, reason: 'clock_skew' }],
  ])('a finish that did not land (%s) keeps its not-saved banner on screen', (_name, res) => {
    expect(writeKeepsEditorOpen(finish, res)).toBe(true);
  });

  it('a write with nothing to report is not held open', () => {
    expect(writeKeepsEditorOpen(finish, undefined)).toBe(false);
  });
});

// What a write came back with, as the editors read it: whether a two-tap
// commit disarms (writeWasRefused) and whether Retry is offered
// (writeRetryable). Only a queued write is worth sending again; a refused one
// is answered the same way however often it is sent.
describe('writeWasRefused / writeRetryable', () => {
  it.each([
    ['nothing handed back (the host reported a refusal)', undefined, true, false],
    ['superseded', { applied: false, reason: 'superseded' }, true, false],
    ['refused for the clock', { applied: false, reason: 'clock_skew' }, true, false],
    ['queued', { queued: true }, false, true],
    ['landed', { applied: true }, false, false],
    ['landed, a bare body', {}, false, false],
  ])('%s', (_name, res, refused, retryable) => {
    expect(writeWasRefused(res)).toBe(refused);
    expect(writeRetryable(res)).toBe(retryable);
  });
});
