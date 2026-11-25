import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve, dirname } from 'path';
import { fileURLToPath } from 'url';
import {
  POSTER_RULES,
  posterList,
  posterAddress,
  posterChoices,
  logoPlacement,
  logoPadModules,
  codeFits,
  bandTextColour,
  paperInkColour,
  posterPrimary,
  posterSponsors,
  withChoice,
  normalisePosters,
  postersChanged,
  textLength,
  logoURL,
  CENTRE_TOO_LONG_SENTENCE,
} from '../posters_model.jsx';
import { qrMatrix } from '../qr.jsx';

const here = dirname(fileURLToPath(import.meta.url));
const rules = JSON.parse(readFileSync(resolve(here, '../../../internal/state/testdata/poster_rules.json'), 'utf8'));

const base = { publicURL: 'https://kendo.example.org', courts: ['A', 'B'], theme: { hasLogo: true } };
const viewer = { kind: 'viewer', key: 'viewer' };
const court = (letter) => ({ kind: 'court', key: 'court:' + letter, letter });

// A public address long enough to fit a plain code but not one with a centre logo:
// level M holds 213 bytes at version 10 and level H only 119.
const LONG = 'https://' + 'a'.repeat(100) + '.example.org';
// Longer than level M holds.
const TOO_LONG = 'https://' + 'a'.repeat(230) + '.example.org';

describe('the shared poster rules table', () => {
  it('is what the browser holds', () => {
    expect(POSTER_RULES.headingMaxLength).toBe(rules.headingMaxLength);
    expect(POSTER_RULES.callToActionMaxLength).toBe(rules.callToActionMaxLength);
    expect([...POSTER_RULES.logoValues]).toEqual(rules.logoValues);
    expect(POSTER_RULES.maxCourtEntries).toBe(rules.maxCourtEntries);
    expect(POSTER_RULES.defaultHeadings).toEqual(rules.defaultHeadings);
  });

  it('counts characters the way the server does', () => {
    for (const c of rules.lengthCases) expect(textLength(c.text)).toBe(c.length);
  });

  it('takes the default headings from the table', () => {
    expect(posterChoices(base, viewer).headingText).toBe(rules.defaultHeadings.viewer);
    expect(posterChoices(base, court('B')).headingText).toBe(rules.defaultHeadings.court);
  });
});

describe('posterList', () => {
  it('lists the viewer poster, then one per court in the tournament order', () => {
    const list = posterList({ ...base, courts: ['C', 'A', 'B'] });
    expect(list.map((p) => p.key)).toEqual(['viewer', 'court:C', 'court:A', 'court:B']);
  });

  it('lists only the viewer poster for a tournament with no courts', () => {
    expect(posterList({ ...base, courts: undefined }).map((p) => p.kind)).toEqual(['viewer']);
  });

  it('drops a removed court from the list and keeps its stored choices', () => {
    const posters = { courts: { A: { heading: 'Welcome' }, Z: { heading: 'Gone' } } };
    const t = { ...base, posters };
    expect(posterList(t).map((p) => p.key)).toEqual(['viewer', 'court:A', 'court:B']);
    // The stored entry for Z is untouched: it returns with the court.
    expect(t.posters.courts.Z).toEqual({ heading: 'Gone' });
    expect(posterList({ ...t, courts: ['A', 'B', 'Z'] }).map((p) => p.key)).toContain('court:Z');
    expect(posterChoices({ ...t, courts: ['Z'] }, court('Z')).heading).toBe('Gone');
  });
});

describe('posterAddress', () => {
  it('builds the viewer and court pages from the public address', () => {
    expect(posterAddress(base, viewer)).toBe('https://kendo.example.org/viewer');
    expect(posterAddress(base, court('B'))).toBe('https://kendo.example.org/display?court=B');
  });

  it('trims every trailing slash of the public address', () => {
    const t = { ...base, publicURL: 'https://kendo.example.org///' };
    expect(posterAddress(t, viewer)).toBe('https://kendo.example.org/viewer');
    expect(posterAddress(t, court('A'))).toBe('https://kendo.example.org/display?court=A');
  });

  it('falls back to this browser\'s origin with no public address', () => {
    const t = { ...base, publicURL: '' };
    expect(posterAddress(t, viewer)).toBe(window.location.origin + '/viewer');
  });

  it('encodes a court label', () => {
    expect(posterAddress(base, court('&'))).toBe('https://kendo.example.org/display?court=%26');
  });

  it('never carries a reveal key, whatever the browser holds', () => {
    const key = 'a'.repeat(64);
    try {
      window.localStorage.setItem('bc_reveal_keys', JSON.stringify([{ compId: 'c1', key }]));
    } catch { /* storage blocked: the assertion below still holds */ }
    const t = { ...base, competitions: [{ id: 'c1', publicNames: 'hidden' }] };
    for (const p of posterList(t)) {
      const address = posterAddress(t, p);
      expect(address).not.toMatch(/reveal/i);
      expect(address).not.toContain(key);
      expect(address).not.toMatch(/[?&]key=/);
      expect(address.split('?').length).toBeLessThanOrEqual(2);
    }
    expect(posterAddress(t, viewer)).not.toContain('?');
  });
});

