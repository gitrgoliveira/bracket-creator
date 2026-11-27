// The shared Modal (ui.jsx) takes focus as it opens and gives it back as it closes,
// through dialog_focus.jsx (useDialogFocus), the one owner of that rule. Before this,
// focus stayed on the control behind an aria-modal dialog (seen on the dashboard's
// Start all dialog), so a keyboard or screen reader user was left outside it.
//
// Pinned here: focus goes to the dialog's Close button (a safe Enter) without
// scrolling the page, Escape gives it back to the control that opened the dialog,
// and a dialog whose text box takes focus itself (autoFocus, the registration
// desk's name box) keeps it there, so an iPad does not drop its keyboard. Tab and
// Shift+Tab stay inside the dialog (keepTabInside), so a keyboard user cannot reach
// the page behind its backdrop.
import React from 'react';
import { render, act, fireEvent } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';

const settle = () => act(async () => { await new Promise((resolve) => setTimeout(resolve, 5)); });

function Host({ withInput = false }) {
  const Modal = window.Modal;
  const [open, setOpen] = React.useState(false);
  return (
    <div>
      <button type="button" onClick={() => setOpen(true)}>Open dialog</button>
      {open && (
        <Modal
          title="Start all competitions"
          onClose={() => setOpen(false)}
          footer={<button type="button" className="btn" onClick={() => setOpen(false)}>Cancel</button>}
        >
          {withInput ? <input aria-label="Name" autoFocus /> : <p>Body</p>}
        </Modal>
      )}
    </div>
  );
}

async function openFrom(utils) {
  const opener = utils.getByRole('button', { name: 'Open dialog' });
  opener.focus();
  await act(async () => { opener.click(); });
  await settle();
  return opener;
}

