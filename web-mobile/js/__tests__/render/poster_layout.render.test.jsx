import React from 'react';
import { render, fireEvent } from '@testing-library/react';
import { describe, it, expect } from 'vitest';
import jsQR from 'jsqr';
import { Poster, codePath } from '../../poster_layout.jsx';
import { posterChoices, posterAddress, logoPadModules, POSTER_SIZES, CODE_TOO_LONG_SENTENCE } from '../../posters_model.jsx';
import { qrMatrix } from '../../qr.jsx';

// jsdom has no layout, no cqw and no print: these tests cover the structure and
// the values the layout hands to the stylesheet. Whether the boxes come out as
// intended on A4 is measured in a real browser (see the poster print check).

const viewer = { kind: 'viewer', key: 'viewer', label: 'Viewer home' };
const courtB = { kind: 'court', key: 'court:B', letter: 'B', label: 'Shiaijo B' };
const base = {
  name: 'Spring Open',
  publicURL: 'https://kendo.example.org',
  courts: ['A', 'B'],
  theme: { primaryColor: '#1d3557', hasLogo: true },
};
const sponsors = (n) => Array.from({ length: n }, (_, i) => ({ name: 'Sponsor ' + (i + 1), file: 'sp' + (i + 1) + '.png' }));

const draw = (t, poster, choices) => render(<Poster tournament={t} poster={poster} choices={choices} logoKey="3" />).container;

describe('Poster sponsor strip', () => {
  it.each([1, 4, 6])('draws one strip holding all %i logos', (n) => {
    const root = draw({ ...base, sponsors: sponsors(n) }, viewer);
    const strips = root.querySelectorAll('.poster__sponsors');
    expect(strips).toHaveLength(1);
    const logos = strips[0].querySelectorAll('img.poster__sponsor');
    expect(logos).toHaveLength(n);
    expect([...logos].map((i) => i.getAttribute('src'))).toEqual(sponsors(n).map((s) => '/api/sponsors/' + s.file));
    expect(strips[0].style.getPropertyValue('--poster-sponsors')).toBe(String(n));
    expect(strips[0].getAttribute('data-count')).toBe(String(n));
  });

  it('lets fewer sponsors grow taller, never under the floor', () => {
    const maxOf = (n) => draw({ ...base, sponsors: sponsors(n) }, viewer)
      .querySelector('.poster__sponsors').style.getPropertyValue('--poster-sponsor-max');
    const caps = [1, 2, 3, 4, 5, 6].map((n) => parseFloat(maxOf(n)));
    expect(caps).toEqual([...POSTER_SIZES.sponsorMax]);
    for (let i = 1; i < caps.length; i += 1) expect(caps[i]).toBeLessThan(caps[i - 1]);
    expect(caps[5]).toBeGreaterThan(POSTER_SIZES.sponsorMin);
    const strip = draw({ ...base, sponsors: sponsors(2) }, viewer).querySelector('.poster__sponsors');
    expect(strip.style.getPropertyValue('--poster-sponsor-min')).toBe(POSTER_SIZES.sponsorMin + 'cqw');
  });

  it('keeps the address right under its code', () => {
    const group = draw(base, viewer).querySelector('.poster__code-group');
    expect([...group.children].map((n) => n.className.split(' ')[0])).toEqual(['poster__code', 'poster__address']);
  });

  it('draws no strip without sponsors', () => {
    const root = draw(base, viewer);
    expect(root.querySelector('.poster__sponsors')).toBeNull();
    expect(root.querySelector('.poster__sponsor')).toBeNull();
  });

  it('hides a sponsor file that fails to load, and the strip when none is left', () => {
    const root = draw({ ...base, sponsors: sponsors(2) }, viewer);
    const logos = root.querySelectorAll('img.poster__sponsor');
    fireEvent.error(logos[0]);
    expect(logos[0].style.display).toBe('none');
    expect(logos[1].style.display).not.toBe('none');
    expect(root.querySelector('.poster__sponsors').style.display).not.toBe('none');
    fireEvent.error(logos[1]);
    expect(root.querySelector('.poster__sponsors').style.display).toBe('none');
  });

  it('draws no strip when the poster hides its sponsors', () => {
    const t = { ...base, sponsors: sponsors(3), posters: { viewer: { hideSponsors: true } } };
    expect(draw(t, viewer, posterChoices(t, viewer)).querySelector('.poster__sponsors')).toBeNull();
  });
});

