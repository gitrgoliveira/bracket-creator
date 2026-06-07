// glossary.jsx — the <Term> tooltip component and the /glossary page.
//
// Slice U1 (003-tournament-gap-closure). Source of truth lives in
// internal/domain/glossary.go; this module reads it via the generated
// web-mobile/js/glossary.js (`make go/build` regenerates).
//
// Display rule (locked, per glossary.md §Display rule):
//   <Term name="kiken">Kiken</Term>
// renders ONLY the children — the gloss "Withdrawal" never appears
// inline. The hover/tap popover is the sole gloss surface. Volunteers
// learn the term via the tooltip; /glossary is the deep-context page.

import { GLOSSARY, lookupTerm } from './glossary_data.js';

const { useState: useStateT, useEffect: useEffectT, useId: useIdT, useRef: useRefT } = React;

// Stable per-component ID for aria-describedby. useId is React 18+;
// preact/compat aliases it to a deterministic-per-mount counter so the
// ARIA wiring stays valid across re-renders without depending on the
// child text. Falls back to a Math.random suffix for the very old
// preact/compat build that pre-dates useId (defensive — current
// vendored build has it).
function useStableId(prefix) {
  if (typeof useIdT === 'function') {
    const id = useIdT();
    return `${prefix}-${id}`;
  }
  // Fallback path: deterministic per first render only.
  const ref = useRefT(null);
  if (!ref.current) {
    ref.current = `${prefix}-${Math.random().toString(36).slice(2, 9)}`;
  }
  return ref.current;
}

// Parse a tooltip string into [text|term-ref] tokens, then render each
// term reference as a nested <Term>. We use the term's SeeAlso list to
// decide which substrings to wrap: a cross-reference declared in
// SeeAlso renders as a nested popover-bearing span; everything else
// stays plain text.
//
// Match strategy: case-insensitive word-boundary match for each
// SeeAlso ID inside the tooltip. Longer IDs (e.g. "ippon-shobu") win
// over their prefixes (e.g. "ippon") so a tooltip that legitimately
// mentions both still renders the compound first. The matching keeps
// the original casing from the tooltip for display ("Ippon-shobu"
// stays "Ippon-shobu") — we never lower-case the visible text.
function renderTooltipBody(tooltipText, seeAlso) {
  if (!seeAlso || seeAlso.length === 0) return tooltipText;
  // Sort by descending length so compound terms win when an ID is a
  // prefix of another. The output is a sequence of [string | { id, label }]
  // tokens which we then map to React elements.
  const ids = [...seeAlso].sort((a, b) => b.length - a.length);
  let tokens = [tooltipText];
  ids.forEach((rawId) => {
    const id = rawId.toLowerCase();
    const re = new RegExp(`\\b(${escapeRegex(id)})\\b`, 'gi');
    const next = [];
    tokens.forEach((tok) => {
      if (typeof tok !== 'string') { next.push(tok); return; }
      let lastIdx = 0;
      let match;
      while ((match = re.exec(tok)) !== null) {
        if (match.index > lastIdx) next.push(tok.slice(lastIdx, match.index));
        next.push({ id, label: match[1] }); // preserve original casing
        lastIdx = re.lastIndex;
      }
      if (lastIdx < tok.length) next.push(tok.slice(lastIdx));
    });
    tokens = next;
  });
  return tokens.map((tok, i) => {
    if (typeof tok === 'string') return tok;
    // Nested <Term> — recursive render so the inner popover works.
    return React.createElement(Term, { key: i, name: tok.id, nested: true }, tok.label);
  });
}

