// A confirm or prompt (DialogHost) must open ABOVE whatever asked for it. The
// at-court lineup panel is a fixed layer at z-index 1000 and the viewer's match
// modal is another, but the dialog sat at the .modal-backdrop default of 100, so
// "Use the previous match's lineup" asked its question UNDER the panel, out of
// reach. The stack, bottom to top: the page and its overlays (1000 at most), the
// dialogs (.modal-backdrop--dialog), the toast, which takes no pointer events and
// so never blocks a dialog it sits over.

import React from 'react';
import { render, act, cleanup } from '@testing-library/react';
import { describe, it, expect, afterEach } from 'vitest';
import { DialogHost, confirmDialog } from '../../ui.jsx';
import { readStylesheet, cssBlock, modules, readCode } from '../helpers/source.js';

afterEach(cleanup);

const css = readStylesheet();
const declared = (text) => [...text.matchAll(/z-index:\s*(\d+)/g)].map((m) => Number(m[1]));
const zOf = (selector) => {
  const block = cssBlock(css, selector);
  expect(block, `rule ${selector} exists`).not.toBeNull();
  return Number(/z-index:\s*(\d+)/.exec(block)[1]);
};

describe('DialogHost', () => {
  it('puts its backdrop on the dialog layer, beside the modal backdrop everything keys on', async () => {
    const { container } = render(<DialogHost />);
    act(() => { confirmDialog({ message: 'Sure?' }); });
    const backdrop = container.querySelector('.modal-backdrop');
    expect(backdrop).not.toBeNull();
    expect(backdrop.classList.contains('modal-backdrop--dialog')).toBe(true);
  });
});

describe('the stacking order of the overlays', () => {
  const dialog = zOf('.modal-backdrop--dialog');
  const toast = zOf('.toast');

  // Every layer that is neither the dialog nor the toast: the stylesheet's own
  // z-indexes, and the ones a module sets inline on a fixed overlay (the lineup
  // panel, the viewer's match modal). A new overlay above the dialog turns this red.
  const otherLayers = () => {
    const rest = css.replace(cssBlock(css, '.modal-backdrop--dialog'), '').replace(cssBlock(css, '.toast'), '');
    const inline = modules().flatMap((file) => [...readCode(file).matchAll(/zIndex:\s*(\d+)/g)].map((m) => Number(m[1])));
    return [...declared(rest), ...inline];
  };

  it('has the dialog above every other overlay, the lineup panel (1000) included', () => {
    const others = otherLayers();
    expect(others).toContain(1000);
    expect(dialog).toBeGreaterThan(Math.max(...others));
  });

  it('has the toast above the dialog, so a toast raised while a dialog is open is not dimmed by its backdrop', () => {
    expect(toast).toBeGreaterThan(dialog);
  });

  it('declares the dialog rule after .modal-backdrop, which has the same specificity', () => {
    expect(css.indexOf('\n.modal-backdrop--dialog {')).toBeGreaterThan(css.indexOf('\n.modal-backdrop {'));
  });

  it('still lets a toast be click-through, so it never blocks the dialog under it', () => {
    expect(cssBlock(css, '.toast')).toMatch(/pointer-events:\s*none/);
  });
});
