// bc-tmfd: the lineup name list must not open under the team sheet's pinned
// header (top) or footer dock (bottom). On open it measures the room between
// the input and the pinned edges: down when that fits the list, else toward
// the larger side, with its height capped to that room. A host with no pinned
// bar uses the viewport edges. getBoundingClientRect is stubbed (jsdom lays
// nothing out); the browser check is the acceptance.

import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, beforeAll, beforeEach, afterEach, vi } from 'vitest';
import { LineupNameInput } from '../../admin_scoring_shared.jsx';

const VIEW_H = 820;
const realRect = Element.prototype.getBoundingClientRect;
let rects;
let savedInnerHeight;

const rect = (top, bottom) => ({ top, bottom, height: bottom - top, left: 0, right: 0, width: 0, x: 0, y: top });

beforeAll(() => {
  if (typeof window.useClickOutside !== 'function') window.useClickOutside = () => {};
});

beforeEach(() => {
  rects = {};
  savedInnerHeight = window.innerHeight;
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: VIEW_H });
  Element.prototype.getBoundingClientRect = function () {
    if (this.classList) {
      for (const cls of Object.keys(rects)) if (this.classList.contains(cls)) return rects[cls];
    }
    return realRect.call(this);
  };
});

afterEach(() => {
  Element.prototype.getBoundingClientRect = realRect;
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: savedInnerHeight });
});

const ROSTER = Array.from({ length: 8 }, (_, i) => `Member ${i + 1}`);

function openList({ pinned = true, foot = true } = {}) {
  const utils = render(
    <div className="scoring-panel scoring-panel--team">
      {pinned && <div className="team-sheet-pin" />}
      <LineupNameInput value="" roster={ROSTER} onSelect={() => {}} ariaLabel="pos" color="shiro" />
      {foot && <div className="editor-modal__foot--nav" />}
    </div>
  );
  act(() => { fireEvent.focus(utils.container.querySelector('input')); });
  return utils.container.querySelector('.lineup-name__dropdown');
}

const setVisualViewport = (vv) => Object.defineProperty(window, 'visualViewport', { configurable: true, value: vv });

describe('LineupNameInput list placement against the pinned bars', () => {
  it('opens downward, uncapped, when the room below the input fits the list', () => {
    rects['team-sheet-pin'] = rect(65, 200);
    rects['pmf__bar'] = rect(300, 340);
    rects['editor-modal__foot--nav'] = rect(740, 820);
    const dd = openList();
    expect(dd.style.bottom).toBe('');
    expect(dd.style.top).toBe('');
    expect(dd.style.maxHeight).toBe('240px');
  });

  it('opens upward when the dock is close and there is more room above', () => {
    rects['team-sheet-pin'] = rect(65, 200);
    rects['pmf__bar'] = rect(600, 640);
    rects['editor-modal__foot--nav'] = rect(700, 820);
    const dd = openList();
    expect(dd.style.top).toBe('auto');
    expect(dd.style.bottom).toBe('calc(100% + 4px)');
    expect(dd.style.maxHeight).toBe('240px');
  });

  it('caps the height to the larger room when neither side fits the list', () => {
    rects['team-sheet-pin'] = rect(65, 260);
    rects['pmf__bar'] = rect(400, 440);
    rects['editor-modal__foot--nav'] = rect(620, 820);
    const dd = openList();
    // Room below is 180, above 140: down, capped to 180 less the 8px margin.
    expect(dd.style.top).toBe('');
    expect(dd.style.maxHeight).toBe('172px');
  });

  it('measures against the pin when opening upward, so the list stops under the header', () => {
    rects['team-sheet-pin'] = rect(65, 330);
    rects['pmf__bar'] = rect(500, 540);
    rects['editor-modal__foot--nav'] = rect(540, 820);
    const dd = openList();
    // Below: 0. Above: 500 - 330 = 170, less the margin.
    expect(dd.style.bottom).toBe('calc(100% + 4px)');
    expect(dd.style.maxHeight).toBe('162px');
  });

  it('uses the footer as the bottom edge in the overlay too, where it is static but always visible', () => {
    rects['team-sheet-pin'] = rect(65, 200);
    rects['pmf__bar'] = rect(500, 540);
    rects['editor-modal__foot--nav'] = rect(700, 820);
    const dd = openList();
    // 700 - 540 = 160 below does not fit 240 and 300 above is larger: up.
    expect(dd.style.top).toBe('auto');
    expect(dd.style.bottom).toBe('calc(100% + 4px)');
  });

  it('clamps the footer edge to the viewport', () => {
    rects['pmf__bar'] = rect(100, 140);
    rects['editor-modal__foot--nav'] = rect(900, 980);
    const dd = openList({ pinned: false });
    // Edge 820 (the viewport), not 900: 680 below, uncapped.
    expect(dd.style.maxHeight).toBe('240px');
    expect(dd.style.top).toBe('');
  });

  it('measures against the visual viewport when the keyboard shrinks it below innerHeight', () => {
    setVisualViewport({ offsetTop: 0, height: 560, addEventListener() {}, removeEventListener() {} });
    try {
      rects['pmf__bar'] = rect(400, 440);
      const dd = openList({ pinned: false, foot: false });
      // 560 - 440 = 120 below, 400 above: up, uncapped. innerHeight (820) would say 380 below.
      expect(dd.style.top).toBe('auto');
      expect(dd.style.maxHeight).toBe('240px');
    } finally {
      setVisualViewport(undefined);
    }
  });

  it('measures again when the page scrolls while the list is open, and stops after it closes', () => {
    const raf = vi.spyOn(window, 'requestAnimationFrame').mockImplementation((cb) => { cb(0); return 1; });
    try {
      rects['team-sheet-pin'] = rect(65, 200);
      rects['pmf__bar'] = rect(300, 340);
      rects['editor-modal__foot--nav'] = rect(740, 820);
      const dd = openList();
      expect(dd.style.top).toBe('');
      // The page scrolled: the input is now close to the dock.
      rects['pmf__bar'] = rect(650, 690);
      act(() => { window.dispatchEvent(new Event('scroll')); });
      expect(dd.style.top).toBe('auto');
      expect(dd.style.bottom).toBe('calc(100% + 4px)');
    } finally {
      raf.mockRestore();
    }
  });

  it('removes its listeners when the list closes', () => {
    const add = vi.spyOn(window, 'addEventListener');
    const remove = vi.spyOn(window, 'removeEventListener');
    try {
      rects['pmf__bar'] = rect(300, 340);
      const { container } = render(<LineupNameInput value="" roster={ROSTER} onSelect={() => {}} ariaLabel="pos" color="shiro" />);
      act(() => { fireEvent.focus(container.querySelector('input')); });
      const added = add.mock.calls.filter(([t]) => t === 'scroll' || t === 'resize').length;
      expect(added).toBe(2);
      act(() => { fireEvent.keyDown(container.querySelector('input'), { key: 'Escape' }); });
      const removed = remove.mock.calls.filter(([t]) => t === 'scroll' || t === 'resize').length;
      expect(removed).toBe(2);
    } finally {
      add.mockRestore();
      remove.mockRestore();
    }
  });

  it('with no pinned bar uses the viewport edges, as the lineup panel and Lineups page do', () => {
    rects['pmf__bar'] = rect(700, 740);
    const dd = openList({ pinned: false, foot: false });
    // Below: 80, above: 700, so it opens upward uncapped.
    expect(dd.style.top).toBe('auto');
    expect(dd.style.bottom).toBe('calc(100% + 4px)');
    expect(dd.style.maxHeight).toBe('240px');
  });
});
