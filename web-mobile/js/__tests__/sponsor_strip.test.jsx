import { describe, it, expect } from 'vitest';
import { SponsorStrip } from '../sponsor_strip.jsx';

// Walks a vnode tree (rendered by the global React stub in vitest.setup.js,
// where React.createElement returns plain objects) and collects every
// element with a given type. Mirrors the helper used in reset.test.jsx.
function findAll(node, predicate) {
  const out = [];
  function walk(n) {
    if (!n || typeof n !== 'object') return;
    if (Array.isArray(n)) { n.forEach(walk); return; }
    if (predicate(n)) out.push(n);
    const kids = n.children || (n.props && n.props.children) || [];
    [].concat(kids).forEach(walk);
  }
  walk(node);
  return out;
}

describe('SponsorStrip', () => {
  const sponsorsWithLink = [
    { name: 'Acme', file: 'aa.png', link: 'https://acme.example' },
  ];
  const sponsorsNoLink = [
    { name: 'BetaCo', file: 'bb.jpg' },
  ];

  it('returns null when sponsors is empty', () => {
    expect(SponsorStrip({ sponsors: [], variant: 'viewer' })).toBeNull();
  });

  it('returns null when sponsors is undefined', () => {
    expect(SponsorStrip({ sponsors: undefined, variant: 'viewer' })).toBeNull();
  });

  it('viewer variant wraps linked sponsors in <a target="_blank" rel="noopener noreferrer">', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink, variant: 'viewer' });
    const anchors = findAll(tree, (n) => n.type === 'a');
    expect(anchors).toHaveLength(1);
    expect(anchors[0].props.target).toBe('_blank');
    expect(anchors[0].props.rel).toBe('noopener noreferrer');
    expect(anchors[0].props.href).toBe('https://acme.example');
  });

  it('viewer variant: sponsor without link renders bare <img> (no anchor)', () => {
    const tree = SponsorStrip({ sponsors: sponsorsNoLink, variant: 'viewer' });
    expect(findAll(tree, (n) => n.type === 'a')).toHaveLength(0);
    expect(findAll(tree, (n) => n.type === 'img')).toHaveLength(1);
  });

  it('lobby variant never renders <a> even when link is set', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink, variant: 'lobby' });
    expect(findAll(tree, (n) => n.type === 'a')).toHaveLength(0);
    expect(findAll(tree, (n) => n.type === 'img')).toHaveLength(1);
  });

  it('tv variant never renders <a> even when link is set', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink, variant: 'tv' });
    expect(findAll(tree, (n) => n.type === 'a')).toHaveLength(0);
    expect(findAll(tree, (n) => n.type === 'img')).toHaveLength(1);
  });

  it('renders one <img> per sponsor with /api/sponsors/<file> src', () => {
    const sponsors = [
      { name: 'A', file: '1.png' },
      { name: 'B', file: '2.jpg', link: 'https://b.example' },
      { name: 'C', file: '3.png' },
    ];
    const tree = SponsorStrip({ sponsors, variant: 'viewer' });
    const imgs = findAll(tree, (n) => n.type === 'img');
    expect(imgs).toHaveLength(3);
    expect(imgs.map((i) => i.props.src)).toEqual([
      '/api/sponsors/1.png',
      '/api/sponsors/2.jpg',
      '/api/sponsors/3.png',
    ]);
  });

  it('applies the variant suffix to the root className', () => {
    expect(SponsorStrip({ sponsors: sponsorsWithLink, variant: 'viewer' }).props.className)
      .toContain('sponsor-strip--viewer');
    expect(SponsorStrip({ sponsors: sponsorsWithLink, variant: 'lobby' }).props.className)
      .toContain('sponsor-strip--lobby');
    expect(SponsorStrip({ sponsors: sponsorsWithLink, variant: 'tv' }).props.className)
      .toContain('sponsor-strip--tv');
  });

  it('defaults variant to viewer when omitted', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink });
    expect(tree.props.className).toContain('sponsor-strip--viewer');
    expect(findAll(tree, (n) => n.type === 'a')).toHaveLength(1);
  });
});
