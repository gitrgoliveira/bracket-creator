# DESIGN.md

Design system reference for the bracket-creator web frontends.

Use this doc when adding UI, naming a new class, picking a color, or reviewing a screen for consistency. It is the source of truth for the visual language; the CSS file is the source of truth for exact values.

## 1. Surfaces

The repo ships two frontends, both embedded into the Go binary at build time:

| Surface | Path | Purpose | Stack |
|---|---|---|---|
| **Mobile/live app** | [web-mobile/](web-mobile/) | Live tournament admin + spectator/player viewer for the `mobile-app` command. **Primary surface.** | Preact (no JSX runtime — `React.createElement` compiled by esbuild via `make go/build`), single `styles.css` (~5,000 lines) |
| **Bracket generator** | [web/](web/) | One-shot Excel-bracket generator served by the `serve` command. | Bootstrap 5.3 + plain JS, ~350 lines of overrides |

When extending the design system, **mobile-app is the canonical surface**. The bracket generator is a form; keep it functional and visually simple, don't import mobile-app patterns.

## 2. Principles

1. **Clarity over decoration.** Operators run tournaments under time pressure; a glanceable card beats a beautiful one. No gratuitous animation, no decorative shadows.
2. **Kendo first.** Red (Aka) is always the left/upper position, White (Shiro) is always right/lower. This is fixed in the rules (see [CLAUDE.md](CLAUDE.md) — "Match Colors") and must hold across every match-rendering component.
3. **Live state is loud.** Anything currently happening on a court gets the red treatment (`--red` border, soft red ring, pulsing dot). Anything else stays neutral. Don't dilute the signal.
4. **Touch targets ≥ 36px.** Operators score on tablets; players check brackets on phones. Buttons and tap zones must clear that threshold even in dense bracket views.
5. **Status drives color, color doesn't drive meaning.** The pipeline `setup → pools → playoffs → completed` has its own palette; reuse the existing `.badge--*` rather than inventing local hues.
6. **Domain coupling is allowed.** Class names like `.bc-tree`, `.pool__table`, `.podium-step` exist because they map 1:1 to bracket concepts. Don't generalize a `.match-card` into a `.list-row` — readability wins.

## 3. Design tokens

All tokens are defined in [web-mobile/css/styles.css:3-31](web-mobile/css/styles.css). Reference them via `var(--name)` — never hardcode hex or px scales.

### Color

| Token | Value | Use |
|---|---|---|
| `--accent` | `#1d3557` | Primary CTAs, active nav, winner-side (Shiro), brand fills |
| `--accent-soft` | `#e7eaf3` | Hover/active tint, focus rings, Shiro court chips |
| `--accent-fg` | `#ffffff` | Text on `--accent` |
| `--red` | `#c1121f` | Aka (Red) winner fill, live indicators, danger buttons |
| `--red-soft` | `#fde7e8` | Live-strip background, `bc-match--live` ring |
| `--white-side` | `#f6f7fb` | Shiro (White) side background — **not** pure white, to keep both sides visually weighted |
| `--ink` | `#1a1d24` | Body text |
| `--ink-2` | `#3a414e` | Secondary text, labels |
| `--ink-3` | `#6b7280` | Meta text, hints |
| `--ink-4` | `#6c7480` | Tertiary text — picked to hold **4.7:1 contrast on white (WCAG AA)**. Do not lighten. |
| `--line` | `#e4e6eb` | Default borders, dividers |
| `--line-2` | `#eef0f4` | Subtle dividers, alt rows, hover backgrounds |
| `--bg` | `#f7f8fa` | Page background |
| `--surface` | `#ffffff` | Cards, modals, inputs |

**No dark mode.** The mobile app is light-only by design — tournaments run under venue lighting and the contrast targets are tuned for that. The legacy `web/` surface has a dark-mode block, but it is not part of the shared system.

