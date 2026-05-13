import { describe, it, expect, vi } from 'vitest';

// Pure logic extracted from the ScoreEditorModal keyboard handler for unit testing.

function isTextEntry(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || !!el.isContentEditable;
}

function isInteractiveTarget(el) {
  if (!el) return false;
  const tag = (el.tagName || "").toLowerCase();
  return tag === "input" || tag === "textarea" || tag === "select" || tag === "button" || tag === "a" || !!el.isContentEditable;
}

// Mirrors admin.jsx: lowercase → SHIRO (b), uppercase → AKA (a)
function sideForKey(k) {
  const upper = k.toUpperCase();
  const isUpper = k === upper && k !== upper.toLowerCase();
  return isUpper ? "a" : "b";
}

// Mirrors the scoring-key filter from admin.jsx
function isScoringKey(k) {
  const upper = k.toUpperCase();
  return "MKDTH".includes(upper) && k.length === 1;
}

describe('isTextEntry', () => {
  it('returns true for input, textarea, select', () => {
    expect(isTextEntry({ tagName: "INPUT" })).toBe(true);
    expect(isTextEntry({ tagName: "TEXTAREA" })).toBe(true);
    expect(isTextEntry({ tagName: "SELECT" })).toBe(true);
  });

  it('returns true for contentEditable elements', () => {
    expect(isTextEntry({ tagName: "DIV", isContentEditable: true })).toBe(true);
  });

  it('returns false for button, a, div', () => {
    expect(isTextEntry({ tagName: "BUTTON" })).toBe(false);
    expect(isTextEntry({ tagName: "A" })).toBe(false);
    expect(isTextEntry({ tagName: "DIV" })).toBe(false);
  });

  it('returns false for null', () => {
    expect(isTextEntry(null)).toBe(false);
  });
});

describe('isInteractiveTarget', () => {
  it('returns true for all form elements', () => {
    expect(isInteractiveTarget({ tagName: "INPUT" })).toBe(true);
    expect(isInteractiveTarget({ tagName: "TEXTAREA" })).toBe(true);
    expect(isInteractiveTarget({ tagName: "SELECT" })).toBe(true);
  });

  it('returns true for button and anchor', () => {
    expect(isInteractiveTarget({ tagName: "BUTTON" })).toBe(true);
    expect(isInteractiveTarget({ tagName: "A" })).toBe(true);
  });

  it('returns false for plain div', () => {
    expect(isInteractiveTarget({ tagName: "DIV" })).toBe(false);
  });

  it('returns false for null', () => {
    expect(isInteractiveTarget(null)).toBe(false);
  });
});

describe('key-to-side mapping', () => {
  it('lowercase keys map to SHIRO (b)', () => {
    expect(sideForKey("m")).toBe("b");
    expect(sideForKey("k")).toBe("b");
    expect(sideForKey("d")).toBe("b");
    expect(sideForKey("t")).toBe("b");
    expect(sideForKey("h")).toBe("b");
  });

  it('uppercase keys map to AKA (a)', () => {
    expect(sideForKey("M")).toBe("a");
    expect(sideForKey("K")).toBe("a");
    expect(sideForKey("D")).toBe("a");
    expect(sideForKey("T")).toBe("a");
    expect(sideForKey("H")).toBe("a");
  });
});

describe('scoring key filter', () => {
  it('accepts MKDTH (case-insensitive)', () => {
    for (const k of ["m", "k", "d", "t", "h", "M", "K", "D", "T", "H"]) {
      expect(isScoringKey(k)).toBe(true);
    }
  });

  it('rejects other single-character keys', () => {
    for (const k of ["a", "b", "c", "e", "x", "X", "z", " "]) {
      expect(isScoringKey(k)).toBe(false);
    }
  });

  it('rejects multi-character key names', () => {
    expect(isScoringKey("Enter")).toBe(false);
    expect(isScoringKey("Escape")).toBe(false);
    expect(isScoringKey("ArrowLeft")).toBe(false);
  });
});

