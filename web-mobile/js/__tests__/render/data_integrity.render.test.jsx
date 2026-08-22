import React from 'react';
import { render, screen } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { DataIssueBanner } from '../../data_integrity.jsx';

// The banner is the LOUD-class surface: a whole file will not parse, every
// write to it is refused, and the operator has to choose between repairing it
// and resetting. Mounted for real, because what matters is which of those two
// options it actually offers.

describe('DataIssueBanner', () => {
  const bracketBroken = [{ file: 'bracket.json', line: 47, column: 12, detail: "invalid character 'x'" }];

  it('renders nothing when there is nothing to repair', () => {
    const { container } = render(<DataIssueBanner issues={[]} competition={{ format: 'mixed' }} />);
    expect(container.firstChild).toBeNull();
  });

  it('names the file and position, and says the file is untouched', () => {
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} />);
    expect(screen.getByRole('alert').textContent).toContain('bracket.json, line 47, column 12');
    // The reassurance is the whole reason repair is the recommended option.
    expect(screen.getByRole('alert').textContent).toMatch(/still exactly as it was last saved/);
  });

  it('offers the reset for a pool-fed competition, and states what it costs', () => {
    const onReset = vi.fn();
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} onReset={onReset} />);
    const text = screen.getByRole('alert').textContent;
    // The four facts an operator needs before choosing.
    expect(text).toMatch(/never deleted/);
    expect(text).toMatch(/Pools, participants and pool results are untouched/);
    expect(text).toMatch(/re-entered from the score sheets/);
    expect(text).toMatch(/Check the rebuilt pairings against your printed bracket/);
    screen.getByRole('button', { name: /Reset the knockout stage/ }).click();
    expect(onReset).toHaveBeenCalled();
  });

  it('does NOT offer the reset for direct elimination, and explains why', () => {
    // An action that can only fail is worse than an explanation: the server
    // refuses this case, so the button must not be offered at all.
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'playoffs' }} onReset={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /Reset the knockout stage/ })).toBeNull();
    expect(screen.getByRole('alert').textContent)
      .toMatch(/only record of who was drawn against whom/);
  });

  it('does not offer a reset when the bracket is fine and another file is not', () => {
    render(<DataIssueBanner issues={[{ file: 'pools.csv', line: 2, column: 1, detail: 'bare quote' }]}
      competition={{ format: 'mixed' }} onReset={vi.fn()} />);
    expect(screen.queryByRole('button', { name: /Reset the knockout stage/ })).toBeNull();
    expect(screen.getByRole('alert').textContent).toContain('pools.csv, line 2, column 1');
  });

  it('disables the reset while it is running', () => {
    render(<DataIssueBanner issues={bracketBroken} competition={{ format: 'mixed' }} onReset={vi.fn()} resetting />);
    expect(screen.getByRole('button', { name: /Resetting/ }).disabled).toBe(true);
  });
});
