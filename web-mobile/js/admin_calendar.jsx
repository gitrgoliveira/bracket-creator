// admin_calendar.jsx: The competition calendar page and pure layout helper.
// Displays competitions placed across tournament days and shiaijo lanes.

import { overlaps } from './calendar_rules.jsx';

const { useState, useEffect, useRef, useMemo, useCallback } = React;

export const MOUSE_SLOP_PX = 6;
export const TOUCH_SLOP_PX = 10;
export const MIN_LANE_PX = 110;
export const CLOCK_COL_PX = 52;
export const PHONE_MAX_PX = 720;
export const PHONE_LANE_PX = 76;
export const PLACEHOLDER_MIN_PX = 52;
export const REFRESH_DEBOUNCE_MS = 1000;
export const PX_PER_MINUTE = 1;

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

/**
 * Formats minutes since midnight into 24-hour "HH:MM".
 */
export function formatClockHHMM(mins) {
  let m = typeof mins === 'number' ? mins : 0;
  if (m < 0) m = 0;
  const h = Math.floor(m / 60);
  const min = Math.floor(m % 60);
  return `${String(h).padStart(2, '0')}:${String(min).padStart(2, '0')}`;
}

/**
 * Formats an ISO "YYYY-MM-DD" date string to "Day DD Mon", e.g. "Sat 16 May".
 */
export function formatDayDate(dateStr) {
  if (!dateStr || typeof dateStr !== 'string') return '';
  const parts = dateStr.split('-');
  if (parts.length !== 3) return dateStr;
  const year = parseInt(parts[0], 10);
  const month = parseInt(parts[1], 10) - 1;
  const day = parseInt(parts[2], 10);
  const d = new Date(Date.UTC(year, month, day, 12, 0, 0));
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  return `${days[d.getUTCDay()]} ${day} ${months[month]}`;
}

/**
 * Formats the day after an ISO date string to "Day DD Mon", e.g. "Sun 17 May".
 */