**Status palette** (badges only — don't pull these into other components):

| Status | Bg | Text | Border |
|---|---|---|---|
| `setup` (Pending) | `#f3f4f6` | `--ink-2` | (none) |
| `pools` | `#fff7ed` | `#9a3412` | `#fed7aa` |
| `playoffs` | `#ecfdf5` | `#065f46` | `#a7f3d0` |
| `completed` | `--accent-soft` | `--accent` | `--accent-soft` |
| `archive` | `#f8fafc` | `--ink-3` | `--line` |

### Typography

```
--font          system UI (Apple → Segoe → Roboto fallback)
--font-mono     SFMono-Regular → Menlo → Consolas
--font-display  SF Pro Display (used sparingly on hero titles)
```

Base: 15px / 1.4. Use the scale below — don't introduce in-between sizes.

| Size | Use |
|---|---|
| 10.5px | Pill labels, court tags, table column headers |
| 12px | Hints, breadcrumbs, secondary meta |
| 13–13.5px | Buttons, inputs, badges, bracket sides |
| 15px | Body text |
| 16–18px | Card titles, modal titles |
| 22px | Hero player name (My Match) |
| 26px | Page-head titles |

Weights: 500 (default UI), 600 (titles, active state, score), 700 (badges, bold scores). No 400 / 800.

### Spacing

There is no formal scale — use 4 / 6 / 8 / 10 / 12 / 14 / 16 / 20 / 24 / 32 px. Round to these. The page container is `24px 32px` (collapses to `16px` under 720px).

### Radius

```
--r-sm  6px    small buttons, badges
--r     10px   match cards, pool wrappers, modals
--r-lg  14px   tournament cards, large cards, full modals
999px          pills, chips, the live dot
```

### Shadows

Three levels only:

```
--shadow-sm   subtle (pressed-tab indication, mode-tabs)
--shadow      card-on-hover, toast
--shadow-lg   modal
```

Never combine shadow with a solid border on the same side — pick one elevation language per component.

### Motion

| Token (de facto) | Use |
|---|---|
| `120ms` | Color/border transitions on buttons, chips, badges |
| `140ms` | Card hovers (tcard, pool, sched-row) |
| `300ms` | Progress bars, toast slide-in |

Keyframes ([web-mobile/css/styles.css:672](web-mobile/css/styles.css), 3790, 3914, 4983):
- `pulse` (1.6s infinite) — `.dot--live` only
- `spin` (0.6s linear infinite) — loading spinners
- `toast-in` (300ms) — toast entrance
- `decision-prompt-in` — match-decision modal entrance

The CSS has **no `prefers-reduced-motion` block.** When adding non-essential animation, gate it behind that media query.

### Breakpoints

Only three media queries exist; match them rather than inventing new ones:

| Query | Trigger |
|---|---|
| `@media (pointer: coarse)` | Touch device — bump tap targets |
| `@media (max-width: 720px)` | Tablet → phone — collapse the admin sidebar, drop 4-col strips to 2-col |
| `@media (max-width: 480px)` | Small phone — viewer-specific refinements |

### Z-index

| Layer | z-index | Examples |
|---|---|---|
| Connectors | 0 | SVG lines under bracket cards |
| Cards | 1 | `.bc-match` |
| Body tabs | 9 | Sticky secondary nav |
| Viewer tabs | 10 | Sticky primary nav |
| Top bar | 30 | `.topbar-stack` |
| Modal | 100 | `.modal-backdrop` |
| Toast | 10000 | Above everything |

## 4. Components

Each component lives in `web-mobile/css/styles.css` and is composed in [web-mobile/js/](web-mobile/js/) via Preact's `React.createElement` (after esbuild). Class naming is loosely BEM with `--` for variants and `is-active` / `.is-` for boolean states.

### Buttons — `.btn`

[styles.css:325](web-mobile/css/styles.css)

| Modifier | Use |
|---|---|
| `.btn` | Default — surface bg, line border |
| `.btn--primary` | Confirm, save, start match |
| `.btn--danger` | Destructive (reset, kiken, archive) |
| `.btn--ghost` | Tertiary, inline cancels |
| `.btn--sm` / `.btn--lg` | Compact rows / hero CTAs |
| `.btn--full` | Full-width (forms, modal footers) |

States: `:hover` brightens border to `--ink-4`; `:disabled` drops to 0.5 opacity. No loading state — show a `.spinner` next to or inside the button instead.

### Cards — `.card`

[styles.css:388](web-mobile/css/styles.css)

`.card` + `.card__head` / `.card__title` / `.card__sub` / `.card__body`. Variants: `.card--pad-lg` (28px), `.card--flat` (no shadow). Tournament-list items use `.tcard` instead (grid item with hover elevation).

### Form fields

`.field > .field__label + .input + .field__hint`. Inputs share padding (`9px 12px`), radius (`8px`), border (`--line`), focus ring (`3px --accent-soft`). For dense controls (in modals/tables), use `.input--sm`. `.lined-textarea` adds a line-number gutter — see the participant paste box in admin.

### Tables — `.table`, `.pool__table`

Uppercase 12px column headers, 13px body, `--line-2` row separators, hover row tint. Numeric columns get `font-family: var(--font-mono)`. Pool tables add `tr.advancing` (light-green bg + `▲` marker) to mark players progressing to the playoffs.

### Badges — `.badge`

Variant maps to tournament status, **not** to severity. Use `<StatusBadge status={...}/>` from [ui.jsx:3](web-mobile/js/ui.jsx) — don't write the class manually unless adding a new status type. Live dot via `<span className="dot dot--live"/>`.

### Match cards — `.bc-match`

[styles.css:828](web-mobile/css/styles.css)

Three layout variants:
- `bc-match--v1` — line bracket (default)
- `bc-match--v2` — filled sides, used in the viewer's "now playing" surface
- `bc-match--v3` — compact, used in dense round columns

State modifiers stack: `bc-match--live`, `bc-match--highlight`, `bc-match--done`, `bc-match--bye`. Sides are `bc-side--a` (Aka, always left/top) and `bc-side--b` (Shiro, always right/bottom). Winner side gets `bc-side--winner` plus a fill swap to `--red` or `--accent`. **Never swap the side order based on seeding** — the geometry is the rule.

### Pools — `.pool`, `.pools-grid`

Auto-fill grid (320px min). Each pool is a card with `.pool__table` inside. `.pool--done` recolors the wrapper to `--accent-soft`.

### Modals — `.modal-backdrop > .modal`

`.modal--lg` widens to 720px (default 460). Always wire `useEscapeToClose(onClose)` from [ui.jsx:133](web-mobile/js/ui.jsx) — every modal in the app supports Escape, and operators rely on it.

### Toasts — `.toast`

Mount via the `<Toast>` primitive. Self-dismiss at 2.7s. Don't stack toasts; the latest replaces the previous one.

### Schedule rows — `.sched-row` (admin), `.vsched-item` (viewer)

Grid: `60px (court) | 70px (time) | 1fr (matchup) | auto (actions)`. `--live` adds the red ring; `--done` drops opacity to 0.7. Court chips reuse `--accent-soft`.

### Podium — `.podium-step--{1,2,3}`

Three-column layout with `order:` reordering so 1st sits in the middle (visual hierarchy: 2-1-3 left-to-right). Gold/silver/bronze gradients are component-local; don't lift them into tokens.

### "My Match" hero — `.my-match`

Player-viewer-only. Solid `--accent` background, `--accent-fg` text. The only place white-on-navy body text appears.

## 5. Patterns

### Page shell

```
.app
  ├── .topbar-stack            (sticky z-30)
  │   ├── .topbar              (logo + nav + actions)
  │   └── .live-strip          (red banner, only when any court is live)
  └── .page                    (max-width 1280, 24×32 padding)
      └── route content
```

### Admin workspace

`.workspace` is `grid-template-columns: 240px 1fr` (sidebar + main). Sidebar is `.side-nav` with sticky positioning. Under 720px the grid collapses to a single column and the sidebar becomes a horizontal scroller.

### Viewer shell

The spectator/player viewer constrains itself to `max-width: 480px` regardless of device width — it's designed as a phone experience even on desktop. Don't break this constraint.

### Live signal

When any match is live, three things must be true simultaneously:
1. `.live-strip` appears in the topbar stack with one chip per live court
2. The relevant `.bc-match`, `.sched-row`, `.vsched-item` carry the `--live` modifier
3. A `.dot--live` pulses next to the status badge

If only one or two surface the signal, it's a bug.

### Match-decision visual suffixes

Decision types ([CLAUDE.md](CLAUDE.md) — "Match Decision Types") map to short tags rendered inside or beside the match card:

| Decision | Tag | Color |
|---|---|---|
| `hikiwake` | `△` | `--ink-2` |
| `kiken` | `Kiken` | `--accent` |
| `fusenpai` | `Fus.` | `--accent` |
| `daihyosen` | `DH` | `--accent` |
| Encho (overtime) | `(E)` | `--accent` |
| `kachinuki-exhaustion` | inherits | — |

All decision tags use the accent (navy) family, not red — red is reserved for liveness, not outcome.

## 6. Accessibility

- **Contrast**: `--ink-4` is the floor at 4.7:1 on `--surface`. Don't introduce new gray tokens without re-checking.
- **Keyboard**: every modal honors Escape via `useEscapeToClose`. The admin score editor supports `←` / `→` to navigate between matches **on the same shiaijo** — see [CLAUDE.md](CLAUDE.md) and the note in [admin_schedule.jsx](web-mobile/js/admin_schedule.jsx). When adding keyboard shortcuts, gate them on `!isTextEntry(e.target)` (also in [ui.jsx:151](web-mobile/js/ui.jsx)) so they don't clobber inputs.
- **Touch**: `@media (pointer: coarse)` blocks bump padding on dense controls. Test any new dense surface on a tablet before merging.
- **Focus rings**: inputs use a 3px `--accent-soft` ring. Don't suppress `:focus-visible` — operators tab through forms.
- **Motion**: there's no global `prefers-reduced-motion` opt-out yet. Pulse, spin, and toast-in are the only ambient animations; if you add more, gate them.

## 7. Frontend conventions

### Naming

- Components: BEM-ish — `.block`, `.block__elem`, `.block--mod`, plus `.is-active` / `.is-done` for boolean state.
- Domain blocks: keep the short prefix (`bc-` = bracket, `pool-`, `sched-`, `vsched-` = viewer schedule, `tcard-` = tournament card). New domain concepts get their own short prefix.
- Don't reach for utility classes (no `.mt-4`, no `.flex-1`) — add semantic class names instead.

### Preact primitives

Shared primitives live in [web-mobile/js/ui.jsx](web-mobile/js/ui.jsx) and are also exposed on `window` for legacy callers:

| Primitive | Purpose |
|---|---|
| `StatusBadge` | Render `.badge--<status>` with optional live dot. Use for any tournament-status pill. |
| `StableInput` | Debounced controlled input — prevents character drops when the parent re-renders the whole tree during typing. Mandatory for any input that lives inside a tree that re-renders on SSE. |
| `Toast` | Auto-dismissing notification. |
| `useEscapeToClose` | Hook every modal needs. |
| `isTextEntry`, `isInteractiveTarget` | Guards for global keyboard shortcuts. |
| `formatDate`, `pluralize` | Display helpers. |

### Build pipeline

`make go/build` runs esbuild on `web-mobile/js/*.jsx` → `web-mobile/dist/*.js`, then `//go:embed web-mobile/*` baked into the binary. **Edits to JSX or CSS require a rebuild to take effect** in a running server — `make run-mobile` rebuilds automatically.

## 8. Bracket generator (`web/`)

The legacy surface is a Bootstrap 5.3 form. Keep changes there scoped to:
- CSV upload UX
- Validation feedback (`.validation-panel`)
- The format-guide explainer

Don't import mobile-app components or tokens — the file size budget is intentionally small (it's served as a single page) and the visual language is Bootstrap's, with light overrides in [web/css/styles.css](web/css/styles.css). It is the only place the project supports a `[data-theme="dark"]` override.

## 9. Adding to the system

Before introducing a new component, color, or pattern:

1. **Reuse first.** Check the component list in §4 — there is almost always a match (especially for status badges, cards, table rows).
2. **Extend, don't fork.** A new button shape is a `.btn--<modifier>`, not a new `.action-button`.
3. **Token-only colors.** If you need a hex literal in a CSS rule, you probably need a new token in `:root` — add it there and reference it.
4. **Domain-specific is fine.** Match-side colors and podium gradients live inside their component blocks intentionally. Don't generalize them.
5. **Verify visually.** UI changes are validated in a running browser via `make run-mobile`, not by diff inspection — see [CLAUDE.md](CLAUDE.md) "Common Pitfalls".
6. **Match the prefix.** Pick the existing prefix that covers your concept (`bc-`, `pool-`, `sched-`, `vsched-`, `tcard-`, `viewer-`, `score-`, `live-`, `my-match-`) before inventing a new one.

When in doubt, read the equivalent block in `styles.css` and copy its structure. This system favors visible consistency over abstraction.
