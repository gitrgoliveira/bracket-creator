import { describe, it, expect } from 'vitest';
import { LinkDot } from '../display_scoreboard.jsx';

// LinkDot: the 3-state connection indicator on the TV scoreboard (mp-9ukk
// Phase 2). JSX compiles to the non-reactive global React stub from
// vitest.setup.js, so calling LinkDot(props) returns a { type, props, children }
// vnode whose props we assert directly. No mount needed (the component is a
// single self-closing <span>).
//
// State contract:
//   connected -> invisible (visibility:hidden, transparent bg, aria-hidden) so
//                the header reserves the dot's space and does not reflow.
//   local     -> amber dot (var(--warn)), no ring, role=status.
//   stale     -> red dot (var(--danger)) WITH a dark ring (boxShadow), role=status.
// The two degraded states are distinguished by TREATMENT (the ring), not hue
// alone (DESIGN.md principle 2), so the meaning survives projector glare and
// colour-blindness.

function render(linkState) {
    return LinkDot({ linkState });
}

describe('LinkDot', () => {
    it('renders a span tagged with the link state via data-link-state', () => {
        for (const state of ['connected', 'local', 'stale']) {
            const v = render(state);
            expect(v.type).toBe('span');
            expect(v.props['data-link-state']).toBe(state);
            expect(v.props['data-testid']).toBe('display-link-dot');
        }
    });

    it('connected: invisible, transparent, aria-hidden, no status role', () => {
        const v = render('connected');
        expect(v.props.style.visibility).toBe('hidden');
        expect(v.props.style.background).toBe('transparent');
        expect(v.props['aria-hidden']).toBe('true');
        expect(v.props.role).toBeUndefined();
        expect(v.props['aria-label']).toBeUndefined();
        expect(v.props.style.boxShadow).toBe('none');
    });

    it('local: amber dot, visible, status role, no ring', () => {
        const v = render('local');
        expect(v.props.style.visibility).toBe('visible');
        expect(v.props.style.background).toContain('--warn');
        expect(v.props.style.boxShadow).toBe('none');
        expect(v.props.role).toBe('status');
        expect(v.props['aria-hidden']).toBeUndefined();
        expect(v.props['aria-label']).toBe('Operator broadcast (server offline)');
    });

    it('stale: red dot, visible, status role, dark ring (boxShadow)', () => {
        const v = render('stale');
        expect(v.props.style.visibility).toBe('visible');
        expect(v.props.style.background).toContain('--danger');
        expect(v.props.style.boxShadow).toContain('--ink-1');
        expect(v.props.role).toBe('status');
        expect(v.props['aria-label']).toBe('No data feed');
    });

    it('distinguishes local and stale by treatment (ring), not hue alone', () => {
        // The ring is the colour-independent differentiator required by DESIGN.md.
        expect(render('local').props.style.boxShadow).toBe('none');
        expect(render('stale').props.style.boxShadow).not.toBe('none');
    });
});
