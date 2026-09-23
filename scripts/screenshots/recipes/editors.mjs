// Score-editor captures: the individual board, and the kachinuki bout board
// through a whole encounter (score, correct, tie, reopen, end again).
//
// Every one of these has a drive(): the subject IS a procedure, and the state
// each shot needs only exists a few clicks into the real editor. They therefore
// run IN ORDER against ONE seeded fixture and each inherits the previous
// shot's state - a running match stays running, a completed one stays
// completed. Each drive() checks the precondition it needs and throws rather
// than photograph the wrong moment.
import { settle, withAdminPage } from '../lib/ui.mjs';
import { roster } from '../lib/api.mjs';
import { EDITOR, finishMatch } from '../lib/editor.mjs';
import { assertBoutPoints, assertLineupIds } from '../lib/fixture.mjs';
import { SCORE_EDITOR_SOURCES } from '../lib/scope.mjs';

// Why the viewports below are so tall: the kachinuki editor is ALWAYS the
// compact, internally-scrolled layout (admin_scoring_team.jsx:1125 -
// `useCompact = teamSize <= 5 || isKachinuki`), so its bout list is capped
// against the viewport and a crop of the whole modal shows a scrollbar instead
// of the board. Each viewport is therefore sized so the modal fits inside it
// uncapped. Do not reach for injected CSS instead: every recipe here navigates
// inside drive(), which discards an injected style sheet.

const IND = 'individual-cup';
const TEAMS = 'kachinuki-teams';
const KO = 'kachinuki-knockout';

// ---------------------------------------------------------------- seeding --

async function teamIds(api, compId) {
  const parts = await api.get(`/api/competitions/${compId}/participants`);
  return parts.map((p) => ({ id: p.id, name: p.name }));
}

