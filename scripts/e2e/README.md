# E2E journeys

Playwright journeys that drive the tournament app (`mobile-app`) in a real
browser, the way an operator and a spectator use it.

    make e2e                                               # every journey
    make e2e E2E_ARGS='journeys/smoke.spec.mjs'            # one spec
    make e2e E2E_ARGS='--grep "score on a shiaijo"'        # one test by title
    make e2e E2E_ARGS='journeys/smoke.spec.mjs --headed'   # watch it run
    make e2e/deps                                          # install only

`make e2e` builds `bin/bracket-creator` first, installs this directory's npm
dependencies and its Chromium on first use (stamped inside `node_modules/`, so
once per worktree), then runs `playwright test` here with `E2E_ARGS` passed
through unchanged. It is not part of `make go/test` and does not run in CI. Run
it before opening a PR that changes `web-mobile/`: the PR template asks.

Playwright is pinned to the same release as `scripts/screenshots`, so the two
share one Chromium under `~/.cache/ms-playwright`. If that cache is removed the
stamp outlives it; run `npx playwright install chromium` here.

## Layout

    playwright.config.mjs   projects, timeouts, output
    fixtures/test.mjs       the `test` every journey imports (server, baseURL, page)
    fixtures/devices.mjs    the operator iPad and the spectator phone
    fixtures/setup.mjs      first-run tournament form, sign in, sign out
    fixtures/wizard.mjs     the create-competition wizard
    fixtures/competition.mjs  roster paste box, Generate / Discard draw, Start
    fixtures/shiaijo.mjs    /admin/shiaijo/:court (queue and inline editor)
    fixtures/scores.mjs     the Scores tab and its overlay editor
    fixtures/scoring.mjs    scoring an individual bout on either editor
    fixtures/public.mjs     the public viewer and the TV board
    fixtures/clumsy.mjs     the clumsy operator (below)
    fixtures/shots.mjs      per-step screenshots
    journeys/*.spec.mjs     the journeys

Each fixture module owns the selectors for one surface, so a UI change breaks
one file. The wizard is the one most likely to move; `fixtures/wizard.mjs` is
the only file that knows its copy.

## Projects and devices

Two projects, both Chromium:

- `operator`: an iPad Air in landscape (1180x820) with touch. Its page is
  checked to match `(pointer: coarse)`, so the tap-target rules apply as on
  the device. Every journey runs here unless its file is named
  `*.public.spec.mjs`.
- `public`: a phone in portrait (390x844), for journeys about a public surface
  alone (`*.public.spec.mjs`).

An operator journey that checks a public surface on the side (the viewer
showing a result the operator just entered) opens a second context with
`PUBLIC_DEVICE` from `fixtures/devices.mjs`.

## One server per worker

`fixtures/test.mjs` starts one `mobile-app` per Playwright worker, on a free
port and a throwaway data folder, through `scripts/screenshots/lib/server.mjs`,
and stops it by PID when the worker ends. Tests in one worker share that
server, so a spec file that seeds once and then walks several tests declares
`test.describe.configure({ mode: 'serial' })`. Journeys navigate by path
(`page.goto('/admin')`); the host is the worker's server.

## Seeding goes through the interface

Every journey builds its tournament the way an operator does: the first-run
form, the real sign-in form, the create wizard, the roster paste box, Generate
draw and Start. The HTTP API is not a seeding path here, and neither are
hand-written files in the data folder. That is an operator ruling, made knowing
it couples every journey to the wizard's copy: the setup steps are part of what
a journey exercises, and a shortcut would skip the part most likely to break.
`scripts/screenshots` seeds over the API for a different reason (a capture is
about one screen, not the path to it) and its `api.mjs`, `seed.mjs` and the
`authAdmin` sign-in shortcut are not used here.

Scoring goes through the score editors too. `scripts/screenshots/lib/editor.mjs`
owns the editor selector and the two-tap Finish; `fixtures/scoring.mjs`
re-exports them beside the ippon helpers.

## The clumsy operator

The review journeys play a distracted court operator: eyes on the shiaijo,
thumb on an iPad, often interrupted. `fixtures/clumsy.mjs` performs the
variants as real input:

| Helper | Variant |
|---|---|
| `tapNeighbour(locator, direction)` | taps the control next to the intended one, found by layout, and returns which it hit |
| `doubleTap(locator)` | two taps inside 300ms |
| `retapWhileSaving(locator)` | a second tap while the button reads "Saving…" (writes are held briefly so the state is observable) |
| `interrupt(page, kind)` | `reload`, `back` (then forward), `hidden` (tab to background and back), `offline` (network down and back) |
| `hastyConfirm(page)` | answers the open confirm dialog with its most prominent button and returns its label |

Each returns what happened. A journey records that as an audit row; whether the
outcome is acceptable is judged by a person from the screenshots, never
asserted. `hidden` needs a note: headless Chromium never changes page
visibility on its own, so the helper overrides `document.hidden` and
`document.visibilityState` and dispatches a real `visibilitychange` for each
edge, which is what the app's listeners read.

## Output

Per-step screenshots land in `output/<journey>/`, numbered in the order they
were taken, and are replaced on the next run. Playwright's own traces and
failure screenshots land in `output/test-results/`. Both are gitignored.
There are no pixel comparisons: a screenshot is for a person to look at.

## When a journey finds a bug

A functional defect found by a journey is filed as a bead, and that step
becomes

    test.fixme('<bead-id>: <one line>', ...)

so the suite stays green while the defect is open. The PR that fixes the bug
removes the `fixme`, and the step then guards the fix.