function escapeRegex(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// Term — the actual component. Renders children inside a
// <span tabindex=0 aria-describedby={tooltipId} class="tw-term"> plus
// a hidden tooltip span. Visibility is CSS-driven: the popover is
// always in the DOM for screen-reader access (`role="tooltip"`), CSS
// shows it on hover/focus.
//
// Tap-on-tablet: toggle `open` state on click and on touchstart so the
// popover stays visible without a hovering pointer. Dismiss via
// Escape, blur (focusout), or a tap outside the component.
//
// Nested instances (rendered from another tooltip's body) skip the
// outer focus-ring class so they don't double-decorate. They also
// render with an EMPTY seeAlso list so the recursive tooltip rendering
// can't loop: encho → ippon-shobu → encho → … would otherwise blow
// the stack. The nested term still has its own popover (with the
// tooltip text) — it just doesn't try to further deepen.
function Term({ name, children, nested }) {
  const term = lookupTerm(name);
  // Defensive: if a wrap site references a missing term, render the
  // children as plain text — better than crashing. Log so the
  // operator-time debug ladder catches the drift.
  if (!term) {
    if (typeof console !== 'undefined') {
      console.warn(`<Term name="${name}">: unknown glossary term`);
    }
    return React.createElement(React.Fragment, null, children);
  }

  const tooltipId = useStableId('tw-tip');
  const [open, setOpen] = useStateT(false);
  const wrapperRef = useRefT(null);

  // Tap-outside / Escape dismiss. Mount once per Term lifecycle.
  useEffectT(() => {
    if (!open) return undefined;
    const onKey = (e) => { if (e.key === 'Escape') setOpen(false); };
    const onDocPointer = (e) => {
      const wrap = wrapperRef.current;
      if (wrap && !wrap.contains(e.target)) setOpen(false);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDocPointer);
    document.addEventListener('touchstart', onDocPointer);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDocPointer);
      document.removeEventListener('touchstart', onDocPointer);
    };
  }, [open]);

  // Cap recursion at one level: a nested Term (rendered from inside
  // another tooltip's body) flattens to plain text in its own tooltip
  // so we don't loop on cross-cross-references (encho → ippon-shobu →
  // encho → ...). The nested popover still surfaces the gloss; the
  // /glossary page is the deeper-context fallback.
  const tooltipBody = nested ? term.tooltip : renderTooltipBody(term.tooltip, term.seeAlso);

  const handleClick = (e) => {
    // Don't propagate the click outside — keeps a Term inside a
    // clickable card (e.g. a match row) from triggering the row click.
    e.stopPropagation();
    setOpen((v) => !v);
  };

  const handleBlur = (e) => {
    // Close on focus loss UNLESS focus moved inside the popover (e.g.
    // user is tabbing into a nested term). relatedTarget is the new
    // focus destination.
    const wrap = wrapperRef.current;
    if (wrap && wrap.contains(e.relatedTarget)) return;
    setOpen(false);
  };

  const wrapClass = nested ? 'tw-term tw-term--nested' : 'tw-term';
  const wrapClassWithOpen = open ? `${wrapClass} tw-term--open` : wrapClass;

  return React.createElement(
    'span',
    {
      ref: wrapperRef,
      tabIndex: 0,
      className: wrapClassWithOpen,
      'data-testid': 'term-wrapper',
      'aria-describedby': tooltipId,
      onClick: handleClick,
      onBlur: handleBlur,
      onKeyDown: (e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setOpen((v) => !v); } },
    },
    children,
    React.createElement(
      'span',
      {
        id: tooltipId,
        role: 'tooltip',
        className: open ? 'tw-tooltip tw-tooltip--open' : 'tw-tooltip',
      },
      term.kanji ? React.createElement('span', { className: 'tw-tooltip__kanji' }, term.kanji + ' · ') : null,
      React.createElement('span', { className: 'tw-tooltip__short' }, term.short),
      React.createElement('br', null),
      React.createElement('span', { className: 'tw-tooltip__body' }, tooltipBody),
    ),
  );
}

// GlossaryPage — the /glossary viewer surface. Lists terms in a flat
// alphabetical layout so volunteers can browse the full register. Wired
// into the router via app.jsx's parsePath ("/glossary" → viewer mode
// with viewerScreen='glossary').
function GlossaryPage({ onBack }) {
  // Derive alphabetical list from the generated GLOSSARY map so new
  // entries added in glossary.go appear automatically (mp-mjaq).
  const ids = Object.keys(GLOSSARY).sort();

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          {onBack && (
            <button className="btn btn--ghost btn--sm" onClick={onBack} style={{ marginRight: 12 }}>
              ← Back
            </button>
          )}
          <div className="viewer__title-block">
            <div className="viewer__title">Kendo glossary</div>
            <div className="viewer__eyebrow">Reference for scoring-table volunteers</div>
          </div>
        </div>

        <div className="viewer__body">
          <div className="glossary-list">
            {ids.map((id) => {
              const entry = lookupTerm(id);
              if (!entry) return null;
              return (
                <article key={id} className="glossary-entry" id={`g-${id}`}>
                  <header className="glossary-entry__head">
                    <span className="glossary-entry__romaji">{entry.id.split('-').map(capitalise).join('-')}</span>
                    {entry.kanji && <span className="glossary-entry__kanji"><span aria-hidden="true"> · </span>{entry.kanji}</span>}
                    <span className="glossary-entry__short"> — {entry.short}</span>
                  </header>
                  <p className="glossary-entry__tooltip">
                    {renderTooltipBody(entry.tooltip, entry.seeAlso)}
                  </p>
                  {entry.seeAlso && entry.seeAlso.length > 0 && (
                    <div className="glossary-entry__see">
                      See also:{' '}
                      {entry.seeAlso.map((refId, i) => (
                        <React.Fragment key={refId}>
                          {i > 0 ? ', ' : ''}
                          <a
                            href={`#g-${refId}`}
                            onClick={(e) => {
                              e.preventDefault();
                              const el = document.getElementById(`g-${refId}`);
                              if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
                            }}
                          >
                            {capitalise(refId)}
                          </a>
                        </React.Fragment>
                      ))}
                    </div>
                  )}
                </article>
              );
            })}
          </div>
          {window.VersionFooter && <window.VersionFooter />}
        </div>
      </div>
    </div>
  );
}

function capitalise(s) {
  if (!s) return '';
  return s.charAt(0).toUpperCase() + s.slice(1);
}

// GlossaryHint — a standalone ？ icon that carries the glossary tooltip
// for a given term. Renders as a sibling next to a button so the tooltip
// is accessible without wrapping (and potentially blocking) the button's
// click target.
function GlossaryHint({ name }) {
  return React.createElement(
    'span',
    { className: 'glossary-hint' },
    React.createElement(Term, { name }, '？'),
  );
}

if (typeof window !== 'undefined') {
  window.Term = Term;
  window.GlossaryHint = GlossaryHint;
  window.GlossaryPage = GlossaryPage;
  window.renderTooltipBody = renderTooltipBody;
}

export { Term, GlossaryHint, GlossaryPage, renderTooltipBody, GLOSSARY, lookupTerm };
