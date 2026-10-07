// dialog_focus.jsx: the ONE owner of focus going into a layer that opens over the
// page and back to the control that opened it (WCAG 2.4.3, the ARIA dialog pattern).
// The court console's confirms and the at-court lineup panel take it from here;
// DialogHost (ui.jsx), which also traps Tab and locks the page's scroll, keeps its
// own.
//
// A leaf with no imports: every consumer ES-imports it directly, and there is no
// window mirror (the tap_guard.jsx pattern). The hook reads the React global at CALL
// time, so importing this module needs no React.

// useDialogFocus: while `open`, focus goes into the layer (its first button, whose
// Enter is a safe cancel or close), and when the layer closes or goes away it goes
// back to the control that had it as the layer opened. `boxRef` is the layer's node.
//   const boxRef = React.useRef(null);
//   useDialogFocus(boxRef, open);
//   <div ref={boxRef} role="dialog" aria-modal="true">
// Neither move scrolls the page (preventScroll): the layer opens over the control that
// was pressed, which is in view. The move in runs on a 0ms timer, as DialogHost's: a
// focus moved during the commit is reset. Focus is not given back to the page itself
// (a tap on a touchscreen focuses nothing) or to a control that has left the page.
export function useDialogFocus(boxRef, open = true) {
  React.useEffect(() => {
    if (!open) return undefined;
    const opener = document.activeElement;
    const timer = setTimeout(() => {
      const first = boxRef.current && boxRef.current.querySelector("button");
      if (first) first.focus({ preventScroll: true });
    }, 0);
    return () => {
      clearTimeout(timer);
      if (opener && opener !== document.body && document.contains(opener)) opener.focus({ preventScroll: true });
    };
  }, [boxRef, open]);
}
