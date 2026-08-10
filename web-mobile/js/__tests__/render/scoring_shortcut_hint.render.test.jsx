import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { ScoringShortcutHint } from '../../admin_scoring_shared.jsx';

// The keyboard ippon shortcuts (M/K/D/T/H, Shift = Aka) exist for individual
// matches and kachinuki bouts but were undiscoverable: the hint listed only the
// nav keys. `pointKeys` now surfaces them, and ONLY where keyboard scoring is
// actually wired (empty for fixed-order team bouts, which score by tap).
const kbds = (container) => [...container.querySelectorAll('kbd')].map((k) => k.textContent);

describe('ScoringShortcutHint keyboard discoverability', () => {
  it('lists the ippon keys with Shiro/Aka when scoring is active', () => {
    const { container, getByTestId } = render(<ScoringShortcutHint pointKeys="MKDTH" />);
    expect(kbds(container)).toEqual(expect.arrayContaining(['M', 'K', 'D', 'T', 'H']));
    const t = getByTestId('scoring-modal-shortcut-hint').textContent;
    expect(t).toContain('Shiro');
    expect(t).toContain('Aka');
    expect(t).toContain('prev/next');
  });

  it('surfaces the naginata Sune key when present', () => {
    const { container } = render(<ScoringShortcutHint pointKeys="MKDTSH" />);
    expect(kbds(container)).toEqual(expect.arrayContaining(['M', 'K', 'D', 'T', 'S', 'H']));
  });

  it('shows only nav shortcuts when scoring is tap-only (fixed-order team bout)', () => {
    const { container, getByTestId } = render(<ScoringShortcutHint pointKeys="" />);
    expect(kbds(container)).not.toContain('M');
    const t = getByTestId('scoring-modal-shortcut-hint').textContent;
    expect(t).not.toContain('Shiro');
    expect(t).toContain('prev/next');
  });
});
