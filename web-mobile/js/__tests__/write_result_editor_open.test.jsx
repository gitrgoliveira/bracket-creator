// bc-plcl: whether a score editor's host keeps the editor open after a write.
// One rule every host asks (the Pools tab, the Scores tab, the bracket panel);
// the Pools tab once closed on every write, Start match and each autosaved
// point included.

import { describe, it, expect } from 'vitest';
import { writeKeepsEditorOpen } from '../write_result.jsx';

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
