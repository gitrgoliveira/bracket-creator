// bc-otpl: a glossary Term is itself a control (its click handler stops
// propagation), so one nested in a button or label swallows the tap meant for
// the outer control. These tests install the REAL Term: a stub cannot see it.

import React from 'react';
import { render, fireEvent, screen, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest';
import { installWindowStubs } from '../helpers/stub_globals.js';

let restoreGlobals;
let EnchoControl;
let DecisionPrompt;

beforeAll(async () => {
  restoreGlobals = installWindowStubs({
    isKikenDecision: (d) => d === 'kiken' || d === 'kiken-voluntary' || d === 'kiken-injury',
  });
  await import('../../glossary.jsx');
  ({ EnchoControl, DecisionPrompt } = await import('../../admin_scoring_shared.jsx'));
});

afterAll(() => restoreGlobals());

const NESTED = 'button [data-testid="term-wrapper"], label [data-testid="term-wrapper"]';

function promptFor(kind) {
  return (
    <DecisionPrompt
      kind={kind}
      sideA={{ name: 'Tora' }}
      sideB={{ name: 'Kuma' }}
      askReason={false}
      onCancel={vi.fn()}
      onSubmit={vi.fn()}
      submitting={false}
    />
  );
}

describe('a Term never swallows the tap of the control it sits in', () => {
  it('tapping the word Overtime on the collapsed pill opens the counter', () => {
    render(<EnchoControl enchoPeriodCount={0} setEnchoPeriodCount={vi.fn()} />);
    expect(screen.queryByTestId('scoring-modal-encho-checkbox')).toBeNull();
    // The word itself, inside the pill (the hint's tooltip also says Overtime).
    fireEvent.click(within(screen.getByTestId('scoring-modal-encho-pill')).getByText('Overtime'));
    expect(screen.getByTestId('scoring-modal-encho-checkbox')).toBeTruthy();
  });

  it('keeps a glossary hint beside the pill, outside the button', () => {
    const { container } = render(<EnchoControl enchoPeriodCount={0} setEnchoPeriodCount={vi.fn()} />);
    const hint = container.querySelector('.glossary-hint [data-testid="term-wrapper"]');
    expect(hint).toBeTruthy();
    expect(hint.closest('button')).toBeNull();
  });
});

describe('no Term inside a button or label', () => {
  it('EnchoControl, collapsed', () => {
    const { container } = render(<EnchoControl enchoPeriodCount={0} setEnchoPeriodCount={vi.fn()} />);
    expect(container.querySelectorAll('[data-testid="term-wrapper"]').length).toBeGreaterThan(0);
    expect(container.querySelectorAll(NESTED)).toHaveLength(0);
  });

  it('EnchoControl, expanded', () => {
    const { container } = render(<EnchoControl enchoPeriodCount={1} setEnchoPeriodCount={vi.fn()} />);
    expect(container.querySelectorAll('[data-testid="term-wrapper"]').length).toBeGreaterThan(0);
    expect(container.querySelectorAll(NESTED)).toHaveLength(0);
  });

  for (const kind of ['kiken-voluntary', 'kiken-injury', 'fusenpai']) {
    it(`DecisionPrompt ${kind}, before and after a side is picked`, () => {
      const { container } = render(promptFor(kind));
      // The title is not a control, so its Term is allowed and proves the
      // real Term is installed.
      expect(container.querySelectorAll('[data-testid="term-wrapper"]').length).toBeGreaterThan(0);
      expect(container.querySelectorAll(NESTED)).toHaveLength(0);
      fireEvent.click(container.querySelector('input[name="decision-side"][value="aka"]'));
      expect(container.querySelectorAll(NESTED)).toHaveLength(0);
    });
  }
});
