// Stage 6 review 8 (m12): the board can tell a pending tap "not applied" from
// a copy behind the server, and the server then applies it. The editor's
// note for that tap goes once the server's applied answer comes; a note for
// another tap stays.
import React from 'react';
import { render, act } from '@testing-library/react';
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { useTapOutcomeNote } from '../../admin_scoring_shared.jsx';

function Note() {
  const { note } = useTapOutcomeNote('c1', 'm1');
  return <div data-testid="note">{note || ''}</div>;
}

describe('the tap outcome note', () => {
  let prev;
  let emit;
  beforeEach(() => {
    prev = window.API;
    window.API = { ...prev, subscribeTapOutcome: (_c, _m, cb) => { emit = cb; return () => {}; } };
  });
  afterEach(() => { window.API = prev; });

  it('goes when the server applies the tap it named', () => {
    const { getByTestId } = render(<Note />);
    act(() => { emit({ id: 't1', outcome: 'noTarget', reason: 'bout_removed' }); });
    expect(getByTestId('note').textContent).not.toBe('');
    act(() => { emit({ id: 't2', outcome: 'applied', takenBack: true }); });
    expect(getByTestId('note').textContent, 'another tap applied leaves it').not.toBe('');
    // A quiet answer the outbox did not take back (a duplicate of a tap still
    // turned away) leaves it (stage 6 review 9, F3).
    act(() => { emit({ id: 't1', outcome: 'duplicate' }); });
    expect(getByTestId('note').textContent).not.toBe('');
    act(() => { emit({ id: 't1', outcome: 'applied', takenBack: true }); });
    expect(getByTestId('note').textContent).toBe('');
  });
});
