# DESIGN.md

Design system reference for the bracket-creator web frontends.

Use this doc when adding UI, naming a new class, picking a color, or reviewing a screen for consistency. It is the source of truth for the visual language; the CSS file is the source of truth for exact values.

## Contents

1. [Surfaces](#1-surfaces) — what frontends ship in this repo
2. [Principles](#2-principles) — six rules that shape every decision
3. [Design tokens](#3-design-tokens) — colors, typography, spacing, radius, shadows, motion, breakpoints, z-index
4. [Components](#4-components) — buttons, cards, badges, modals, match cards, pools, score input, …
5. [Patterns](#5-patterns) — page shell, admin workspace, viewer shell, live signal
6. [Accessibility](#6-accessibility)
7. [Frontend conventions](#7-frontend-conventions) — naming, JSX→surface map, Preact primitives, build pipeline
8. [Bracket generator (`web/`)](#8-bracket-generator-web) — legacy form-only surface
9. [Adding to the system](#9-adding-to-the-system) — **start here** when introducing new patterns

**Quick start by goal:**
- *Picking a color* → §3
- *Naming a class or finding the right component* → §4 (component index, then drill in)
- *Adding a new component or pattern* → §9 first, then §4 for the closest existing example
- *Touching the live-tournament UI* → §4 Match cards, then §5 Live signal
- *Writing an admin screen* → §7 JSX→surface map, then §4 component index

## 1. Surfaces

The repo ships two frontends, both embedded into the Go binary at build time:

| Surface | Path | Purpose | Stack |
|---|---|---|---|
| **Mobile/live app** | [web-mobile/](web-mobile/) | Live tournament admin + spectator/player viewer for the `mobile-app` command. **Primary surface.** | Preact, with JSX compiled to `React.createElement` via esbuild's classic transform (see [Makefile:esbuild-jsx](Makefile#L110)). Single ~5,000-line `styles.css`. |
| **Bracket generator** | [web/](web/) | One-shot Excel-bracket generator served by the `serve` command. | Bootstrap 5.3 + plain JS, ~350 lines of overrides |

When extending the design system, **mobile-app is the canonical surface**. The bracket generator is a form; keep it functional and visually simple, don't import mobile-app patterns.

> Adding a new component, color, or pattern? Read [§9 Adding to the system](#9-adding-to-the-system) first — it's a 6-step checklist that prevents most consistency drift.

## 2. Principles

1. **Clarity over decoration.** Operators run tournaments under time pressure; a glanceable card beats a beautiful one. No gratuitous animation, no decorative shadows.
2. **Kendo first.** Red (Aka) and White (Shiro) are positional, never swapped. Web bracket cards put Aka first/top; Excel puts White in the left column. See [§4 Match cards](#match-cards--bc-match) and [CLAUDE.md](CLAUDE.md) "Match Colors" for the full rule.
3. **Live state is loud.** Anything currently happening on a court gets the red treatment (`--red` border, soft red ring, pulsing dot). Anything else stays neutral. Don't dilute the signal.
4. **Touch-friendly dense surfaces.** Operators score on tablets; players check brackets on phones. The existing pattern bumps tap targets under `@media (pointer: coarse)` — see `.btn--icon-sm` at 44px in [styles.css#L2356](web-mobile/css/styles.css#L2356). Aim for ≥ 36px in shared surfaces, ≥ 44px under coarse pointers.
5. **Status drives color, color doesn't drive meaning.** The pipeline `setup → pools → playoffs → completed` has its own palette; reuse the existing `.badge--*` rather than inventing local hues.
6. **Domain coupling is allowed.** Class names like `.bc-tree`, `.pool__table`, `.podium-step` exist because they map 1:1 to bracket concepts. Don't generalize a `.match-card` into a `.list-row` — readability wins.

## 3. Design tokens

All tokens are defined in [styles.css#L3-L31](web-mobile/css/styles.css#L3). Reference them via `var(--name)` — never hardcode hex or px scales. The file is ~5,000 lines on purpose; search before adding, but file growth is not a budget.

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
| `--ink-1` | `#111827` | **AAA-grade text (~18:1 on white)** — tournament-critical numerals, glossary terms, score displays. Use when `--ink` isn't dark enough. |
| `--ink-2` | `#3a414e` | Secondary text, labels |
| `--ink-3` | `#6b7280` | Tertiary text (meta, hints) |
| `--ink-4` | `#6c7480` | **Contrast floor for fine print** — holds **4.7:1 on white (WCAG AA)**. Do not lighten. |
| `--ink-5` | `#f1f3f6` | **Inverse text/border** — use only on dark (`--ink` / `--ink-1`) backgrounds (e.g. `.sb-draw-toggle--active`). Never use on `--surface` or `--bg`. |
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

| Token | Stack |
|---|---|
| `--font` | System UI: Apple → Segoe → Roboto fallback |
| `--font-mono` | SFMono-Regular → Menlo → Consolas |
| `--font-display` | SF Pro Display (hero titles only) |

Base: 15px / 1.4. Use the documented sizes below — don't introduce new in-between values. The CSS today contains a few stragglers (11, 11.5, 12.5, 14, 17, 24); treat them as drift to fold back, not as license to invent more.

| Size | Use |
|---|---|
| 10px | Decision chips, encho marker (`.bc-decision-chip`, `.bc-encho`) |
| 10.5px | Pill labels, court tags, table column headers |
| 12px | Hints, breadcrumbs, secondary meta |
| 13–13.5px | Buttons, inputs, badges, bracket sides |
| 15px | Body text |
| 16–18px | Card titles, modal titles |
| 22px | Large scoreboard numbers (`.sb-name`, `.match-detail-card__ippons-val`, `.stat-box .v`) |
| 26px | Page-head titles |
| 28px | Player-viewer hero (`.my-match__name`) — intentionally larger than page-head so the player's name dominates on the viewer screen |

Common weights: 500 (default UI), 600 (titles, active state), 700 (badges, scores). 800 appears in localized emphasis (podium step numbers, live strip); 400 in de-emphasis. Prefer the common three unless matching an existing emphasis pattern.

### Spacing

There is no formal scale; the conventions are honest rather than aspirational. Round to **4 / 6 / 8 / 10 / 12 / 14 / 16 / 20 / 24 / 32 px** for new work. The CSS also contains a long tail of `5 / 7 / 9 / 18 / 22 / 28` px values (mostly inside specific component blocks). Those are drift, not exemplars — match the documented set when adding new rules, and don't lean on the strays to justify new ones. The page container is `24px 32px` (collapses to `16px` under 720px).

### Radius

| Token | Value | Use |
|---|---|---|
| `--r-sm` | 6px | Small buttons, badges |
| `--r` | 10px | Match cards, pool wrappers, modals |
| `--r-lg` | 14px | Tournament cards, large cards, full modals |
| (literal) | 999px | Pills, chips, the live dot |

### Shadows

Three levels only:

| Token | Use |
|---|---|
| `--shadow-sm` | Subtle (pressed-tab indication, mode-tabs) |
| `--shadow` | Card-on-hover, toast |
| `--shadow-lg` | Modal |

Never combine shadow with a solid border on the same side — pick one elevation language per component.

### Motion

| Token (de facto) | Use |
|---|---|
| `120ms` | Color/border transitions on buttons, chips, badges |
| `140ms` | Card hovers (tcard, pool, sched-row) |
| `160ms` | Match-decision modal entrance (`decision-prompt-in`) |
| `300ms` | Progress bars, toast slide-in |

Keyframes (find each `@keyframes` block in [styles.css](web-mobile/css/styles.css)):
- `pulse` (1.6s infinite) — `.dot--live` only
- `spin` (0.6s linear infinite) — loading spinners
- `toast-in` (300ms) — toast entrance
- `decision-prompt-in` (160ms, cubic-bezier(0.2, 0.8, 0.2, 1)) — match-decision modal entrance

A `prefers-reduced-motion: reduce` block at the bottom of `styles.css` disables all four animations (`.dot--live`, `.spinner`, `.toast`, `.decision-prompt`). Gate any new non-essential animation behind this media query.

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
| Popovers | 50 | `.court-popover` |
| Sticky action rows | 60 | Admin tab/action sticky bar |
| Modal | 100 | `.modal-backdrop` |
| Popover dropdowns | 200 | Court popover dropdown |
| Toast | 500 | `.toast` — sits above modals and dropdowns but within the same order of magnitude as the rest |

If a new overlay doesn't fit one of these, lift the layer for the entire band rather than slotting in a one-off value.

## 4. Components

Each component lives in `web-mobile/css/styles.css` and is composed in [web-mobile/js/](web-mobile/js/) via Preact's `React.createElement` (after esbuild). Class naming is loosely BEM with `--` for variants and `is-active` / `.is-` for boolean states.

> **On the line numbers below:** they're accurate at time of writing but `styles.css` is ~5,000 lines and edits shift them. If a link points to the wrong rule, **grep the class name in `styles.css`** — that's the durable lookup. New entries should prefer class-name references over line numbers.

### Index

Quick lookup — scan, then `Ctrl+F` the class name to jump to its subsection.

| Component | Class | Use |
|---|---|---|
| Buttons | `.btn` + variants | All CTAs and inline actions |
| Cards | `.card`, `.tcard` | Generic + tournament-list containers |
| Form fields | `.field`, `.input` | Labeled inputs (debounced via `StableInput` when in SSE trees) |
| Tables | `.table`, `.pool__table` | Generic + pool tabular data |
| Badges | `.badge--{status}` | Status pills (setup / pools / playoffs / completed / archive) |
| Match cards | `.bc-match` | Bracket-tree card with two sides (Aka top / Shiro bottom) |
| Pools | `.pool`, `.pools-grid` | Pool standings + matchups |
| Modals | `.modal-backdrop`, `.modal` | Overlay dialogs (always wire `useEscapeToClose`) |
| Toasts | `.toast` | Auto-dismissing notifications (single-slot, no stacking) |
| Mode tabs | `.mode-tabs` | Pill-group tab switcher |
| Viewer head & tabs | `.viewer__head`, `.viewer__tabs` | Sticky viewer chrome |
| Score input | `.score-card`, `.score-pt` | Operator ippon entry widget |
| Live strip chip | `.live-strip__chip` | Court chips in the live banner |
| Tournament-card add | `.tcard--add` | Dashed "create tournament" tile |
| Schedule rows | `.sched-row` (admin), `.vsched-item` (viewer) | Schedule list items |
| Podium | `.podium-step--{1,2,3}` | Final-standings podium |
| "My Match" hero | `.my-match` | Player-viewer hero card |

### Buttons — `.btn`

[styles.css#L325](web-mobile/css/styles.css#L325)

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

[styles.css#L388](web-mobile/css/styles.css#L388)

`.card` + `.card__head` / `.card__title` / `.card__sub` / `.card__body`. Variants: `.card--pad-lg` (28px), `.card--flat` (no shadow). Tournament-list items use `.tcard` instead (grid item with hover elevation).

### Form fields

`.field > .field__label + .input + .field__hint`. Inputs share padding (`9px 12px`), radius (`8px`), border (`--line`), focus ring (`3px --accent-soft`). For dense controls (in modals/tables), use `.input--sm`. `.lined-textarea` adds a line-number gutter — see the participant paste box in admin.

### Tables — `.table`, `.pool__table`

Uppercase 12px column headers, 13px body, `--line-2` row separators, hover row tint. Numeric columns get `font-family: var(--font-mono)`. Pool tables add `tr.advancing` (light-green bg + `▲` marker) to mark players progressing to the playoffs.

### Badges — `.badge`

Variant maps to tournament status, **not** to severity. Use `<StatusBadge status={...}/>` from [ui.jsx#L3](web-mobile/js/ui.jsx#L3) — don't write the class manually unless adding a new status type. Live dot via `<span className="dot dot--live"/>`.

### Match cards — `.bc-match`

[styles.css#L830](web-mobile/css/styles.css#L830)

Layout variants (composed by [bracket.jsx#L148](web-mobile/js/bracket.jsx#L148) as `bc-match--v${variant}`):
- **Default** — plain `.bc-match` (variant `1` carries no extra rules, so passing `variant=1` is equivalent to the default)
- `bc-match--v2` — filled sides, used in the viewer's "now playing" surface ([styles.css#L986](web-mobile/css/styles.css#L986))
- `bc-match--v3` — compact, used in dense round columns ([styles.css#L1055](web-mobile/css/styles.css#L1055))

State modifiers: `bc-match--live` (red ring), `bc-match--highlight` (accent ring), and `bc-match--done` (0.75 opacity — completed matches fade back so active ones stand out) all have CSS rules.

Side composition (via `PlayerLine` in [bracket.jsx#L104](web-mobile/js/bracket.jsx#L104)): sides are `bc-side--a` (Aka/Red) and `bc-side--b` (Shiro/White), rendered in that order with a `.bc-divider` between them. In the horizontal bracket-tree layout this places Aka on top and Shiro on bottom. Winner side gets `bc-side--winner` plus a fill swap to `--red` (Aka) or `--accent` (Shiro). **Never swap side order based on seeding** — the geometry is the rule. TBD/empty rows reuse the same structure with `bc-side--empty` and a `.bc-name--tbd` text node.

Meta-row chips (rendered inside `.bc-match-meta`): `.bc-court`, `.bc-time`, `.bc-live` (red, 700-weight "● LIVE"), `.bc-bye-tag` (BYE marker, `--ink-4`), `.bc-draw` (△ for hikiwake, H for hantei, `--ink-3`), `.bc-decision-chip` (Kiken/Fus./DH, `--accent`, 10px 700), `.bc-encho` ((E), `--accent`, 10px 700).

#### Match-decision visual suffixes

Decision types ([CLAUDE.md](CLAUDE.md) "Match Decision Types") map to short tags rendered inside `.bc-match-meta`. Source: [bracket.jsx#L159-L165](web-mobile/js/bracket.jsx#L159).

| Decision | Tag | Class | Color |
|---|---|---|---|
| `hikiwake` | `△` | `.bc-draw` | `--ink-3` |
| `hantei` | `H` | `.bc-draw` | `--ink-3` |
| `kiken` | `Kiken` | `.bc-decision-chip` | `--accent` |
| `fusenpai` | `Fus.` | `.bc-decision-chip` | `--accent` |
| `daihyosen` | `DH` | `.bc-decision-chip` | `--accent` |
| Encho (overtime) | `(E)` | `.bc-encho` | `--accent` |
| `kachinuki-exhaustion` | rendered via score-line suffix only | — | inherits |

Outcome tags use either the muted ink-3 (draws) or the navy accent (decisions). **Red is reserved for liveness, not outcome.** If you add a new decision tag, follow the same color rule — don't let red bleed into outcome chips.

### Pools — `.pool`, `.pools-grid`

Auto-fill grid (320px min). Each pool is a card with `.pool__table` inside. `.pool--done` recolors the wrapper to `--accent-soft`.

### Modals — `.modal-backdrop > .modal`

`.modal--lg` widens to 720px (default 460). Always wire `useEscapeToClose(onClose)` from [ui.jsx#L133](web-mobile/js/ui.jsx#L133) — every modal in the app supports Escape, and operators rely on it.

### Toasts — `.toast`

Mount via the `<Toast>` primitive. Self-dismiss at 2.7s. The host component (see `app.jsx:181`/`214`) keeps a single toast in state, so a new `showToast` call replaces the previous one — toasts never stack.

### Mode tabs — `.mode-tabs`

[styles.css#L1560](web-mobile/css/styles.css#L1560). Pill-group tab control used for view switches (e.g., pools vs. playoffs in the admin schedule). `.mode-tabs button.is-active` lifts to `--surface` background + `--shadow-sm` — the active button is a tile on a tinted track. Use this rather than inventing a new tab pattern.

### Viewer head & tabs — `.viewer__head`, `.viewer__tabs`

[styles.css#L1601](web-mobile/css/styles.css#L1601), [#L1654](web-mobile/css/styles.css#L1654). Two distinct sticky elements: `.viewer__head` is the title/breadcrumb row (z-10), `.viewer__tabs` is the secondary nav below it (z-9). `.viewer__head--hero` swaps to `--accent` background for the player's "My Match" landing.

### Score input — `.score-card`

[styles.css#L1439](web-mobile/css/styles.css#L1439). Operator-critical widget for entering ippon. Layout: `.score-card` wraps two `.score-side` columns (`--white` / `--red`) split by a vs-cell. Each side carries `.score-side__lbl`, `.score-side__name`, `.score-side__dojo`, `.score-side__points`. Individual ippon buttons are `.score-pt` with `--filled` / `--empty` and `--aka` / `--shiro` modifiers ([#L3671](web-mobile/css/styles.css#L3671)). Match the `.score-pt--aka` color hooks if you add new score-entry surfaces.

### Live strip chip — `.live-strip__chip`

[styles.css#L216](web-mobile/css/styles.css#L216). Red-bordered pill rendered inside `.live-strip__chips`. Each chip represents one live court and is clickable to jump to the corresponding match. When adding a live-surface entry-point, surface it here rather than introducing a parallel chip strip.

### Tournament-card add — `.tcard--add`

[styles.css#L2034](web-mobile/css/styles.css#L2034). The dashed-border "create new tournament" tile that lives in the tournament-list grid. Use this pattern when adding a "create" CTA inline with a list of cards.

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

Match-decision visual suffixes are documented in [§4 Match cards](#match-cards--bc-match).

## 6. Accessibility

- **Contrast**: `--ink-4` is the floor at 4.7:1 on `--surface` (WCAG AA). For tournament-critical surfaces that must survive venue glare, use `--ink-1` (~18:1, AAA). Don't introduce new gray tokens without re-checking contrast.
- **Keyboard**: every modal honors Escape via `useEscapeToClose`. The admin score editor supports `←` / `→` to navigate between matches **on the same shiaijo** — see [CLAUDE.md](CLAUDE.md) and the note in [admin_schedule.jsx](web-mobile/js/admin_schedule.jsx). When adding keyboard shortcuts, gate them on `!isTextEntry(e.target)` (defined in [ui.jsx#L151](web-mobile/js/ui.jsx#L151)) so they don't clobber inputs.
- **Touch**: `@media (pointer: coarse)` blocks bump padding on dense controls. The internal floor is ≥ 36px on shared surfaces and ≥ 44px under coarse pointers — note that platform guidance (Apple HIG, WCAG 2.5.5 AAA) wants 44px universally; the 36px floor is a pragmatic choice for laptop-mouse admin surfaces, not a target to aim for. Test any new dense surface on a tablet before merging.
- **Focus rings**: inputs use a 3px `--accent-soft` ring. Don't suppress `:focus-visible` — operators tab through forms.
- **Motion**: there's no global `prefers-reduced-motion` opt-out yet — tracked in `bd show bracket-creator-3ch`. Pulse, spin, toast-in, and decision-prompt-in are the only ambient animations; if you add more, gate them yourself until the global block lands.

## 7. Frontend conventions

### Naming

- Components: BEM-ish — `.block`, `.block__elem`, `.block--mod`, plus `.is-active` / `.is-done` for boolean state.
- Domain blocks: keep the short prefix (`bc-` = bracket, `pool-`, `sched-`, `vsched-` = viewer schedule, `tcard-` = tournament card). New domain concepts get their own short prefix.
- Don't reach for utility classes (no `.mt-4`, no `.flex-1`) — add semantic class names instead.

### Which JSX owns which surface

When a screenshot lands on your desk, this is where to start reading:

| Surface | File |
|---|---|
| App shell, routing, toast host | [app.jsx](web-mobile/js/app.jsx) |
| Admin tournament list & landing | [admin.jsx](web-mobile/js/admin.jsx) |
| Admin competition CRUD (config, participants, pools setup) | [admin_competition.jsx](web-mobile/js/admin_competition.jsx), [admin_setup.jsx](web-mobile/js/admin_setup.jsx), [admin_participants.jsx](web-mobile/js/admin_participants.jsx), [admin_pools.jsx](web-mobile/js/admin_pools.jsx) |
| Admin live scoring & schedule | [admin_schedule.jsx](web-mobile/js/admin_schedule.jsx), [admin_scoring_modal.jsx](web-mobile/js/admin_scoring_modal.jsx), [admin_lineup.jsx](web-mobile/js/admin_lineup.jsx) |
| Admin chrome (nav, side-nav, breadcrumbs) | [admin_shell.jsx](web-mobile/js/admin_shell.jsx) |
| Spectator/player viewer | [viewer.jsx](web-mobile/js/viewer.jsx) |
| Bracket tree rendering | [bracket.jsx](web-mobile/js/bracket.jsx) |
| Read-only display components (cards, podiums) | [display.jsx](web-mobile/js/display.jsx) |
| Glossary popover (kendo terms) | [glossary.jsx](web-mobile/js/glossary.jsx) |
| Auth/reset page | [reset.jsx](web-mobile/js/reset.jsx) |

### Preact primitives

Shared primitives live in [web-mobile/js/ui.jsx](web-mobile/js/ui.jsx) and are also exposed on `window` for legacy callers:

| Primitive | Purpose |
|---|---|
| `StatusBadge` | Render `.badge--<status>` with optional live dot. Use for any tournament-status pill. |
| `StableInput` | Debounced controlled input — keeps a local value during typing and pushes to the parent on debounce/blur. Reach for it when an input lives inside a tree that re-renders on SSE; the parent's setState can otherwise drop characters. |
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
3. **Token-only colors, with two carve-outs.** If you need a hex literal in a CSS rule, you probably need a new token in `:root` — add it there and reference it. Two existing exceptions: (a) the **status palette** in §3 (badge-only colors that live inside `.badge--*` blocks, never lifted into other components), and (b) the **podium gold/silver/bronze gradients** in `.podium-step--*`. New exceptions need to be argued for, not assumed — and the decision-chip inline styles ([§4 Match cards](#match-cards--bc-match)) are debt, not a precedent.
4. **Domain-specific is fine.** Match-side colors and podium gradients live inside their component blocks intentionally. Don't generalize them.
5. **Verify visually.** UI changes are validated in a running browser via `make run-mobile`, not by diff inspection — see [CLAUDE.md](CLAUDE.md) "Common Pitfalls".
6. **Match the prefix.** Pick the existing prefix that covers your concept (`bc-`, `pool-`, `sched-`, `vsched-`, `tcard-`, `viewer-`, `score-`, `live-`, `my-match-`) before inventing a new one.

When in doubt, read the equivalent block in `styles.css` and copy its structure. This system favors visible consistency over abstraction.