describe('posterChoices', () => {
  it('reads the stored choices and trims what prints', () => {
    const t = { ...base, posters: { viewer: { heading: ' Hello ', callToAction: ' Scan me ', logo: 'above', hideSponsors: true } } };
    expect(posterChoices(t, viewer)).toEqual({
      heading: ' Hello ',
      callToAction: ' Scan me ',
      headingText: 'Hello',
      callToActionText: 'Scan me',
      logo: 'above',
      hideSponsors: true,
    });
  });

  it('falls back to the default heading when the box is empty or blank', () => {
    const t = { ...base, posters: { courts: { A: { heading: '   ' } } } };
    expect(posterChoices(t, court('A')).headingText).toBe(rules.defaultHeadings.court);
  });

  it('reads an absent block as every default', () => {
    expect(posterChoices(base, viewer)).toMatchObject({ heading: '', callToActionText: '', logo: '', hideSponsors: false });
  });
});

describe('logoPlacement', () => {
  const centreAsked = { ...base, posters: { viewer: { logo: 'centre' } } };

  it('offers nothing with no logo: no choice, no logo, level M', () => {
    const t = { ...base, theme: {} };
    expect(logoPlacement(t, viewer)).toMatchObject({ offered: false, placement: 'none', level: 'M' });
    expect(logoPlacement({ ...base, theme: undefined }, viewer).offered).toBe(false);
  });

  it('puts the logo in the centre at level H by default and when chosen', () => {
    expect(logoPlacement(base, viewer)).toMatchObject({ offered: true, placement: 'centre', level: 'H', centreAvailable: true, centreRefusedSentence: '' });
    expect(logoPlacement(centreAsked, viewer).placement).toBe('centre');
  });

  it('puts the logo above at level M when that was chosen', () => {
    const t = { ...base, posters: { viewer: { logo: 'above' } } };
    expect(logoPlacement(t, viewer)).toMatchObject({ placement: 'above', level: 'M', centreAvailable: true, centreRefusedSentence: '' });
  });

  it('refuses the centre for a long address and says why', () => {
    const t = { ...centreAsked, publicURL: LONG };
    expect(codeFits(posterAddress(t, viewer))).toBe(true);
    const lp = logoPlacement(t, viewer);
    expect(lp).toMatchObject({ offered: true, placement: 'above', level: 'M', centreAvailable: false });
    expect(lp.centreRefusedSentence).toBe(CENTRE_TOO_LONG_SENTENCE);
    // The default choice is refused the same way.
    expect(logoPlacement({ ...base, publicURL: LONG }, viewer).placement).toBe('above');
  });

  it('knows an address that fits no code at all', () => {
    const t = { ...base, publicURL: TOO_LONG };
    expect(codeFits(posterAddress(t, viewer))).toBe(false);
    expect(logoPlacement(t, viewer).placement).toBe('above');
  });
});

describe('logoPadModules', () => {
  it('stays odd, within 16% of the side and under the share the encoder allows', () => {
    for (let version = 2; version <= 10; version++) {
      const size = 17 + 4 * version;
      const pad = logoPadModules(size);
      expect(pad % 2).toBe(1);
      expect(pad).toBeGreaterThan(0);
      expect(pad).toBeLessThanOrEqual(Math.floor(0.4 * size));
      expect(pad * pad).toBeLessThanOrEqual((version >= 5 ? 0.2 : 0.1) * size * size);
    }
  });

  it('fits the codes the poster really draws', () => {
    const { size } = qrMatrix('https://kendo.example.org/display?court=B', { level: 'H' });
    expect(logoPadModules(size) * logoPadModules(size)).toBeLessThanOrEqual(0.2 * size * size);
  });
});

describe('bandTextColour', () => {
  it('reads white on dark and near-black on light colours', () => {
    expect(bandTextColour('#1d3557')).toBe('#ffffff');
    expect(bandTextColour('#000000')).toBe('#ffffff');
    expect(bandTextColour('#ffffff')).toBe('#111111');
    expect(bandTextColour('#ffd60a')).toBe('#111111');
    expect(bandTextColour('#fff')).toBe('#111111');
  });

  it('reads a value that is not a colour as white', () => {
    expect(bandTextColour('')).toBe('#ffffff');
    expect(bandTextColour('navy')).toBe('#ffffff');
  });

  it('uses the app colour when the theme sets none', () => {
    expect(posterPrimary(base)).toBe('#1d3557');
    expect(posterPrimary({ theme: { primaryColor: '#c1121f' } })).toBe('#c1121f');
  });
});

