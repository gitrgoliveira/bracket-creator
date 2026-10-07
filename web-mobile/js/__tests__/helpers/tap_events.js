// tap_events.js: the two kinds of click a tap_guard.jsx test needs.
//
// fireEvent.click defaults to detail 0, which is what a click synthesized from
// the keyboard carries and what tap_guard.jsx never treats as a bounce. A
// pointer tap therefore has to pass detail: 1, or a bounce test can never go
// red. Both go through act so the component has rendered before the next one.
import { act, fireEvent } from '@testing-library/react';

export const pointerTap = (el) => act(async () => { fireEvent.click(el, { detail: 1 }); });
export const keyboardClick = (el) => act(async () => { fireEvent.click(el, { detail: 0 }); });