describe('Modal focus', () => {
  it('goes to the Close button as it opens and back to the opener on Escape, without scrolling', async () => {
    const focus = vi.spyOn(HTMLElement.prototype, 'focus');
    try {
      let utils;
      await act(async () => { utils = render(<Host />); });
      const opener = await openFrom(utils);

      const dialog = utils.getByRole('dialog', { name: 'Start all competitions' });
      expect(document.activeElement).toBe(utils.getByRole('button', { name: 'Close' }));
      expect(dialog.contains(document.activeElement)).toBe(true);

      await act(async () => { fireEvent.keyDown(window, { key: 'Escape' }); });
      await settle();
      expect(utils.queryByRole('dialog')).toBeNull();
      expect(document.activeElement, 'back on the control that opened it').toBe(opener);

      const moves = focus.mock.calls.slice(1).map(([options]) => options);
      expect(moves, 'both moves leave the page where it is').toEqual([{ preventScroll: true }, { preventScroll: true }]);
    } finally {
      focus.mockRestore();
    }
  });

  it('gives focus back when a footer button closes it', async () => {
    let utils;
    await act(async () => { utils = render(<Host />); });
    const opener = await openFrom(utils);

    await act(async () => { fireEvent.click(utils.getByRole('button', { name: 'Cancel' }), { detail: 0 }); });
    await settle();
    expect(utils.queryByRole('dialog')).toBeNull();
    expect(document.activeElement).toBe(opener);
  });

  it('leaves focus in a text box that took it as the dialog opened', async () => {
    let utils;
    await act(async () => { utils = render(<Host withInput />); });
    await openFrom(utils);

    expect(document.activeElement).toBe(utils.getByRole('textbox', { name: 'Name' }));
  });

  // The app's Preact writes autoFocus as the autofocus attribute and focuses nothing,
  // and the browser skips the attribute while the opener holds focus, so the dialog
  // focuses the marked text box itself (the registration desk's Edit dialog).
  it('focuses a text box marked autofocus that nothing focused', async () => {
    function MarkedInput() {
      const ref = React.useRef(null);
      React.useLayoutEffect(() => { ref.current.setAttribute('autofocus', ''); }, []);
      return <input ref={ref} aria-label="Name" />;
    }
    function MarkedHost() {
      const Modal = window.Modal;
      const [open, setOpen] = React.useState(false);
      return (
        <div>
          <button type="button" onClick={() => setOpen(true)}>Open dialog</button>
          {open && <Modal title="Edit competitor" onClose={() => setOpen(false)}><MarkedInput /></Modal>}
        </div>
      );
    }
    let utils;
    await act(async () => { utils = render(<MarkedHost />); });
    await openFrom(utils);

    expect(document.activeElement).toBe(utils.getByRole('textbox', { name: 'Name' }));
  });

  // jsdom lays nothing out, so offsetParent, by which keepTabInside skips a hidden
  // control, is null for every element; this test stands in for a laid-out page.
  it('keeps Tab and Shift+Tab inside the dialog', async () => {
    const laidOut = vi.spyOn(HTMLElement.prototype, 'offsetParent', 'get').mockImplementation(function parent() { return this.parentElement; });
    try {
      await tabStaysInside();
    } finally {
      laidOut.mockRestore();
    }
  });

  async function tabStaysInside() {
    let utils;
    await act(async () => { utils = render(<Host />); });
    await openFrom(utils);
    const close = utils.getByRole('button', { name: 'Close' });
    const cancel = utils.getByRole('button', { name: 'Cancel' });
    expect(document.activeElement).toBe(close);

    const tab = (shiftKey) => fireEvent.keyDown(document.activeElement, { key: 'Tab', shiftKey });
    cancel.focus();
    expect(tab(false), 'the browser does not move on: the dialog wraps it').toBe(false);
    expect(document.activeElement, 'Tab from the last control wraps to the first').toBe(close);
    expect(tab(true)).toBe(false);
    expect(document.activeElement, 'Shift+Tab from the first wraps to the last').toBe(cancel);
    expect(tab(true), 'Shift+Tab from the last control is the browser\'s own').toBe(true);
  }

  // The Posters page's Print all question wraps its Modal in a box of its own that
  // also takes focus through useDialogFocus: the two layers hear the same Tab, and
  // focus moves once.
  it('moves focus once when a Modal sits inside another layer', async () => {
    const laidOut = vi.spyOn(HTMLElement.prototype, 'offsetParent', 'get').mockImplementation(function parent() { return this.parentElement; });
    try {
      const { useDialogFocus } = await import('../../dialog_focus.jsx');
      function Nested() {
        const Modal = window.Modal;
        const [open, setOpen] = React.useState(false);
        const boxRef = React.useRef(null);
        useDialogFocus(boxRef, open);
        return (
          <div>
            <button type="button" onClick={() => setOpen(true)}>Open dialog</button>
            {open && (
              <div ref={boxRef}>
                <Modal title="Print all posters?" onClose={() => setOpen(false)}
                  footer={<button type="button" className="btn" onClick={() => setOpen(false)}>Print all</button>}>
                  <button type="button" className="btn btn--sm">Set the address</button>
                </Modal>
              </div>
            )}
          </div>
        );
      }
      let utils;
      await act(async () => { utils = render(<Nested />); });
      await openFrom(utils);
      const close = utils.getByRole('button', { name: 'Close' });
      const middle = utils.getByRole('button', { name: 'Set the address' });
      const last = utils.getByRole('button', { name: 'Print all' });
      expect(document.activeElement).toBe(close);
      last.focus();
      fireEvent.keyDown(last, { key: 'Tab' });
      expect(document.activeElement).toBe(close);
      fireEvent.keyDown(close, { key: 'Tab', shiftKey: true });
      expect(document.activeElement).toBe(last);
      middle.focus();
      expect(fireEvent.keyDown(middle, { key: 'Tab' }), 'between the ends the browser moves it').toBe(true);
    } finally {
      laidOut.mockRestore();
    }
  });

  it('leaves Tab alone while focus is outside the dialog (a layer stacked above it)', async () => {
    let utils;
    await act(async () => { utils = render(<Host />); });
    const opener = await openFrom(utils);
    opener.focus();
    expect(fireEvent.keyDown(opener, { key: 'Tab' })).toBe(true);
    expect(document.activeElement).toBe(opener);
  });
});