describe('paperInkColour', () => {
  const rgb = (hex) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));
  const contrastOnWhite = (hex) => {
    const [r, g, b] = rgb(hex).map((c) => {
      const v = c / 255;
      return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
    });
    return 1.05 / (0.2126 * r + 0.7152 * g + 0.0722 * b + 0.05);
  };

  it('keeps a dark primary colour as it is', () => {
    expect(paperInkColour('#1d3557')).toBe('#1d3557');
  });

  it('darkens a light colour until it reads on white, keeping its hue', () => {
    const ink = paperInkColour('#ffd60a');
    expect(ink).toMatch(/^#[0-9a-f]{6}$/);
    expect(ink).not.toBe('#ffd60a');
    expect(contrastOnWhite(ink)).toBeGreaterThanOrEqual(3);
    const [r, , b] = rgb(ink);
    expect(r).toBeGreaterThanOrEqual(b);
  });

  it('darkens white into a colour that reads', () => {
    const ink = paperInkColour('#ffffff');
    expect(ink).toMatch(/^#[0-9a-f]{6}$/);
    expect(contrastOnWhite(ink)).toBeGreaterThanOrEqual(3);
  });

  it('returns a value that is not a colour as it is', () => {
    expect(paperInkColour('red')).toBe('red');
    expect(paperInkColour('')).toBe('');
  });
});

describe('posterSponsors', () => {
  const t = { sponsors: [{ name: 'A', file: 'a.png' }, { name: 'B', file: 'b.png' }, { name: 'none' }] };
  it('lists the sponsors that have a file', () => {
    expect(posterSponsors(t, {}).map((s) => s.file)).toEqual(['a.png', 'b.png']);
  });
  it('lists none when the poster hides them or the tournament has none', () => {
    expect(posterSponsors(t, { hideSponsors: true })).toEqual([]);
    expect(posterSponsors({}, {})).toEqual([]);
  });
});

describe('logoURL', () => {
  it('versions the logo address with the key the page was opened with', () => {
    expect(logoURL('17')).toBe('/api/branding/logo?v=17');
    expect(logoURL(undefined)).toBe('/api/branding/logo');
  });
});

describe('editing the stored block', () => {
  it('sets a viewer choice and a court choice', () => {
    let p = withChoice(undefined, viewer, { heading: 'Hi' }, ['A']);
    p = withChoice(p, court('A'), { callToAction: 'Scan' }, ['A']);
    expect(p).toEqual({ viewer: { heading: 'Hi' }, courts: { A: { callToAction: 'Scan' } } });
  });

  it('keeps the text as typed in a draft and trims it when saved', () => {
    const p = withChoice({}, viewer, { heading: 'Hello ' }, []);
    expect(p.viewer.heading).toBe('Hello ');
    expect(normalisePosters(p).viewer.heading).toBe('Hello');
  });

  it('always answers an object, an empty one when every choice is cleared', () => {
    let p = withChoice({}, viewer, { heading: 'Hi' }, ['A']);
    p = withChoice(p, viewer, { heading: '' }, ['A']);
    expect(p).toEqual({});
    p = withChoice({ courts: { A: { logo: 'above' } } }, court('A'), { logo: '' }, ['A']);
    expect(p).toEqual({});
    expect(withChoice(undefined, viewer, {}, [])).toEqual({});
    expect(normalisePosters(undefined)).toEqual({});
    expect(normalisePosters(null)).toEqual({});
  });

  it('drops an entry that holds only blanks', () => {
    expect(withChoice({}, court('B'), { heading: '   ', callToAction: '' }, ['B'])).toEqual({});
  });

  it('keeps entries of courts that left the tournament', () => {
    const p = withChoice({ courts: { Z: { heading: 'Old' } } }, court('A'), { heading: 'New' }, ['A']);
    expect(p.courts).toEqual({ Z: { heading: 'Old' }, A: { heading: 'New' } });
  });

  it('drops stale entries first when a new one would pass the bound', () => {
    const courts = {};
    for (let i = 0; i < POSTER_RULES.maxCourtEntries; i++) courts['k' + i] = { heading: 'x' };
    courts.A = { heading: 'live' };
    delete courts.k0;
    const live = ['A', 'B'];
    const p = withChoice({ courts }, court('B'), { heading: 'new' }, live);
    expect(Object.keys(p.courts).length).toBe(POSTER_RULES.maxCourtEntries);
    expect(p.courts.A).toEqual({ heading: 'live' });
    expect(p.courts.B).toEqual({ heading: 'new' });
  });

  it('ignores a logo value the server would refuse', () => {
    expect(withChoice({}, viewer, { logo: 'sideways' }, [])).toEqual({});
  });

  it('is not a change when only trailing spaces differ', () => {
    const a = { viewer: { heading: 'Hello' } };
    expect(postersChanged(a, { viewer: { heading: 'Hello  ' } })).toBe(false);
    expect(postersChanged(a, { viewer: { heading: 'Hello!' } })).toBe(true);
    expect(postersChanged(undefined, {})).toBe(false);
    expect(postersChanged({ courts: { A: { logo: 'above' } } }, {})).toBe(true);
  });
});
