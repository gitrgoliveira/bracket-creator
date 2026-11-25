package mobileapp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// --- Per-subscriber streams (bc-hnam, Q4 a) ---------------------------------
//
// The stream hides names the way the public reads do, per subscriber. A
// browser cannot send a header on an EventSource, so a caller with
// credentials (the password, reveal keys) first asks POST /api/events/ticket
// for a single-use ticket bound to its audience and opens
// /api/events?ticket=..., so no secret ever sits in a URL. No ticket is the
// public.
//
// Every event is classified in sseEventCatalogue. One that can name a
// competitor is masked: it names its competition, and each stream not
// entitled to that competition's names is sent a masked copy, built from the
// stored bytes when it is sent and judged on the competition's option and
// reveal expiry at that moment, so changing the option needs no reset of the
// replay history. The public copy is built once per event and shared
// (historyEntry.masked); only a stream holding competitors' own links has a
// copy built for it alone.

// maskedPayload memoises what every stream asks of a masked event or a draw
// event (sseEventSpec.judgesCompetition): the judgement of the competition it
// names, and with it the public projection (no names revealed) built from
// that competition.
type maskedPayload struct {
	mu sync.Mutex
	// judged is the competition's judgement, taken by the first stream that
	// needs it and reused by the others while the competition is not written
	// again (judgeCompetition).
	judged *competitionJudgement
}

// competitionJudgement is a masked event's competition as read, valid for the
// write token it was taken at, and the public copy built from it. Whether the
// public pages hide its names is not kept: it reads the tournament's mode too,
// which no competition token moves. The copy lives on the judgement so it goes
// with it: a stream still holding an older judgement can never keep its copy
// for the streams judged after a write (publicCopy).
type competitionJudgement struct {
	token   uint64
	comp    *state.Competition
	copied  bool
	payload string
}

// sseEventClass is what a stream not entitled to every name is sent for an
// event.
type sseEventClass int

const (
	// sseEventPasses: the event names no competitor, so every stream is sent
	// it as broadcast.
	sseEventPasses sseEventClass = iota + 1
	// sseEventMasked: the event can name competitors of the competition it
	// names (data.competitionId), so a stream not entitled to that
	// competition's names is sent a masked copy.
	sseEventMasked
	// sseEventOrganiserOnly: only the organiser's stream is sent its data;
	// every other stream is sent the event with no data, so its seq still
	// arrives and no gap is seen.
	sseEventOrganiserOnly
)

// sseEventSpec classifies one event type. mask rewrites a masked event's
// data object in place through v. draw marks an event that carries draw
// content or the draw's shape (a match, a status record naming a match, a
// lineup saved for a match, a Swiss round's match count) of the competition
// it names (data.competitionId): while that competition's draw is hidden from
// the public (publicNames.drawHidden), every stream but the organiser's is
// sent it with no data, whatever its class (bc-drpb). drawIf narrows draw to
// the events whose data it accepts: a withdrawal before the draw starts, or a
// team member added or renamed, names no match and carries nothing of the draw,
// so the public pages still hear of it.
type sseEventSpec struct {
	class  sseEventClass
	mask   func(data map[string]json.RawMessage, v maskView) error
	draw   bool
	drawIf func(data map[string]json.RawMessage) bool
}

// carriesDraw reports whether the event whose marshalled envelope is payload
// carries draw content: its type is marked draw and, when the spec narrows it,
// its data passes drawIf. A payload that cannot be read carries it, so it is
// withheld rather than sent.
func (spec sseEventSpec) carriesDraw(payload string) bool {
	if !spec.draw {
		return false
	}
	if spec.drawIf == nil {
		return true
	}
	var env struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &env); err != nil {
		return true
	}
	return spec.drawIf(env.Data)
}

// statusNamesMatch is the drawIf of competitor_status_updated: the status
// record names a match.
func statusNamesMatch(data map[string]json.RawMessage) bool {
	var st struct {
		MatchID string `json:"matchId"`
	}
	if err := json.Unmarshal(data["status"], &st); err != nil {
		return true
	}
	return st.MatchID != ""
}

