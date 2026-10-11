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
    fixtures/test.mjs       the `test` every journey imports (server, baseURL,
                            page) and the console policy (below)
    fixtures/devices.mjs    the operator iPad and the spectator phone
    fixtures/setup.mjs      first-run tournament form, sign in, sign out
    fixtures/pace.mjs       the dwell the app's tap-bounce guard needs (below)
    fixtures/wizard.mjs     the create-competition wizard
    fixtures/competition.mjs  roster paste box, Generate / Discard draw, Start
    fixtures/shiaijo.mjs    /admin/shiaijo/:court (queue, inline editor, which
                            match an editor holds)
    fixtures/scores.mjs     the Scores tab and its overlay editor
    fixtures/scoring.mjs    scoring an individual bout on either editor, and
                            the Finish check every editor shares
    fixtures/individual.mjs individual rosters, the decision panel, playing and
                            correcting a bout on the court console
    fixtures/team.mjs       team members, the three lineup entry points (the
                            Lineups page, the lineup panel, the score sheet
                            row) and the team score sheet's bout rows
    fixtures/kachinuki.mjs  the kachinuki sheet: bouts, Record bout, Encho, End
                            match, the correction doors
    fixtures/shared-court.mjs  two competitions on one shiaijo and a moved
                            match: seeding, the competition picker, the nudges
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
owns the editor selectors and Start, which `fixtures/scoring.mjs` re-exports
beside the ippon helpers. Finish is `fixtures/scoring.mjs`'s own: a journey
taps it, on the inline editor as well as the overlay, and `finishMatch` (and
`finishTeam`, through the same check) returns only once the write landed. It
fails at once when the server does not answer the completing write within the
expect timeout, answers it with an error status, or answers `applied: false`
(the refusal a disarmed Finish follows). It then waits for the editor to let
the match go, and fails if the editor shows its not-saved banner or a new
error alert.

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
End match), and a second ippon or foul tap on the same SIDE of a bout inside
that window, whichever button it lands on (`web-mobile/js/tap_guard.jsx`; a
tap on the other side counts at once). Playwright's second tap is always
inside it, so `fixtures/pace.mjs` holds the dwell (the screenshot harness's
`TAP_DWELL_MS`) and every helper that taps into a fresh layer, confirms an arm
or strikes for a side waits it out: `login`, `hastyConfirm`,
`hastyShiaijoConfirm`, `lineupsNameSlot`, `finishMatch`, `finishTeam`,
`endMatch`, `awardIppon`, `boutIppon`, `awardBoutIppon` (`tapOnSide` keeps the
time of the last tap on each side). A journey that taps such a thing by hand
does the same. `doubleTap` is the one helper that deliberately does not: it is
the bounce, and the app is meant to ignore the second tap.

## The console policy

`fixtures/test.mjs` listens to every page of every browser context a test
opens: the default page, and every context a spec opens itself with
`browser.newContext` (in the test or in a `beforeAll` hook; the `browser`
fixture is wrapped so none is missed). When the test ends:

- a console **error** or an uncaught page error FAILS the test, unless it
  matches `CONSOLE_ALLOWLIST` in that file;
- a console **warning** never fails: it is added to the spec's audit as a row
  with `journey: "console"` when the spec keeps one, and printed as a
  `CONSOLE {json}` line otherwise, and is listed on the test as a
  `console-warning` annotation.

Everything a test's pages logged is attached to it as `console.json`, with the
page it came from, whether the device was offline at the time, and, for a
failed request, the status and body of the `/api` answer. What a `beforeAll`
hook's pages log is checked with the first test of the file and marked as
such; what an `afterAll` hook's log is checked when the worker stops.

The allowlist holds only noise a journey's scenario makes: requests the
browser could not send while a journey has the device offline, and the 409s of
a start or reopen the app sends through the server's gate and shows refused
(the court or the competitor busy, or the knockout match it feeds being
fought).
Each entry matches as narrowly as it can (the message, the URL, the refusal
code, offline only) and says why in one line. An error that looks like the
app's own fault does not go on it: the test stays red until a person decides.

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
