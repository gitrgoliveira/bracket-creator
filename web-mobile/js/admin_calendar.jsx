// admin_calendar.jsx: The competition calendar page and pure layout helper.
// Displays competitions placed across tournament days and shiaijo lanes.

import { formatClockHHMM, overlaps } from './calendar_rules.jsx';
import {
  LONG_PRESS_MS, PX_PER_MINUTE, MoveDialog, dropTarget, isDrawn, isMovable, moveErrorMessage,
  saveMove, shiftClock, shiftLanes, undoMove,
} from './calendar_move.jsx';
import { readRetimePreview } from './retime_warning.jsx';
import { pushToast } from './toast_queue.jsx';

const { useState, useEffect, useRef, useMemo, useCallback } = React;

export const MOUSE_SLOP_PX = 6;
export const TOUCH_SLOP_PX = 10;
export const MIN_LANE_PX = 110;
export const CLOCK_COL_PX = 52;
export const PHONE_MAX_PX = 720;
export const PHONE_LANE_PX = 76;
export const PLACEHOLDER_MIN_PX = 52;
// How far a block that overlaps an earlier one on its lane is inset from the
// left (a quarter of the lane at most, so a phone's narrow lane keeps room).
export const CASCADE_PX = 24;
// A block that starts this close below an earlier one it overlaps would hide
// the earlier one's name and time, so it is inset to half the earlier block.
export const NEAR_START_PX = 60;
// Under this much visible width a touch tablet's 44 px Move button and a
// readable name do not fit side by side, so Move goes on a row of its own
// under the name. (A touch phone shows no Move button at all.)
export const NARROW_BLOCK_PX = 120;
// A touch screen's Move row: the 44 px button and the 2 px under it. Beside
// the name (a wide block) Move needs that much height; under it (a narrow
// block) it needs the name's row as well: 6 px padding, one 15 px name line
// (12 px at 1.25) and the block's 1 px border.
export const MOVE_ROW_PX = 46;
export const NAME_ROW_PX = 22;

/**
 * Does a touch tablet's Move button fit in a block this tall? A block is drawn
 * at its real length, so one too short for it shows its name only, opens on a
 * tap and is moved by press and hold (or the M key) instead.
 */
export function touchMoveFits(height, narrow) {
  return height >= (narrow ? NAME_ROW_PX + MOVE_ROW_PX : MOVE_ROW_PX);
}
export const REFRESH_DEBOUNCE_MS = 1000;
export { PX_PER_MINUTE, shiftLanes };

const CALENDAR_SSE_EVENTS = new Set([
  'tournament_updated',
  'schedule_updated',
  'match_updated',
  'draw_generated',
  'draw_discarded',
  'competition_started',
  'competition_completed',
  'participants_updated',
  'resync_required',
]);

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

// shortDayDate formats the tournament day dateStr (DD-MM-YYYY, the shape the
// calendar's days carry), moved on by addDays, as "Sat 16 May"; null when the
// string is not a DD-MM-YYYY date.
function shortDayDate(dateStr, addDays) {
  const m = typeof dateStr === 'string' ? /^(\d{2})-(\d{2})-(\d{4})$/.exec(dateStr) : null;
  if (!m) return null;
  const d = new Date(Date.UTC(Number(m[3]), Number(m[2]) - 1, Number(m[1]) + addDays, 12, 0, 0));
  return `${WEEKDAYS[d.getUTCDay()]} ${d.getUTCDate()} ${MONTHS[d.getUTCMonth()]}`;
}

/**
 * Formats a tournament day's DD-MM-YYYY date as "Sat 16 May"; '' when it is not one.
 */
export function formatDayDate(dateStr) {
  return shortDayDate(dateStr, 0) || '';
}

/**
 * Formats the day after a tournament day's DD-MM-YYYY date as "Sun 17 May".
 */
export function getNextDayFormatted(dateStr) {
  return shortDayDate(dateStr, 1) || 'the next day';
}

/**
 * Formats an overlap duration into "2h 19m" or "28 min".
 */
export function formatOverlapDuration(durationMins) {
  if (durationMins >= 60) {
    const h = Math.floor(durationMins / 60);
    const m = durationMins % 60;
    return m > 0 ? `${h}h ${m}m` : `${h}h`;
  }
  return `${durationMins} min`;
}

/**
 * Explains why an unused pause was dropped from the schedule.
 */
export function formatUnusedPauseReason(reason) {
  switch (reason) {
    case 'no_start_time':
      return 'it has no start time.';
    case 'no_length':
      return 'it has no length.';
    case 'day_not_in_tournament':
      return 'its day is outside the tournament.';
    case 'unreadable_start_time':
      return 'its start time could not be read.';
    case 'no_tournament_date':
      return 'the tournament has no date.';
    default:
      return 'it cannot be placed on the schedule.';
  }
}

/**
 * Explains why a competition was not placed on the schedule.
 */
export function formatNotPlacedReason(reason) {
  switch (reason) {
    case 'no_start_time':
      return 'Needs a start time.';
    case 'no_date':
      return 'Needs a date.';
    case 'date_outside_tournament':
      return 'Date is outside the tournament.';
    case 'draw_unreadable':
      return 'Draw could not be read.';
    default:
      return 'Cannot be placed on the calendar.';
  }
}


/**
 * The line naming the competitions whose settings could not be read, by the
 * ids the calendar lists, so a missing one is never taken for one not made.
 */
export function unreadableCompetitionsText(ids) {
  if (ids.length === 1) {
    return `The settings of competition ${ids[0]} could not be read, so it is not on the calendar. Check its config.md file.`;
  }
  return `The settings of ${ids.length} competitions could not be read (${ids.join(', ')}), so they are not on the calendar. Check their config.md files.`;
}

/**
 * Computes whole-hour range: earliest block start less 1 h to latest end plus 1 h.
 */
export function computeHourRange(calendar) {
  let earliestStart = null;
  let latestEnd = null;

  const comps = calendar?.competitions || [];
  for (const c of comps) {
    if (!c?.placement?.placed || !c.placement.windows) continue;
    for (const w of Object.values(c.placement.windows)) {
      const s = w.start ?? w.Start;
      const e = w.end ?? w.End;
      if (typeof s === 'number') {
        if (earliestStart === null || s < earliestStart) earliestStart = s;
      }
      if (typeof e === 'number') {
        if (latestEnd === null || e > latestEnd) latestEnd = e;
      }
    }
  }

  const days = calendar?.days || [];
  for (const d of days) {
    for (const b of (d?.breaks || [])) {
      const s = b.startMins ?? b.StartMins;
      const e = b.endMins ?? b.EndMins;
      if (typeof s === 'number') {
        if (earliestStart === null || s < earliestStart) earliestStart = s;
      }
      if (typeof e === 'number') {
        if (latestEnd === null || e > latestEnd) latestEnd = e;
      }
    }
  }

  if (earliestStart === null) earliestStart = 9 * 60; // 09:00 default
  if (latestEnd === null) latestEnd = 17 * 60; // 17:00 default

  const startHour = Math.max(0, Math.floor((earliestStart - 60) / 60));
  let endHour = Math.min(24, Math.ceil((latestEnd + 60) / 60));
  if (endHour <= startHour) endHour = Math.min(24, startHour + 1);

  const hours = [];
  for (let h = startHour; h <= endHour; h++) {
    hours.push(h);
  }

  return {
    startHour,
    endHour,
    hours,
    totalMinutes: (endHour - startHour) * 60,
  };
}