export const families = {
  // A single competition on court A, scored end to end, so the court console
  // has nothing left to start. Deliberately separate from the demo tournament,
  // which keeps a category mid-run on every court.
  driedCourt: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the court console it captures, plus
    // the score editor its seed drives to run the court dry.
    sources: ['web-mobile/js/admin_shiaijo', ...SCORE_EDITOR_SOURCES],
    seed: async ({ api, base, browser }) => {
      await api.tournament({ courts: ['A'] });
      const id = await api.competition('sixth-dan-and-up', '6D and up', {
        format: 'knockout', courts: ['A'], numberPrefix: 'D',
      });
      await api.participants(id, [
        { name: 'Akira Sato', dojo: 'Mumeishi' },
        { name: 'Bea Lindqvist', dojo: 'Nenriki' },
        { name: 'Chen Wei', dojo: 'Kenshinkan' },
        { name: 'Dai Fujita', dojo: 'Sanshukan' },
      ]);
      await api.generateDraw(id);
      await api.start(id);

      // Score every match through the editor, so the queue drains the way the
      // application drains it. Driving this from the score-editor list rather
      // than the court console on purpose: the console only offers "Start
      // match" when nothing is open, so a loop keyed on that button stops as
      // soon as it has started one and the bracket never finishes.
      await withAdminPage(browser, { width: 1180, height: 820 }, async (page) => {
        for (let guard = 0; guard < 16; guard += 1) {
          await page.goto(`${base}/admin/score-editor`, { waitUntil: 'domcontentloaded' });
          await page.waitForTimeout(700);
          const score = page.locator('button').filter({ hasText: /^Score$/ }).first();
          if (!(await score.count())) break;
          await score.click();
          await page.locator(EDITOR).waitFor({ state: 'visible', timeout: 15000 });
          const begin = page.locator(EDITOR).locator('button').filter({ hasText: /^Start match$/ }).first();
          if (await begin.count()) { await begin.click(); await page.waitForTimeout(400); }
          for (let i = 0; i < 2; i += 1) {
            await page.locator('.sb-side--shiro .ipt-btn').filter({ hasText: /^M$/ }).first().click();
            await page.waitForTimeout(250);
          }
          await finishMatch(page);
          await page.waitForTimeout(800);
        }
      });
      return { compId: id };
    },
  },

  editors: {
    server: 'mobile',
    // SINCE scoping inputs (lib/scope.mjs): the score editors and the schedule
    // page that hosts them (admin_schedule_score_editor.jsx).
    sources: SCORE_EDITOR_SOURCES,
    seed: async ({ api }) => {
      // Three courts, one per competition. Court locks are cross-competition,
      // so two competitions sharing a court cannot both hold a running match,
      // which is exactly what these captures need.
      await api.tournament({
        name: 'London Cup Demo', date: '10-05-2026', venue: 'London',
        durationDays: 2, courts: ['A', 'B', 'C'],
      });

      // Individual: 6 entrants over 2 pools of 3, so pool A reads
      // "MATCH 1 OF 3" and the editor's Next/Finish+Start-Next chain has
      // somewhere to go.
      await api.competition(IND, 'Individual Cup', {
        format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['B'],
        numberPrefix: 'I', startTime: '10:00', date: '10-05-2026',
      });
      await api.participants(IND, [
        ...roster(['Baba', 'Goto'], 'Kita Dojo'),
        ...roster(['Doi', 'Endo'], 'Minami Dojo'),
        ...roster(['Chiba', 'Fujii'], 'Higashi Dojo'),
      ]);
      await api.generateDraw(IND);
      await api.start(IND);

      // Kachinuki pool: 3 teams in one pool -> "POOL A - MATCH 1 OF 3".
      await api.competition(TEAMS, 'Kachinuki Teams', {
        teamSize: 5, teamMatchType: 'kachinuki',
        format: 'mixed', poolSize: 3, poolWinners: 2, courts: ['A'],
        numberPrefix: 'K', startTime: '09:00', date: '10-05-2026',
      });
      // Six teams, not three: a mixed competition needs at least two pools
      // (the draw refuses one), and two pools of three give pool A its
      // "MATCH 1 OF 3".
      await api.participants(TEAMS, [
        { name: 'Team Daito', dojo: 'Kita Dojo' },
        { name: 'Team Hagi', dojo: 'Minami Dojo' },
        { name: 'Team Kiri', dojo: 'Higashi Dojo' },
        { name: 'Team Sakura', dojo: 'Kita Dojo' },
        { name: 'Team Tsubaki', dojo: 'Minami Dojo' },
        { name: 'Team Ume', dojo: 'Higashi Dojo' },
      ]);
      await api.generateDraw(TEAMS);
      await api.start(TEAMS);

      // Kachinuki knockout: 2 teams -> a single FINAL.
      await api.competition(KO, 'Kachinuki Knockout', {
        teamSize: 5, teamMatchType: 'kachinuki',
        format: 'knockout', roundRobin: false, courts: ['C'],
        numberPrefix: 'T', startTime: '11:00', date: '10-05-2026',
      });
      await api.participants(KO, [
        { name: 'Team Ryu', dojo: 'Kita Dojo' },
        { name: 'Team Tora', dojo: 'Minami Dojo' },
      ]);
      await api.generateDraw(KO);
      await api.start(KO);

      const memberNames = {
        'Team Daito': ['Hoshino', 'Fujita', 'Ueda', 'Sakai', 'Nomura'],
        'Team Kiri': ['Yamada', 'Ogawa', 'Kubo', 'Maeda', 'Ito'],
        'Team Hagi': ['Arai', 'Shiba', 'Tada', 'Oda', 'Nishi'],
        'Team Sakura': ['Inoue', 'Koga', 'Taniguchi', 'Yano', 'Ozaki'],
        'Team Tsubaki': ['Fukuda', 'Matsui', 'Sugita', 'Wada', 'Horie'],
        'Team Ume': ['Kawai', 'Nagai', 'Segawa', 'Tomita', 'Uchida'],
        'Team Ryu': ['Ishida', 'Kono', 'Sano', 'Hara', 'Abe'],
        'Team Tora': ['Mori', 'Ueno', 'Kimura', 'Aoki', 'Saito'],
      };
      for (const comp of [TEAMS, KO]) {
        for (const t of await teamIds(api, comp)) {
          await api.lineup(comp, t.id, await api.nameMembers(comp, t.id, memberNames[t.name]));
        }
      }

      // Hand each recipe the sides of the match it opens. Who lands where is
      // the draw's business (the tree-aware distributor places entrants, and
      // the numbers follow the draw positions), so read it back rather than
      // assume the roster order survived.
      const firstPool = async (id) => {
        const v = await api.viewer(id);
        const m = v.poolMatches.find((x) => x.id === 'Pool A-0');
        return { a: m.sideA, b: m.sideB };
      };
      const koFinal = await api.viewer(KO);
      const final = koFinal.bracket.rounds.at(-1)[0];
      return {
        ind: await firstPool(IND),
        teams: await firstPool(TEAMS),
        ko: { a: final.sideA, b: final.sideB },
      };
    },
  },
};

