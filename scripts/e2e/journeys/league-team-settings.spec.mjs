// TEAM LEAGUE SETTINGS journey: what the two league settings change on a team
// league, from the desk organiser's seat. "Break ties for top" (Top 3 or Top 4)
// is the band of finishing places whose ties the organiser must decide, and
// "Award two joint 3rd places" lets a tie at 3rd or below stand without a
// decision (engine isConsequentialTie, internal/engine/league_tiebreak.go: a
// tied group is asked when its best place is within the band, except a group
// wholly at 3rd or below when two joint 3rd places is on).
//
// Each competition is a team league of teams of two, seeded through the
// interface (fixtures/league.mjs seedLeague) with the setting under test, and
// every encounter is scripted by team INDEX, never by the draw: the numbered
// row says which team the draw made Shiro, and the script says who wins.
// Expected figures, ranks and the groups the banner must ask are TALLIED from
// the script below (the same ranking the app documents: team matches won, then
// lost, drawn, individual wins, losses, draws, points scored, points lost),
// never read back from the app.
//
//   tie at 4th    5 teams, the 4th and 5th level on every figure
//                 Top 3                      not asked, the league finishes
//                 Top 4                      asked ("Positions 4-5")
//                 Top 4 + two joint 3rd      not asked (the exemption covers 4th)
//   tie at 3rd    4 teams, the 3rd and 4th level
//                 two joint 3rd off          asked ("Positions 3-4")
//                 two joint 3rd on           not asked, two joint 3rds
//   three-way     5 teams, the 3rd, 4th and 5th level (a 2-0 cycle)
//                 two joint 3rd off          asked as ONE group of three
//                 two joint 3rd on           not asked, three joint 3rds
//
// F9 (the settings copy) is read beside each control in the first test and
// asserted in its own test below.
//
// Not serial: each test signs in (or creates the tournament when this file's
// server has none), seeds a competition of its own on a shiaijo of its own, and
// keeps its own folder of screenshots and audit rows, so one failing test
// hides nothing else here.
import { test, expect } from '../fixtures/test.mjs';
import { PUBLIC_DEVICE } from '../fixtures/devices.mjs';
import { journeyShots } from '../fixtures/shots.mjs';
import { flushAudits, journeyRecorder } from '../fixtures/audit.mjs';
import { enterTournament } from '../fixtures/setup.mjs';
import { createCompetition } from '../fixtures/wizard.mjs';
import {
  chooseTiebreakTop, openLeague, openLeagueSettings, openNumberedRow, openViewerAwards, openViewerLeague,
  playEncounter, readBanner, readLeagueSettings, readLeagueTable, readNumberedRows, readPodium, saveSettings,
  seedLeague, setTwoThirdPlaces, tiebreakBanner,
} from '../fixtures/league.mjs';

const JOURNEY = 'league-team-settings';
// Enough shiaijo that no two leagues played here share one. The settings league
// is never drawn, so it may name court A again.
const LEAGUE_CUP = { name: 'League Cup', courts: 8 };
const TEAM_SIZE = 2;

const NAMES = ['Hayabusa', 'Inoshishi', 'Kitsune', 'Tanuki', 'Washi'];
const DOJOS = ['Kita Dojo', 'Minami Dojo', 'Higashi Dojo', 'Nishi Dojo', 'Chuo Dojo'];
const teamsOf = (tag, n) => NAMES.slice(0, n).map((name, i) => [`${tag} ${name}`, DOJOS[i]]);

// ------------------------------------------------------------------- scripts

// An encounter between teams i and j (indexes into the roster): `beats(w, l)`
// is won 2-0 (both bouts, one men each); `level(a, b)` is a 0-0 draw (both
// bouts hikiwake).
const beats = (w, l) => ({ w, l });
const level = (a, b) => ({ draw: [a, b] });