/**
 * Computes how many days can be shown side-by-side (4-day rule).
 */
export function computeDaysShown({ width, frameWidth, shiaijoCount, tournamentDays, phone = false }) {
  const isPhone = phone || (typeof width === 'number' && width <= PHONE_MAX_PX);
  if (isPhone) return 1;
  const numDays = Math.max(1, tournamentDays || 1);
  const numCourts = Math.max(1, shiaijoCount || 1);
  const gridWidth = Math.max(0, (frameWidth ?? width ?? 1024) - CLOCK_COL_PX);
  const denom = numCourts * MIN_LANE_PX;
  const fits = Math.floor(gridWidth / denom);
  const maxAllowed = Math.min(4, numDays);
  return Math.max(1, Math.min(fits, maxAllowed));
}

/**
 * Computes paging state for tournament days.
 */
export function computePaging({ days, daysShown, page = 0 }) {
  const dayList = Array.isArray(days) ? days : [];
  const tournamentDays = dayList.length;
  const numShown = Math.max(1, daysShown || 1);
  const totalPages = Math.max(1, Math.ceil(tournamentDays / numShown));
  const currentPage = Math.max(0, Math.min(page, totalPages - 1));
  const startIndex = currentPage * numShown;
  const endIndex = Math.min(tournamentDays, startIndex + numShown);
  const visibleDays = dayList.slice(startIndex, endIndex);
  const hasPrev = currentPage > 0;
  const hasNext = currentPage < totalPages - 1;

  let label = '';
  if (tournamentDays > 1) {
    if (endIndex - startIndex <= 1) {
      label = `Day ${startIndex + 1} of ${tournamentDays}`;
    } else {
      label = `Days ${startIndex + 1} to ${endIndex} of ${tournamentDays}`;
    }
  }

  return {
    totalPages,
    currentPage,
    startIndex,
    endIndex,
    visibleDays,
    hasPrev,
    hasNext,
    label,
  };
}

// joinCourts lists shiaijo letters as "A", "A and B" or "A, B and C".
function joinCourts(list) {
  if (list.length <= 1) return list[0] || '';
  return `${list.slice(0, -1).join(', ')} and ${list[list.length - 1]}`;
}

/**
 * Detects overlaps on days that are not on the currently displayed page.
 */
export function detectOffScreenOverlaps({ calendar, currentPageDays }) {
  const visibleDayNums = new Set((currentPageDays || []).map(d => d.day));
  const comps = calendar?.competitions || [];
  const compMap = new Map(comps.map(c => [c.id, c]));

  const overlapsByDay = new Map();

  for (const c of comps) {
    if (!c?.placement?.placed || !Array.isArray(c.clashPartners) || c.clashPartners.length === 0) {
      continue;
    }
    const day = c.placement.day;
    if (day === undefined || day === null) continue;

    for (const partnerId of c.clashPartners) {
      const partner = compMap.get(partnerId);
      if (!partner?.placement?.placed) continue;
      const cWindows = c.placement.windows || {};
      const pWindows = partner.placement.windows || {};
      for (const court of Object.keys(cWindows)) {
        if (pWindows[court]) {
          const w1 = { start: cWindows[court].start ?? cWindows[court].Start, end: cWindows[court].end ?? cWindows[court].End };
          const w2 = { start: pWindows[court].start ?? pWindows[court].Start, end: pWindows[court].end ?? pWindows[court].End };
          if (overlaps(w1, w2)) {
            if (!overlapsByDay.has(day)) overlapsByDay.set(day, new Set());
            overlapsByDay.get(day).add(court);
          }
        }
      }
    }
  }

  const offScreen = [];
  for (const [day, courtsSet] of overlapsByDay.entries()) {
    if (!visibleDayNums.has(day)) {
      const courtList = Array.from(courtsSet).sort();
      const date = (calendar?.days || []).find((d) => d.day === day)?.date;
      const dated = formatDayDate(date);
      offScreen.push({
        day,
        courts: courtList,
        label: `Overlap on ${dated ? `${dated} (Day ${day})` : `Day ${day}`}, Shiaijo ${joinCourts(courtList)}`,
      });
    }
  }

  offScreen.sort((a, b) => a.day - b.day);
  return offScreen;
}

/**
 * Computes block coordinates and clipping for placed competitions.
 */
export function computeBlocksLayout({ calendar, visibleDays, courts, laneWidth, startHour, endHour }) {
  const visibleDayNums = new Set((visibleDays || []).map(d => d.day));
  const comps = calendar?.competitions || [];
  const compMap = new Map(comps.map(c => [c.id, c]));
  const placedBlocks = [];

  for (const c of comps) {
    if (!c?.placement?.placed) continue;
    const day = c.placement.day;
    if (!visibleDayNums.has(day)) continue;

    const compCourts = c.placement.courts || [];
    const courtIndices = compCourts.map(ct => courts.indexOf(ct)).filter(idx => idx !== -1);
    const minCourtIdx = courtIndices.length > 0 ? Math.min(...courtIndices) : 0;
    const maxCourtIdx = courtIndices.length > 0 ? Math.max(...courtIndices) : 0;
    const laneSpan = Math.max(1, maxCourtIdx - minCourtIdx + 1);

    const left = minCourtIdx * laneWidth;
    const width = laneSpan * laneWidth;

    const windows = c.placement.windows || {};
    const starts = Object.values(windows).map(w => w.start ?? w.Start).filter(v => typeof v === 'number');
    const ends = Object.values(windows).map(w => w.end ?? w.End).filter(v => typeof v === 'number');
    const startMins = starts.length > 0 ? Math.min(...starts) : (startHour * 60);
    const endMins = ends.length > 0 ? Math.max(...ends) : (startMins + 60);

    const isPlaceholder = c.placement.lengthSource === 'minimum';
    const effectiveStart = Math.max(startMins, startHour * 60);
    const maxRangeEnd = Math.min(1440, endHour * 60);
    const effectiveEnd = Math.min(endMins, maxRangeEnd);

    const top = (effectiveStart - startHour * 60) * PX_PER_MINUTE;
    const rawHeight = Math.max(0, (effectiveEnd - effectiveStart) * PX_PER_MINUTE);
    const height = isPlaceholder ? Math.max(PLACEHOLDER_MIN_PX, rawHeight) : rawHeight;

    const pastMidnight = !!c.placement.pastMidnight || endMins > 1440;
    const hasOverlap = Array.isArray(c.clashPartners) && c.clashPartners.length > 0;

    // Resolve overlap detail (partner name, overlapping courts, duration)
    let overlapDetail = null;
    if (hasOverlap) {
      let maxOverlapMins = 0;
      let worstPartner = null;
      const clashingCourts = new Set();
      for (const pId of c.clashPartners) {
        const partner = compMap.get(pId);
        if (!partner?.placement?.windows) continue;
        for (const [court, winA] of Object.entries(windows)) {
          const winB = partner.placement.windows[court];
          if (!winB) continue;
          const wA = { start: winA.start ?? winA.Start, end: winA.end ?? winA.End };
          const wB = { start: winB.start ?? winB.Start, end: winB.end ?? winB.End };
          if (overlaps(wA, wB)) {
            clashingCourts.add(court);
            const oDuration = Math.min(wA.end, wB.end) - Math.max(wA.start, wB.start);
            if (oDuration > maxOverlapMins) {
              maxOverlapMins = oDuration;
              worstPartner = partner;
            }
          }
        }
      }
      if (worstPartner) {
        overlapDetail = {
          partnerName: worstPartner.name,
          courtsStr: joinCourts(Array.from(clashingCourts).sort()),
          durationStr: formatOverlapDuration(maxOverlapMins),
        };
      }
    }

    placedBlocks.push({
      comp: c,
      day,
      minCourtIdx,
      maxCourtIdx,
      laneSpan,
      left,
      width,
      startMins,
      endMins,
      effectiveStart,
      effectiveEnd,
      top,
      height,
      isPlaceholder,
      short: rawHeight < PLACEHOLDER_MIN_PX,
      pastMidnight,
      hasOverlap,
      overlapDetail,
      cascadeDepth: 0,
    });
  }

  return cascadeBlocks(placedBlocks, laneWidth);
}

