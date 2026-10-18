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
    act(() => { emit({ id: 't2', outcome: 'applied' }); });
    expect(getByTestId('note').textContent, 'another tap applied leaves it').not.toBe('');
    act(() => { emit({ id: 't1', outcome: 'applied' }); });
    expect(getByTestId('note').textContent).toBe('');
  });
});