// ------------------------------------------------------------- local ui ----
// The team editor's drivers, local for the reason lib/ui.mjs gives in its
// header.

async function openScoreEditorRow(page, base, a, b) {
  await page.goto(base + '/admin/score-editor', { waitUntil: 'domcontentloaded' });
  const row = page.locator('.score-edit-row', { hasText: a }).filter({ hasText: b }).first();
  await row.waitFor({ state: 'visible', timeout: 20000 });
  // admin_schedule_score_editor.jsx:243 (.score-edit-row); the per-row button
  // reads "Score" while the match is scheduled or running and "Correct" once
  // it is complete.
  await row.locator('button').filter({ hasText: /^(Score|Correct)$/ }).first().click();
  await page.locator(EDITOR).waitFor({ state: 'visible', timeout: 20000 });
  await page.waitForTimeout(600);
}

async function startIfOffered(page) {
  const btn = page.locator(EDITOR).locator('button', { hasText: 'Start match' });
  if (await btn.count()) {
    await btn.first().click();
    await page.waitForTimeout(900);
  }
}

// Score one ippon on the CURRENT kachinuki bout through the editor's own key
// handler (admin_scoring_team.jsx:2599-2622: plain key = Shiro/sideB,
// Shift = Aka/sideA). The handler ignores a key pressed while an interactive
// element has focus, which after a click is always the case, so blur first.
async function boutIppon(page, side, waza = 'M') {
  await page.evaluate(() => document.activeElement && document.activeElement.blur());
  await page.keyboard.press(side === 'aka' ? `Shift+Key${waza}` : `Key${waza}`);
  await page.waitForTimeout(700);
}

const teamBtn = (page, testid) => page.locator(`[data-testid="${testid}"]`).first();

const recordBoutBtn = (page) =>
  page.locator(EDITOR).locator('button').filter({ hasText: /^Record bout$/ }).first();

// Score the current bout only if nothing is on it yet. Record bout is disabled
// until the bout has been played (`!kachinukiCurrentBoutPlayed`,
// admin_scoring_team.jsx:3793), which makes the button the board's own answer
// to "is this bout still blank" - and these recipes run in sequence, so the
// one before may already have scored it.
async function scoreCurrentBout(page, side = 'shiro') {
  if (await recordBoutBtn(page).isDisabled()) await boutIppon(page, side);
}

async function recordBout(page) {
  const before = await fought(page);
  await recordBoutBtn(page).click();
  // Wait for the bout to actually become a read-only row, rather than sleeping
  // a fixed interval and hoping. Recording a bout is a round trip - the server
  // appends the next pairing and the SPA re-renders - and a fixed wait that is
  // USUALLY long enough is precisely how this family ended up capturing a
  // different number of bouts from one run to the next, which in turn made the
  // "what changed" comparison report three captures as changed every run.
  await page.waitForFunction(
    (n) => document.querySelectorAll('.team-sub-match--readonly').length > n,
    before,
    { timeout: 15000 },
  );
  await settle(page);
}

// End match is the same deliberate two-tap guard the individual editor uses
// (admin_scoring_team.jsx:3817, data-testid kachinuki-end-match-button).
async function endMatchTwice(page) {
  const b = teamBtn(page, 'kachinuki-end-match-button');
  await b.click();
  await page.waitForTimeout(600);
  await b.click();
  await page.waitForTimeout(1200);
}