/**
 * Keeps every block under root to whole lines. A block is as tall as its
 * minutes, so its text can run past its foot; the line the foot would cut is
 * clamped to the lines that fit and the lines after it are left out. The
 * name always stays, and a short block is left alone (it shows one line at
 * rest and lifts on hover or focus). A block that shows its name only (too
 * short for a touch screen's Move button) keeps the whole name lines that fit,
 * one at least. Their aria-label still says it all.
 */
export function fitBlockLines(root) {
  if (!root) return;
  root.querySelectorAll('.cal-block:not(.cal-block--short) .cal-block__open, .cal-block--name-only .cal-block__open').forEach((open) => {
    const content = open.querySelector('.cal-block__content');
    if (!content) return;
    const [name, ...lines] = Array.from(content.children);
    if (open.closest?.('.cal-block')?.classList.contains('cal-block--name-only')) {
      lines.forEach((el) => { el.style.display = 'none'; });
      if (!name || open.clientHeight === 0) return;
      const pad = getComputedStyle(open);
      const avail = open.clientHeight - (parseFloat(pad.paddingTop) || 0) - (parseFloat(pad.paddingBottom) || 0);
      const lineH = parseFloat(getComputedStyle(name).lineHeight) || 15;
      name.style.webkitLineClamp = String(Math.max(1, Math.floor(avail / lineH)));
      return;
    }
    if (name?.style.webkitLineClamp) name.style.webkitLineClamp = '';
    lines.forEach((el) => {
      el.style.display = '';
      el.style.webkitLineClamp = '';
      el.style.webkitBoxOrient = '';
      el.style.overflow = '';
    });
    if (open.clientHeight === 0) return;
    const avail = open.clientHeight - (parseFloat(getComputedStyle(open).paddingBottom) || 0);
    let cut = false;
    lines.forEach((el) => {
      if (cut) { el.style.display = 'none'; return; }
      const bottom = el.offsetTop + el.offsetHeight;
      if (bottom <= avail) return;
      cut = true;
      const lineH = parseFloat(getComputedStyle(el).lineHeight) || 14;
      const fit = Math.floor((avail - el.offsetTop) / lineH);
      if (fit < 1) { el.style.display = 'none'; return; }
      el.style.display = '-webkit-box';
      el.style.webkitLineClamp = String(fit);
      el.style.webkitBoxOrient = 'vertical';
      el.style.overflow = 'hidden';
    });
  });
}

/**
 * The lane a pause band's name goes in: the first one no block covers during
 * the pause, or the first lane when every lane is covered (each block then
 * names the pause in its own strip).
 */
export function pauseLabelLane(blocks, laneCount, start, end) {
  const covered = new Set();
  for (const b of blocks || []) {
    if (b.startMins < end && start < b.endMins) {
      for (let i = b.minCourtIdx; i <= b.maxCourtIdx; i++) covered.add(i);
    }
  }
  for (let i = 0; i < laneCount; i++) if (!covered.has(i)) return i;
  return 0;
}

/**
 * Cascades blocks that overlap on a lane, as the approved calendar draws a
 * clash: the later block sits over the earlier one, inset from the left, so
 * the earlier one's name and time stay readable beside it. Blocks come back in
 * start order within each day, so a later start is drawn on top.
 */
export function cascadeBlocks(blocks, laneWidth) {
  const ordered = [...blocks].sort((a, b) => (a.day - b.day)
    || (a.startMins - b.startMins)
    || String(a.comp?.name || '').localeCompare(String(b.comp?.name || '')));
  const step = Math.max(8, Math.min(CASCADE_PX, Math.floor((laneWidth || 0) / 4)));
  const placed = [];
  const clashes = (p, b) => p.day === b.day
    && p.minCourtIdx <= b.maxCourtIdx && b.minCourtIdx <= p.maxCourtIdx
    && p.top < b.top + b.height && b.top < p.top + p.height;
  for (const b of ordered) {
    let depth = 0;
    let minLeft = b.left;
    for (const p of placed) {
      if (!clashes(p, b)) continue;
      depth = Math.max(depth, p.cascadeDepth + 1);
      // Starting at about the same time (or at the same time) it would cover
      // the earlier block's name and time, so it takes the right half of the
      // earlier block, at its real start.
      if (b.top - p.top < NEAR_START_PX) minLeft = Math.max(minLeft, p.left + Math.floor(p.width / 2));
    }
    // Half of the block keeps its width, and each deeper clash one step more,
    // so a third block that starts with two others still stands out from the
    // second; a quarter of the block always stays.
    const maxInset = Math.min(Math.floor(b.width / 2) + Math.max(0, depth - 1) * step, Math.floor((b.width * 3) / 4));
    const inset = Math.min(Math.max(depth * step, minLeft - b.left), maxInset);
    placed.push({ ...b, cascadeDepth: depth, left: b.left + inset, width: b.width - inset, coveredRight: 0 });
  }
  // The part of each block a later one covers on the right: its text and its
  // Move button keep to the part that stays in view.
  for (let i = 0; i < placed.length; i++) {
    const b = placed[i];
    for (let j = i + 1; j < placed.length; j++) {
      const p = placed[j];
      if (!clashes(b, p) || p.left <= b.left) continue;
      const covered = Math.max(0, Math.min(b.left + b.width, p.left + p.width) - p.left);
      if (p.left + p.width >= b.left + b.width) b.coveredRight = Math.max(b.coveredRight, covered);
    }
    b.coveredRight = Math.min(b.coveredRight, b.width - Math.floor(b.width / 2));
  }
  return placed;
}

/**
 * Pure layout helper for the tournament calendar. width is the page's width,
 * which decides the phone layout; frameWidth, when known, is the inside of the
 * grid's frame, which the lanes share (the page's padding and the frame's
 * border are not theirs, or lanes that fit would still scroll sideways).
 */
export function calendarLayout({ calendar, width = 1024, frameWidth, page = 0, phone = false }) {
  const courts = calendar?.courts || [];
  const days = calendar?.days || [];
  const shiaijoCount = courts.length;
  const tournamentDays = days.length;

  const isPhone = phone || (typeof width === 'number' && width <= PHONE_MAX_PX);
  const daysShown = computeDaysShown({ width, frameWidth, shiaijoCount, tournamentDays, phone: isPhone });

  const gridWidth = Math.max(0, (frameWidth ?? width) - CLOCK_COL_PX);
  let laneWidth;
  if (isPhone) {
    laneWidth = Math.max(PHONE_LANE_PX, shiaijoCount > 0 ? Math.floor(gridWidth / shiaijoCount) : PHONE_LANE_PX);
  } else {
    const totalLanesShown = daysShown * shiaijoCount;
    laneWidth = Math.max(MIN_LANE_PX, totalLanesShown > 0 ? Math.floor(gridWidth / totalLanesShown) : MIN_LANE_PX);
  }

  const dayGridWidth = shiaijoCount * laneWidth;
  const lanesScrollSideways = isPhone
    ? (shiaijoCount * PHONE_LANE_PX > gridWidth)
    : (dayGridWidth > gridWidth / daysShown);

  const hourRange = computeHourRange(calendar);
  const paging = computePaging({ days, daysShown, page });
  const offScreenOverlaps = detectOffScreenOverlaps({ calendar, currentPageDays: paging.visibleDays });

  const placedBlocks = computeBlocksLayout({
    calendar,
    visibleDays: paging.visibleDays,
    courts,
    laneWidth,
    startHour: hourRange.startHour,
    endHour: hourRange.endHour,
  });

  return {
    isPhone,
    daysShown,
    laneWidth,
    dayGridWidth,
    lanesScrollSideways,
    hourRange,
    paging,
    offScreenOverlaps,
    placedBlocks,
  };
}

