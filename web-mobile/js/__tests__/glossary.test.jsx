import { describe, it, expect, vi } from 'vitest';
import { GLOSSARY, lookupTerm } from '../glossary_data.js';
import { Term, renderTooltipBody } from '../glossary.jsx';

// glossary.jsx ships:
//   - Term: the <span>...<span role="tooltip">...</span></span> wrapper
//     that the UI surfaces wrap kendo terms with.
//   - renderTooltipBody: the pure substring-split helper that replaces
//     cross-referenced IDs inside a tooltip with nested <Term> spans.
//   - GlossaryPage: the /glossary viewer (covered via UI smoke tests in
//     viewer.test.jsx, not here — the component is mostly markup).
//
// The React global in vitest.setup.js stubs hooks to vi.fn() (identity
// useMemo, no-op useEffect, [val, vi.fn()] useState), so we exercise
// the component by calling it as a function and asserting the virtual
// tree shape. Same pattern as admin_scoring_modal.test.jsx.

describe('GLOSSARY constant', () => {
  // Pin the contract that the generated data file exposes the map and
  // that it covers the agreed term IDs the UI relies on.
  it('exports the canonical kendo terms', () => {
    expect(GLOSSARY).toBeTruthy();
    expect(typeof GLOSSARY).toBe('object');
    // Spot-check the Tier 1 must-haves the scoring modal wraps.
    expect(GLOSSARY.kiken).toBeTruthy();
    expect(GLOSSARY.fusenpai).toBeTruthy();
    expect(GLOSSARY.fusensho).toBeTruthy();
    expect(GLOSSARY.shiro).toBeTruthy();
    expect(GLOSSARY.aka).toBeTruthy();
    expect(GLOSSARY.shiaijo).toBeTruthy();
    expect(GLOSSARY.encho).toBeTruthy();
    expect(GLOSSARY.hansoku).toBeTruthy();
    expect(GLOSSARY.ippon).toBeTruthy();
    expect(GLOSSARY.daihyosen).toBeTruthy();
    expect(GLOSSARY.hikiwake).toBeTruthy();
  });

  it('has at least 22 entries', () => {
    // Slice U1 ships 23 entries (11 + 7 + 5 — see glossary.md). The
    // count guard exists so a future deletion that drops below the
    // agreed floor fails loudly.
    expect(Object.keys(GLOSSARY).length).toBeGreaterThanOrEqual(22);
  });

  it('every entry has id/short/tooltip', () => {
    Object.entries(GLOSSARY).forEach(([id, entry]) => {
      expect(entry.id).toBe(id);
      expect(entry.short).toBeTruthy();
      expect(entry.tooltip).toBeTruthy();
    });
  });

  it('every seeAlso reference resolves to another GLOSSARY key', () => {
    Object.entries(GLOSSARY).forEach(([id, entry]) => {
      (entry.seeAlso || []).forEach((ref) => {
        expect(GLOSSARY[ref.toLowerCase()]).toBeTruthy();
        expect(`broken seeAlso in ${id}: ${ref}`).toBeTruthy();
      });
    });
  });
});

describe('lookupTerm', () => {
  it('looks up by exact ID', () => {
    const term = lookupTerm('kiken');
    expect(term).toBeTruthy();
    expect(term.id).toBe('kiken');
  });

  it('normalises case and whitespace', () => {
    expect(lookupTerm('KIKEN').id).toBe('kiken');
    expect(lookupTerm('  Ippon-Shobu  ').id).toBe('ippon-shobu');
  });

  it('returns null on unknown ID', () => {
    expect(lookupTerm('bogus')).toBeNull();
    expect(lookupTerm('')).toBeNull();
    expect(lookupTerm(null)).toBeNull();
    expect(lookupTerm(undefined)).toBeNull();
    expect(lookupTerm(42)).toBeNull();
  });
});

