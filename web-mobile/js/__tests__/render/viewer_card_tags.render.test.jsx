import React from 'react';
import { render } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import { MatchDetailCard } from '../../viewer_match.jsx';

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
