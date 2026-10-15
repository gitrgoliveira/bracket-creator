// Package state, score_log.go owns each match's score event log (bc-evts):
// the taps made on a running match (domain.ScoreEvent) and the baselines the
// other writes leave (ScoreBaseline), one JSON line each, in the order the
// server took them. engine.ReplayScoring folds the log into the match the
// results file holds, which is a cache of it.
//
// One small file per match, competitions/<id>/events/<escaped match id>.jsonl,
// beside the match's history (match_history.go) and built the same way: the
// append is staged through the SAME transaction as the match write
// (StoreTx.AppendScoreLog), so a line and the write it belongs to land or
// vanish together, and LoadScoreLog reads the transaction's own staged bytes.
package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// ScoreLogFormatVersion is the format of every line this build writes
// ("v"). A line with any other version is refused on load, never guessed at
// (operator decision 2026-10-15).
const ScoreLogFormatVersion = 1

// scoreLogDir is the per-competition directory the event logs live in.
const scoreLogDir = "events"

// ScoreLogLine is one line of a match's event log: exactly one of Event (a
// tap) or Baseline (what a write other than a tap left the match holding).
type ScoreLogLine struct {
	V        int                `json:"v"`
	Event    *domain.ScoreEvent `json:"event,omitempty"`
	Baseline *ScoreBaseline     `json:"baseline,omitempty"`
}

// ScoreBaseline is the value a write other than a tap gave some of the
// match's groups (match_groups.go): a finish, a correction, Record bout, a
// decision, a reopen, the server's own kachinuki advance, an older build's
// running snapshot, and the migration line a log starts with. Every group it
// names is set to its value, at Stamp in the replay's order; events made
// before it that it did not see (Basis) do not count (D5).
type ScoreBaseline struct {
	// ID is the line's own id, unique in the log; marks a baseline brings are
	// named b:<ID>:<pos>:<side>:<i>.
	ID string `json:"id"`
	// Door names the write ("score", "decision", "kachinuki-advance",
	// "migrate", ...).
	Door string `json:"door"`
	// WriteID is the client's id for the write, when it sent one, so a resend
	// is recognised.
	WriteID string `json:"writeId,omitempty"`
	// Session is the writing device's event session, when it has one.
	Session string `json:"session,omitempty"`
	// Stamp places the baseline in the replay's order: after every event
	// stamped at or before it.
	Stamp int64 `json:"stamp"`
	// ReceivedAt is when the server took the write (server ms).
	ReceivedAt int64 `json:"receivedAt,omitempty"`
	// Values holds each group the baseline sets, as state.GroupValue projects
	// it; a bout group's null removes the bout (a tombstone).
	Values map[string]json.RawMessage `json:"values"`
	// Stamps holds the stamp the merge gave each group it set.
	Stamps map[string]int64 `json:"stamps,omitempty"`
	// Basis is what the writer had in view when it built the write.
	Basis ScoreBasis `json:"basis"`
}

// ScoreBasis says which events a baseline was built with: the first Seen
// lines of the log (the scoreVersion the writer's board showed), the event
// ids in Own (its own taps, sent or still queued), and every event of the
// baseline's own Session with a Seq up to Seq. An event it contains is
// already in the baseline's values and is never folded a second time.
type ScoreBasis struct {
	Seen int      `json:"seen"`
	Own  []string `json:"own,omitempty"`
	Seq  int64    `json:"seq,omitempty"`
}

// Validate checks one line's own shape.
func (l ScoreLogLine) Validate() error {
	switch {
	case (l.Event == nil) == (l.Baseline == nil):
		return errors.New("score log: a line holds exactly one event or one baseline")
	case l.Event != nil:
		return l.Event.Validate()
	}
	b := l.Baseline
	if b.ID == "" {
		return errors.New("score log: a baseline without an id")
	}
	for g, raw := range b.Values {
		if !ValidGroup(g) {
			return fmt.Errorf("score log: baseline %q names %q, which is not a group", b.ID, g)
		}
		// A value that does not read as its group is refused here, on append
		// and on load alike, never skipped by the replay.
		if err := SetGroupFromValue(&MatchResult{}, g, raw); err != nil {
			return fmt.Errorf("score log: baseline %q: %w", b.ID, err)
		}
	}
	return nil
}

// ID is the line's id: the event's, or the baseline's.
func (l ScoreLogLine) ID() string {
	if l.Event != nil {
		return l.Event.ID
	}
	if l.Baseline != nil {
		return l.Baseline.ID
	}
	return ""
}

// Stamp is the line's place in the replay's order.
func (l ScoreLogLine) Stamp() int64 {
	if l.Event != nil {
		return l.Event.Stamp
	}
	if l.Baseline != nil {
		return l.Baseline.Stamp
	}
	return 0
}

func scoreLogFile(matchID string) string {
	return url.PathEscape(matchID) + ".jsonl"
}

func (s *Store) scoreLogPath(compID, matchID string) string {
	return s.compPath(compID, scoreLogDir, scoreLogFile(matchID))
}

