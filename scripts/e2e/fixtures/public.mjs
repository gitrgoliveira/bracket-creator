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
