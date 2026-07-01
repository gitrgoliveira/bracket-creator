import React from 'react';
import { render, fireEvent, screen } from '@testing-library/react';
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
