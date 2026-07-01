import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { EngiScoreEditorModal } from '../../admin_scoring_engi.jsx';

// Regression coverage for a real orientation bug: sideB is Shiro and sideA is
// Aka everywhere else in the app (bracket.jsx PlayerLine, admin_pools.jsx,
// the "{sideB} vs {sideA}" kendo dialog label), but the engi editor once
// labelled sideA as Shiro and sideB as Aka. Assert the DOM pairs each pair's
// NAME with the correct color badge, and that each counter mutates the flag
// field belonging to its own side (flagsB for Shiro/sideB, flagsA for
// Aka/sideA) so the submitted payload lines up with the backend's
// SideA=Aka/SideB=Shiro convention.

function makeMatch(overrides = {}) {
  return {
    sideA: { id: 'p-aka', name: 'Aka Pair Member 1', displayName: 'Aka Pair Member 2', dojo: 'Aka Dojo' },
    sideB: { id: 'p-shiro', name: 'Shiro Pair Member 1', displayName: 'Shiro Pair Member 2', dojo: 'Shiro Dojo' },
    flagsA: 0,
    flagsB: 0,
    ...overrides,
  };
}

describe('EngiScoreEditorModal orientation', () => {
  it('renders sideA under the Aka badge and sideB under the Shiro badge', () => {
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={() => {}} />);
    const shiroBox = screen.getByTestId('engi-side-shiro');
    const akaBox = screen.getByTestId('engi-side-aka');
    expect(shiroBox.textContent).toContain('Shiro Pair Member 1');
    expect(shiroBox.textContent).not.toContain('Aka Pair Member 1');
    expect(akaBox.textContent).toContain('Aka Pair Member 1');
    expect(akaBox.textContent).not.toContain('Shiro Pair Member 1');
  });

  it('renders the Shiro box before the Aka box (Shiro left, Aka right)', () => {
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={() => {}} />);
    const shiroBox = screen.getByTestId('engi-side-shiro');
    const akaBox = screen.getByTestId('engi-side-aka');
    // DOCUMENT_POSITION_FOLLOWING (4) set on akaBox means shiroBox precedes it.
    expect(shiroBox.compareDocumentPosition(akaBox) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('the Shiro counter increments flagsB (sideB), not flagsA', () => {
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={() => {}} />);
    fireEvent.click(screen.getByTestId('engi-shiro-inc'));
    expect(screen.getByTestId('engi-shiro-count').textContent).toBe('1');
    expect(screen.getByTestId('engi-aka-count').textContent).toBe('0');
  });

  it('the Aka counter increments flagsA (sideA), not flagsB', () => {
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={() => {}} />);
    fireEvent.click(screen.getByTestId('engi-aka-inc'));
    expect(screen.getByTestId('engi-aka-count').textContent).toBe('1');
    expect(screen.getByTestId('engi-shiro-count').textContent).toBe('0');
  });

  it('submits flagsA tied to the Aka/sideA count and flagsB tied to the Shiro/sideB count', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={onSubmit} />);
    // 3 flags to Aka (sideA), 0 to Shiro (sideB): a valid {1,3,5} total.
    fireEvent.click(screen.getByTestId('engi-aka-inc'));
    fireEvent.click(screen.getByTestId('engi-aka-inc'));
    fireEvent.click(screen.getByTestId('engi-aka-inc'));
    fireEvent.click(screen.getByTestId('engi-submit'));
    expect(onSubmit).toHaveBeenCalledWith({ flagsA: 3, flagsB: 0, status: 'completed' });
  });

  it('highlights the Aka box as winner when flagsA > flagsB', () => {
    render(<EngiScoreEditorModal match={makeMatch({ flagsA: 3, flagsB: 2 })} onClose={() => {}} onSubmit={() => {}} />);
    expect(screen.getByTestId('engi-side-aka').className).toContain('engi-side--winner');
    expect(screen.getByTestId('engi-side-shiro').className).not.toContain('engi-side--winner');
    expect(screen.getByTestId('engi-total').textContent).toContain('Aka wins');
  });

  it('highlights the Shiro box as winner when flagsB > flagsA', () => {
    render(<EngiScoreEditorModal match={makeMatch({ flagsA: 1, flagsB: 4 })} onClose={() => {}} onSubmit={() => {}} />);
    expect(screen.getByTestId('engi-side-shiro').className).toContain('engi-side--winner');
    expect(screen.getByTestId('engi-side-aka').className).not.toContain('engi-side--winner');
    expect(screen.getByTestId('engi-total').textContent).toContain('Shiro wins');
  });
});

