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
    fixtures/pace.mjs       the dwell the app's tap-bounce guard needs (below)
    fixtures/wizard.mjs     the create-competition wizard
    fixtures/competition.mjs  roster paste box, Generate / Discard draw, Start
    fixtures/shiaijo.mjs    /admin/shiaijo/:court (queue and inline editor)
    fixtures/scores.mjs     the Scores tab and its overlay editor
    fixtures/scoring.mjs    scoring an individual bout on either editor
    fixtures/public.mjs     the public viewer and the TV board
    fixtures/clumsy.mjs     the clumsy operator (below)
    fixtures/pointer.mjs    the coarse pointer: check, restore, tap-target size
    fixtures/shots.mjs      per-step screenshots, and each journey's folder
    fixtures/audit.mjs      audit rows (AUDIT lines and audit.json)
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

## One server per spec file

`fixtures/test.mjs` starts one `mobile-app` per spec file, on a free port and
a throwaway data folder, through `scripts/screenshots/lib/server.mjs`, and
stops it by PID when the file is done. Playwright runs several files one after
another in one worker when there are more files than workers (`--workers=1`
runs them all in one), and each file starts with a fresh server, never the
previous file's tournament. Tests in one file share its server, so a spec file
that seeds once and then walks several tests declares
`test.describe.configure({ mode: 'serial' })`. A `beforeAll` hook asks for it
with `serverForFile(testInfo.file)`. Journeys navigate by path
(`page.goto('/admin')`); the host is the file's server.

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
| `hastyShiaijoConfirm(page)` | the same for the shiaijo page's own confirms (Send back to queue, Move court) |

Each returns what happened. A journey records that as an audit row; whether the
outcome is acceptable is judged by a person from the screenshots, never
asserted. `hidden` needs a note: headless Chromium never changes page
visibility on its own, so the helper overrides `document.hidden` and
`document.visibilityState` and dispatches a real `visibilitychange` for each
edge, which is what the app's listeners read.

## The pace of a tap

The app ignores a pointer tap that lands within 400ms of the layer it opened (a
confirm, the sign-in dialog) or of the tap that armed a two-tap commit (Finish,
End match), and a second tap on the same ippon button inside that window
(`web-mobile/js/tap_guard.jsx`). Playwright's second tap is always inside it, so
`fixtures/pace.mjs` holds the dwell (`TAP_DWELL_MS`) and every helper that taps
into a fresh layer or confirms an arm waits it out: `login`, `hastyConfirm`,
`hastyShiaijoConfirm`, `answerDialog`, `finishMatch`, `finishTeam`, `endMatch`, `awardBoutIppon`. A
journey that taps such a thing by hand does the same. `doubleTap` is the one
helper that deliberately does not: it is the bounce, and the app is meant to
ignore the second tap.

## Output

Each journey has a folder, `output/<journey>/`. Its per-step screenshots land
there, numbered in the order they were taken, beside its `audit.json` (the
audit rows, also printed to the run log as `AUDIT {json}` lines), and are
replaced on the next run. Playwright's own traces and failure screenshots land
in `output/test-results/`. All of it is gitignored. There are no pixel
comparisons: a screenshot is for a person to look at.

Every run empties `output/test-results/` when it starts. Two runs at the same
time in one checkout would delete each other's traces mid-test, so give the
second one its own folder: `E2E_RESULTS_DIR=output/test-results-2 make e2e ...`.
With `E2E_RESULTS_DIR` set, the journey folders move inside it too
(`<E2E_RESULTS_DIR>/journeys/<journey>/`), so neither run empties the other's.
A relative `E2E_RESULTS_DIR` is relative to this directory.

## When a journey finds a bug

A functional defect found by a journey is filed as a bead, and that step
becomes

    test.fixme('<bead-id>: <one line>', ...)

so the suite stays green while the defect is open. The PR that fixes the bug
removes the `fixme`, and the step then guards the fix.