// lineupNamesMatch is the drawIf of lineup_updated: the lineup was saved for a
// match (lineupUpdatedPayload sets matchId only then).
func lineupNamesMatch(data map[string]json.RawMessage) bool {
	_, ok := data["matchId"]
	return ok
}

// judgesCompetition reports whether an event of this spec, which carries draw
// content or not (carriesDraw), is judged on the competition it names: a masked
// event for its names, a draw event for its draw. Broadcast records the
// competition for these and the self-run memo.
func (spec sseEventSpec) judgesCompetition(carriesDraw bool) bool {
	return spec.class == sseEventMasked || carriesDraw
}

// sseEventCatalogue classifies EVERY event type the hub sends; a type missing
// from it is sent to no one but the organiser with its data (fail closed).
// TestSSEEventCatalogue_ClassifiesEveryEventType holds it to every EventType
// constant in the package.
var sseEventCatalogue = map[EventType]sseEventSpec{
	// A match's result: its sides, winner, bout rows and representative-bout
	// fighters name competitors and team members.
	EventMatchUpdated: {class: sseEventMasked, mask: maskMatchUpdatedData, draw: true},
	// The rest carry ids, numbers of rounds or nothing at all.
	EventCompetitionStarted:      {class: sseEventPasses},
	EventCompetitionCompleted:    {class: sseEventPasses},
	EventTournamentUpdated:       {class: sseEventPasses},
	EventScheduleUpdated:         {class: sseEventPasses},
	EventCompetitorStatusUpdated: {class: sseEventPasses, draw: true, drawIf: statusNamesMatch}, // a player id, a reason code and, once drawn, the match it names
	EventParticipantsUpdated:     {class: sseEventPasses},
	EventPasswordReset:           {class: sseEventPasses}, // an originator id, or nothing
	// Free text the organiser typed: shown to everyone as typed (E6; the
	// announcement box warns while a competition hides names).
	EventAnnouncement:       {class: sseEventPasses},
	EventDrawGenerated:      {class: sseEventPasses},
	EventDrawDiscarded:      {class: sseEventPasses},
	EventBracketQuarantined: {class: sseEventPasses},
	EventLineupUpdated:      {class: sseEventPasses, draw: true, drawIf: lineupNamesMatch}, // competition, team and, for a match's lineup, match ids
	EventResyncRequired:     {class: sseEventPasses},
	// Swiss never hides names (Q-swiss), and the event carries counts only,
	// but a round's match count is the draw's shape.
	EventSwissRoundGenerated: {class: sseEventPasses, draw: true},
	// A draft's draw was published or hidden again: a competition id and
	// whether it is hidden now, which the public pages refetch on.
	EventDrawVisibilityChanged: {class: sseEventPasses},
}

// sseEventClassOf is t's class; an unknown type is organiser-only.
func sseEventClassOf(t EventType) sseEventClass {
	if spec, ok := sseEventCatalogue[t]; ok {
		return spec.class
	}
	return sseEventOrganiserOnly
}