describe('Term component', () => {
  // The React mock returns [initial, vi.fn()] from useState, so when we
  // call Term as a function we get the *initial* render — open=false.
  // That's exactly what we want for shape tests; for the toggle behaviour
  // we exercise the onClick handler directly.

  it('renders children inside a span with the term class and aria wiring', () => {
    const tree = Term({ name: 'kiken', children: 'Kiken' });
    expect(tree.type).toBe('span');
    expect(tree.props.className).toContain('tw-term');
    expect(tree.props.tabIndex).toBe(0);
    expect(tree.props['aria-describedby']).toBeTruthy();
    expect(tree.props['aria-describedby']).toMatch(/tw-tip-/);

    // children = [label, tooltipSpan]
    const childArray = tree.children.flat ? tree.children.flat() : tree.children;
    expect(childArray[0]).toBe('Kiken');

    // Find the tooltip span among the children.
    const findTooltip = (arr) => arr.find((c) => c && c.props && c.props.role === 'tooltip');
    const tip = findTooltip(childArray);
    expect(tip).toBeTruthy();
    expect(tip.props.id).toBe(tree.props['aria-describedby']);
    // Initial state: tooltip is rendered but visibility is CSS-driven —
    // class is 'tw-tooltip' (not 'tw-tooltip--open') so screen readers
    // can associate via aria-describedby even when not visible.
    expect(tip.props.className).toBe('tw-tooltip');
  });

  it('falls through to plain children when the term name is unknown', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {});
    try {
      const tree = Term({ name: 'not-a-real-term', children: 'Foo' });
      // The mock React.createElement returns { type, props, children };
      // an unknown term falls through to React.Fragment which is
      // undefined-typed in the mock, but the call still produces a
      // truthy node carrying the children.
      expect(tree).toBeTruthy();
      expect(tree.children.includes('Foo') || tree.children[0] === 'Foo').toBe(true);
      expect(warn).toHaveBeenCalledWith('<Term name="not-a-real-term">: unknown glossary term');
    } finally {
      warn.mockRestore();
    }
  });

  it('attaches click/blur/keydown handlers for tap-to-toggle + Escape dismiss', () => {
    const tree = Term({ name: 'kiken', children: 'Kiken' });
    expect(typeof tree.props.onClick).toBe('function');
    expect(typeof tree.props.onBlur).toBe('function');
    expect(typeof tree.props.onKeyDown).toBe('function');
  });

  it('onClick stops propagation (keeps a wrapping clickable row inert)', () => {
    const tree = Term({ name: 'kiken', children: 'Kiken' });
    const stopProp = vi.fn();
    tree.props.onClick({ stopPropagation: stopProp });
    expect(stopProp).toHaveBeenCalled();
  });

  it('onKeyDown toggles on Enter and Space, with preventDefault', () => {
    const tree = Term({ name: 'kiken', children: 'Kiken' });
    const prevent1 = vi.fn();
    const prevent2 = vi.fn();
    const prevent3 = vi.fn();
    tree.props.onKeyDown({ key: 'Enter', preventDefault: prevent1 });
    tree.props.onKeyDown({ key: ' ', preventDefault: prevent2 });
    tree.props.onKeyDown({ key: 'a', preventDefault: prevent3 });
    expect(prevent1).toHaveBeenCalled();
    expect(prevent2).toHaveBeenCalled();
    expect(prevent3).not.toHaveBeenCalled();
  });

  it('renders the short label and tooltip text inside the popover', () => {
    const tree = Term({ name: 'kiken', children: 'Kiken' });
    const childArray = (tree.children.flat ? tree.children.flat() : tree.children).filter(Boolean);
    const tip = childArray.find((c) => c && c.props && c.props.role === 'tooltip');
    expect(tip).toBeTruthy();
    // Tooltip body contains the GLOSSARY.kiken.tooltip and the Short label.
    // The body is a list of child elements; collect their text descendants.
    const collectText = (node) => {
      if (typeof node === 'string') return node;
      if (Array.isArray(node)) return node.map(collectText).join('');
      if (node && node.children) return collectText(node.children.flat ? node.children.flat() : node.children);
      return '';
    };
    const text = collectText(tip);
    expect(text).toContain(GLOSSARY.kiken.short);
    expect(text).toContain(GLOSSARY.kiken.tooltip);
  });
});

