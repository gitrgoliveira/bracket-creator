import { describe, it, expect } from 'vitest';
import { isTextEntry, isInteractiveTarget } from '../ui.jsx';

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
  // Simulates the keyboard handler decision tree using the real isTextEntry /
  // isInteractiveTarget implementations imported from ui.jsx.
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
      // ev.shiftKey determines side — matches admin.jsx production logic
      const side = ev.shiftKey ? "a" : "b";
      return `action/pt/${side}${isDrawClear ? "/cleardraw" : ""}`;
    }
    if (k === "x" || k === "X") {
      return isDrawToggled ? "action/draw/off" : "action/draw/on";
    }
    return "noop/unhandled";
  }

  const base = { submitting: false, isDrawToggled: false, canFinish: true, hasPrev: true, hasNext: true };
  const body = { tagName: "DIV" };
  const btn = { tagName: "BUTTON" };
  const inp = { tagName: "INPUT" };

  it('Escape closes from body', () => {
    expect(handleKey({ key: "Escape", target: body }, base)).toBe("action/close");
  });

  it('Escape closes even from button', () => {
    expect(handleKey({ key: "Escape", target: btn }, base)).toBe("action/close");
  });

  it('Escape closes even from input', () => {
    expect(handleKey({ key: "Escape", target: inp }, base)).toBe("action/close");
  });

  it('ArrowLeft navigates when not in text-entry', () => {
    expect(handleKey({ key: "ArrowLeft", target: body }, base)).toBe("action/prev");
  });

  it('ArrowLeft blocked inside input (cursor movement)', () => {
    expect(handleKey({ key: "ArrowLeft", target: inp }, base)).toBe("noop/interactive");
  });

  it('ArrowLeft allowed from button (not text-entry)', () => {
    expect(handleKey({ key: "ArrowLeft", target: btn }, base)).toBe("action/prev");
  });

  it('scoring key M blocked from button', () => {
    expect(handleKey({ key: "m", shiftKey: false, target: btn }, base)).toBe("noop/interactive");
  });

  it('lowercase m (no Shift) awards point to SHIRO (b)', () => {
    expect(handleKey({ key: "m", shiftKey: false, target: body }, base)).toBe("action/pt/b");
  });

  it('Shift+m awards point to AKA (a)', () => {
    expect(handleKey({ key: "M", shiftKey: true, target: body }, base)).toBe("action/pt/a");
  });

  it('X toggles draw on when no draw', () => {
    expect(handleKey({ key: "x", target: body }, base)).toBe("action/draw/on");
  });

  it('X toggles draw off when draw is active', () => {
    expect(handleKey({ key: "x", target: body }, { ...base, isDrawToggled: true })).toBe("action/draw/off");
  });

  it('scoring key while draw active clears draw and adds point', () => {
    expect(handleKey({ key: "m", shiftKey: false, target: body }, { ...base, isDrawToggled: true })).toBe("action/pt/b/cleardraw");
  });

  it('Enter submits when canFinish', () => {
    expect(handleKey({ key: "Enter", target: body }, base)).toBe("action/finish");
  });

  it('Enter does nothing when canFinish=false', () => {
    expect(handleKey({ key: "Enter", target: body }, { ...base, canFinish: false })).toBe("noop/unhandled");
  });

  it('noop when submitting', () => {
    expect(handleKey({ key: "m", shiftKey: false, target: body }, { ...base, submitting: true })).toBe("noop/submitting");
  });
});
