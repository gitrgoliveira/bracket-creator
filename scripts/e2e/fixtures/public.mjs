// The public surfaces: the competition viewer (/competition/:id) a spectator
// opens on a phone, and the TV board (/display?court=X) the venue projects.
// Neither needs a sign-in; call these on a context that never signed in.
import { expect } from '@playwright/test';

export async function openViewer(page, id) {
  await page.goto(`/competition/${encodeURIComponent(id)}`);
  await expect(page.locator('.viewer__body')).toBeVisible();
}

// A row of the viewer's "Recent results" list naming both competitors.
// The list is the `.vsched` block that follows its section title.
export function recentResult(page, { shiro, aka }) {
  return page.locator('.section-title', { hasText: /^Recent results$/ })
    .locator('xpath=following-sibling::div[contains(@class, "vsched")][1]')
    .locator('button.vsched-item')
    .filter({ hasText: shiro })
    .filter({ hasText: aka });
}

export const tvBoard = (page) => page.getByTestId('tv-display-root');

export async function openTvBoard(page, court) {
  await page.goto(`/display?court=${encodeURIComponent(court)}`);
  await expect(tvBoard(page)).toContainText(`SHIAIJO ${court}`);
}

// ------------------------------------------------- search, watchlist, cards --
//
// The home's watchlist picker (viewer_watchlist.jsx WatchPicker) and the
// schedule page's filter (viewer_schedule.jsx PlayerMultiFilter, with the text
// chip that filters the rows) both apply the competitor-number rule of
// web-mobile/js/competitor_search.jsx (bc-nsrc): the whole number, or the
// competition's prefix. They share the `.pmf` markup.

// The home's picker box, whether it reads "Add a name, dojo or number to
// watch…" (empty watchlist) or "Add another name, dojo or number…".
export const watchInput = (page) => page.locator('.pmf__input[aria-label^="Add a"]');
// The schedule page's filter box.
export const scheduleFilterInput = (page) => page.getByLabel('Filter by player, team, dojo or number', { exact: true });

// A competitor option of an open picker: not a dojo row (WatchPicker) and not
// the "Match "x" in any name, dojo or number" row (PlayerMultiFilter).
const PLAYER_OPTION = '.pmf__dropdown .pmf__option:not(.pmf__option--dojo):not(.pmf__option--text)';
export const NATURAL = (a, b) => a.localeCompare(b, undefined, { numeric: true });

// What the open picker lists, competitors only: { numbers, names }, each sorted
// (numbers naturally, so K2 precedes K10). WatchPicker draws a number chip
// (`.num-prefix`) and the name in `.numbered-name__text`; PlayerMultiFilter
// writes "K1 · Akira Tanaka" in one string.
export async function pickerEntries(page) {
  const rows = await pickerPairs(page);
  return {
    numbers: rows.map((r) => r.number).filter(Boolean).sort(NATURAL),
    names: rows.map((r) => r.name).sort(NATURAL),
  };
}

// The same options as [{ number, name }], in the order they are listed.
export async function pickerPairs(page) {
  return page.locator(PLAYER_OPTION).evaluateAll((els) => els.map((el) => {
    const chip = el.querySelector('.num-prefix');
    const text = el.querySelector('.numbered-name__text');
    if (text) return { number: chip ? chip.textContent.trim() : '', name: text.textContent.trim() };
    const joined = el.querySelector('.pmf__opt-name').textContent.trim();
    const m = /^(\S+) · (.+)$/.exec(joined);
    return m ? { number: m[1], name: m[2] } : { number: '', name: joined };
  }));
}

// The competitor options of an open picker, to tap one.
export const pickerOption = (page, name) => page.locator(PLAYER_OPTION).filter({ hasText: name });

// K1..K<n>: the numbers a draw with this prefix and `n` competitors hands out.
export const numbersOf = (prefix, n) => Array.from({ length: n }, (_, i) => `${prefix}${i + 1}`).sort(NATURAL);

// The schedule page's own count line ("18 matches of 18") and its rows.
export const scheduleCount = (page) => page.getByText(/^\d+ match(es)? of \d+$/);
export const scheduleRows = (page) => page.locator('button.tw-match');
// A schedule row naming both competitors.
export const scheduleRow = (page, { shiro, aka }) => scheduleRows(page).filter({ hasText: shiro }).filter({ hasText: aka });

// Type a query in the schedule's filter and press Enter: it becomes a text chip
// that filters the match rows by name, dojo or number (matchMentions). Returns
// once the chip shows; clearScheduleText takes it off again.
export async function applyScheduleText(page, text) {
  const box = scheduleFilterInput(page);
  await box.fill(text);
  await box.press('Enter');
  await expect(page.locator('.pmf__chip--text')).toContainText(`"${text}"`);
}
export async function clearScheduleText(page) {
  await page.locator('.pmf__chip--text').getByRole('button', { name: 'Remove' }).tap();
  await expect(page.locator('.pmf__chip--text')).toHaveCount(0);
}

// The watchlist hero (WatchHeroCard) and the button inside it that opens the
// match card.
export const watchHero = (page) => page.getByTestId('watch-hero');
export const watchHeroMatch = (page) => watchHero(page).locator('.wl-hero__sides--btn');

// A competition page's tab (Overview, Pools, Bracket).
export const competitionTab = (page, label) => page.locator('.viewer__tab', { hasText: new RegExp(`^${label}$`) });

// The doors a spectator opens a match card through, besides the schedule's own
// rows: Home's NOW list, a pool's match list, and an Up next row (Home's or a
// competition's Overview). Where one list holds several matches the row is
// named by both competitors.
export const homeNowRow = (page) => page.locator('.hero-running .vsched-item');
export const poolRow = (page, { shiro, aka }) => page.locator('button.pool-match-numbered-row').filter({ hasText: shiro }).filter({ hasText: aka });
export const upNextRow = (page, { shiro, aka }) => page.locator('button.vsched-item').filter({ hasText: shiro }).filter({ hasText: aka });

// The Overview's inline cards (officiated tournaments, where no dialog opens
// there): the one an Up next row expanded (it has a Close button) and the ON NOW
// card of the running match (it has none).
export const expandedCard = (page) => page.locator('.match-detail-card').filter({ has: page.getByRole('button', { name: 'Close' }) });
export const nowCard = (page) => page.locator('.match-detail-card').filter({ hasNot: page.getByRole('button', { name: 'Close' }) });

// The match card (MatchViewerModal): a dialog named "Match: <shiro> vs <aka>".
export const matchCard = (page) => page.getByRole('dialog', { name: /^Match:/ });

// The ippon letters the card shows per side, sorted ("DM", "K"): a side has two
// slots, each holding a letter once scored and nothing before.
export async function cardMarks(card) {
  const marks = card.getByTestId('sub-marks');
  const letters = async (loc) => (await loc.allTextContents()).map((t) => t.trim()).filter(Boolean).sort().join('');
  return {
    shiro: await letters(marks.locator('.msb-slots:not(.msb-slots--aka) .msb-slot')),
    aka: await letters(marks.locator('.msb-slots--aka .msb-slot')),
  };
}