export function getNextDayFormatted(dateStr) {
  if (!dateStr || typeof dateStr !== 'string') return 'the next day';
  const parts = dateStr.split('-');
  if (parts.length !== 3) return 'the next day';
  const year = parseInt(parts[0], 10);
  const month = parseInt(parts[1], 10) - 1;
  const day = parseInt(parts[2], 10);
  const d = new Date(Date.UTC(year, month, day + 1, 12, 0, 0));
  const days = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  return `${days[d.getUTCDay()]} ${d.getUTCDate()} ${months[d.getUTCMonth()]}`;
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
 * Shifts an array of courts by delta lanes, keeping court count and clamping at bounds.
 */
export function shiftLanes(courts, allCourts, delta) {
  if (!Array.isArray(courts) || courts.length === 0 || !Array.isArray(allCourts) || allCourts.length === 0) {
    return [];
  }
  const indices = courts.map(c => allCourts.indexOf(c)).filter(i => i !== -1);
  if (indices.length === 0) return [];
  const minIdx = Math.min(...indices);
  const maxIdx = Math.max(...indices);
  let shift = delta;
  if (minIdx + shift < 0) {
    shift = -minIdx;
  }
  if (maxIdx + shift >= allCourts.length) {
    shift = allCourts.length - 1 - maxIdx;
  }
  return indices.map(i => allCourts[i + shift]);
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
export function computeDaysShown({ width, shiaijoCount, tournamentDays, phone = false }) {
  const isPhone = phone || (typeof width === 'number' && width <= PHONE_MAX_PX);
  if (isPhone) return 1;
  const numDays = Math.max(1, tournamentDays || 1);
  const numCourts = Math.max(1, shiaijoCount || 1);
  const gridWidth = Math.max(0, (width || 1024) - CLOCK_COL_PX);
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
      let courtStr = '';
      if (courtList.length === 1) {
        courtStr = courtList[0];
      } else if (courtList.length === 2) {
        courtStr = `${courtList[0]} and ${courtList[1]}`;
      } else {
        courtStr = `${courtList.slice(0, -1).join(', ')} and ${courtList[courtList.length - 1]}`;
      }
      offScreen.push({
        day,
        courts: courtList,
        label: `Overlap on Day ${day}, Shiaijo ${courtStr}`,
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
        const sortedCourts = Array.from(clashingCourts).sort();
        let courtStr = sortedCourts[0] || '';
        if (sortedCourts.length === 2) {
          courtStr = `${sortedCourts[0]} and ${sortedCourts[1]}`;
        } else if (sortedCourts.length > 2) {
          courtStr = `${sortedCourts.slice(0, -1).join(', ')} and ${sortedCourts[sortedCourts.length - 1]}`;
        }
        overlapDetail = {
          partnerName: worstPartner.name,
          courtsStr: courtStr,
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
      pastMidnight,
      hasOverlap,
      overlapDetail,
    });
  }

  return placedBlocks;
}

/**
 * Pure layout helper for the tournament calendar.
 */
export function calendarLayout({ calendar, width = 1024, page = 0, phone = false }) {
  const courts = calendar?.courts || [];
  const days = calendar?.days || [];
  const shiaijoCount = courts.length;
  const tournamentDays = days.length;

  const isPhone = phone || (typeof width === 'number' && width <= PHONE_MAX_PX);
  const daysShown = computeDaysShown({ width, shiaijoCount, tournamentDays, phone: isPhone });

  const gridWidth = Math.max(0, width - CLOCK_COL_PX);
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
      setLoading(false);
    }
  }, [password]);

  // Initial fetch and SSE subscription
  useEffect(() => {
    fetchCal();

    const debouncedRefresh = () => {
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
      setContainerWidth(el.clientWidth || window.innerWidth || 1024);
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
      page: activePage,
      phone: isPhoneScreen,
    });
  }, [calendar, containerWidth, activePage, isPhoneScreen]);

  // Slop detection for click vs drag
  const handlePointerDown = (e) => {
    downPosRef.current = {
      x: e.clientX,
      y: e.clientY,
      pointerType: e.pointerType,
    };
  };

  const handleBlockClick = (e, compId) => {
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

  const handleBlockKeyDown = (e, compId) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      onOpenCompetition?.(compId, 'overview');
    }
  };

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

  const AdminTopbar = window.AdminTopbar || (({ onLogout, onViewerMode, tournament }) => null);
  const Breadcrumbs = window.Breadcrumbs || (({ items }) => null);

  // Loading state
  if (loading && !calendar) {
    return (
      <div className="app">
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
        <div className="page page--wide cal-page" style={{ maxWidth: 1400 }}>
          <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />
          <div className="cal-state-card">
            <div className="cal-state-title">Loading the calendar...</div>
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
        <div className="page page--wide cal-page" style={{ maxWidth: 1400 }}>
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
        <div className="page page--wide cal-page" style={{ maxWidth: 1400 }}>
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
        <div className="page page--wide cal-page" style={{ maxWidth: 1400 }}>
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
  const placeholderCount = (calendar.competitions || []).filter(c => c.placement?.placed && c.placement.lengthSource === 'minimum').length;
  const unusedPauses = calendar.unusedPauses || [];

  // Set of day numbers that hold overlaps for phone tabs
  const daysWithOverlaps = new Set();
  for (const c of calendar.competitions) {
    if (c.placement?.placed && Array.isArray(c.clashPartners) && c.clashPartners.length > 0) {
      daysWithOverlaps.add(c.placement.day);
    }
  }

  const instructionCopy = isCoarse
    ? 'Tap Move on a competition to move it, or press and hold it to drag. Tap the competition to open it.'
    : 'Drag a competition to move it, or use its Move button. Click it to open it.';

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      <div className="page page--wide cal-page" style={{ maxWidth: 1400 }} ref={containerRef}>
        <Breadcrumbs items={[{ label: tournament.name, onClick: onBack }, { label: 'Competition calendar' }]} />

        <div className="page-head">
          <div>
            <h1 className="page-head__title">Competition calendar</h1>
            <div className="cal-key-line">{instructionCopy}</div>
            {unusedPauses.length > 0 && (
              <div className="cal-unused-pauses">
                {unusedPauses.map((p, idx) => (
                  <div key={idx} className="cal-unused-pause">
                    {p.name} is not used: {formatUnusedPauseReason(p.reason)}{' '}
                    <button type="button" className="cal-link" onClick={onEditTournament}>
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
              <button type="button" className="cal-link" onClick={handleScrollToTray}>
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
                <button type="button" className="cal-link" onClick={() => handleShowDay(o.day)}>
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
                  {hasWarnDot && <span className="cal-warn-dot" aria-label="Has overlap" />}
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
              className="cal-paging-btn"
              disabled={!layout.paging.hasPrev}
              onClick={() => setDesktopPage(p => Math.max(0, p - 1))}
              aria-label="Previous days"
            >
              ‹
            </button>
            <button
              type="button"
              className="cal-paging-btn"
              disabled={!layout.paging.hasNext}
              onClick={() => setDesktopPage(p => p + 1)}
              aria-label="Next days"
            >
              ›
            </button>
          </div>
        )}

        {/* Calendar Grid Frame */}
        <div className="cal-grid-frame">
          {/* Clock column (sticky) */}
          <div className="cal-clock-col">
            <div className="cal-clock-header" />
            <div className="cal-lanes-header-spacer" style={{ height: 26 }} />
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
                    {(day.breaks || []).map((b, bi) => {
                      const bStart = b.startMins ?? b.StartMins;
                      const bEnd = b.endMins ?? b.EndMins;
                      const bTop = (bStart - layout.hourRange.startHour * 60) * PX_PER_MINUTE;
                      const bHeight = (bEnd - bStart) * PX_PER_MINUTE;
                      return (
                        <div
                          key={bi}
                          className="cal-pause-band"
                          style={{
                            top: `${bTop}px`,
                            height: `${bHeight}px`,
                          }}
                        >
                          {b.name} {b.start} to {b.end}
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
                        const isRunning = comp.status === 'running';
                        const isCompleted = comp.status === 'completed';

                        let blockClass = 'cal-block';
                        if (isPlaceholder) blockClass += ' cal-block--placeholder';
                        else if (isDraw) blockClass += ' cal-block--draw';
                        else blockClass += ' cal-block--estimate';

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
                          if (comp.placement?.bestEnd && comp.placement?.worstEnd) {
                            timeSpan += ` · Could end ${comp.placement.bestEnd} to ${comp.placement.worstEnd}`;
                          }
                        }

                        // Screen reader description
                        const pausesInBlock = (day.breaks || []).filter(b => {
                          const bs = b.startMins ?? b.StartMins;
                          const be = b.endMins ?? b.EndMins;
                          return overlaps({ start: bs, end: be }, { start: block.startMins, end: block.endMins });
                        });
                        const pauseSummary = pausesInBlock.length > 0
                          ? ` Pauses: ${pausesInBlock.map(p => `${p.name} ${p.start} to ${p.end}`).join(', ')}.`
                          : '';
                        const ariaLabel = `${comp.name}, ${timeSpan || 'length unknown'}.${pauseSummary}`;

                        return (
                          <div
                            key={comp.id}
                            role="button"
                            tabIndex={0}
                            className={blockClass}
                            style={{
                              left: `${block.left}px`,
                              width: `${block.width}px`,
                              top: `${block.top}px`,
                              height: `${block.height}px`,
                            }}
                            aria-label={ariaLabel}
                            onPointerDown={handlePointerDown}
                            onClick={(e) => handleBlockClick(e, comp.id)}
                            onKeyDown={(e) => handleBlockKeyDown(e, comp.id)}
                          >
                            {/* In-block pause strips */}
                            {pausesInBlock.map((p, pi) => {
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
                                  key={pi}
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

                            <div className="cal-block__content">
                              <div className="cal-block__name" title={comp.name}>
                                {isPlaceholder && <span className="cal-block__chip">?</span>}
                                {comp.name}
                              </div>

                              {isPlaceholder ? (
                                <div className="cal-block__meta">
                                  {comp.placement?.lengthReason === 'estimate_failed'
                                    ? 'Could not estimate: length unknown'
                                    : 'No participants yet: length unknown'}
                                </div>
                              ) : (
                                <div className="cal-block__time">{timeSpan}</div>
                              )}

                              {isExpected && (
                                <div className="cal-block__meta">
                                  Estimate for {comp.expectedParticipants || 24} expected
                                </div>
                              )}

                              {isRunning && (
                                <div className="cal-block__meta">Running: cannot be moved</div>
                              )}

                              {isCompleted && (
                                <div className="cal-block__meta">Completed: cannot be moved</div>
                              )}

                              {block.overlapDetail && (
                                <div className="cal-block__warn-line">
                                  Overlaps {block.overlapDetail.partnerName} on {block.overlapDetail.courtsStr}, {block.overlapDetail.durationStr}
                                </div>
                              )}

                              {block.pastMidnight && (
                                <div className="cal-block__warn-line">
                                  Ends after midnight, at {isDraw ? '' : 'about '}{formatClockHHMM(block.endMins % 1440)} on {getNextDayFormatted(day.date)}. Start it earlier or give it more shiaijo.
                                </div>
                              )}
                            </div>

                            {/* Slot for Move button (rendered by c-move) */}
                            <div className="cal-block__move-slot">
                              {renderBlockExtras?.(comp)}
                            </div>
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
                <div
                  key={c.id}
                  className="cal-tray__item"
                  role="button"
                  tabIndex={0}
                  onClick={() => onOpenCompetition?.(c.id, 'overview')}
                  onKeyDown={(e) => { if (e.key === 'Enter') onOpenCompetition?.(c.id, 'overview'); }}
                >
                  <span className="cal-tray__item-name">{c.name}</span>
                  <span className="cal-tray__item-reason">{formatNotPlacedReason(c.placement?.notPlacedReason)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
