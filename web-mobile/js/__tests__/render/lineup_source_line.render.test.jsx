// LineupSourceLine and LineupProblem (lineup_draft.jsx) are the ONE rendering of
// where a lineup editor's lineup was saved, of the button that gives a match's
// own lineup up, and of why a lineup could not be read. The at-court panel and
// the Lineups page both render them, so the two can no longer drift (they had:
// the page's label was 12px and the panel's 11px, each with its own inline style).

import React from 'react';
import { render, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import { LineupSourceLine, LineupProblem } from '../../lineup_draft.jsx';
import { readStylesheet, cssBlock } from '../helpers/source.js';

const MATCHES = [{ id: 'Pool D-0', phase: 'pool', poolName: 'Pool D' }];
const formOf = (extra = {}) => ({
  read: true, source: { matchId: 'Pool D-1' }, saveQueued: false, removing: false, dropOwnLineup: vi.fn(), ...extra,
});
const line = (form, props = {}) => render(
  <LineupSourceLine form={form} matchId="Pool D-1" allMatches={MATCHES} busy={false} testId="src" {...props} />
);
const USE = "Use the previous match's lineup";

describe('LineupSourceLine', () => {
  it('shows nothing before the lineup was read: it would name a source the form does not hold', () => {
    const { container } = line(formOf({ read: false }));
    expect(container.firstChild).toBeNull();
  });

  it('names a match\'s own lineup in the one emphasis, with the button that gives it up', () => {
    const form = formOf();
    const utils = line(form);
    const label = utils.getByTestId('src');
    expect(label.textContent).toBe('Lineup for this match');
    expect(label.className.split(/\s+/)).toContain('lineup-source__label--own');
    expect(label.hasAttribute('style')).toBe(false);

    const button = utils.getByRole('button', { name: USE });
    expect(button.disabled).toBe(false);
    fireEvent.click(button);
    expect(form.dropOwnLineup).toHaveBeenCalledTimes(1);
  });

  it('puts the button in a plain .btn, so the coarse-pointer floor reaches it through the class', () => {
    const button = line(formOf()).getByRole('button', { name: USE });
    expect(button.className.split(/\s+/)).toEqual(expect.arrayContaining(['btn', 'btn--sm', 'lineup-source__use']));
    expect(button.hasAttribute('style')).toBe(false);
    expect(button.getAttribute('type')).toBe('button');
  });

  it('offers no button, and no emphasis, on a lineup the match carries', () => {
    const utils = line(formOf({ source: { matchId: 'Pool D-0' } }));
    expect(utils.getByTestId('src').textContent).toBe('Same as Pool D · Match 1');
    expect(utils.getByTestId('src').className.split(/\s+/)).not.toContain('lineup-source__label--own');
    expect(utils.queryByRole('button')).toBeNull();
  });

  it.each([
    ['the starting lineup', { round: 0 }, 'Starting lineup'],
    ['nothing saved', null, 'No lineup saved yet'],
  ])('offers no button on %s', (_name, source, text) => {
    const utils = line(formOf({ source }));
    expect(utils.getByTestId('src').textContent).toBe(text);
    expect(utils.queryByRole('button')).toBeNull();
  });

  it('asks for the matches only when the lineup is carried from another match', () => {
    const matches = vi.fn(() => MATCHES);
    line(formOf(), { allMatches: matches });
    line(formOf({ source: null }), { allMatches: matches });
    line(formOf({ source: { round: 0 } }), { allMatches: matches });
    expect(matches).not.toHaveBeenCalled();
    line(formOf({ source: { matchId: 'Pool D-0' } }), { allMatches: matches });
    expect(matches).toHaveBeenCalledTimes(1);
  });

  describe('while a save of the lineup is still queued', () => {
    it('disables the button and says why in a line, never in a title alone', () => {
      const form = formOf({ saveQueued: true });
      const utils = line(form);
      const button = utils.getByRole('button', { name: USE });
      expect(button.disabled).toBe(true);
      expect(button.hasAttribute('title')).toBe(false);
      const why = utils.getByText('A save of this lineup is still waiting to be sent.');
      expect(why.getAttribute('role')).toBe('status');
      fireEvent.click(button);
      expect(form.dropOwnLineup).not.toHaveBeenCalled();
    });

    it('says nothing about it where there is no button to wait', () => {
      const utils = line(formOf({ saveQueued: true, source: { matchId: 'Pool D-0' } }));
      expect(utils.queryByText('A save of this lineup is still waiting to be sent.')).toBeNull();
    });
  });

  it('disables the button while a save or a removal is out, with no reason line', () => {
    const saving = line(formOf(), { busy: true });
    expect(saving.getByRole('button', { name: USE }).disabled).toBe(true);
    expect(saving.queryByRole('status')).toBeNull();
    saving.unmount();
    const removing = line(formOf({ removing: true }));
    expect(removing.getByRole('button', { name: USE }).disabled).toBe(true);
  });
});

describe('LineupProblem', () => {
  it('shows nothing when the lineup was read', () => {
    const { container } = render(<LineupProblem form={{ loadError: '', retry: vi.fn(), removing: false }} />);
    expect(container.firstChild).toBeNull();
  });

  it('says why, with a Try again that reads again', () => {
    const retry = vi.fn();
    const utils = render(<LineupProblem form={{ loadError: 'competition not found', retry, removing: false }} testId="problem" />);
    const box = utils.getByTestId('problem');
    expect(box.getAttribute('role')).toBe('alert');
    expect(box.className).toContain('alert--error');
    expect(utils.getByText('competition not found')).toBeTruthy();
    fireEvent.click(utils.getByRole('button', { name: 'Try again' }));
    expect(retry).toHaveBeenCalledTimes(1);
  });

  it('puts Try again in a plain .btn, and holds it while a removal is out', () => {
    const utils = render(<LineupProblem form={{ loadError: 'down', retry: vi.fn(), removing: true }} />);
    const button = utils.getByRole('button', { name: 'Try again' });
    expect(button.className.split(/\s+/)).toEqual(expect.arrayContaining(['btn', 'btn--sm']));
    expect(button.hasAttribute('style')).toBe(false);
    expect(button.disabled).toBe(true);
  });
});

describe('the stylesheet gives the source line ONE size and ONE emphasis', () => {
  const css = readStylesheet();
  const block = (selector) => {
    const b = cssBlock(css, selector);
    expect(b, `rule ${selector} exists`).not.toBeNull();
    return b;
  };

  it('sizes the line once, at 12px, and emphasises an own lineup by colour and weight alone', () => {
    expect(block('.lineup-source')).toMatch(/font-size:\s*12px/);
    expect(block('.lineup-source__label--own')).not.toMatch(/font-size/);
    expect(block('.lineup-source__label--own')).toMatch(/color:\s*var\(--accent\)/);
    expect(block('.lineup-source__label--own')).toMatch(/font-weight:\s*600/);
  });

  it('pushes the button to the end of the line, and wraps the queued reason onto a line of its own', () => {
    expect(block('.lineup-source__use')).toMatch(/margin-left:\s*auto/);
    expect(block('.lineup-source')).toMatch(/flex-wrap:\s*wrap/);
    expect(block('.lineup-source__why')).toMatch(/flex:\s*1 0 100%/);
  });

  it('leaves the buttons their .btn floor: no min-height or height of their own', () => {
    for (const selector of ['.lineup-source__use', '.lineup-problem']) {
      expect(block(selector)).not.toMatch(/(^|[\s;])(min-)?height:/);
    }
  });
});