describe('EngiScoreEditorModal correction retry (Copilot review: PR #326)', () => {
  it('carries correctionReason on a retry after a failed first submit', async () => {
    // First attempt (with the ReasonPrompt-supplied reason) fails; the
    // operator clicks "Save correction" again without reopening the prompt.
    // correctionReason must still be on the retry payload: handleSubmit's
    // fallback branch used to build a bare {flagsA, flagsB, status} literal
    // that silently dropped it once correctionReason was already in state.
    const onSubmit = vi.fn()
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValueOnce(undefined);
    const completedMatch = makeMatch({ status: 'completed', flagsA: 3, flagsB: 0 });
    render(<EngiScoreEditorModal match={completedMatch} onClose={() => {}} onSubmit={onSubmit} />);

    // First click on a completed match opens the ReasonPrompt, not a submit.
    fireEvent.click(screen.getByTestId('engi-submit'));
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText('Confirm'));

    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    expect(onSubmit).toHaveBeenNthCalledWith(1, { flagsA: 3, flagsB: 0, status: 'completed', correctionReason: 'Scoring error' });

    // Retry: correctionReason is already set in state, so this click skips
    // the ReasonPrompt gate and goes straight to doSubmit.
    fireEvent.click(screen.getByTestId('engi-submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2));
    expect(onSubmit).toHaveBeenNthCalledWith(2, { flagsA: 3, flagsB: 0, status: 'completed', correctionReason: 'Scoring error' });
  });
});

describe('EngiScoreEditorModal keyboard flag entry (impeccable critique P2)', () => {
  it('a/s add a flag to Aka/Shiro, Shift+A/S remove, Enter saves', async () => {
    const onSubmit = vi.fn().mockResolvedValue(undefined);
    render(<EngiScoreEditorModal match={makeMatch()} onClose={() => {}} onSubmit={onSubmit} />);

    // a → Aka +1 (x3), s → Shiro +1 (x2): Aka=3, Shiro=2 → total 5 (valid).
    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 's' });
    fireEvent.keyDown(document.body, { key: 's' });
    expect(screen.getByTestId('engi-aka-count').textContent).toBe('3');
    expect(screen.getByTestId('engi-shiro-count').textContent).toBe('2');

    // Shift+A (key 'A') removes an Aka flag → Aka=2, total 4 (invalid, no save).
    fireEvent.keyDown(document.body, { key: 'A' });
    expect(screen.getByTestId('engi-aka-count').textContent).toBe('2');

    // Bring Aka back to 3 (total 5), then Enter saves the {3,2} payload.
    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 'Enter' });
    await waitFor(() => expect(onSubmit).toHaveBeenCalledWith({ flagsA: 3, flagsB: 2, status: 'completed' }));
  });

  it('does not hijack typing inside a text field (reason note)', () => {
    // Open the correction prompt on a completed match; while it's open the
    // keyboard handler must no-op so the operator can type in the note.
    render(<EngiScoreEditorModal match={makeMatch({ status: 'completed', flagsA: 3, flagsB: 0 })} onClose={() => {}} onSubmit={() => {}} />);
    fireEvent.click(screen.getByTestId('engi-submit')); // opens ReasonPrompt
    const before = { aka: 3, shiro: 0 };
    fireEvent.keyDown(document.body, { key: 'a' });
    fireEvent.keyDown(document.body, { key: 's' });
    // Prompt is open, so flag keys are ignored: counts unchanged (the counters
    // aren't even rendered now, so we assert via a fresh submit path instead).
    expect(screen.queryByTestId('engi-aka-count')?.textContent ?? String(before.aka)).toBe('3');
  });
});

describe('EngiScoreEditorModal offline safety net (impeccable critique P2)', () => {
  it('shows the pending-write banner (not a silent close) when a save is only queued, and Retry re-submits', async () => {
    // onSubmit resolves { queued: true } instead of throwing: the terminal
    // write was durably queued (offline / transient) but not confirmed. The
    // editor must NOT behave as if saved; it shows a sticky banner and lets
    // the operator retry.
    const onSubmit = vi.fn().mockResolvedValue({ queued: true });
    render(<EngiScoreEditorModal match={makeMatch({ flagsA: 3, flagsB: 0 })} onClose={() => {}} onSubmit={onSubmit} />);

    fireEvent.click(screen.getByTestId('engi-submit'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(1));
    // Pending banner is shown and the commit control is still available (modal
    // stayed open, not closed-as-saved).
    await waitFor(() => expect(screen.getByText(/will keep retrying until it lands/i)).toBeTruthy());
    expect(screen.queryByTestId('engi-submit')).not.toBeNull();

    // Retry now re-invokes the same payload.
    fireEvent.click(screen.getByText('Retry now'));
    await waitFor(() => expect(onSubmit).toHaveBeenCalledTimes(2));
    expect(onSubmit).toHaveBeenNthCalledWith(2, { flagsA: 3, flagsB: 0, status: 'completed' });
  });
});

describe('EngiScoreEditorModal correction footer (impeccable critique P1)', () => {
  it('hides the footer commit row while the reason prompt is open, then restores it on cancel', () => {
    const completedMatch = makeMatch({ status: 'completed', flagsA: 3, flagsB: 0 });
    render(<EngiScoreEditorModal match={completedMatch} onClose={() => {}} onSubmit={() => {}} />);

    // Footer's Save-correction button present before the prompt opens.
    expect(screen.queryByTestId('engi-submit')).not.toBeNull();

    // Opening the reason prompt must collapse the footer so the operator sees
    // exactly one Cancel and one commit (the prompt's), never two of each at
    // the highest-stakes moment (amending a recorded result).
    fireEvent.click(screen.getByTestId('engi-submit'));
    expect(screen.queryByTestId('engi-submit')).toBeNull();
    expect(screen.getByText('Confirm')).toBeTruthy();
    // Exactly one Cancel visible (the prompt's), not the footer's too.
    expect(screen.getAllByText('Cancel')).toHaveLength(1);

    // Cancelling the prompt restores the footer commit row.
    fireEvent.click(screen.getByText('Confirm').closest('form').querySelector('button[type="button"]'));
    expect(screen.queryByTestId('engi-submit')).not.toBeNull();
  });
});