// 5 teams: 0 beats everyone, 1 beats 2, 3 and 4, 2 beats 3 and 4, and 3 and 4
// draw. Teams 3 and 4 end level on every figure, for 4th place.
const TIE_AT_4TH = [
  beats(0, 1), beats(0, 2), beats(0, 3), beats(0, 4),
  beats(1, 2), beats(1, 3), beats(1, 4),
  beats(2, 3), beats(2, 4),
  level(3, 4),
];
// 4 teams: 0 beats everyone, 1 beats 2 and 3, and 2 and 3 draw: level for 3rd.
const TIE_AT_3RD = [
  beats(0, 1), beats(0, 2), beats(0, 3),
  beats(1, 2), beats(1, 3),
  level(2, 3),
];
// 5 teams: 0 beats everyone, 1 beats 2, 3 and 4, and 2, 3 and 4 beat one
// another in a ring (2 beats 3, 3 beats 4, 4 beats 2): each ends W1 L3 on 2-0
// results, level on every figure for 3rd to 5th.
const THREE_WAY_AT_3RD = [
  beats(0, 1), beats(0, 2), beats(0, 3), beats(0, 4),
  beats(1, 2), beats(1, 3), beats(1, 4),
  beats(2, 3), beats(3, 4), beats(4, 2),
];

const FIGURE_NAMES = ['W', 'L', 'T', 'IV', 'IL', 'IT', 'PW', 'PL'];

// Each team's expected figures, tallied from the script.
function tally(n, script) {
  const s = Array.from({ length: n }, () => Object.fromEntries(FIGURE_NAMES.map((f) => [f, 0])));
  for (const r of script) {
    if (r.draw) {
      for (const i of r.draw) { s[i].T += 1; s[i].IT += 2; }
    } else {
      s[r.w].W += 1; s[r.w].IV += 2; s[r.w].PW += 2;
      s[r.l].L += 1; s[r.l].IL += 2; s[r.l].PL += 2;
    }
  }
  return s;
}

// The ranking key, best first: team matches won, fewer lost, more drawn,
// individual wins, fewer individual losses, individual draws, points scored,
// fewer points lost.
const rankKey = (s) => [s.W, -s.L, s.T, s.IV, -s.IL, s.IT, s.PW, -s.PL];
function better(a, b) {
  const ka = rankKey(a);
  const kb = rankKey(b);
  for (let i = 0; i < ka.length; i += 1) if (ka[i] !== kb[i]) return ka[i] > kb[i];
  return false;
}

// Standard competition ranks (1, 2, 3, 3, 5) of every team, and the groups of
// teams level on every figure: { min, max, idx }.
function expectedTable(n, script) {
  const stats = tally(n, script);
  const ranks = stats.map((s) => 1 + stats.filter((o) => better(o, s)).length);
  const groups = [];
  for (const rank of new Set(ranks)) {
    const idx = ranks.flatMap((r, i) => (r === rank ? [i] : []));
    if (idx.length >= 2) groups.push({ min: rank, max: rank + idx.length - 1, idx });
  }
  return { stats, ranks, groups };
}

// The groups the organiser must decide: the best place within the band, except
// a group wholly at 3rd or below when two joint 3rd places is on.
const asked = (groups, { topN, joint }) => groups.filter((g) => g.min <= topN && !(joint && g.min >= 3));

// -------------------------------------------------------------------- driving

// Score every encounter of the league from its numbered row, in list order,
// the winner of each as the script says, whichever side the draw gave it.
async function playLeague(page, names, script) {
  const rows = await readNumberedRows(page);
  expect(rows).toHaveLength(script.length);
  let played = 0;
  for (const row of rows) {
    const shiro = names.indexOf(row.shiro);
    const aka = names.indexOf(row.aka);
    expect(shiro, `${row.shiro} is on the roster`).toBeGreaterThanOrEqual(0);
    expect(aka, `${row.aka} is on the roster`).toBeGreaterThanOrEqual(0);
    const r = script.find((x) => (x.draw
      ? x.draw.includes(shiro) && x.draw.includes(aka)
      : (x.w === shiro && x.l === aka) || (x.w === aka && x.l === shiro)));
    expect(r, `the script has an encounter for ${row.shiro} v ${row.aka}`).toBeTruthy();
    let bouts;
    if (r.draw) bouts = ['tie', 'tie'];
    else bouts = r.w === shiro ? ['shiro', 'shiro'] : ['aka', 'aka'];
    const ed = await openNumberedRow(page, { shiro: row.shiro, aka: row.aka });
    await playEncounter(ed, bouts);
    played += 1;
    await expect.poll(async () => (await readLeagueTable(page)).played).toBe(played);
  }
}

