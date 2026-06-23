import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { MatchDetailCard, VSchedItem } from '../../viewer_match.jsx';

// The viewer match-detail card renders competitor names in its OWN header (not
// via IndividualScore's showNames), so the registration tag badge is added
// alongside those header names. Confirms tags surface on the viewer card too.
describe('MatchDetailCard — registration tag badges in the header', () => {
  const baseMatch = (overrides = {}) => ({
    id: 'Pool A-0', status: 'completed',
    sideA: { id: 'a', name: 'Aka Player', tag: 'transfer' },
    sideB: { id: 'b', name: 'Shiro Player', tag: 'registered' },
    ipponsA: ['M'], ipponsB: [],
    ...overrides,
  });

  it('renders the tag badge next to each header name when present', () => {
    const { container } = render(<MatchDetailCard match={baseMatch()} onClose={() => {}} />);
    const shiroTag = container.querySelector('[data-testid="card-shiro-tag"]');
    const akaTag = container.querySelector('[data-testid="card-aka-tag"]');
    expect(shiroTag?.textContent).toBe('registered'); // sideB = shiro
    expect(akaTag?.textContent).toBe('transfer');      // sideA = aka
  });

  it('renders no tag badge when the sides have no tag', () => {
    const m = baseMatch({ sideA: { id: 'a', name: 'Aka Player' }, sideB: { id: 'b', name: 'Shiro Player' } });
    const { container } = render(<MatchDetailCard match={m} onClose={() => {}} />);
    expect(container.querySelector('[data-testid="card-shiro-tag"]')).toBeNull();
    expect(container.querySelector('[data-testid="card-aka-tag"]')).toBeNull();
  });
});

describe('VSchedItem — registration tag badges on schedule rows', () => {
  it('renders a tag badge next to each name when present', () => {
    const m = {
      id: 'Pool A-0', status: 'scheduled', phase: 'pool', ipponsA: [], ipponsB: [],
      sideA: { id: 'a', name: 'Aka Player', tag: 'transfer' },
      sideB: { id: 'b', name: 'Shiro Player', tag: 'manual' },
    };
    const { container } = render(<VSchedItem m={m} tweaks={{}} />);
    expect(container.querySelector('[data-testid="vsched-shiro-tag"]')?.textContent).toBe('manual'); // sideB
    expect(container.querySelector('[data-testid="vsched-aka-tag"]')?.textContent).toBe('transfer');  // sideA
  });

  it('renders no tag badge when sides have no tag', () => {
    const m = { id: 'Pool A-0', status: 'scheduled', phase: 'pool', ipponsA: [], ipponsB: [], sideA: { id: 'a', name: 'A' }, sideB: { id: 'b', name: 'B' } };
    const { container } = render(<VSchedItem m={m} tweaks={{}} />);
    expect(container.querySelector('[data-testid="vsched-shiro-tag"]')).toBeNull();
    expect(container.querySelector('[data-testid="vsched-aka-tag"]')).toBeNull();
  });
});