// ensureScoreLogDirLocked creates the events directory, but only inside a
// competition directory that exists: a write landing after DeleteCompetition
// must fail rather than rebuild the competition around a lone log (the rule
// ensureMatchHistoryDirLocked follows).
func (s *Store) ensureScoreLogDirLocked(compID string) error {
	compDir := s.compPath(compID)
	info, err := os.Stat(compDir)
	if err != nil {
		return fmt.Errorf("score log: competition %s: %w", compID, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("score log: competition %s is not a directory", compID)
	}
	return os.Mkdir(s.compPath(compID, scoreLogDir), 0o700)
}

func (s *Store) readScoreLogBytesLocked(compID, matchID string) ([]byte, error) {
	b, err := os.ReadFile(s.scoreLogPath(compID, matchID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// appendScoreLogLocked appends lines to existing and writes the whole file
// through write (the transaction's staging, or a direct write). Each line is
// stamped with this build's format version.
func (s *Store) appendScoreLogLocked(compID, matchID string, existing []byte, lines []ScoreLogLine, write writeFn) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	if matchID == "" {
		return errors.New("score log: lines without a match id")
	}
	if len(lines) == 0 {
		return nil
	}
	if err := s.ensureScoreLogDirLocked(compID); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	// A copy, never existing itself: it may be the staged bytes another read
	// still holds.
	buf := append([]byte(nil), existing...)
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}
	for _, l := range lines {
		if l.V == 0 {
			l.V = ScoreLogFormatVersion
		}
		if l.V != ScoreLogFormatVersion {
			return fmt.Errorf("score log: this build writes version %d lines, not %d", ScoreLogFormatVersion, l.V)
		}
		if err := l.Validate(); err != nil {
			return err
		}
		b, err := json.Marshal(l)
		if err != nil {
			return fmt.Errorf("score log: %w", err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	return write(s.scoreLogPath(compID, matchID), buf, 0o600)
}

// ErrScoreLogUnreadable is returned for a log line this build cannot read:
// damaged JSON, or a format version it does not know. The log is the match's
// truth, so a line is never skipped or guessed at.
var ErrScoreLogUnreadable = errors.New("score log line unreadable")

// parseScoreLog reads every line of a log, in file order. Any line it cannot
// read is an error naming the file and the line.
func parseScoreLog(path string, raw []byte) ([]ScoreLogLine, error) {
	var out []ScoreLogLine
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var l ScoreLogLine
		if err := json.Unmarshal(line, &l); err != nil {
			return nil, fmt.Errorf("%w: %s line %d: %v", ErrScoreLogUnreadable, path, n, err)
		}
		if l.V != ScoreLogFormatVersion {
			return nil, fmt.Errorf("%w: %s line %d: format version %d, this build reads version %d", ErrScoreLogUnreadable, path, n, l.V, ScoreLogFormatVersion)
		}
		if err := l.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %s line %d: %v", ErrScoreLogUnreadable, path, n, err)
		}
		out = append(out, l)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s after line %d: %v", ErrScoreLogUnreadable, path, n, err)
	}
	return out, nil
}

// AppendScoreLog appends lines to a match's event log outside a transaction
// (Store satisfies StoreTx). A door that writes a match takes the
// transaction's twin, so its lines land with the write.
func (s *Store) AppendScoreLog(compID, matchID string, lines ...ScoreLogLine) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	existing, err := s.readScoreLogBytesLocked(compID, matchID)
	if err != nil {
		return err
	}
	return s.appendScoreLogLocked(compID, matchID, existing, lines, s.directWrite)
}

// LoadScoreLog reads a match's event log; none yet reads as empty.
func (s *Store) LoadScoreLog(compID, matchID string) ([]ScoreLogLine, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()
	raw, err := s.readScoreLogBytesLocked(compID, matchID)
	if err != nil {
		return nil, err
	}
	return parseScoreLog(s.scoreLogPath(compID, matchID), raw)
}

// DeleteScoreLog removes every match's event log of a competition, as
// DiscardDraw does with the histories: a draw generated again reuses the
// match ids.
func (s *Store) DeleteScoreLog(compID string) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	dir := s.compPath(compID, scoreLogDir)
	if filepath.Dir(dir) != s.compPath(compID) {
		return fmt.Errorf("score log: refusing to remove %q", dir)
	}
	return os.RemoveAll(dir)
}

// AppendScoreLog stages lines onto a match's event log in this transaction.
func (t *storeTx) AppendScoreLog(compID, matchID string, lines ...ScoreLogLine) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	existing, ok := t.pendingFor(filepath.Join(scoreLogDir, scoreLogFile(matchID)))
	if !ok {
		var err error
		if existing, err = t.store.readScoreLogBytesLocked(compID, matchID); err != nil {
			return err
		}
	}
	return t.store.appendScoreLogLocked(compID, matchID, existing, lines, t.txWriteFn())
}

// LoadScoreLog reads a match's event log, the lines this transaction staged
// included.
func (t *storeTx) LoadScoreLog(compID, matchID string) ([]ScoreLogLine, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	path := t.store.scoreLogPath(compID, matchID)
	if pending, ok := t.pendingFor(filepath.Join(scoreLogDir, scoreLogFile(matchID))); ok {
		return parseScoreLog(path, pending)
	}
	raw, err := t.store.readScoreLogBytesLocked(compID, matchID)
	if err != nil {
		return nil, err
	}
	return parseScoreLog(path, raw)
}