const groupKey = (teams) => [...teams].sort().join('|');

// A recorder per test: audit rows and screenshots in the test's own folder.
function recorder(folder) {
  const dir = `${JOURNEY}/${folder}`;
  return { ...journeyRecorder(JOURNEY, dir), shot: journeyShots(dir) };
}

// One league, one setting pair, one scripted set of results: seed, play every
// encounter, then judge whether the organiser is asked and what the table says.
//   court     the shiaijo of this competition
//   n, script the roster size and the scripted encounters
//   topN      the "Break ties for top" pill (3 or 4)
//   joint     "Award two joint 3rd places"
async function runLeague({ page, browser, baseURL, folder, tag, court, n, script, topN, joint }) {
  const { record, shot } = recorder(folder);
  const teams = teamsOf(tag, n);
  const names = teams.map(([t]) => t);
  const want = expectedTable(n, script);
  const wantAsked = asked(want.groups, { topN, joint });
  const setting = { topN, joint };
  let id;

  await test.step(`seed a team league: Top ${topN}, two joint 3rd places ${joint ? 'on' : 'off'}`, async () => {
    await enterTournament(page, LEAGUE_CUP);
    id = await seedLeague(page, {
      name: `${tag} League`, kind: 'team', entrants: teams, teamSize: TEAM_SIZE, teamMatchType: 'fixed',
      courts: [court], twoThirdPlaces: joint, topN,
    });
    await openLeagueSettings(page, id);
    const stored = await readLeagueSettings(page);
    record({ step: 'settings', action: 'read once drawn', ...setting, ...stored });
    expect(stored.tiebreakTop.active).toBe(`Top ${topN}`);
    expect(stored.twoThirdPlaces.checked).toBe(joint);
  });

  await test.step('score every encounter from its numbered row', async () => {
    await openLeague(page, id);
    await playLeague(page, names, script);
  });

  const table = await test.step('read the table the results make', async () => {
    // The league finishes by itself when nothing is asked; otherwise it waits.
    if (wantAsked.length === 0) {
      await expect.poll(async () => (await readLeagueTable(page)).heading, { timeout: 30_000 }).toBe('Final standings');
    } else {
      await expect(tiebreakBanner(page)).toBeVisible();
    }
    const t = await readLeagueTable(page);
    record({ step: 'pools', action: 'table after the last encounter', ...setting, expectedRanks: want.ranks, table: t });
    await shot(page, 'pools-after-last-encounter', { fullPage: true });
    return t;
  });

  await test.step('the figures and ranks are the ones the results make', async () => {
    expect(table.played).toBe(script.length);
    expect(table.total).toBe(script.length);
    expect(table.rows).toHaveLength(n);
    for (const [i, name] of names.entries()) {
      const row = table.rows.find((r) => r.name === name);
      expect(row, `${name} is in the table`).toBeTruthy();
      expect(row).toMatchObject(want.stats[i]);
      // A tie that is asked reads as consecutive ranks inside its places (3
      // and 4 for a tie at 3rd and 4th) until it is decided; a tie nothing
      // asks (below the band, or one the joint 3rd places cover) shares its
      // better place (4 and 4 below a Top 3), and a team in no tie reads the
      // plain rank the results make.
      const group = wantAsked.find((g) => g.idx.includes(i));
      if (group) {
        expect(row.rank, `${name} is ranked inside places ${group.min}-${group.max}`).toBeGreaterThanOrEqual(group.min);
        expect(row.rank, `${name} is ranked inside places ${group.min}-${group.max}`).toBeLessThanOrEqual(group.max);
      } else {
        expect(row.rank, `${name} is ranked ${want.ranks[i]}`).toBe(want.ranks[i]);
      }
    }
  });

  await test.step(wantAsked.length ? 'the organiser is asked about the tie' : 'the organiser is not asked and the league has finished', async () => {
    const banner = await readBanner(page);
    record({ step: 'pools', action: 'tie-break banner', ...setting, expectedAsked: wantAsked.map((g) => ({ min: g.min, max: g.max, teams: g.idx.map((i) => names[i]) })), banner });
    if (wantAsked.length === 0) {
      expect(banner.shown, 'no tie-break banner').toBe(false);
      expect(table.heading).toBe('Final standings');
      expect(table.winner).toBe(names[want.ranks.indexOf(1)]);
      return;
    }
    expect(banner.shown, 'the tie-break banner').toBe(true);
    expect(table.heading, 'the league waits for the decision').toBe('Standings');
    expect(table.winner).toBe('');
    expect(banner.acceptOffered).toBe(true);
    expect(banner.groups.map((g) => ({ min: g.min, max: g.max, teams: groupKey(g.teams) })).sort((a, b) => a.min - b.min))
      .toEqual(wantAsked.map((g) => ({ min: g.min, max: g.max, teams: groupKey(g.idx.map((i) => names[i])) })).sort((a, b) => a.min - b.min));
    for (const g of banner.groups) {
      expect(g.run.offered, `Run tie-breaker for ${g.teams.join(', ')}`).toBe(true);
    }
  });

  if (wantAsked.length === 0) {
    await test.step('a spectator phone reads the finished league and its podium', async () => {
      const spectator = await browser.newContext({ ...PUBLIC_DEVICE, baseURL });
      try {
        const phone = await spectator.newPage();
        await openViewerLeague(phone, id);
        const league = await readLeagueTable(phone);
        record({ step: 'public', action: 'League tab', ...setting, table: league });
        await shot(phone, 'public-league-tab', { fullPage: true });
        expect(league.heading).toBe('Final standings');
        await openViewerAwards(phone, id);
        const podium = await readPodium(phone);
        record({ step: 'public', action: 'Awards tab', ...setting, podium });
        await shot(phone, 'public-awards', { fullPage: true });
        expect(podium.winner || (podium.places.find((p) => p.place === 1) || {}).name).toBe(names[want.ranks.indexOf(1)]);
      } finally {
        await spectator.close();
      }
    });
  }
}

