# E2E journeys

Playwright journeys that drive the tournament app (`mobile-app`) in a real
browser, the way an operator and a spectator use it. This is developer
documentation, for a contributor who runs, debugs and extends the journeys.

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

It needs Go (for the build), Node.js with npm, and a network connection on
every run: the first `make e2e/deps` (or `make e2e`) downloads the npm packages
and Chromium, and each `make e2e` rebuilds the binary, whose `vendor-frontend`
step downloads the vendored frontend libraries from unpkg every time and whose
`esbuild-jsx` step runs esbuild through `npx --yes`. "Green" means no test
failed: the `test.fixme` steps of open beads (see
[When a journey finds a bug](#when-a-journey-finds-a-bug)) are reported as
skipped.

The Playwright version here and the one `scripts/screenshots` resolves are the
same release, so the two share one Chromium under `~/.cache/ms-playwright`. If
that cache is removed the stamp outlives it; run `npx playwright install
chromium` here.

To debug, pass Playwright's own flags through `E2E_ARGS`: `--headed` shows the
browser, `--debug` opens the inspector, and a failing test leaves a trace
(see [Output](#output)).

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
  the device. If it does not, `fixtures/pointer.mjs` turns on CDP touch
  emulation and the test carries a `coarse-pointer` annotation saying so.
  Every journey runs here unless its file is named `*.public.spec.mjs`.
- `public`: a phone in portrait (390x844), for journeys about a public surface
  alone (`*.public.spec.mjs`). No journey uses this project yet; the public
  surfaces are checked from `operator` journeys through a second context.

240 s is the default test timeout (`expect` waits 15 s); some journeys raise it
for a long test with `test.setTimeout`. `retries` is 0 and `forbidOnly` is on, so a stray `test.only` fails the run.

An operator journey that needs a second device opens another context with one
of the two devices from `fixtures/devices.mjs`: `PUBLIC_DEVICE` for a public
surface on the side (the viewer showing a result the operator just entered),
`OPERATOR_DEVICE` for a second operator (the head table, or the console of
another court).

## One server per spec file

`fixtures/test.mjs` starts one `mobile-app` per spec file, on a free port and
a throwaway data folder, through `scripts/screenshots/lib/server.mjs`, and
stops it by PID. Spec files run in parallel across workers (Playwright's
default, about half the cores), each worker with its own browser and its own
server; on a slow or loaded machine pass `E2E_ARGS='--workers=1'`. A worker
runs its files one after another (all of them with `--workers=1`), and each
file starts with a fresh server, never the previous file's tournament. A
worker stops a file's server when it starts the next file's server, or when
the worker ends. Tests in one file share its server, so a spec file
that seeds once and then walks several tests declares
`test.describe.configure({ mode: 'serial' })`. A `beforeAll` hook asks for it
with `serverForFile(testInfo.file)`. Journeys navigate by path
(`page.goto('/admin')`); the host is the file's server.

## Seeding goes through the interface

Every journey builds its tournament the way an operator does: the first-run
form, the real sign-in form, the create wizard, the roster paste box, Generate
draw and Start. The HTTP API is not a seeding path here, and neither are
hand-written files in the data folder. This is a standing rule, and it couples
every journey to the wizard's copy: the setup steps are part of what
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
| `doubleTap(locator)` | two taps inside the app's bounce window less 100 ms; throws when the machine was too slow to land them that close, so a caller may assert on the outcome unconditionally |
| `retapWhileSaving(locator)` | a second tap while the button reads "Saving…" (writes are held briefly so the state is observable); throws when "Saving…" is never seen |
| `interrupt(page, kind, { awayMs, during })` | `reload`, `back` (then forward, only if Back moved), `hidden` (tab to background and back), `offline` (network down and back); `awayMs` (600 by default) is the time away and `during(page)` runs while the operator is away |
| `hastyConfirm(page)` | answers the open confirm dialog with its most prominent button and returns its label |
| `hastyShiaijoConfirm(page)` | the same for the shiaijo page's own confirms (Send back to queue, Move court) |
| `loudestButton(scope)` | the ranking both confirm helpers use, for any other panel |

Each returns what happened, and a journey records that as an audit row. Where
the docs or an operator ruling say what must happen (a double tap on an ippon
button lands one mark, an interruption leaves the operator on the same court),
the journey also asserts it, so a regression fails the suite; the rest is
judged by a person from the screenshots. `hidden` needs a note: headless Chromium never changes page
visibility on its own, so the helper overrides `document.hidden` and
`document.visibilityState` and dispatches a real `visibilitychange` for each
edge, which is what the app's listeners read.

## The pace of a tap

The app ignores a pointer tap that lands within 400 ms of the layer it opened (a
confirm, the sign-in dialog) or of the tap that armed a two-tap commit (Finish,
End match), and a second ippon or foul tap on the same SIDE of a bout inside
that window, whichever button it lands on (`web-mobile/js/tap_guard.jsx`; a
tap on the other side counts at once). Playwright's second tap is always
inside it, so `fixtures/pace.mjs` holds the dwell (the screenshot harness's
`TAP_DWELL_MS`, 500 ms) and every helper that taps into a fresh layer, confirms an arm
or strikes for a side waits it out: `login`, `hastyConfirm`,
`hastyShiaijoConfirm`, `lineupsNameSlot`, `finishMatch`, `finishTeam`,
`endMatch`, `awardIppon`, `boutIppon`, `awardBoutIppon` (`tapOnSide` keeps the
time of the last tap on each side). A journey that taps such a thing by hand
does the same. `doubleTap` is the one helper that deliberately does not: it is
the bounce, and the app is meant to ignore the second tap. `pace.mjs` reads the
app's window from `web-mobile/js/tap_guard.jsx` (`TAP_BOUNCE_MS`) rather than
copying it, and fails to load if the dwell is not longer than it.

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

Everything a test's pages logged is attached to it as `console.json` (kept in a failing test's trace), with the
page it came from, whether the device was offline at the time, and, for a
failed request, the status and body of the `/api` answer. What a `beforeAll`
hook's pages log, or what the previous test's pages still had open, is checked
with the first test of the file and marked "before this test (a beforeAll hook or
the previous test's open pages)"; what an `afterAll` hook's pages log is reported under its own file when
the next file's server starts or the worker stops, and an error there fails
the run.

The allowlist holds only noise a journey's scenario makes, in three entries:
requests the browser could not send while a journey has the device offline;
the app's own "Failed to fetch court matches" log from the court console
while the device is offline; and the 409s of a start (a `/score` write whose
body carries `status: "running"`) or a reopen the app sends through the
server's gate and shows refused (the court busy, a competitor fighting
elsewhere or barred by a withdrawal, or the knockout match it feeds being
fought). Any other write refused that way is an error. A browser "Failed to
load resource" line is paired with the `/api` answer it is about one to one,
in arrival order per URL.
Each entry matches as narrowly as it can (the message, the URL, the refusal
code, offline only) and says why in one line. An error that looks like the
app's own fault does not go on it: the test stays red until a person decides.

## Output

Each journey has a folder, `output/<journey>/`, named by the journey passed to
`journeyShots` (it may be a subfolder, `knockout-mixed-individual/J1`). Its
per-step screenshots land there, numbered in the order they were taken, beside its `audit.json` (the
audit rows, also printed to the run log as `AUDIT {json}` lines), and are
replaced on the next run. Playwright's own traces and failure screenshots land
in `output/test-results/`. All of it is gitignored. There are no pixel
comparisons: a screenshot is for a person to look at.

Traces are kept for a failing test only (`retain-on-failure`). Open one with

    npx playwright show-trace output/test-results/<test folder>/trace.zip

from this directory. The trace's Attachments tab holds the test's
`console.json` (see [The console policy](#the-console-policy)); the list reporter
does not write it to a file of its own, and a passing test keeps no trace, so
its console errors are only in the run log as `CONSOLE {json}` lines. A failing
test also carries the server's output as `server.log`.

Every run empties its whole results folder (`output/test-results/`, or
`E2E_RESULTS_DIR` when set) when it starts, and with `E2E_RESULTS_DIR` set the
journey folders live inside it, so pointing it at a folder that holds anything
else deletes that content. A journey empties
its own `output/<journey>/` in `beforeAll`. Two runs at the same time in one
checkout would delete each other's traces mid-test, and two runs without
`E2E_RESULTS_DIR` share every `output/<journey>/`, so give the second one its
own folder: `E2E_RESULTS_DIR=output/test-results-2 make e2e ...`.
With `E2E_RESULTS_DIR` set, the journey folders move inside it too
(`<E2E_RESULTS_DIR>/journeys/<journey>/`), so neither run empties the other's.
A relative `E2E_RESULTS_DIR` is relative to this directory.

## When a journey finds a bug

A functional defect found by a journey is filed as a bead, and that step
becomes

    test.fixme('<bead-id>: <one line>', ...)

so the suite stays green while the defect is open. The PR that fixes the bug
removes the `fixme`, and the step then guards the fix.

## Adding a journey

1. Name the file `journeys/<name>.spec.mjs`. A journey about a public surface
   alone is `journeys/<name>.public.spec.mjs` and runs on the phone project.
2. Import `test` and `expect` from `../fixtures/test.mjs`, never from
   `@playwright/test`: only that `test` starts the file's server and applies
   the console policy and the coarse-pointer check.
3. Seed through the interface with the fixtures (`createTournament`, `login`,
   `createCompetition`, `pasteRoster`, `generateDraw`, `startCompetition`).
4. Take screenshots and keep an audit: `const shot = journeyShots('<journey>')`
   in `beforeAll` (it empties the folder), `const audit = auditLog('<journey>')`
   at describe level with `audit.flush()` in `afterAll`, so a journey that fails
   part way still leaves its rows. `record` from the audit prints each row as an
   `AUDIT` line.
5. When tests share the seeded server, declare
   `test.describe.configure({ mode: 'serial' })`.
6. A defect the journey finds becomes a `test.fixme('<bead-id>: ...')` step
   (see below).

`smoke.spec.mjs` is the smallest complete example.