describe('renderTooltipBody (cross-reference splitting)', () => {
  // The function is the heart of the nested-popover support: every
  // SeeAlso ID inside the tooltip text must become a <Term> element
  // so its own popover is reachable.

  it('returns the raw text when no seeAlso list is given', () => {
    expect(renderTooltipBody('Plain text only', [])).toBe('Plain text only');
    expect(renderTooltipBody('Plain text only', undefined)).toBe('Plain text only');
  });

  it('splits a tooltip around a single cross-reference', () => {
    // encho's tooltip mentions "ippon-shobu" — verify the helper
    // produces three tokens: prefix string, nested Term, suffix string.
    const text = 'Scoring in Encho follows ippon-shobu rules.';
    const tokens = renderTooltipBody(text, ['ippon-shobu']);
    expect(Array.isArray(tokens)).toBe(true);
    // Find the nested Term in the token array
    const termTokens = tokens.filter((t) => t && t.type === Term);
    expect(termTokens).toHaveLength(1);
    expect(termTokens[0].props.name).toBe('ippon-shobu');
    expect(termTokens[0].props.nested).toBe(true);
    // Original casing of the in-text label is preserved (here it was
    // lowercase, so "ippon-shobu"). The mock React.createElement
    // collapses the children spread into an array; pull the first.
    const childrenArr = termTokens[0].children || [].concat(termTokens[0].props.children || []);
    expect(childrenArr[0] || childrenArr).toBe('ippon-shobu');
  });

  it('handles multiple SeeAlso references in one tooltip', () => {
    // ippon-shobu's tooltip mentions both encho and daihyosen.
    const text = 'Used in overtime (encho) and tiebreaker (daihyosen) bouts.';
    const tokens = renderTooltipBody(text, ['encho', 'daihyosen']);
    const termTokens = tokens.filter((t) => t && t.type === Term);
    expect(termTokens).toHaveLength(2);
    const names = termTokens.map((t) => t.props.name).sort();
    expect(names).toEqual(['daihyosen', 'encho']);
  });

  it('longest match wins (compound term over its prefix)', () => {
    // When both "ippon" and "ippon-shobu" are in seeAlso AND the text
    // contains "ippon-shobu", we should get ONE nested Term for the
    // compound — not "ippon" + "-shobu" + "ippon".
    const text = 'See ippon-shobu for the rule.';
    const tokens = renderTooltipBody(text, ['ippon', 'ippon-shobu']);
    const termTokens = tokens.filter((t) => t && t.type === Term);
    expect(termTokens).toHaveLength(1);
    expect(termTokens[0].props.name).toBe('ippon-shobu');
  });

  it('case-insensitive matching preserves original casing', () => {
    // Display labels in some tooltips may be capitalised. The wrap
    // should still trigger, but the visible text remains the original.
    const text = 'See Ippon-Shobu for details.';
    const tokens = renderTooltipBody(text, ['ippon-shobu']);
    const termTokens = tokens.filter((t) => t && t.type === Term);
    expect(termTokens).toHaveLength(1);
    const childrenArr = termTokens[0].children || [].concat(termTokens[0].props.children || []);
    expect(childrenArr[0] || childrenArr).toBe('Ippon-Shobu');
  });

  it('word-boundary match prevents false positives inside larger words', () => {
    // None of our IDs collide with English words today, but pin the
    // principle so a future addition (e.g. "do" — already excluded but
    // illustrative) doesn't break tooltips that legitimately use the
    // word in English context.
    const text = 'The dancer did not perform.'; // contains "dan" as a substring
    const tokens = renderTooltipBody(text, ['dan']);
    const termTokens = (Array.isArray(tokens) ? tokens : []).filter((t) => t && t.type === Term);
    expect(termTokens).toHaveLength(0);
  });
});