// eventCompetitionID is the competition a masked event names: its data's
// competitionId, read from the value when it is a map and from the
// marshalled envelope otherwise.
func eventCompetitionID(data any, payload []byte) string {
	switch d := data.(type) {
	case gin.H:
		id, _ := d["competitionId"].(string)
		return id
	case map[string]any:
		id, _ := d["competitionId"].(string)
		return id
	case map[string]string:
		return d["competitionId"]
	}
	var probe struct {
		Data struct {
			CompetitionID string `json:"competitionId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return ""
	}
	return probe.Data.CompetitionID
}

// project is what sub's stream is sent for e: the event as broadcast for the
// organiser, for an event that names no one and carries no draw, in a
// self-run tournament (which hides no names and shows every draw, so every
// stream is sent every event as it always was), and for a competition that
// shows names or whose names sub may see; a masked copy otherwise. A draw
// event (historyEntry.draws) of a competition whose draw is hidden from sub
// (publicNames.drawHidden) is sent with no data, judged first, before the
// event's class, the names option or a reveal link count: a hidden draft is
// the public's competition in setup, whose events carry nothing to show.
// Whatever cannot be judged (an unknown event type, an event naming no
// competition that it must be judged on, a competition that cannot be read,
// a copy that cannot be built) is sent with no data, never as broadcast.
func (h *Hub) project(e *historyEntry, sub *sseSubscriber) string {
	names := h.names
	if names == nil || sub == nil || sub.aud.isOrganiser() {
		return e.payload
	}
	spec, known := sseEventCatalogue[e.typ]
	if known && spec.class == sseEventPasses && !e.draws {
		return e.payload
	}
	if e.inSelfRun(names) {
		return e.payload
	}
	if !known || spec.class == sseEventOrganiserOnly {
		return withoutData(e)
	}
	if e.compID == "" {
		return withoutData(e)
	}
	comp, hides := e.judgeCompetition(names)
	if comp == nil {
		return withoutData(e)
	}
	if e.draws && names.drawHidden(comp, sub.aud) {
		return withoutData(e)
	}
	if spec.class == sseEventPasses || !hides {
		return e.payload
	}
	aud := streamRevealFor(names, comp, sub.aud)
	if aud.revealsAll(comp.ID) {
		return e.payload
	}
	if len(aud.revealedCompetitors(comp.ID)) > 0 {
		// A stream holding competitors' own links: a copy for it alone.
		out, ok := maskEvent(names, comp, aud, e, spec)
		if !ok {
			return withoutData(e)
		}
		return out
	}
	return e.publicCopy(comp, func() (string, bool) { return maskEvent(names, comp, audience{}, e, spec) })
}

// selfRunJudgement memoises whether the tournament is self-run for one
// event, so the tournament is read once per event however many streams it
// is sent to, not once per stream.
type selfRunJudgement struct {
	once    sync.Once
	selfRun bool
}

// judgedSelfRun is a judgement already taken (a stream's replay, judged when
// the stream opens).
func judgedSelfRun(selfRun bool) *selfRunJudgement {
	j := &selfRunJudgement{selfRun: selfRun}
	j.once.Do(func() {})
	return j
}

// inSelfRun reports whether the tournament is self-run for e: judged by the
// first stream that sends e and reused by the others (historyEntry.selfRun),
// read afresh for an entry that carries no judgement.
func (e *historyEntry) inSelfRun(names *publicNames) bool {
	if e.selfRun == nil {
		return names.selfRun()
	}
	e.selfRun.once.Do(func() { e.selfRun.selfRun = names.selfRun() })
	return e.selfRun.selfRun
}

// judgeCompetition is the competition e names and whether the public pages
// hide its names, nil when the competition cannot be read. The first stream
// to ask reads the competition and the rest reuse it, so a masked event costs
// one read of the competition however many streams are open, not one per
// stream. The competition is kept only while it is not written again
// (CompetitionWriteToken, read before the competition is, so a write in
// between is never missed), so a replayed event is still judged on the option
// as it stands then. Whether its names are hidden is judged on every call,
// because it also reads the tournament's mode, which a restore that keeps the
// competition in place can change without moving the competition's token (a
// kept answer would show the names of a tournament that was self-run when the
// event was first judged); the store caches the tournament read. Whether
// its draw is hidden is not kept either, for the same reason: project asks
// publicNames.drawHidden on every call.
func (e *historyEntry) judgeCompetition(names *publicNames) (*state.Competition, bool) {
	if e.masked == nil {
		comp := loadJudgedCompetition(names, e.compID)
		return comp, names.hides(comp)
	}
	e.masked.mu.Lock()
	defer e.masked.mu.Unlock()
	token := names.store.CompetitionWriteToken(e.compID)
	if j := e.masked.judged; j != nil && j.token == token {
		return j.comp, names.hides(j.comp)
	}
	// The competition was written since it was judged (a number prefix or a
	// team roster changed), so the judgement and the public copy built from it
	// are stale: a new judgement starts with no copy.
	comp := loadJudgedCompetition(names, e.compID)
	e.masked.judged = nil
	if comp != nil {
		e.masked.judged = &competitionJudgement{token: token, comp: comp}
	}
	return comp, names.hides(comp)
}

// loadJudgedCompetition reads the competition compID, nil when it cannot be
// read.
func loadJudgedCompetition(names *publicNames, compID string) *state.Competition {
	comp, err := names.store.LoadCompetition(compID)
	if err != nil {
		return nil
	}
	return comp
}

// publicCopy is e's public projection of comp (the competition the stream
// judged), built by build the first time a stream needs it and shared after.
// It is kept only on the judgement comp came from while that judgement is
// still the current one: a stream that judged before the competition was
// written is sent the copy it built, but the streams judged after build their
// own from the competition as written. A copy build could not finish (a file
// that could not be read: the mask failed closed) is sent but not kept, so
// the next stream builds it again.
func (e *historyEntry) publicCopy(comp *state.Competition, build func() (string, bool)) string {
	if e.masked == nil {
		if out, ok := build(); ok {
			return out
		}
		return withoutData(e)
	}
	e.masked.mu.Lock()
	defer e.masked.mu.Unlock()
	j := e.masked.judged
	current := j != nil && j.comp == comp
	if current && j.copied {
		return j.payload
	}
	out, ok := build()
	if !ok {
		return withoutData(e)
	}
	if current {
		j.payload, j.copied = out, true
	}
	return out
}

// streamRevealFor is what aud's reveal links unlock in comp NOW: a stream
// keeps the audience its ticket was judged for, so the option and the
// expiry are judged again on every event. A competition link counts only
// while the competition gives competition links, competitors' own links only
// while it gives per-person links, and either only while its links are open
// (state.RevealOpen).
func streamRevealFor(names *publicNames, comp *state.Competition, aud audience) audience {
	wantAll := aud.competitions[comp.ID]
	own := aud.revealedCompetitors(comp.ID)
	if !wantAll && len(own) == 0 {
		return audience{}
	}
	t, err := names.store.LoadTournament()
	if err != nil || !state.RevealOpen(comp, t, names.now()) {
		return audience{}
	}
	switch {
	case comp.PublicNames == state.PublicNamesCompetition && wantAll:
		return audience{competitions: map[string]bool{comp.ID: true}}
	case comp.PublicNames == state.PublicNamesPerson && len(own) > 0:
		return audience{competitors: map[string]map[string]bool{comp.ID: own}}
	}
	return audience{}
}

// sseEnvelope is the wire envelope, read back from the stored bytes to be
// masked. Field order matches SSEEvent's.
type sseEnvelope struct {
	Type EventType                  `json:"type"`
	Data map[string]json.RawMessage `json:"data"`
	Seq  int64                      `json:"seq"`
}

// maskEvent builds e's masked copy for aud from the stored bytes. The second
// return is false when no copy could be built. A copy built through a mask
// that failed closed (a file that could not be read) is still a copy, sent
// with every unknown name as "?", but is reported as not finished, so it is
// not shared.
func maskEvent(names *publicNames, comp *state.Competition, aud audience, e *historyEntry, spec sseEventSpec) (string, bool) {
	v, viewErr := names.view(comp, aud)
	if viewErr != nil {
		slog.Warn("mobile-app: event stream: names masked with what could be read", "competition", comp.ID, "err", viewErr)
	}
	var env sseEnvelope
	if err := json.Unmarshal([]byte(e.payload), &env); err != nil || env.Data == nil {
		return "", false
	}
	if err := spec.mask(env.Data, v); err != nil {
		slog.Warn("mobile-app: event stream: an event could not be masked; sent without its data", "type", e.typ, "competition", comp.ID, "err", err)
		return "", false
	}
	if v.namesHidden() {
		env.Data["namesHidden"] = json.RawMessage("true")
	}
	out, err := json.Marshal(env)
	if err != nil {
		return "", false
	}
	if viewErr != nil {
		return string(out), false
	}
	return string(out), true
}

// maskMatchUpdatedData masks a match_updated's result: the stored match the
// push carried (pool, league or knockout, as a MatchResult). A push that
// carries no result, or a null one, names no one.
func maskMatchUpdatedData(data map[string]json.RawMessage, v maskView) error {
	raw, ok := data["result"]
	if !ok || string(raw) == "null" {
		return nil
	}
	var m state.MatchResult
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("match_updated result: %w", err)
	}
	v.match(&m)
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("match_updated result: %w", err)
	}
	data["result"] = b
	return nil
}

// withoutData is e with its data dropped: its type and seq still reach the
// stream, so the client sees no gap and refetches what it reads.
func withoutData(e *historyEntry) string {
	out, err := json.Marshal(SSEEvent{Type: e.typ, Seq: e.seq})
	if err != nil {
		return fmt.Sprintf(`{"type":%q,"data":null,"seq":%d}`, e.typ, e.seq)
	}
	return string(out)
}

// endOrganiserStreamsLocked ends every stream opened as the organiser, after
// the admin password changed: a stream is judged once, when its ticket is
// redeemed, so one opened with the old password would otherwise keep being
// sent unmasked results and organiser-only events. The password_reset that
// ends them is already in each stream's channel, and an ended stream sends
// what its channel holds first. Its client asks for a new ticket with the
// password it holds now, and is refused if that is the old one. Caller holds
// h.mu for writing.
func (h *Hub) endOrganiserStreamsLocked() {
	for ch, sub := range h.clients {
		if sub != nil && sub.aud.isOrganiser() {
			h.unsubscribeLocked(ch)
		}
	}
}

// holdsReveal reports whether sub is a stream opened with reveal keys, which
// a tournament_updated judges again (rejudgeRevealStreams).
func (sub *sseSubscriber) holdsReveal() bool {
	return sub != nil && sub.reveal != "" && !sub.aud.isOrganiser()
}

// holdsRevealStreamLocked reports whether any open stream holds reveal keys.
// Caller holds h.mu.
func (h *Hub) holdsRevealStreamLocked() bool {
	for _, sub := range h.clients {
		if sub.holdsReveal() {
			return true
		}
	}
	return false
}

// rejudgeRevealStreams judges every stream opened with reveal keys again,
// after a tournament_updated (a competition's option, its expiry or its
// format may have changed), and ends each one whose keys now unlock
// something else. Its client opens a new stream with a new ticket and reads
// again, so it shows what the keys unlock now. Streams whose keys unlock the
// same are left open: what they are sent is judged on every event anyway
// (streamRevealFor). The event that started this is already in every
// stream's channel, and an ended stream sends what its channel holds before
// it ends.
func (h *Hub) rejudgeRevealStreams() {
	names := h.names
	if names == nil {
		return
	}
	type held struct {
		ch  chan historyEntry
		sub *sseSubscriber
	}
	var reveal []held
	h.mu.RLock()
	for ch, sub := range h.clients {
		if sub.holdsReveal() {
			reveal = append(reveal, held{ch, sub})
		}
	}
	h.mu.RUnlock()
	if len(reveal) == 0 {
		return
	}
	judged := map[string]string{}
	var ended []chan historyEntry
	for _, r := range reveal {
		key, ok := judged[r.sub.reveal]
		if !ok {
			key = names.revealAudience(r.sub.reveal).key()
			judged[r.sub.reveal] = key
		}
		if key != r.sub.aud.key() {
			ended = append(ended, r.ch)
		}
	}
	if len(ended) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range ended {
		h.unsubscribeLocked(ch)
	}
}
