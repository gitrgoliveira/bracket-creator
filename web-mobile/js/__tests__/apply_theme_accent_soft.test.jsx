import { describe, it, expect, beforeEach } from 'vitest';
import { applyTheme, tintHex } from '../app.jsx';
import { normalizeTheme, BRANDING_DEFAULTS } from '../admin_branding.jsx';

// bc-csst: --accent-soft must follow the Branding primary colour.
//
// The Branding UI has two independent colour pickers, and normalizeTheme fills
// accentSoftColor with the stock #e7eaf3 whenever the operator has not chosen
// one. So an operator who sets ONLY the primary used to get their colour on the
// ~180 rules that read --accent directly, while the 61 rules reading
// --accent-soft stayed navy. A stored value equal to the stock tint therefore
// means "not chosen", and the tint is derived from the primary instead.

const prop = (name) => document.documentElement.style.getPropertyValue(name);
const soft = () => prop('--accent-soft');
const accent = () => prop('--accent');
const strong = () => prop('--accent-strong');

const clearAll = () => {
  for (const name of ['--accent', '--accent-strong', '--accent-soft']) {
    document.documentElement.style.removeProperty(name);
  }
};

describe('tintHex', () => {
  it('keeps `amount` of the colour and mixes the rest toward white', () => {
    expect(tintHex('#000000', 1)).toBe('#000000');
    expect(tintHex('#000000', 0)).toBe('#ffffff');
  });

  it('only approximates the stock pair, which is why the stock primary is never re-derived', () => {
    // The :root token is hand-tuned to #e7eaf3; 0.08 of the stock navy lands
    // 6/255 off in red. Close enough to justify 0.08 for a custom brand, not
    // close enough to replace the token under the stock one (see applyTheme).
    expect(tintHex('#1d3557', 0.08)).toBe('#edeff2');
    expect(BRANDING_DEFAULTS.accentSoftColor).toBe('#e7eaf3');
  });

  it('returns null on malformed input so the caller keeps the CSS default', () => {
    expect(tintHex('nonsense', 0.08)).toBeNull();
    expect(tintHex('', 0.08)).toBeNull();
    expect(tintHex(undefined, 0.08)).toBeNull();
  });
});

describe('applyTheme --accent-soft', () => {
  beforeEach(clearAll);

  it('derives the soft tint when the operator sets only the primary colour', () => {
    // Exactly what the Branding UI stores in that case.
    const stored = normalizeTheme({ primaryColor: '#8e24aa' });
    expect(stored.accentSoftColor).toBe('#e7eaf3'); // premise: the stock fill-in

    applyTheme(stored);

    expect(accent()).toBe('#8e24aa');
    expect(soft()).toBe('#f6edf8'); // 8% of the purple toward white, pinned
    expect(soft()).toBe(tintHex('#8e24aa', 0.08));
  });

  it('keeps an explicitly chosen soft colour', () => {
    applyTheme({ primaryColor: '#8e24aa', accentSoftColor: '#f3e5f5' });
    expect(soft()).toBe('#f3e5f5');
  });

  it('is case-insensitive about the stock tint', () => {
    applyTheme({ primaryColor: '#8e24aa', accentSoftColor: '#E7EAF3' });
    expect(soft()).toBe(tintHex('#8e24aa', 0.08));
  });

  it('falls back to the CSS default when there is no primary to derive from', () => {
    applyTheme({ accentSoftColor: '#e7eaf3' });
    expect(soft()).toBe('');
  });

  it('leaves every token to :root when the stored theme is the stock pair', () => {
    // What normalizeTheme stores for an operator who only typed a window
    // title: both colours filled in with the stock defaults. Deriving from
    // the stock navy would replace the hand-tuned #e7eaf3 with #edeff2 on
    // every running ring and focus halo of an unbranded tournament.
    const stored = normalizeTheme({ windowTitle: 'Cup' });
    expect(stored.primaryColor).toBe(BRANDING_DEFAULTS.primaryColor);

    applyTheme(stored);

    expect(accent()).toBe('');
    expect(strong()).toBe('');
    expect(soft()).toBe('');
    expect(document.title).toBe('Cup');
  });

  it('still honours an explicit soft colour beside the stock primary', () => {
    applyTheme({ primaryColor: '#1d3557', accentSoftColor: '#f3e5f5' });
    expect(accent()).toBe('');
    expect(soft()).toBe('#f3e5f5');
  });

  it('clears both overrides when the theme is absent', () => {
    applyTheme({ primaryColor: '#8e24aa' });
    applyTheme(null);
    expect(accent()).toBe('');
    expect(soft()).toBe('');
  });
});

// state.ValidateTheme guards the API write path but is never run on load, so a
// hand-edited tournament.md reaches applyTheme unvalidated. Verified against a
// real server: GET /api/tournament happily returns primaryColor "not-a-colour",
// and before this guard the SPA set --accent to it, which made every
// declaration reading --accent invalid at computed-value time. The navy hero
// rendered transparent.
describe('applyTheme rejects malformed colours', () => {
  beforeEach(clearAll);

  it('leaves every accent token unset rather than poisoning the rules that read them', () => {
    applyTheme({ primaryColor: 'not-a-colour', accentSoftColor: '#e7eaf3' });
    expect(accent()).toBe('');
    expect(strong()).toBe('');
    expect(soft()).toBe('');
  });

  it('ignores a malformed explicit soft colour', () => {
    applyTheme({ primaryColor: '#8e24aa', accentSoftColor: 'rgb(1,2,3)' });
    // Not applied, and not treated as an explicit choice either: the primary
    // still drives the derived tint.
    expect(soft()).toBe(tintHex('#8e24aa', 0.08));
  });

  it('rejects a hashless hex, as the server does: it is not a CSS colour', () => {
    // "8e24aa" passes a lenient parser but substitutes into var(--accent) as
    // an invalid value, so every rule reading it would render transparent.
    applyTheme({ primaryColor: '8e24aa' });
    expect(accent()).toBe('');
    expect(strong()).toBe('');
  });
});
