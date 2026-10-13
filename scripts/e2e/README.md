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

This directory pins `@playwright/test` at 1.63.0, and `scripts/screenshots`
resolves `playwright-core` 1.63.0 from its lockfile (its `package.json` only
asks for `^1.56.0`, so the match holds because both lockfiles are current, not
because anything enforces it). While the two agree they share one Chromium under
`~/.cache/ms-playwright`; if either lockfile moves to another release, each
downloads its own. If that cache is removed the stamp outlives it; run
`npx playwright install chromium` here.

To debug, pass Playwright's own flags through `E2E_ARGS`: `--headed` shows the
browser, `--debug` opens the inspector, and a failing test leaves a trace
(see [Output](#output)).

## Layout

    package.json            Playwright, pinned; package-lock.json is its lockfile
    playwright.config.mjs   projects, timeouts, output
    fixtures/test.mjs       the `test` every journey imports (server, baseURL,
                            page) and the console policy (below)
    fixtures/devices.mjs    the operator iPad and the spectator phone, and
                            EXPECT_TIMEOUT_MS, the one expect timeout
    fixtures/setup.mjs      first-run tournament form, sign in, sign out, and
                            enterTournament (sign in, or create the tournament
                            when the file's server has none): the one owner
    fixtures/pace.mjs       the dwell the app's tap-bounce guard needs (below)
    fixtures/wizard.mjs     the create-competition wizard
    fixtures/competition.mjs  roster paste box, Generate / Discard draw, Start
    fixtures/shiaijo.mjs    /admin/shiaijo/:court (queue, inline editor, which
                            match an editor holds)
    fixtures/scores.mjs     the Scores tab and its overlay editor
    fixtures/scoring.mjs    scoring an individual bout on either editor, Start
                            match (startMatch), clearMarks, and the Finish
                            check every editor shares
    fixtures/individual.mjs individual rosters, the decision panel, playing and
                            correcting a bout on the court console
    fixtures/team.mjs       team members, the three lineup entry points (the
                            Lineups page, the lineup panel, the score sheet
                            row) and the team score sheet's bout rows
    fixtures/kachinuki.mjs  the kachinuki sheet: bouts, Record bout, Encho, End
                            match, the correction doors
    fixtures/engi.mjs       the engi (kata pairs) flag editor: flag counters, entering
                            flags, the two-tap Save result
    fixtures/held-write.mjs holding a running match's score write in flight, to
                            observe a race with its echo (below)
    fixtures/shared-court.mjs  two competitions on one shiaijo and a moved
                            match: seeding, the competition picker, the nudges
    fixtures/public.mjs     the public viewer and the TV board
    fixtures/clumsy.mjs     the clumsy operator (below)
    fixtures/pointer.mjs    the coarse pointer: check, restore, tap-target size
    fixtures/shots.mjs      per-step screenshots, and each journey's folder
    fixtures/audit.mjs      audit rows (AUDIT lines and audit.json)
    journeys/*.spec.mjs     the journeys, one line each:
      smoke                       the harness end to end: seeding through the
                                  interface, scoring on both editors, every
                                  clumsy helper once, a spectator's phone
      knockout-mixed-individual   the court operator's review of individual
                                  competitions (pools and knockout), and the
                                  bead guards (bc-ltap and others)
      knockout-mixed-team         team lineups from the three entry points,
                                  scoring a team encounter, one result on every
                                  surface
      knockout-mixed-kachinuki    kachinuki (winner stays on) played eight bouts
                                  deep, every correction door, the final
      moved-match                 a waiting match moved to another shiaijo
                                  reaches that court's console without a reload
      shared-shiaijo              two competitions on one shiaijo: the picker,
                                  the nudge lines, correcting across them
      engi                        an engi (kata pairs) knockout scored in flags,
                                  and the bc-evts tap-back guard
    output/                 screenshots, audit.json and traces (below);
                            gitignored

Each fixture module owns the selectors for one surface, so a UI change
usually breaks one fixture file. Not only one: `fixtures/shared-court.mjs`
reads the create wizard page itself (`/admin/create-competition`, its "Add
competition" heading, `#create-start-time`) beside `fixtures/wizard.mjs`, and
a spec holds raw locators for what it checks once. The wizard is the surface
most likely to move; look in both files when it does.

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

240 s is the default test timeout; some journeys raise it for a long test with
`test.setTimeout`. `expect` waits 15 s (`EXPECT_TIMEOUT_MS` in
`fixtures/devices.mjs`, which the fixtures use for their own waits), an action
such as a tap waits 15 s (`actionTimeout`) and a navigation 30 s
(`navigationTimeout`). `retries` is 0 and `forbidOnly` is on, so a stray
`test.only` fails the run. Chromium runs with `--no-sandbox` only when the
process is root, which it refuses to sandbox; anywhere else the sandbox stays
on (the same rule as `scripts/screenshots/run.mjs`).

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
the worker ends. Tests in one file share its server, so a spec file that seeds
once and then walks several tests declares
`test.describe.configure({ mode: 'serial' })`. A `beforeAll` hook asks for it
with `serverForFile(testInfo.file)`. Journeys navigate by path
(`page.goto('/admin')`); the host is the file's server.

When a test fails, Playwright discards its worker. In a serial file the rest of
that file is skipped, because the later tests depend on what the earlier ones
seeded. A file that is not serial (`knockout-mixed-team.spec.mjs`, and the bead
guards at the end of `knockout-mixed-individual.spec.mjs`) goes on in a fresh
worker, with a fresh server and an empty data folder, so each of its tests must
seed or sign in again itself (`enterTournament` creates the tournament when the
server has none).

`knockout-mixed-kachinuki.spec.mjs` is serial but cannot let a guard skip the
rest. Its four regression guards (bc-crpn, bc-emsl, bc-kheb, bc-sync) need the
final's live console, so they run as steps inside the final's tests, wrapped in
`guarded()`. A guard that fails is not thrown. It is recorded as a
`guard-failure` annotation on the test and as an audit row, the console is put
back, and the final's own checks still run; the played-out test then fails at
its end naming every guard that failed.

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
owns the editor selectors, which `fixtures/scoring.mjs` re-exports beside the
ippon helpers. Start and Finish are `fixtures/scoring.mjs`'s own, not that
library's, which clicks and can return without having acted. `startMatch` taps
the overlay editor's Start match, fails when the button never appears, and
returns once the button has left. A journey taps Finish, on the inline editor as
well as the overlay, and `finishMatch` (and
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
| `interrupt(page, kind, { awayMs, during })` | `reload`, `back` (then forward, only if Back moved), `hidden` (tab to background and back), `offline` (network down and back); `awayMs` (600 by default) is the time away and `during(page)` runs while the operator is away, for every kind but `reload`, which reloads at once and uses neither |
| `hastyConfirm(page)` | answers the open confirm dialog with its most prominent button and returns its label |
| `hastyShiaijoConfirm(page)` | the same for the shiaijo page's own confirms (Send back to queue, Move court) |
| `loudestButton(scope)` | the ranking both confirm helpers use, for any other panel |

Each returns what happened, and a journey records that as an audit row. Where
the docs or an operator ruling say what must happen (a double tap on an ippon
button lands one mark, an interruption leaves the operator on the same court),
the journey also asserts it, so a regression fails the suite; the rest is
judged by a person from the screenshots.

A helper asserts nothing about the app, but it throws when it could not perform
its variant: `tapNeighbour` finds no neighbour, `doubleTap` lands its taps too
far apart, `retapWhileSaving` never sees "Saving…", and the confirm helpers find
no dialog.

`hidden` needs a note: headless Chromium never changes page visibility on its
own, so the helper overrides `document.hidden` and `document.visibilityState`
and dispatches a real `visibilitychange` for each edge, which is what the app's
listeners read.

## Holding a save in flight

On localhost the answer to an autosave comes back within milliseconds of the save
going out, so a tap made "while the save is out" never lands in that window by
itself. `fixtures/held-write.mjs` (`holdScoreWrites(page)`) intercepts the match
score PUTs: `holdNext()` holds the next one, its `sent` promise resolves once that
PUT is out (so the autosave debounce has run), and `release()` lets it through
after the journey has made the next tap, as a slow uplink would. `bodies` lists
every score PUT the page sent. The bc-ltap step of
`knockout-mixed-individual.spec.mjs` (individual editor) and the bc-evts step of
`engi.spec.mjs` (engi editor) use it to guard a flag or mark taken back while its
add is still saving.

## The pace of a tap

The app ignores a pointer tap that lands within 400 ms of the layer it opened (a
confirm, the sign-in dialog) or of the tap that armed a two-tap commit (Finish,
End match), and a second ippon or foul tap on the same SIDE of a bout inside
that window, whichever button it lands on (`web-mobile/js/tap_guard.jsx`; a
tap on the other side counts at once). Playwright's second tap is always
inside it, so `fixtures/pace.mjs` holds the dwell (the screenshot harness's
`TAP_DWELL_MS`, 500 ms) and every helper that taps into a fresh layer, confirms an arm
or strikes for a side waits it out. Into a fresh layer: `login`,
`hastyConfirm`, `hastyShiaijoConfirm`, `lineupsNameSlot`. Onto an armed button,
through `settled`: `armFinish` (so `finishMatch`), `finishTeam`, `endMatch`,
`saveEngiResult`. Per side of a bout, through `tapOnSide`, which keeps the time
of the last tap on each side: `awardIppon` and `boutIppon`. `awardBoutIppon`
taps by itself and dwells after its tap instead, so the next strike is ready.
A journey that taps such a thing by hand does the same. `doubleTap` is the one
helper that deliberately does not: it is the bounce, and the app is meant to
ignore the second tap. `pace.mjs` reads the
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

Everything a test's pages logged is attached to it as `console.json`, but only
when there is something to attach. A failing test keeps it in its trace. Each
entry carries the page it came from and whether the device was offline at the
time. For a failed request it also carries the status and body of the `/api`
answer.

What a `beforeAll` hook's pages log, or what the previous test's pages still had
open, is checked with the first test of the file and marked "before this test (a
beforeAll hook or the previous test's open pages)". What an `afterAll` hook's
pages log is reported under its own file when the next file's server starts or
the worker stops, and an error there fails the run.

The allowlist holds only noise a journey's scenario makes, in three entries:
requests the browser could not send while a journey has the device offline;
the app's own "Failed to fetch court matches" log from the court console
while the device is offline; and the 409s of a start (a `/score` write whose
body carries `status: "running"`) or a reopen the app sends through the
server's gate and shows refused (the court busy, a competitor fighting
elsewhere or barred by a withdrawal, or the knockout match it feeds being
fought). Any other write refused that way is an error. A browser "Failed to
load resource" line is paired with the `/api` answer it is about one to one,
in arrival order per URL. When the page closed before the 409's body was read,
the refusal code cannot be checked, and the entry then stands on the answer's
status, URL and request alone.
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
test also carries the server's output as `server.log`, when a server is running
for its file.

Every run empties its whole results folder (`output/test-results/`, or
`E2E_RESULTS_DIR` when set) when it starts, so pointing `E2E_RESULTS_DIR` at a
folder that holds anything else deletes that content. A journey empties its own
folder when it calls `journeyShots`, which is once per spec file or once per
test (see below).

Two runs at the same time in one checkout would delete each other's traces
mid-test, and two runs without `E2E_RESULTS_DIR` share every `output/<journey>/`,
so give the second one its own folder:
`E2E_RESULTS_DIR=output/test-results-2 make e2e ...`. With `E2E_RESULTS_DIR` set,
the journey folders live inside it, as `<E2E_RESULTS_DIR>/journeys/<journey>/`,
so neither run empties the other's. A relative `E2E_RESULTS_DIR` is relative to
this directory.

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
   (it empties the folder) either in `beforeAll`, for a file whose tests share
   one folder, or inside a test, for a file that gives each test a folder of its
   own (`knockout-mixed-individual` and `knockout-mixed-team` do);
   `const audit = auditLog('<journey>')` at describe level with `audit.flush()`
   in `afterAll`, so a journey that fails part way still leaves its rows. `record` from the audit prints each row as an
   `AUDIT` line.
5. When tests share the seeded server, declare
   `test.describe.configure({ mode: 'serial' })`.
6. A defect the journey finds becomes a `test.fixme('<bead-id>: ...')` step
   (see below).

`smoke.spec.mjs` is the smallest complete example. `engi.spec.mjs` is the one
journey on an engi (kata pairs) competition: it seeds four pairs through the
wizard's "Engi (kata competition)" box (`engi: true` in `createCompetition`) and
the roster paste box (`Name 1 - Name 2, Dojo`), scores a match in flags on the
court console and checks the result on a spectator's phone, and guards the engi
editor's tap-back race.
