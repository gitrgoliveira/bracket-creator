// The glyphs Icon draws in place of the text symbols that used to act as icons
// (operator decision H1, round ship-jsxs-hallmark-1): the team sheet's bout
// caret, the paging and queue chevrons, the court picker's chevron, the back
// arrows, the dismiss cross, the watchlist pin star, the dojo house, the edit
// pen and the setup step markers. Each is drawn in currentColor and hidden from
// assistive technology, since it always sits beside a label or inside a named
// control; the star's pinned state is a filled shape, so it does not rest on
// colour alone.
import React from 'react';
import { render, cleanup } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { Icon } from '../../icon.jsx';

afterEach(cleanup);

const NEW_GLYPHS = [
  'chevron-right', 'chevron-left', 'chevron-down', 'arrow-left', 'arrow-right',
  'x', 'house', 'pen', 'star', 'star-filled', 'circle', 'circle-dot',
];

describe('Icon draws the glyphs that replaced text symbols', () => {
  it.each(NEW_GLYPHS)('%s is an aria-hidden svg named by data-icon, with no text', (name) => {
    const { container } = render(<Icon name={name} size={12} className="probe" />);
    const svg = container.querySelector('svg');
    expect(svg, `${name} draws`).not.toBeNull();
    expect(svg.getAttribute('data-icon')).toBe(name);
    expect(svg.getAttribute('aria-hidden')).toBe('true');
    expect(svg.getAttribute('stroke')).toBe('currentColor');
    expect(svg.getAttribute('class')).toBe('probe');
    expect(svg.childElementCount).toBeGreaterThan(0);
    expect(svg.textContent).toBe('');
  });

  it('the pinned star is filled and the unpinned one is not', () => {
    const filled = render(<Icon name="star-filled" />).container.querySelector('path');
    cleanup();
    const outline = render(<Icon name="star" />).container.querySelector('path');
    expect(filled.getAttribute('fill')).toBe('currentColor');
    expect(outline.getAttribute('fill')).toBeNull();
    expect(filled.getAttribute('d')).toBe(outline.getAttribute('d'));
  });

  it('an unknown name draws nothing rather than a blank box', () => {
    const { container } = render(<Icon name="no-such-glyph" />);
    expect(container.querySelector('svg')).toBeNull();
  });
});