// How many bouts the encounter has already recorded: one read-only row each
// (admin_scoring_team.jsx:2156). Counted by CLASS, not by the row's test id:
// that id is a prefix of the row's two member-label ids as well
// (:2166, :2185), so a prefix match returns three elements per bout.
const fought = (page) => page.locator('.team-sub-match--readonly').count();

// Put every fought bout back in its collapsed read-only form, so the board
// shows the current bout and nothing else expanded
// (admin_scoring_team.jsx:3017, the caret on an opened row).
async function collapseDoneBouts(page) {
  for (const caret of await page.locator('[data-testid^="kachinuki-done-collapse-"]').all()) {
    await caret.click();
    await page.waitForTimeout(250);
  }
}

// Both individual shots want the same board state, and an ippon tap autosaves
// while the match is running (admin_scoring_individual.jsx:279), so the second
// recipe would double the score if it tapped blindly. Tap only what is missing.
async function ensureOneIpponEachSide(page) {
  for (const side of ['shiro', 'aka']) {
    // admin_scoring_individual.jsx:553 + :866 - a scored slot carries
    // .sb-slot--filled inside .sb-slots--<color>.
    if (await page.locator(`.sb-slots--${side} .sb-slot--filled`).count()) continue;
    // admin_scoring_individual.jsx:824 + :841 - the waza buttons live in
    // .sb-side--<color> > .sb-points-grid > .ipt-btn.
    await page.locator(`.sb-side--${side} .sb-points-grid .ipt-btn`, { hasText: 'M' }).first().click();
    await page.waitForTimeout(700);
  }
}

// Open the overtime counter and wind it to `periods`. Collapsed to a pill
// until asked for (EnchoControl, admin_scoring_shared.jsx:573-622).
async function setEncho(page, periods) {
  // Click the pill's ICON, never its label: the label is a glossary <Term>
  // (TermAS name="encho", admin_scoring_shared.jsx:585) which takes the tap to
  // open its tooltip and never lets the pill's own handler run - so a click on
  // the button's centre only flashes the gloss.
  const pill = page.locator('.encho-pill__icon');
  if (await pill.count()) {
    await pill.first().click();
    await page.waitForTimeout(400);
  }
  const box = page.locator('[data-testid="scoring-modal-encho-checkbox"]');
  if (!(await box.isChecked())) {
    await box.check();
    await page.waitForTimeout(400);
  }
  const plus = page.locator('.encho-row button[aria-label="Increase overtime period count"]');
  for (let i = 0; i < 8; i++) {
    if ((await page.locator('.encho-row__count').innerText()).trim() === `×${periods}`) return;
    await plus.click();
    await page.waitForTimeout(250);
  }
  throw new Error('encho counter never reached the requested period count');
}

// ------------------------------------------------------------- recipes -----
//
// ORDER IS THE FLOW. These run against one fixture on one server and each
// inherits the last one's state, so the kachinuki encounter is scored,
// completed, reopened and re-ended down the list.

