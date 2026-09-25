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
    const { container, getByTestId } = render(<ScoringShortcutHint pointKeys="MKDTH" hasNav canClose />);
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
    const { container, getByTestId } = render(<ScoringShortcutHint pointKeys="" hasNav canClose />);
    expect(kbds(container)).not.toContain('M');
    const t = getByTestId('scoring-modal-shortcut-hint').textContent;
    expect(t).not.toContain('Shiro');
    expect(t).toContain('prev/next');
  });
});

// bc-kbhn: the hint showed on the iPad and advertised keys the court console
// ignores.
describe('bc-kbhn: the hint lists only keys that act, and can hide', () => {
  it('lists no arrows and no Esc when the host wires neither (the court console)', () => {
    const { container, getByTestId } = render(<ScoringShortcutHint pointKeys="MKDTH" />);
    expect(kbds(container)).not.toContain('←');
    expect(kbds(container)).not.toContain('→');
    expect(kbds(container)).not.toContain('Esc');
    const t = getByTestId('scoring-modal-shortcut-hint').textContent;
    expect(t).not.toContain('prev/next');
    expect(t).not.toContain('close');
    // No dangling separator after the last group.
    expect(t.trim().endsWith('·')).toBe(false);
  });

  it('lists Esc without the arrows when the host can close but has no neighbour', () => {
    const { container } = render(<ScoringShortcutHint pointKeys="" canClose />);
    expect(kbds(container)).toEqual(['Esc']);
  });

  it('renders nothing when it would list nothing', () => {
    const { container } = render(<ScoringShortcutHint pointKeys="" />);
    expect(container.innerHTML).toBe('');
  });

  it('carries no inline display, so the coarse-pointer rule in styles.css can hide it', () => {
    // jsdom cannot evaluate (pointer: coarse); what it can pin is the defect:
    // an inline display:flex outranks any stylesheet rule, media query or not.
    const { getByTestId } = render(<ScoringShortcutHint pointKeys="MKDTH" hasNav canClose />);
    const el = getByTestId('scoring-modal-shortcut-hint');
    expect(el.classList.contains('scoring-shortcut-hint')).toBe(true);
    expect(el.style.display).toBe('');
  });
});