test.describe(JOURNEY, () => {
  test.afterAll(() => flushAudits(test.info().file));

  test('the settings: the band and the joint 3rd places are shown with their copy, kept across a reload', async ({ page }) => {
    test.setTimeout(240_000);
    const { record, shot } = recorder('settings');
    let id;

    await test.step('create a team league and read its league settings and their copy (F9)', async () => {
      await enterTournament(page, LEAGUE_CUP);
      id = await createCompetition(page, {
        name: 'Settings League', kind: 'team', format: 'league', teamSize: TEAM_SIZE, teamMatchType: 'fixed', courts: ['A'], numberPrefix: 'SL',
      });
      await openLeagueSettings(page, id);
      const s = await readLeagueSettings(page);
      record({ step: 'settings', action: 'read before the draw', ...s });
      await shot(page, 'settings-before-draw', { fullPage: true });
      expect(s.tiebreakTop.shown).toBe(true);
      expect(s.tiebreakTop.options.map((o) => o.label)).toEqual(['Top 3', 'Top 4']);
      expect(s.tiebreakTop.active).toBe('Top 3');
      expect(s.tiebreakTop.hint, 'the band has copy beside it').not.toBe('');
      expect(s.twoThirdPlaces.shown).toBe(true);
      expect(s.twoThirdPlaces.hint, 'the joint 3rd places have copy beside them').not.toBe('');
    });

    await test.step('Top 4 and two joint 3rd places are kept across a reload, and put back', async () => {
      await chooseTiebreakTop(page, 4);
      await setTwoThirdPlaces(page, true);
      await saveSettings(page, id);
      await openLeagueSettings(page, id);
      const kept = await readLeagueSettings(page);
      record({ step: 'settings', action: 'read after saving Top 4 and joint 3rd on', ...kept });
      await shot(page, 'settings-top4-joint-on', { fullPage: true });
      expect(kept.tiebreakTop.active).toBe('Top 4');
      expect(kept.twoThirdPlaces.checked).toBe(true);

      await chooseTiebreakTop(page, 3);
      await setTwoThirdPlaces(page, false);
      await saveSettings(page, id);
      await openLeagueSettings(page, id);
      const back = await readLeagueSettings(page);
      expect(back.tiebreakTop.active).toBe('Top 3');
      expect(back.twoThirdPlaces.checked).toBe(false);
    });
  });

  // F9: the copy beside the two controls says what they change. It names the
  // third choice (accepting shared ranks), never says "finalised" (nobody
  // finalises a league: it finishes), and speaks of a league's tied 3rd place
  // rather than semi-finalists and a bronze match.
  test('F9: the copy beside the league settings says what changes, in a league\'s words', async ({ page }) => {
    test.setTimeout(240_000);
    const { record, shot } = recorder('settings-copy');
    await enterTournament(page, LEAGUE_CUP);
    const id = await createCompetition(page, {
      name: 'Copy League', kind: 'team', format: 'league', teamSize: TEAM_SIZE, teamMatchType: 'fixed', courts: ['B'], numberPrefix: 'CL',
    });
    await openLeagueSettings(page, id);
    const s = await readLeagueSettings(page);
    record({ step: 'settings', action: 'read the copy', tiebreakHint: s.tiebreakTop.hint, twoThirdPlacesHint: s.twoThirdPlaces.hint });
    await shot(page, 'settings-copy', { fullPage: true });
    expect(s.tiebreakTop.hint, 'names the choice to accept shared ranks').toMatch(/shared ranks/i);
    expect(s.tiebreakTop.hint, 'nobody finalises a league').not.toMatch(/finalis/i);
    expect(s.twoThirdPlaces.hint, 'speaks of a league tie').toMatch(/tie|tied|level/i);
    expect(s.twoThirdPlaces.hint, 'not written for a knockout').not.toMatch(/semi-?finalist|bronze/i);
  });

  test('Top 3: a tie at 4th is not asked and the league finishes', async ({ page, browser, baseURL }) => {
    test.setTimeout(720_000);
    await runLeague({ page, browser, baseURL, folder: 'top3-tie-at-4th', tag: 'A', court: 'C', n: 5, script: TIE_AT_4TH, topN: 3, joint: false });
  });

  test('Top 4: the same tie at 4th is asked', async ({ page, browser, baseURL }) => {
    test.setTimeout(720_000);
    await runLeague({ page, browser, baseURL, folder: 'top4-tie-at-4th', tag: 'B', court: 'D', n: 5, script: TIE_AT_4TH, topN: 4, joint: false });
  });

  test('Top 4 with two joint 3rd places: the tie at 4th is not asked (the joint places cover it)', async ({ page, browser, baseURL }) => {
    test.setTimeout(720_000);
    await runLeague({ page, browser, baseURL, folder: 'top4-joint-tie-at-4th', tag: 'C', court: 'E', n: 5, script: TIE_AT_4TH, topN: 4, joint: true });
  });

  test('two joint 3rd places off: a tie at 3rd is asked', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    await runLeague({ page, browser, baseURL, folder: 'joint-off-tie-at-3rd', tag: 'D', court: 'F', n: 4, script: TIE_AT_3RD, topN: 3, joint: false });
  });

  test('two joint 3rd places on: a tie at 3rd is not asked and the two share 3rd', async ({ page, browser, baseURL }) => {
    test.setTimeout(480_000);
    await runLeague({ page, browser, baseURL, folder: 'joint-on-tie-at-3rd', tag: 'E', court: 'G', n: 4, script: TIE_AT_3RD, topN: 3, joint: true });
  });

  test('two joint 3rd places off: a three-way tie at 3rd is asked as one group', async ({ page, browser, baseURL }) => {
    test.setTimeout(720_000);
    await runLeague({ page, browser, baseURL, folder: 'joint-off-three-way', tag: 'F', court: 'H', n: 5, script: THREE_WAY_AT_3RD, topN: 3, joint: false });
  });

  test('two joint 3rd places on: a three-way tie at 3rd is not asked and the three share 3rd', async ({ page, browser, baseURL }) => {
    test.setTimeout(720_000);
    await runLeague({ page, browser, baseURL, folder: 'joint-on-three-way', tag: 'G', court: 'A', n: 5, script: THREE_WAY_AT_3RD, topN: 3, joint: true });
  });
});
