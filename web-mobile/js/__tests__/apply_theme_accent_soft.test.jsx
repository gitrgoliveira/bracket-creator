import { describe, it, expect, beforeEach } from 'vitest';
import { applyTheme, tintHex } from '../app.jsx';
import { normalizeTheme } from '../admin_branding.jsx';

// bc-csst: --accent-soft must follow the Branding primary colour.
//
// The Branding UI has two independent colour pickers, and normalizeTheme fills
// accentSoftColor with the stock #e7eaf3 whenever the operator has not chosen
// one. So an operator who sets ONLY the primary used to get their colour on the
// nine rules that read --accent directly, while the 61 rules reading
// --accent-soft stayed navy. A stored value equal to the stock tint therefore
// means "not chosen", and the tint is derived from the primary instead.

const soft = () => document.documentElement.style.getPropertyValue('--accent-soft');
const accent = () => document.documentElement.style.getPropertyValue('--accent');

describe('tintHex', () => {
  it('keeps `amount` of the colour and mixes the rest toward white', () => {
    expect(tintHex('#000000', 1)).toBe('#000000');
    expect(tintHex('#000000', 0)).toBe('#ffffff');
  });

  it('reproduces the stock soft tint from the stock navy within 6/255', () => {
    const out = tintHex('#1d3557', 0.08);
    const ch = (h, i) => parseInt(h.slice(1 + i * 2, 3 + i * 2), 16);
    const stock = '#e7eaf3';
    for (let i = 0; i < 3; i++) {
      expect(Math.abs(ch(out, i) - ch(stock, i))).toBeLessThanOrEqual(6);
    }
  });

  it('returns null on malformed input so the caller keeps the CSS default', () => {
    expect(tintHex('nonsense', 0.08)).toBeNull();
    expect(tintHex('', 0.08)).toBeNull();
    expect(tintHex(undefined, 0.08)).toBeNull();
  });
});

describe('applyTheme --accent-soft', () => {
  beforeEach(() => {
    document.documentElement.style.removeProperty('--accent');
    document.documentElement.style.removeProperty('--accent-soft');
  });

  it('derives the soft tint when the operator sets only the primary colour', () => {
    // Exactly what the Branding UI stores in that case.
    const stored = normalizeTheme({ primaryColor: '#8e24aa' });
    expect(stored.accentSoftColor).toBe('#e7eaf3'); // premise: the stock fill-in

    applyTheme(stored);

    expect(accent()).toBe('#8e24aa');
    expect(soft()).toBe(tintHex('#8e24aa', 0.08));
    expect(soft()).not.toBe('#e7eaf3'); // the bug: navy tint under a purple brand
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

  it('clears both overrides when the theme is absent', () => {
    applyTheme({ primaryColor: '#8e24aa' });
    applyTheme(null);
    expect(accent()).toBe('');
    expect(soft()).toBe('');
  });

  it('keeps the CSS default rather than a broken value on a malformed primary', () => {
    applyTheme({ primaryColor: 'not-a-colour' });
    expect(soft()).toBe('');
  });
});