export const recipes = [
  {
    // The individual board mid-fight: one ippon each side, the match running,
    // the clear hint under the slots.
    name: 'mobile-score-editor',
    family: 'editors',
    // 1181, not 1180: the backdrop centres the 560px modal, so an even
    // viewport puts it on a whole pixel and the crop comes out 560 wide
    // against the committed 561. One pixel of viewport moves the modal half a
    // pixel and changes nothing else.
    viewport: { width: 1181, height: 820 },
    capture: { selector: EDITOR },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.ind.a, fixture.ind.b);
      await startIfOffered(page);
      await ensureOneIpponEachSide(page);
    },
  },
  {
    // The same fight in overtime: the encho row expanded at x2, which the
    // eyebrow echoes as "(E) OVERTIME x2".
    name: 'mobile-encho-overtime',
    family: 'editors',
    viewport: { width: 921, height: 892 },
    capture: 'viewport',
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.ind.a, fixture.ind.b);
      await startIfOffered(page);
      await ensureOneIpponEachSide(page);
      await setEncho(page, 2);
    },
  },
  {
    // Kachinuki mid-encounter with a fought bout opened for correction. The
    // MIDDLE of three recorded bouts is the one reopened, so a read-only row
    // sits above it AND below it. That is what the caption on
    // team-tournaments.md describes, and what the paragraph above it needs to
    // be legible: it explains that the triangle "points right when the bout is
    // collapsed and down when it is open", which an image containing only an
    // open bout cannot show. Reopening the FIRST bout, as this did, left no
    // collapsed row in shot at all.
    name: 'kachinuki-correct-bout',
    family: 'editors',
    viewport: { width: 520, height: 1700 },
    capture: { selector: EDITOR },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      await startIfOffered(page);
      while (await fought(page) < 3) {
        await scoreCurrentBout(page);
        await recordBout(page);
      }
      if (await fought(page) !== 3) {
        throw new Error(`expected exactly three recorded bouts, found ${await fought(page)}`);
      }
      await scoreCurrentBout(page);
      // A recorded bout collapses to a read-only row; clicking it reopens it
      // for correction (admin_scoring_team.jsx:2156-2159). Bout index 1 is the
      // middle of the three, so bouts 0 and 2 stay collapsed either side of it.
      await page.locator('[data-testid="kachinuki-done-bout-1"]').click();
      await page.waitForTimeout(500);
    },
    assert: ({ dataDir }) => {
      assertLineupIds(dataDir, TEAMS);
      assertBoutPoints(dataDir, TEAMS);
    },
  },
  {
    // The same encounter one step on: bout 2 recorded, so the server appended
    // bout 3 and the footer explains what Record bout and End match will do.
    name: 'kachinuki-scoring-buttons',
    family: 'editors',
    viewport: { width: 520, height: 1250 },
    capture: { selector: EDITOR },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      await startIfOffered(page);
      while (await fought(page) < 2) {
        await scoreCurrentBout(page);
        await recordBout(page);
      }
      await collapseDoneBouts(page);
    },
    assert: ({ dataDir }) => {
      assertLineupIds(dataDir, TEAMS);
      assertBoutPoints(dataDir, TEAMS);
    },
  },
  {
    // A knockout can't end level, so a tied bout offers Encho instead of End
    // match - the footer says so and End match is disabled.
    name: 'kachinuki-knockout-tie-encho',
    family: 'editors',
    viewport: { width: 520, height: 1250 },
    capture: { selector: EDITOR },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.ko.a, fixture.ko.b);
      await startIfOffered(page);
      // admin_scoring_team.jsx:3159 - the bout's own "Tie (hikiwake)" toggle.
      const tie = page.locator('[data-testid="scoring-modal-tie-button"]').first();
      if ((await tie.getAttribute('class') || '').indexOf('btn--primary') === -1) {
        await tie.click();
        await page.waitForTimeout(900);
      }
    },
  },
  {
    // The finished encounter, opened again from a completed row: the summary
    // band carries the verdict and the footer offers Reopen match.
    name: 'kachinuki-reopen',
    family: 'editors',
    viewport: { width: 800, height: 1100 },
    capture: { selector: EDITOR },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      if (await page.locator('[data-testid="kachinuki-end-match-button"]').count()) {
        await endMatchTwice(page);
        await page.waitForTimeout(800);
        await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      }
      await page.locator('[data-testid="kachinuki-reopen-button"]').waitFor({ state: 'visible', timeout: 15000 });
    },
  },
  {
    // Ending a REOPENED encounter by decision: the server refuses a
    // finalization with no justification, so the prompt makes the reason box
    // mandatory (admin_scoring_shared.jsx:637-694).
    name: 'decision-reason-after-reopen',
    family: 'editors',
    // The prompt is as wide as the modal's body allows (modal - 2x14px), and
    // the modal is min(760px, 96vw) (styles.css:5881), so its width is chosen
    // by the viewport: 671 puts the committed 617px crop on the pixel.
    viewport: { width: 671, height: 1100 },
    capture: { selector: '.decision-prompt' },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      const reopen = page.locator('[data-testid="kachinuki-reopen-button"]');
      if (await reopen.count()) {
        // One tap, no prompt: the audit reason is owed on the way OUT
        // (admin_scoring_team.jsx:3726-3744). Reopening closes the modal.
        await reopen.first().click();
        await page.waitForTimeout(1500);
        await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      }
      // admin_scoring_team.jsx:3481 - the decision controls live behind a
      // <details> disclosure.
      const disclosure = page.locator(EDITOR).locator('details.decision-disclosure').first();
      if (!(await disclosure.evaluate((d) => d.open))) {
        await disclosure.locator('summary').click();
        await page.waitForTimeout(400);
      }
      await page.locator('[data-testid="scoring-modal-fusenpai-button"]').first().click();
      await page.locator('.decision-prompt').waitFor({ state: 'visible', timeout: 10000 });
      await page.waitForTimeout(400);
    },
    assert: async ({ page }) => {
      // The point of this shot is the MANDATORY reason, which only a reopened
      // match asks for. Without it the prompt still renders, two rows shorter.
      const note = page.locator('.decision-prompt', { hasText: 'This match was reopened' });
      if (!(await note.count())) {
        throw new Error('the decision prompt is not asking for a reason: this encounter is '
          + 'not reopened. These recipes run in order against one fixture - run the family.');
      }
    },
  },
  {
    // The other way out of a reopen: End match asks for the audit reason
    // instead of arming, and its Confirm IS the commit
    // (admin_scoring_team.jsx:3825-3827).
    name: 'kachinuki-reopen-reason',
    family: 'editors',
    // Same arithmetic as decision-reason-after-reopen: 693 gives the
    // committed 638px crop.
    viewport: { width: 693, height: 1100 },
    capture: { selector: '.reason-prompt' },
    auth: 'admin',
    drive: async ({ page, base, fixture }) => {
      await openScoreEditorRow(page, base, fixture.teams.a, fixture.teams.b);
      // ONE tap: on a reopened encounter the prompt REPLACES the arm step.
      await page.locator('[data-testid="kachinuki-end-match-button"]').first().click();
      try {
        await page.locator('.reason-prompt').waitFor({ state: 'visible', timeout: 6000 });
      } catch {
        throw new Error('End match armed instead of asking for a reason, so this encounter '
          + 'is not reopened. These recipes run in order against one fixture - run the family.');
      }
      await page.waitForTimeout(400);
    },
  },
  {
    // The court console after a court has run dry: every match in the queue is
    // complete and each offers Correct.
    //
    // This gets its OWN family rather than riding the demo tournament. The
    // console is cross-competition (admin_shiaijo.jsx: "All competitions that
    // have at least one match on this court"), every demo category is created
    // on courts A and B, and lib/seed.mjs now deliberately leaves the Teams
    // category mid-run - so court A there shows a live UP NEXT queue with
    // Start match, which is the opposite of this shot's subject. It did
    // exactly that, and only the size check ran, so it passed.
    name: 'console-correct-completed',
    family: 'driedCourt',
    viewport: { width: 1180, height: 820 },
    capture: 'viewport',
    auth: 'admin',
    route: '/admin/shiaijo/A',
    waitFor: 'text=Shiaijo A',
    drive: async ({ page }) => {
      await page.waitForTimeout(800);
    },
    // The subject is the ABSENCE of pending work. Assert it: a court with
    // anything left to play offers "Start match", and this shot must not.
    assert: async ({ page }) => {
      const pending = await page.locator('button', { hasText: /^Start match$/ }).count();
      if (pending) {
        throw new Error(`court A still offers "Start match" (${pending}x), so it has not run ` +
          'dry and this capture would show a live queue instead of completed matches');
      }
      const correct = await page.locator('button', { hasText: /^Correct$/ }).count();
      if (!correct) {
        throw new Error('court A shows no "Correct" button, so there are no completed ' +
          'matches to photograph');
      }
    },
  },
];