// Stands in for a shell component (AdminTopbar, Breadcrumbs) not yet on window.
const RenderNothing = () => null;

// Pairs each item with a render key made from its own content. Two pauses can
// be identical (two unnamed rows with no start, or the same pause entered
// twice), so a repeat takes its occurrence count after the key and never
// shares one.
export function withContentKeys(items, keyOf) {
  const seen = new Map();
  return items.map((item) => {
    const base = keyOf(item);
    const n = seen.get(base) || 0;
    seen.set(base, n + 1);
    return { key: n ? `${base}#${n}` : base, item };
  });
}

/**
 * AdminCalendarPage component.
 */
export function AdminCalendarPage({
  tournament,
  password,
  onBack,
  onLogout,
  onViewerMode,
  onOpenCompetition,
  onEditTournament,
  renderBlockExtras,
}) {
  const [calendar, setCalendar] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const [desktopPage, setDesktopPage] = useState(0);
  const [phoneDay, setPhoneDay] = useState(1);
  const [containerWidth, setContainerWidth] = useState(1024);
  const [frameWidth, setFrameWidth] = useState(undefined);

  const [isCoarse, setIsCoarse] = useState(() => {
    if (typeof window !== 'undefined' && window.matchMedia) {
      return window.matchMedia('(pointer: coarse)').matches;
    }
    return false;
  });

  const containerRef = useRef(null);
  const trayRef = useRef(null);
  const activeTabRef = useRef(null);
  const abortControllerRef = useRef(null);
  const debounceRef = useRef(null);
  const downPosRef = useRef(null);

  // Moving: a competition is moved by dragging its block (a mouse drags past a
  // few pixels, a touch screen presses and holds) or with its Move button.
  const [moveDialog, setMoveDialog] = useState(null); // { comp, initial }
  const [lastMove, setLastMove] = useState(null); // the move Undo would take back
  const [undoing, setUndoing] = useState(false);
  const [drag, setDrag] = useState(null); // the picked-up block, for its ghost
  const dragRef = useRef(null);
  const suppressClickRef = useRef(false);
  const refreshHeldRef = useRef(false);

  // Monitor coarse pointer media query
  useEffect(() => {
    if (typeof window === 'undefined' || !window.matchMedia) return;
    const mql = window.matchMedia('(pointer: coarse)');
    const handler = (e) => setIsCoarse(e.matches);
    if (mql.addEventListener) {
      mql.addEventListener('change', handler);
      return () => mql.removeEventListener('change', handler);
    } else if (mql.addListener) {
      mql.addListener(handler);
      return () => mql.removeListener(handler);
    }
  }, []);

  // Fetch calendar from API
  const fetchCal = useCallback(async () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
    }
    const controller = new AbortController();
    abortControllerRef.current = controller;

    setLoading(true);
    setError(null);

    try {
      const apiFn = (window.API && window.API.fetchCalendar) || null;
      if (!apiFn) {
        throw new Error('API client not available');
      }
      const data = await apiFn(password, controller.signal);
      setCalendar(data);
    } catch (err) {
      if (err.name === 'AbortError') return;
      setError(err.message || 'Could not load the calendar.');
    } finally {
      // A newer fetch aborted this one and owns the loading flag now.
      if (abortControllerRef.current === controller) setLoading(false);
    }
  }, [password]);

  // Initial fetch and SSE subscription
  useEffect(() => {
    fetchCal();

    const debouncedRefresh = () => {
      // The page holds still while a block is held; it reads again at the drop.
      if (dragRef.current?.active) {
        refreshHeldRef.current = true;
        return;
      }
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        fetchCal();
      }, REFRESH_DEBOUNCE_MS);
    };

    let unsub = () => {};
    if (window.API && typeof window.API.subscribeToEvents === 'function') {
      unsub = window.API.subscribeToEvents((event) => {
        if (event && CALENDAR_SSE_EVENTS.has(event.type)) {
          debouncedRefresh();
        }
      });
    }

    return () => {
      if (abortControllerRef.current) abortControllerRef.current.abort();
      if (debounceRef.current) clearTimeout(debounceRef.current);
      unsub();
    };
  }, [fetchCal]);

  // Track container width
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const updateWidth = () => {
      const width = el.clientWidth || window.innerWidth || 1024;
      setContainerWidth(width);
      // The lanes share the inside of the grid's frame: the page's padding
      // and the frame's border (1 px each side) are not theirs.
      const cs = typeof getComputedStyle === 'function' ? getComputedStyle(el) : null;
      const padding = cs ? (parseFloat(cs.paddingLeft) || 0) + (parseFloat(cs.paddingRight) || 0) : 0;
      setFrameWidth(el.clientWidth ? el.clientWidth - padding - 2 : undefined);
    };
    updateWidth();

    if (typeof ResizeObserver !== 'undefined') {
      const ro = new ResizeObserver(() => updateWidth());
      ro.observe(el);
      return () => ro.disconnect();
    } else {
      window.addEventListener('resize', updateWidth);
      return () => window.removeEventListener('resize', updateWidth);
    }
  }, [calendar]);

  // Keep phone tab scrolled into view
  useEffect(() => {
    if (activeTabRef.current) {
      activeTabRef.current.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'nearest' });
    }
  }, [phoneDay]);

  const isPhoneScreen = typeof containerWidth === 'number' && containerWidth <= PHONE_MAX_PX;
  const activePage = isPhoneScreen
    ? Math.max(0, (calendar?.days || []).findIndex(d => d.day === phoneDay))
    : desktopPage;

  const layout = useMemo(() => {
    return calendarLayout({
      calendar,
      width: containerWidth,
      frameWidth,
      page: activePage,
      phone: isPhoneScreen,
    });
  }, [calendar, containerWidth, frameWidth, activePage, isPhoneScreen]);

  const refitBlock = useCallback((e) => {
    const el = e.currentTarget;
    requestAnimationFrame(() => fitBlockLines(el));
  }, []);

  // A block shows only whole lines: the line its height cuts through is
  // clamped to the lines that fit, and the lines after it are left out.
  useEffect(() => {
    fitBlockLines(containerRef.current);
  }, [layout]);

  // Slop detection for click vs drag
  const handlePointerDown = (e) => {
    downPosRef.current = {
      x: e.clientX,
      y: e.clientY,
      pointerType: e.pointerType,
    };
  };

  const handleBlockClick = (e, compId) => {
    if (suppressClickRef.current) return;
    if (e.detail === 0) {
      onOpenCompetition?.(compId, 'overview');
      return;
    }
    const down = downPosRef.current;
    if (!down) {
      onOpenCompetition?.(compId, 'overview');
      return;
    }
    const dx = e.clientX - down.x;
    const dy = e.clientY - down.y;
    const dist = Math.sqrt(dx * dx + dy * dy);
    const slop = down.pointerType === 'touch' ? TOUCH_SLOP_PX : MOUSE_SLOP_PX;
    if (dist <= slop) {
      onOpenCompetition?.(compId, 'overview');
    }
  };

  const handleBlockKeyDown = (e, comp) => {
    if (e.target !== e.currentTarget) return;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      onOpenCompetition?.(comp.id, 'overview');
    } else if ((e.key === 'm' || e.key === 'M') && isMovable(comp)) {
      e.preventDefault();
      setMoveDialog({ comp });
    }
  };

  const dayLabelFor = (date) => {
    const info = (calendar?.days || []).find((d) => d.date === date);
    return info ? `${formatDayDate(date)} (Day ${info.day})` : date;
  };

  // Saves a move, tells the operator, and keeps what Undo needs. Throws when
  // the save is refused so the Move dialog can show why.
  const commitMove = async (args) => {
    const move = await saveMove({ password, ...args });
    setLastMove(move);
    pushToast(`Moved ${move.name} from ${dayLabelFor(move.before.date)} at ${move.before.startTime} to ${dayLabelFor(move.after.date)} at ${move.after.startTime}.`, 'success');
    setMoveDialog(null);
    fetchCal();
  };

  const commitMoveFromDrop = async (args) => {
    try {
      await commitMove(args);
    } catch (err) {
      pushToast(moveErrorMessage(err), 'error');
      if (err.code === 'config_changed') fetchCal();
    }
  };

  const handleUndo = async () => {
    if (!lastMove || undoing) return;
    setUndoing(true);
    try {
      await undoMove({ password, move: lastMove });
      pushToast(`Undone. ${lastMove.name} is back where it was.`, 'success');
      setLastMove(null);
      fetchCal();
    } catch (err) {
      pushToast(moveErrorMessage(err), 'error');
      // After a refusal the move can no longer be taken back safely.
      if (err.code === 'undo_stale' || err.code === 'config_changed') setLastMove(null);
      fetchCal();
    } finally {
      setUndoing(false);
    }
  };

  // A drop on a day, start and shiaijo. A drawn competition shows what its
  // matches would do first, unless no match changes its time.
  const handleDrop = async (comp, block, target) => {
    const dayInfo = (calendar?.days || []).find((d) => d.day === target.day);
    if (!dayInfo) return;
    const deltaMins = target.startMins - block.startMins;
    const courts = isDrawn(comp) ? (comp.placement.courts || []) : target.courts;
    if (!isDrawn(comp)) {
      await commitMoveFromDrop({ competitionId: comp.id, date: dayInfo.date, deltaMins, courts });
      return;
    }
    try {
      const comps = await window.API.fetchCompetitions();
      const fresh = (Array.isArray(comps) ? comps : []).find((c) => c.id === comp.id);
      const startTime = fresh ? shiftClock(fresh.startTime, deltaMins) : null;
      if (!fresh || !startTime) {
        pushToast('That would move the start out of the day. Pick a start time on the same day.', 'error');
        return;
      }
      const answer = await window.API.previewMove({ competitionId: comp.id, date: dayInfo.date, startTime, courts: fresh.courts || [] }, password);
      const preview = readRetimePreview(answer);
      if (preview && !preview.anyChange) {
        await commitMoveFromDrop({ competitionId: comp.id, date: dayInfo.date, startTime, courts: fresh.courts });
        return;
      }
      setMoveDialog({ comp, initial: { date: dayInfo.date, startTime, courts: fresh.courts || [] } });
    } catch (err) {
      pushToast(moveErrorMessage(err), 'error');
    }
  };

  const readGeometry = () => {
    const days = [];
    document.querySelectorAll('[data-cal-day]').forEach((el) => {
      const r = el.getBoundingClientRect();
      const num = Number(el.getAttribute('data-cal-day'));
      const info = (calendar?.days || []).find((d) => d.day === num);
      days.push({ day: num, left: r.left, top: r.top, right: r.right, bottom: r.bottom, breaks: info?.breaks || [] });
    });
    return { days, laneWidth: layout.laneWidth, startHour: layout.hourRange.startHour, courts: calendar?.courts || [] };
  };

  // Pointer down on a movable block: a mouse picks it up after a few pixels,
  // a touch after a press and hold (a scroll that starts first picks nothing
  // up). Esc puts it back.
  const handleBlockPointerDown = (e, comp, block) => {
    handlePointerDown(e);
    if (!isMovable(comp) || (e.pointerType === 'mouse' && e.button !== 0)) return;
    if (e.target.closest && e.target.closest('.cal-block__move-btn')) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const isTouch = e.pointerType === 'touch';
    const st = {
      comp, block, active: false, target: null,
      grabX: e.clientX - rect.left, grabY: e.clientY - rect.top, width: rect.width, height: rect.height,
      x: e.clientX, y: e.clientY, timer: null, cleanup: null,
    };
    dragRef.current = st;

    const compute = (x, y) => {
      const target = dropTarget({
        geometry: readGeometry(), blockLeft: x - st.grabX, blockTop: y - st.grabY,
        pointerX: x, pointerY: y, blockCourts: comp.placement.courts || [],
      });
      if (target && isDrawn(comp)) target.courts = comp.placement.courts || [];
      st.target = target;
      const dur = block.endMins - block.startMins;
      const info = target ? (calendar?.days || []).find((d) => d.day === target.day) : null;
      const label = target && info
        ? `${formatDayDate(info.date)}, ${formatClockHHMM(target.startMins)} to ${formatClockHHMM((target.startMins + dur) % 1440)}${isDrawn(comp) ? '' : `, Shiaijo ${target.courts.join(' and ')}`}`
        : 'Drop it on a day';
      setDrag({ comp, x: x - st.grabX, y: y - st.grabY, width: st.width, height: st.height, label, hint: target && isDrawn(comp) && target.courts.join() !== (comp.placement.courts || []).join() ? 'Its shiaijo stay as drawn.' : '' });
    };

    const pickUp = () => {
      if (dragRef.current !== st) return;
      st.active = true;
      st.timer = null;
      try { if (navigator.vibrate) navigator.vibrate(10); } catch (_e) { /* no vibration here */ }
      try { e.target.setPointerCapture?.(e.pointerId); } catch (_e) { /* capture is best effort */ }
      compute(st.x, st.y);
    };

    const stop = () => {
      if (st.timer) clearTimeout(st.timer);
      st.cleanup?.();
      if (dragRef.current === st) dragRef.current = null;
      setDrag(null);
      if (refreshHeldRef.current) {
        refreshHeldRef.current = false;
        fetchCal();
      }
    };

    const onMove = (ev) => {
      st.x = ev.clientX;
      st.y = ev.clientY;
      const dist = Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY);
      if (!st.active) {
        if (isTouch ? dist > TOUCH_SLOP_PX : dist > MOUSE_SLOP_PX) {
          if (isTouch) stop(); // the browser is scrolling
          else pickUp();
        }
        return;
      }
      compute(ev.clientX, ev.clientY);
    };
    const onTouchMove = (ev) => { if (st.active && ev.cancelable) ev.preventDefault(); };
    const onKey = (ev) => { if (ev.key === 'Escape' && st.active) { ev.preventDefault(); stop(); } };
    const onUp = (ev) => {
      const wasActive = st.active;
      const target = st.target;
      const movedPx = Math.hypot(ev.clientX - e.clientX, ev.clientY - e.clientY);
      stop();
      if (!wasActive) return;
      suppressClickRef.current = true;
      setTimeout(() => { suppressClickRef.current = false; }, 0);
      const same = target && target.day === comp.placement.day
        && target.startMins === block.startMins
        && (isDrawn(comp) || target.courts.join() === (comp.placement.courts || []).join());
      if (!target || same) {
        // A touch held and let go where it was opens the Move dialog.
        if (isTouch && movedPx <= TOUCH_SLOP_PX) setMoveDialog({ comp });
        return;
      }
      handleDrop(comp, block, target);
    };

    window.addEventListener('pointermove', onMove);
    window.addEventListener('pointerup', onUp);
    window.addEventListener('pointercancel', stop);
    window.addEventListener('keydown', onKey);
    if (isTouch) window.addEventListener('touchmove', onTouchMove, { passive: false });
    st.cleanup = () => {
      window.removeEventListener('pointermove', onMove);
      window.removeEventListener('pointerup', onUp);
      window.removeEventListener('pointercancel', stop);
      window.removeEventListener('keydown', onKey);
      window.removeEventListener('touchmove', onTouchMove);
    };
    if (isTouch) st.timer = setTimeout(pickUp, LONG_PRESS_MS);
  };

  useEffect(() => () => { dragRef.current?.cleanup?.(); }, []);

  const handleShowDay = (dayNum) => {
    if (isPhoneScreen) {
      setPhoneDay(dayNum);
    } else {
      const pageIndex = Math.floor((dayNum - 1) / layout.daysShown);
      setDesktopPage(pageIndex);
    }
  };

  const handleScrollToTray = () => {
    trayRef.current?.scrollIntoView({ behavior: 'smooth' });
  };

  const AdminTopbar = window.AdminTopbar || RenderNothing;
  const Breadcrumbs = window.Breadcrumbs || RenderNothing;

  // Loading state
  if (loading && !calendar) {
    return (
      <div className="app">
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
        <div className="page page--wide cal-page">
          <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />
          <div className="cal-state-card">
            <div className="cal-state-title">Loading the calendar…</div>
          </div>
        </div>
      </div>
    );
  }

  // Error / failed state
  if (error && !calendar) {
    return (
      <div className="app">
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
        <div className="page page--wide cal-page">
          <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />
          <div className="cal-state-card">
            <div className="cal-state-title">Could not load the calendar.</div>
            <div className="cal-state-sub">Check the connection to the tournament server.</div>
            <button type="button" className="btn btn--primary" onClick={fetchCal}>
              Try again
            </button>
          </div>
        </div>
      </div>
    );
  }

  // No tournament date / no days state
  if (!calendar?.days || calendar.days.length === 0) {
    return (
      <div className="app">
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
        <div className="page page--wide cal-page">
          <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />
          <div className="cal-state-card">
            <div className="cal-state-title">Set the tournament date first.</div>
            <div className="cal-state-sub">
              The tournament has no dates set yet. Add start and end dates in Edit tournament to place competitions on the calendar.
            </div>
            <button type="button" className="btn btn--primary" onClick={onEditTournament}>
              Edit tournament
            </button>
          </div>
        </div>
      </div>
    );
  }

  // No competitions at all state
  if (!calendar?.competitions || calendar.competitions.length === 0) {
    return (
      <div className="app">
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
        <div className="page page--wide cal-page">
          <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />
          <div className="cal-state-card">
            <div className="cal-state-title">No competitions yet.</div>
            <div className="cal-state-sub">Add one from the dashboard and it appears here on its day.</div>
            <button type="button" className="btn btn--primary" onClick={onBack}>
              Back to the dashboard
            </button>
          </div>
        </div>
      </div>
    );
  }

  const notPlacedComps = (calendar.competitions || []).filter(c => !c.placement?.placed);
  const unreadableComps = calendar.unreadableCompetitions || [];
  const placeholderCount = (calendar.competitions || []).filter(c => c.placement?.placed && c.placement.lengthSource === 'minimum').length;
  const unusedPauses = calendar.unusedPauses || [];

  // Set of day numbers that hold overlaps for phone tabs
  const daysWithOverlaps = new Set();
  for (const c of calendar.competitions) {
    if (c.placement?.placed && Array.isArray(c.clashPartners) && c.clashPartners.length > 0) {
      daysWithOverlaps.add(c.placement.day);
    }
  }

  const instructionCopy = isCoarse && isPhoneScreen
    ? 'Tap a competition to open it, or press and hold it to drag it to another time.'
    : isCoarse
    ? 'Tap Move on a competition to move it, or press and hold it to drag. Tap the competition to open it.'
    : 'Drag a competition to move it, or use its Move button. Click it to open it.';

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide cal-page" ref={containerRef}>
        <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />

        <div className="page-head">
          <div>
            <h1 className="page-head__title">Competition calendar</h1>
            <div className="cal-key-line">{instructionCopy}</div>
            {unreadableComps.length > 0 && (
              <div className="cal-unused-pauses">
                <div className="cal-unused-pause">
                  {unreadableCompetitionsText(unreadableComps)}
                </div>
              </div>
            )}
            {unusedPauses.length > 0 && (
              <div className="cal-unused-pauses">
                {withContentKeys(unusedPauses, (u) => `${u.name}|${u.reason}`).map(({ key, item: p }) => (
                  <div key={key} className="cal-unused-pause">
                    {p.name} is not used: {formatUnusedPauseReason(p.reason)}{' '}
                    <button type="button" className="btn btn--link" onClick={onEditTournament}>
                      Edit tournament
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>

        {/* Tray line above the grid */}
        {notPlacedComps.length > 0 && (
          <div className="cal-tray-banner">
            <span>
              {notPlacedComps.length === 1
                ? '1 competition is not on the calendar.'
                : `${notPlacedComps.length} competitions are not on the calendar.`}{' '}
              <button type="button" className="btn btn--link" onClick={handleScrollToTray}>
                Show them
              </button>
            </span>
          </div>
        )}

        {/* Off-screen overlap alert lines */}
        {layout.offScreenOverlaps.length > 0 && (
          <div className="cal-offscreen-overlaps">
            {layout.offScreenOverlaps.map(o => (
              <div key={o.day} className="cal-offscreen-overlap">
                <span>{o.label}</span>
                <button type="button" className="btn btn--link" onClick={() => handleShowDay(o.day)}>
                  Show Day {o.day}
                </button>
              </div>
            ))}
          </div>
        )}

        {/* Phone day tabs (one scrolling row) */}
        {layout.isPhone && calendar.days.length > 1 && (
          <div className="cal-daytabs day-tabs" role="tablist">
            {calendar.days.map(d => {
              const isActive = d.day === phoneDay;
              const hasWarnDot = daysWithOverlaps.has(d.day);
              return (
                <button
                  key={d.day}
                  ref={isActive ? activeTabRef : null}
                  type="button"
                  role="tab"
                  className={`day-tab ${isActive ? 'is-active' : ''}`}
                  aria-selected={isActive}
                  onClick={() => setPhoneDay(d.day)}
                >
                  Day {d.day}
                  {hasWarnDot && (
                    <>
                      <span className="cal-warn-dot" aria-hidden="true" />
                      <span className="sr-only">, has an overlap</span>
                    </>
                  )}
                </button>
              );
            })}
          </div>
        )}

        {/* Paging header for desktop when more days than daysShown */}
        {!layout.isPhone && calendar.days.length > layout.daysShown && (
          <div className="cal-paging-bar">
            <span>{layout.paging.label}</span>
            <button
              type="button"
              className="btn btn--icon-sm"
              disabled={!layout.paging.hasPrev}
              onClick={() => setDesktopPage(p => Math.max(0, p - 1))}
              aria-label="Previous days"
            >
              ‹
            </button>
            <button
              type="button"
              className="btn btn--icon-sm"
              disabled={!layout.paging.hasNext}
              onClick={() => setDesktopPage(p => p + 1)}
              aria-label="Next days"
            >
              ›
            </button>
          </div>
        )}

        {/* Calendar Grid Frame */}
        {lastMove && (
          <div className="cal-moved-line" role="status" data-testid="cal-moved-line">
            <span>Moved <strong>{lastMove.name}</strong> just now. Undo puts it back where it was.</span>
            <button type="button" className="btn btn--sm" onClick={handleUndo} disabled={undoing}>
              {undoing ? 'Undoing…' : 'Undo'}
            </button>
          </div>
        )}

        <div className="cal-grid-frame">
          {/* Clock column (sticky) */}
          <div className="cal-clock-col">
            <div className="cal-clock-header" />
            <div className="cal-lanes-header-spacer" />
            {layout.hourRange.hours.map(h => (
              <div key={h} className="cal-clock-cell">
                {String(h).padStart(2, '0')}:00
              </div>
            ))}
          </div>

          {/* Days container */}
          <div className="cal-days-container">
            {layout.paging.visibleDays.map(day => {
              const dayComps = (calendar.competitions || []).filter(c => c.placement?.placed && c.placement.day === day.day);
              const dayBlocks = layout.placedBlocks.filter(b => b.day === day.day);

              return (
                <div
                  key={day.day}
                  className="cal-day-col"
                  style={{ width: `${layout.dayGridWidth}px`, minWidth: `${layout.dayGridWidth}px` }}
                >
                  {/* Day header */}
                  <div className="cal-day-header">
                    <div className="cal-day-header__title">
                      {formatDayDate(day.date) || `Day ${day.day}`}
                    </div>
                    {dayComps.length === 0 && (
                      <div className="cal-day-header__empty">No competitions</div>
                    )}
                  </div>

                  {/* Shiaijo lanes header */}
                  <div className="cal-lanes-header">
                    {(calendar.courts || []).map(court => (
                      <div
                        key={court}
                        className="cal-lane-header"
                        style={{ width: `${layout.laneWidth}px`, minWidth: `${layout.laneWidth}px` }}
                      >
                        {court}
                      </div>
                    ))}
                  </div>

                  {/* Day body with pause bands and blocks */}
                  <div
                    className="cal-day-body"
                    data-cal-day={day.day}
                    style={{
                      height: `${layout.hourRange.totalMinutes * PX_PER_MINUTE}px`,
                      width: `${layout.dayGridWidth}px`,
                    }}
                  >
                    {/* Background lane lines */}
                    {(calendar.courts || []).map(court => (
                      <div
                        key={court}
                        className="cal-lane-col"
                        style={{ width: `${layout.laneWidth}px`, minWidth: `${layout.laneWidth}px` }}
                      >
                        {layout.hourRange.hours.map(h => (
                          <div key={h} className="cal-lane-hour-cell" />
                        ))}
                      </div>
                    ))}

                    {/* Day pause bands across all lanes (under blocks) */}
                    {withContentKeys(day.breaks || [], (x) => `${x.name}|${x.startMins ?? x.StartMins}-${x.endMins ?? x.EndMins}`).map(({ key: bandKey, item: b }) => {
                      const bStart = b.startMins ?? b.StartMins;
                      const bEnd = b.endMins ?? b.EndMins;
                      const bTop = (bStart - layout.hourRange.startHour * 60) * PX_PER_MINUTE;
                      const bHeight = (bEnd - bStart) * PX_PER_MINUTE;
                      // The band's name sits in the first lane no block covers
                      // during the pause, so a block never hides it.
                      const labelLane = pauseLabelLane(dayBlocks, (calendar.courts || []).length, bStart, bEnd);
                      return (
                        <div
                          key={bandKey}
                          className="cal-pause-band"
                          style={{
                            top: `${bTop}px`,
                            height: `${bHeight}px`,
                          }}
                        >
                          <span className="cal-pause-band__label" style={{ marginLeft: `${labelLane * layout.laneWidth}px` }}>
                            {b.name} {b.start} to {b.end}
                          </span>
                        </div>
                      );
                    })}

                    {/* Placed competition blocks */}
                    <div className="cal-blocks-layer">
                      {dayBlocks.map(block => {
                        const { comp } = block;
                        const isPlaceholder = block.isPlaceholder;
                        const isDraw = comp.placement?.lengthSource === 'draw';
                        const isExpected = comp.placement?.lengthSource === 'expected';
                        const isRunning = comp.status === 'pools' || comp.status === 'knockout';
                        const isCompleted = comp.status === 'completed';

                        let blockClass = 'cal-block';
                        if (isPlaceholder) blockClass += ' cal-block--placeholder';
                        else if (isDraw) blockClass += ' cal-block--draw';
                        else blockClass += ' cal-block--estimate';

                        if (block.short && !isPlaceholder) blockClass += ' cal-block--short';
                        // Too little of it in view for the name and a touch
                        // screen's Move button side by side: Move takes a row
                        // of its own at the foot.
                        const covered = block.coveredRight || 0;
                        const narrow = block.width - covered < NARROW_BLOCK_PX;
                        if (narrow) blockClass += ' cal-block--narrow';
                        // A touch phone shows no Move button: a tap opens the
                        // block, press and hold drags it (or, let go in place,
                        // opens the Move dialog), and the M key moves it. A
                        // touch tablet's block too short for Move shows its name
                        // only, as a phone's short block does.
                        const phoneTouch = isCoarse && layout.isPhone;
                        const moveShown = isMovable(comp) && !phoneTouch && (!isCoarse || touchMoveFits(block.height, narrow));
                        const extras = renderBlockExtras?.(comp);
                        if (moveShown) blockClass += ' cal-block--movable';
                        else if (phoneTouch ? block.short && !isPlaceholder : isMovable(comp)) blockClass += ' cal-block--name-only';
                        if (block.hasOverlap || block.pastMidnight) {
                          blockClass += ' cal-block--warn';
                        }

                        // Time span copy
                        let timeSpan = '';
                        if (!isPlaceholder) {
                          const startStr = formatClockHHMM(block.startMins);
                          const endStr = formatClockHHMM(block.endMins);
                          if (isDraw) {
                            timeSpan = `${startStr} to ${endStr}`;
                          } else {
                            timeSpan = `${startStr} to about ${endStr}`;
                          }
                        }
                        // A kachinuki competition's range, on its own line so the
                        // time line never cuts it off.
                        const rangeLine = !isPlaceholder && comp.placement?.bestEnd && comp.placement?.worstEnd
                          ? `Could end ${comp.placement.bestEnd} to ${comp.placement.worstEnd}`
                          : '';
                        const placeholderLine = isPlaceholder
                          ? (comp.placement?.lengthReason === 'estimate_failed'
                            ? 'Could not estimate: length unknown'
                            : 'No participants yet: length unknown')
                          : '';
                        const expectedLine = isExpected ? `Estimate for ${comp.expectedParticipants} expected` : '';
                        const statusLine = isRunning ? 'Running: cannot be moved' : isCompleted ? 'Completed: cannot be moved' : '';
                        const overlapLine = block.overlapDetail
                          ? `Overlaps ${block.overlapDetail.partnerName} on Shiaijo ${block.overlapDetail.courtsStr}, ${block.overlapDetail.durationStr}`
                          : '';
                        const midnightLine = block.pastMidnight
                          ? `Ends after midnight, at ${isDraw ? '' : 'about '}${formatClockHHMM(block.endMins % 1440)} on ${getNextDayFormatted(day.date)}. Start it earlier or give it more shiaijo.`
                          : '';

                        // Screen reader description: every line the block shows,
                        // and the pauses it holds.
                        const pausesInBlock = (day.breaks || []).filter(b => {
                          const bs = b.startMins ?? b.StartMins;
                          const be = b.endMins ?? b.EndMins;
                          return overlaps({ start: bs, end: be }, { start: block.startMins, end: block.endMins });
                        });
                        const pauseSummary = pausesInBlock.length > 0
                          ? ` Pauses: ${pausesInBlock.map(p => `${p.name} ${p.start} to ${p.end}`).join(', ')}.`
                          : '';
                        const spokenLines = [rangeLine, expectedLine, statusLine, overlapLine, midnightLine]
                          .filter(Boolean)
                          .map((l) => (/[.]$/.test(l) ? ` ${l}` : ` ${l}.`))
                          .join('');
                        const ariaLabel = `${comp.name}, ${timeSpan || placeholderLine || 'length unknown'}.${spokenLines}${pauseSummary}`;

                        // The block is a frame: the part that opens the
                        // competition and its Move button sit side by side in
                        // it, so neither control is inside the other.
                        return (
                          <div
                            key={comp.id}
                            className={blockClass}
                            data-testid="cal-block"
                            style={{
                              left: `${block.left}px`,
                              width: `${block.width}px`,
                              top: `${block.top}px`,
                              height: `${block.height}px`,
                              '--cal-covered': `${covered}px`,
                            }}
                            onPointerDown={(e) => handleBlockPointerDown(e, comp, block)}
                            // Raised, a block's lines take its full width and keep
                            // clear of its Move button: fit them again.
                            onPointerEnter={refitBlock}
                            onPointerLeave={refitBlock}
                            onFocus={refitBlock}
                            onBlur={refitBlock}
                          >
                            {/* In-block pause strips */}
                            {withContentKeys(pausesInBlock, (x) => `${x.name}|${x.startMins ?? x.StartMins}-${x.endMins ?? x.EndMins}`).map(({ key: stripKey, item: p }) => {
                              const pStart = p.startMins ?? p.StartMins;
                              const pEnd = p.endMins ?? p.EndMins;
                              const stripStart = Math.max(pStart, block.effectiveStart);
                              const stripEnd = Math.min(pEnd, block.effectiveEnd);
                              const stripTop = (stripStart - block.effectiveStart) * PX_PER_MINUTE;
                              const stripHeight = Math.max(0, (stripEnd - stripStart) * PX_PER_MINUTE);
                              const sitsInFirst50 = stripStart < block.startMins + 50;
                              const showPauseName = !sitsInFirst50 && block.width >= 140;

                              return (
                                <div
                                  key={stripKey}
                                  className="cal-block-pause-strip"
                                  style={{
                                    top: `${stripTop}px`,
                                    height: `${stripHeight}px`,
                                  }}
                                >
                                  {showPauseName ? p.name : ''}
                                </div>
                              );
                            })}

                            <div
                              role="button"
                              tabIndex={0}
                              className="cal-block__open"
                              aria-label={ariaLabel}
                              onClick={(e) => handleBlockClick(e, comp.id)}
                              onKeyDown={(e) => handleBlockKeyDown(e, comp)}
                            >
                              <div className="cal-block__content">
                                <div className="cal-block__name" title={comp.name}>
                                  {isPlaceholder && <span className="cal-block__chip">?</span>}
                                  {comp.name}
                                  {/* A short block has room for one line: its start goes there. */}
                                  {block.short && !isPlaceholder && (
                                    <span className="cal-block__short-start"> · {formatClockHHMM(block.startMins)}</span>
                                  )}
                                </div>

                                {/* A warning comes straight after the name, before the
                                    time, so a block too short for every line (a phone's
                                    block with Move on its own row) still shows it. */}
                                {overlapLine && <div className="cal-block__warn-line">{overlapLine}</div>}
                                {midnightLine && <div className="cal-block__warn-line">{midnightLine}</div>}

                                {isPlaceholder ? (
                                  <div className="cal-block__meta">{placeholderLine}</div>
                                ) : (
                                  <div className="cal-block__time">{timeSpan}</div>
                                )}
                                {rangeLine && <div className="cal-block__meta">{rangeLine}</div>}
                                {expectedLine && <div className="cal-block__meta">{expectedLine}</div>}
                                {statusLine && <div className="cal-block__meta">{statusLine}</div>}
                              </div>
                            </div>

                            {(moveShown || extras) && (
                            <div className="cal-block__move-slot">
                              {moveShown && (
                                <button
                                  type="button"
                                  className="cal-block__move-btn"
                                  aria-label={`Move ${comp.name}`}
                                  onPointerDown={(e) => e.stopPropagation()}
                                  onClick={(e) => { e.stopPropagation(); setMoveDialog({ comp }); }}
                                >Move</button>
                              )}
                              {extras}
                            </div>
                            )}
                          </div>
                        );
                      })}
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        </div>

        {/* Not on the calendar Tray */}
        {notPlacedComps.length > 0 && (
          <div ref={trayRef} className="cal-tray">
            <div className="cal-tray__title">Not on the calendar ({notPlacedComps.length})</div>
            <div className="cal-tray__intro">These competitions cannot be placed yet. Each says what it needs.</div>
            {placeholderCount > 0 && (
              <div className="cal-tray__placeholder-count">
                {placeholderCount === 1
                  ? '1 more is on the calendar with no real length.'
                  : `${placeholderCount} more are on the calendar with no real length.`}
              </div>
            )}
            <div className="cal-tray__list">
              {notPlacedComps.map(c => (
                <button
                  key={c.id}
                  type="button"
                  className="cal-tray__item"
                  onClick={() => onOpenCompetition?.(c.id, 'overview')}
                >
                  <span className="cal-tray__item-name">{c.name}</span>
                  <span className="cal-tray__item-reason">{formatNotPlacedReason(c.placement?.notPlacedReason)}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      {drag && (
        <div className="cal-ghost" aria-hidden="true"
          style={{ left: `${drag.x}px`, top: `${drag.y}px`, width: `${drag.width}px`, minHeight: `${drag.height}px` }}>
          <div className="cal-ghost__name">{drag.comp.name}</div>
          <div className="cal-ghost__label">{drag.label}</div>
          {drag.hint && <div className="cal-ghost__label">{drag.hint}</div>}
        </div>
      )}

      {moveDialog && (
        <MoveDialog
          comp={moveDialog.comp}
          initial={moveDialog.initial}
          allCourts={calendar.courts || []}
          dayOptions={(calendar.days || []).map((d) => ({ date: d.date, label: `${formatDayDate(d.date)} (Day ${d.day})` }))}
          password={password}
          onSave={(fields) => commitMove({ competitionId: moveDialog.comp.id, ...fields })}
          onClose={() => setMoveDialog(null)}
        />
      )}
    </div>
  );
}