describe('keyboard shortcut routing', () => {
  // Simulates the keyboard handler decision tree
  function handleKey(ev, state) {
    const { submitting, isDrawToggled, canFinish, hasPrev, hasNext } = state;
    if (submitting) return "noop/submitting";
    if (ev.ctrlKey || ev.metaKey || ev.altKey) return "noop/modifier";

    if (ev.key === "Escape") return "action/close";

    if (!isTextEntry(ev.target)) {
      if (ev.key === "ArrowLeft" && hasPrev) return "action/prev";
      if (ev.key === "ArrowRight" && hasNext) return "action/next";
    }

    if (isInteractiveTarget(ev.target)) return "noop/interactive";

    if (ev.key === "Enter" && canFinish) return "action/finish";

    const k = ev.key;
    const upper = k.toUpperCase();
    if ("MKDTH".includes(upper) && k.length === 1) {
      const isDrawClear = isDrawToggled;
      const side = sideForKey(k);
      return `action/pt/${side}${isDrawClear ? "/cleardraw" : ""}`;
    }
    if (k === "x" || k === "X") {
      return isDrawToggled ? "action/draw/off" : "action/draw/on";
    }
    return "noop/unhandled";
  }

  const base = { submitting: false, isDrawToggled: false, canFinish: true, hasPrev: true, hasNext: true };

  it('Escape closes from body', () => {
    expect(handleKey({ key: "Escape", target: { tagName: "DIV" } }, base)).toBe("action/close");
  });

  it('Escape closes even from button', () => {
    expect(handleKey({ key: "Escape", target: { tagName: "BUTTON" } }, base)).toBe("action/close");
  });

  it('Escape closes even from input', () => {
    expect(handleKey({ key: "Escape", target: { tagName: "INPUT" } }, base)).toBe("action/close");
  });

  it('ArrowLeft navigates when not in text-entry', () => {
    expect(handleKey({ key: "ArrowLeft", target: { tagName: "DIV" } }, base)).toBe("action/prev");
  });

  it('ArrowLeft blocked inside input (cursor movement)', () => {
    expect(handleKey({ key: "ArrowLeft", target: { tagName: "INPUT" } }, base)).toBe("noop/interactive");
  });

  it('ArrowLeft allowed from button (not text-entry)', () => {
    expect(handleKey({ key: "ArrowLeft", target: { tagName: "BUTTON" } }, base)).toBe("action/prev");
  });

  it('scoring key M blocked from button', () => {
    expect(handleKey({ key: "m", target: { tagName: "BUTTON" } }, base)).toBe("noop/interactive");
  });

  it('lowercase m adds point to SHIRO (b)', () => {
    expect(handleKey({ key: "m", target: { tagName: "DIV" } }, base)).toBe("action/pt/b");
  });

  it('uppercase M adds point to AKA (a)', () => {
    expect(handleKey({ key: "M", target: { tagName: "DIV" } }, base)).toBe("action/pt/a");
  });

  it('X toggles draw on when no draw', () => {
    expect(handleKey({ key: "x", target: { tagName: "DIV" } }, base)).toBe("action/draw/on");
  });

  it('X toggles draw off when draw is active', () => {
    expect(handleKey({ key: "x", target: { tagName: "DIV" } }, { ...base, isDrawToggled: true })).toBe("action/draw/off");
  });

  it('scoring key while draw active clears draw and adds point', () => {
    expect(handleKey({ key: "m", target: { tagName: "DIV" } }, { ...base, isDrawToggled: true })).toBe("action/pt/b/cleardraw");
  });

  it('Enter submits when canFinish', () => {
    expect(handleKey({ key: "Enter", target: { tagName: "DIV" } }, base)).toBe("action/finish");
  });

  it('Enter does nothing when canFinish=false', () => {
    expect(handleKey({ key: "Enter", target: { tagName: "DIV" } }, { ...base, canFinish: false })).toBe("noop/unhandled");
  });

  it('noop when submitting', () => {
    expect(handleKey({ key: "m", target: { tagName: "DIV" } }, { ...base, submitting: true })).toBe("noop/submitting");
  });
});