describe('Poster sizes', () => {
  it('sets the court letter above the court code and the viewer code larger than the court code', () => {
    const court = draw(base, courtB);
    const style = court.querySelector('.poster').style;
    expect(style.getPropertyValue('--poster-code')).toBe(POSTER_SIZES.courtCode + 'cqw');
    expect(style.getPropertyValue('--poster-letter')).toBe(POSTER_SIZES.courtLetter + 'cqw');
    expect(POSTER_SIZES.courtLetter).toBeGreaterThan(POSTER_SIZES.courtCode);
    expect(court.querySelector('.poster__court-letter').textContent).toBe('B');
    expect(court.querySelector('.poster__court-label').textContent).toBe('Shiaijo');

    const home = draw(base, viewer);
    expect(home.querySelector('.poster').style.getPropertyValue('--poster-code')).toBe(POSTER_SIZES.viewerCode + 'cqw');
    expect(POSTER_SIZES.viewerCode).toBeGreaterThan(POSTER_SIZES.courtCode);
    expect(home.querySelector('.poster__court')).toBeNull();
  });

  it('colours the band from the theme with a readable ink', () => {
    const style = draw(base, viewer).querySelector('.poster').style;
    expect(style.getPropertyValue('--poster-primary')).toBe('#1d3557');
    expect(style.getPropertyValue('--poster-band-ink')).toBe('#ffffff');
    const light = draw({ ...base, theme: { primaryColor: '#ffd60a' } }, viewer).querySelector('.poster').style;
    expect(light.getPropertyValue('--poster-band-ink')).toBe('#111111');
  });

  it('sets the text on the paper in a darker ink when the primary colour is light', () => {
    const style = draw({ ...base, theme: { primaryColor: '#ffd60a' } }, courtB).querySelector('.poster').style;
    expect(style.getPropertyValue('--poster-primary')).toBe('#ffd60a');
    const ink = style.getPropertyValue('--poster-ink');
    expect(ink).toMatch(/^#[0-9a-f]{6}$/);
    expect(ink).not.toBe('#ffd60a');
  });
});

describe('Poster text', () => {
  it('prints the tournament name, the default heading and the address', () => {
    const root = draw(base, viewer);
    expect(root.querySelector('.poster__tournament').textContent).toBe('Spring Open');
    expect(root.querySelector('.poster__heading').textContent).toBe('Follow the tournament live');
    expect(root.querySelector('.poster__address').textContent).toBe('https://kendo.example.org/viewer');
    expect(root.querySelector('.poster__cta')).toBeNull();
  });

  it('prints a typed heading and a call to action under it', () => {
    const t = { ...base, posters: { courts: { B: { heading: 'Pools here', callToAction: 'Scan for results' } } } };
    const root = draw(t, courtB);
    expect(root.querySelector('.poster__heading').textContent).toBe('Pools here');
    expect(root.querySelector('.poster__cta').textContent).toBe('Scan for results');
    expect(root.querySelector('.poster__address').textContent).toBe('https://kendo.example.org/display?court=B');
  });

  it('sets a longer heading smaller', () => {
    const long = 'x'.repeat(70);
    const t = { ...base, posters: { viewer: { heading: long } } };
    expect(draw(t, viewer).querySelector('.poster__heading').className).toContain('poster__heading--long');
    expect(draw(base, viewer).querySelector('.poster__heading').className).toContain('poster__heading--short');
  });

  it('prints no banned word', () => {
    const t = { ...base, sponsors: sponsors(2), posters: { courts: { B: { callToAction: 'Scan' } } } };
    const text = draw(t, courtB).textContent.toLowerCase();
    expect(text).not.toMatch(/\bmats?\b|squad|default win/);
  });
});

describe('Poster code', () => {
  it('draws one black path of crisp square modules on white', () => {
    const root = draw({ ...base, theme: { primaryColor: '#c1121f' } }, viewer);
    const svg = root.querySelector('svg.poster__code-svg');
    expect(svg.getAttribute('shape-rendering')).toBe('crispEdges');
    const paths = svg.querySelectorAll('path');
    expect(paths).toHaveLength(1);
    expect(paths[0].getAttribute('fill')).toBe('#000');
    const rect = svg.querySelector('rect');
    expect(rect.getAttribute('fill')).toBe('#fff');
    const [, , w, h] = svg.getAttribute('viewBox').split(' ').map(Number);
    expect(w).toBe(h);
    expect(w).toBe(Number(root.querySelector('.poster__code').getAttribute('data-size')) + 8);
  });

  it('encodes exactly the address, which a scan reads back', () => {
    for (const poster of [viewer, courtB]) {
      for (const theme of [{ hasLogo: true }, {}]) {
        const t = { ...base, theme };
        const root = draw(t, poster);
        const code = root.querySelector('.poster__code');
        const size = Number(code.getAttribute('data-size'));
        const pad = Number(code.getAttribute('data-pad'));
        const total = size + 8;
        const scale = 4;
        const n = total * scale;
        const data = new Uint8ClampedArray(n * n * 4).fill(255);
        // Rasterise the drawn path itself (M x y h w v1 h-w z runs).
        const d = root.querySelector('path').getAttribute('d');
        for (const m of d.matchAll(/M(\d+) (\d+)h(\d+)v1h-\d+z/g)) {
          const [x, y, w] = [Number(m[1]), Number(m[2]), Number(m[3])];
          for (let px = x * scale; px < (x + w) * scale; px++) {
            for (let py = y * scale; py < (y + 1) * scale; py++) {
              const i = (py * n + px) * 4;
              data[i] = data[i + 1] = data[i + 2] = 0;
            }
          }
        }
        const result = jsQR(data, n, n);
        expect(result && result.data, poster.key + ' pad ' + pad).toBe(posterAddress(t, poster));
      }
    }
  });

  it('leaves the modules under the centre pad out of the path', () => {
    const address = posterAddress(base, viewer);
    const matrix = qrMatrix(address, { level: 'H' });
    const pad = logoPadModules(matrix.size);
    const withPad = codePath(matrix, pad);
    const without = codePath(matrix, 0);
    expect(withPad.length).toBeLessThan(without.length);
    // No run starts or ends inside the pad square.
    const start = Math.floor((matrix.size - pad) / 2) + 4;
    for (const m of withPad.matchAll(/M(\d+) (\d+)h(\d+)v1h-\d+z/g)) {
      const [x, y, w] = [Number(m[1]), Number(m[2]), Number(m[3])];
      const insideRows = y >= start && y < start + pad;
      const overlapsCols = x < start + pad && x + w > start;
      expect(insideRows && overlapsCols).toBe(false);
    }
  });
});

describe('Poster logo', () => {
  it('puts the logo on a white pad in the centre of a level H code, within 20% of the area', () => {
    const root = draw(base, viewer);
    const code = root.querySelector('.poster__code');
    expect(root.querySelector('.poster').getAttribute('data-placement')).toBe('centre');
    expect(code.getAttribute('data-level')).toBe('H');
    const size = Number(code.getAttribute('data-size'));
    const pad = Number(code.getAttribute('data-pad'));
    expect(pad).toBeGreaterThan(0);
    expect(pad * pad).toBeLessThanOrEqual(0.2 * size * size);
    const logo = code.querySelector('img.poster__logo-centre');
    expect(logo.getAttribute('src')).toBe('/api/branding/logo?v=3');
    const total = size + 8;
    // Inside the pad, centred, and square.
    const left = parseFloat(logo.style.left);
    const width = parseFloat(logo.style.width);
    expect(width).toBeCloseTo(((pad - 2) / total) * 100, 2);
    expect(left + width / 2).toBeCloseTo(50, 1);
    expect(logo.style.top).toBe(logo.style.left);
    expect(logo.style.height).toBe(logo.style.width);
    expect(root.querySelector('.poster__logo-above')).toBeNull();
  });

  it('puts the logo above a level M code when chosen, with nothing over the code', () => {
    const t = { ...base, posters: { viewer: { logo: 'above' } } };
    const root = draw(t, viewer);
    expect(root.querySelector('.poster').getAttribute('data-placement')).toBe('above');
    expect(root.querySelector('.poster__code').getAttribute('data-level')).toBe('M');
    expect(root.querySelector('.poster__code').getAttribute('data-pad')).toBe('0');
    expect(root.querySelector('.poster__logo-centre')).toBeNull();
    const above = root.querySelector('img.poster__logo-above');
    expect(above.getAttribute('src')).toBe('/api/branding/logo?v=3');
    // Above the code in reading order.
    const nodes = [...root.querySelectorAll('.poster__logo-above, .poster__code')];
    expect(nodes[0]).toBe(above);
  });

  it('sits in the top corner beside the letter on a court poster, not above the code', () => {
    const t = { ...base, posters: { courts: { B: { logo: 'above' } } } };
    const root = draw(t, courtB);
    expect(root.querySelector('.poster').getAttribute('data-placement')).toBe('above');
    expect(root.querySelector('.poster__logo-above')).toBeNull();
    const corner = root.querySelector('.poster__court > img.poster__logo-corner');
    expect(corner.getAttribute('src')).toBe('/api/branding/logo?v=3');
    const style = root.querySelector('.poster').style;
    expect(style.getPropertyValue('--poster-logo-corner')).toBe(POSTER_SIZES.courtLogoCorner + 'cqw');
    expect(style.getPropertyValue('--poster-logo-corner-width')).toBe(POSTER_SIZES.courtLogoCornerWidth + 'cqw');
  });

  it('draws no logo and a level M code when the tournament has none', () => {
    const t = { ...base, theme: { primaryColor: '#1d3557' }, posters: { viewer: { logo: 'centre' } } };
    const root = draw(t, viewer);
    expect(root.querySelector('img[src^="/api/branding/logo"]')).toBeNull();
    expect(root.querySelector('.poster').getAttribute('data-placement')).toBe('none');
    expect(root.querySelector('.poster__code').getAttribute('data-level')).toBe('M');
  });

  it('moves a logo above when the address is too long for the centre', () => {
    const t = { ...base, publicURL: 'https://' + 'a'.repeat(100) + '.example.org' };
    const root = draw(t, viewer);
    expect(root.querySelector('.poster').getAttribute('data-placement')).toBe('above');
    expect(root.querySelector('.poster__code').getAttribute('data-level')).toBe('M');
    expect(root.querySelector('.poster__logo-centre')).toBeNull();
  });
});

describe('Poster past level M', () => {
  const tooLong = { ...base, publicURL: 'https://' + 'a'.repeat(230) + '.example.org' };

  it('shows the warning in place of the code and keeps the address line', () => {
    const root = draw(tooLong, viewer);
    expect(root.querySelector('svg')).toBeNull();
    expect(root.querySelector('.poster__code-warning').textContent).toBe(CODE_TOO_LONG_SENTENCE);
    expect(root.querySelector('.poster__address').textContent).toBe(posterAddress(tooLong, viewer));
    expect(root.querySelector('.poster__logo-centre')).toBeNull();
  });
});
