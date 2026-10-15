// The pool standings tables: the admin Pools tab (/admin/competition/:id/pools,
// admin_pools.jsx) and the public viewer's Pools tab (/competition/:id,
// viewer_competition.jsx) both render PoolsViewer (viewer_standings.jsx), so
// one set of selectors reads either page. A team pool's row lists the team in
// draw order with a rank badge, then eight figures: W L T (team matches won,
// lost, tied), IV IL IT (individual victories, losses, ties) and PW PL
// (points won, lost).
import { expect } from '@playwright/test';
import { openViewer } from './public.mjs';

export const FIGURES = ['W', 'L', 'T', 'IV', 'IL', 'IT', 'PW', 'PL'];

// Pool blocks on the page; a standings table sits inside each.
export const poolBlocks = (page) => page.locator('.pool').filter({ has: page.locator('table.pool__table') });

// The admin Pools tab, loaded once at least one pool is drawn.
export async function openAdminPools(page, id) {
  await page.goto(`/admin/competition/${encodeURIComponent(id)}/pools`);
  await expect(poolBlocks(page).first()).toBeVisible();
}

// The public viewer's Pools tab.
export async function openViewerPools(page, id) {
  await openViewer(page, id);
  await page.locator('.viewer__tab', { hasText: /^Pools$/ }).tap();
  await expect(poolBlocks(page).first()).toBeVisible();
}

// A team's standings row, found by the team name its cell shows.
export const teamRow = (page, team) => page.locator('table.pool__table tbody tr')
  .filter({ has: page.locator('.pool__player-name', { hasText: team }) });

// Everything a row says: its eight figures by name, the rank badge ("1st"),
// (lower-cased: the page upper-cases it with CSS), and whether the row is marked as advancing to the knockout.
export async function rowFigures(page, team) {
  const row = teamRow(page, team);
  await expect(row).toHaveCount(1);
  const cells = (await row.locator('td.num').allInnerTexts()).map((t) => t.trim());
  const figures = Object.fromEntries(FIGURES.map((f, i) => [f, cells[i] === undefined ? NaN : Number(cells[i])]));
  const badge = row.locator('.rank-badge');
  return {
    ...figures,
    rank: (await badge.count()) ? (await badge.first().innerText()).trim().toLowerCase() : '',
    advancing: ((await row.getAttribute('class')) || '').split(/\s+/).includes('advancing'),
  };
}

// An INDIVIDUAL pool's row carries five figures, not the team table's eight:
// W L D (fights won, lost, drawn: hikiwake) and PW PL (points won, lost: ippons
// scored and conceded). Beside them it reads the rank badge, whether the row is
// marked as advancing, and whether it is marked tied (the amber row: a tie at a
// place still to be broken; a group a tie-break bout already ordered is not).
export const INDIVIDUAL_FIGURES = ['W', 'L', 'D', 'PW', 'PL'];

export async function playerRowFigures(page, name) {
  const row = teamRow(page, name);
  await expect(row).toHaveCount(1);
  const cells = (await row.locator('td.num').allInnerTexts()).map((t) => t.trim());
  const badge = row.locator('.rank-badge');
  const classes = ((await row.getAttribute('class')) || '').split(/\s+/);
  return {
    ...Object.fromEntries(INDIVIDUAL_FIGURES.map((f, i) => [f, cells[i] === undefined ? NaN : Number(cells[i])])),
    rank: (await badge.count()) ? (await badge.first().innerText()).trim().toLowerCase() : '',
    advancing: classes.includes('advancing'),
    tied: classes.includes('pool__row--tied'),
  };
}

// The names of the teams in a pool, in the table's (draw) order.
export async function poolTeams(block) {
  return (await block.locator('table.pool__table tbody .pool__player-name').allInnerTexts())
    .map((t) => t.replace(/\s+/g, ' ').trim());
}
