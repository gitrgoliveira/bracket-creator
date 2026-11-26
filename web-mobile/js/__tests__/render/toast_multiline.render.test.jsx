// F7: a toast that holds several lines is a readable card, and an error carries
// a danger accent so a refusal never reads like a confirmation. It is CSS on the
// one Toast component: the render pins the classes the rules hang on, and the
// stylesheet is pinned for the rules themselves (jsdom draws no layout, so the
// 390 px look is judged from a screenshot).
import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { Toast } from '../../ui.jsx';
import { pushToast, resetToastQueue } from '../../toast_queue.jsx';

afterEach(() => { act(() => { resetToastQueue(); }); cleanup(); });

const css = readFileSync(resolve(__dirname, '../../../css/styles.css'), 'utf8');
// The declarations of every rule whose selector list holds `selector` exactly.
const declarations = (selector) => {
  const out = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m;
  while ((m = re.exec(css))) {
    const selectors = m[1].replace(/\/\*[\s\S]*?\*\//g, '').split(',').map((x) => x.trim());
    if (selectors.includes(selector)) out.push(m[2]);
  }
  return out.join('\n');
};

describe('a toast of several lines', () => {
  const LONG = 'A result was refused by the server and cannot be saved. This match result has already been reported. Contact the tournament organizer to correct it.';

  it('is one message in the one component, an error carrying its own class', () => {
    const { container } = render(<Toast />);
    act(() => { pushToast(LONG, 'error'); });
    const toast = container.querySelector('.toast');
    expect(toast.className).toContain('toast--error');
    expect(toast.getAttribute('role')).toBe('alert');
    expect(container.querySelector('.toast__msg').textContent).toBe(LONG);
    expect(container.querySelectorAll('.toast').length).toBe(1);
  });

  it('is a card that fits the phone: capped to the viewport less the gutters and to a readable width, left-aligned, top-aligned', () => {
    const toast = declarations('.toast');
    expect(toast).toMatch(/max-width:\s*min\(calc\(100vw - 32px\),\s*560px\)/);
    expect(toast).toMatch(/width:\s*max-content/);
    expect(toast).toMatch(/text-align:\s*left/);
    expect(toast).toMatch(/align-items:\s*flex-start/);
    expect(declarations('.toast__msg')).toMatch(/min-width:\s*0/);
  });

  it('an error toast has a danger ring, as a warning has its amber one', () => {
    expect(declarations('.toast--error')).toMatch(/0 0 0 2px var\(--danger\)/);
    expect(declarations('.toast--warning')).toMatch(/var\(--warn-strong\)/);
  });
});
