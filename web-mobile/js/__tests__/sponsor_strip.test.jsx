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
    expect(SponsorStrip({ sponsors: [] })).toBeNull();
  });

  it('returns null when sponsors is undefined', () => {
    expect(SponsorStrip({ sponsors: undefined })).toBeNull();
  });

  it('wraps linked sponsors in <a target="_blank" rel="noopener noreferrer">', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink });
    const anchors = findAll(tree, (n) => n.type === 'a');
    expect(anchors).toHaveLength(1);
    expect(anchors[0].props.target).toBe('_blank');
    expect(anchors[0].props.rel).toBe('noopener noreferrer');
    expect(anchors[0].props.href).toBe('https://acme.example');
  });

  it('sponsor without link renders bare <img> (no anchor)', () => {
    const tree = SponsorStrip({ sponsors: sponsorsNoLink });
    expect(findAll(tree, (n) => n.type === 'a')).toHaveLength(0);
    expect(findAll(tree, (n) => n.type === 'img')).toHaveLength(1);
  });

  it('renders one <img> per sponsor with /api/sponsors/<file> src', () => {
    const sponsors = [
      { name: 'A', file: '1.png' },
      { name: 'B', file: '2.jpg', link: 'https://b.example' },
      { name: 'C', file: '3.png' },
    ];
    const tree = SponsorStrip({ sponsors });
    const imgs = findAll(tree, (n) => n.type === 'img');
    expect(imgs).toHaveLength(3);
    expect(imgs.map((i) => i.props.src)).toEqual([
      '/api/sponsors/1.png',
      '/api/sponsors/2.jpg',
      '/api/sponsors/3.png',
    ]);
  });

  it('always uses the viewer-only root className (sponsors render on the viewer page only)', () => {
    expect(SponsorStrip({ sponsors: sponsorsWithLink }).props.className)
      .toContain('sponsor-strip--viewer');
  });

  it('every <img> onError hides the wrapper element, not just the img', () => {
    // A broken logo that only hides the <img> still leaves its <a> or <span>
    // wrapper as an empty flex item, which shows as a visible blank gap.
    // The onError handler must climb to parentElement and hide the wrapper.
    // We verify this by simulating the DOM event: stub parentElement with a
    // spy, call the handler, and confirm style.display was set on the parent.
    const tree = SponsorStrip({ sponsors: sponsorsWithLink });
    const imgs = findAll(tree, (n) => n.type === 'img');
    expect(imgs.length).toBeGreaterThan(0);
    imgs.forEach((img) => {
      expect(typeof img.props.onError).toBe('function');
      // Simulate the browser onError event with a fake currentTarget.
      const parentStyle = {};
      const fakeEvent = { currentTarget: { parentElement: { style: parentStyle } } };
      img.props.onError(fakeEvent);
      expect(parentStyle.display).toBe('none');
    });
  });

  it('root div has role=complementary and aria-label=Sponsors', () => {
    const tree = SponsorStrip({ sponsors: sponsorsWithLink });
    expect(tree.props.role).toBe('complementary');
    expect(tree.props['aria-label']).toBe('Sponsors');
  });
});
