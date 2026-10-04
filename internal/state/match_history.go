// Package state, match_history.go owns each match's append-only history
// (bc-mrgc): one JSON line per write that reached the match, applied or not,
// so nothing an operator entered is ever lost. A change the merge did not
// apply (a newer change to the same group is stored) is kept here with its
// values; nothing else keeps it.
//
// One small file per match, competitions/<id>/history/<escaped match id>.jsonl,
// so an append re-stages one match's lines rather than the competition's. The
// append is staged through the SAME transaction as the match write
// (StoreTx.AppendMatchHistory), so a write and its history entry land or
// vanish together.
package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/url"
	"os"
	"path/filepath"
)

// The per-group outcomes a history entry records: applied, held (a newer
// stored change outranks it; its value is in Held), or unchanged (applied,
// but the value equalled the stored one).
const (
	HistoryOutcomeApplied   = "applied"
	HistoryOutcomeHeld      = "held"
	HistoryOutcomeUnchanged = "unchanged"
)

// matchHistoryDir is the per-competition directory the history files live in.
const matchHistoryDir = "history"

// MatchHistoryEntry is one write that reached a match.
type MatchHistoryEntry struct {
	MatchID string `json:"matchId"`
	// Door is the endpoint the write came through ("score", "decision",
	// "daihyosen-add", "reopen", ...).
	Door string `json:"door"`
	// Stamp is when the change was made (the write's modifiedAt,
	// server-relative ms); 0 for an unstamped write.
	Stamp int64 `json:"stamp"`
	// ReceivedAt is when the server took the write (server ms).
	ReceivedAt int64 `json:"receivedAt"`
	// Session is the client scoring session, when the write named one.
	Session string `json:"session,omitempty"`
	// Changed lists the groups the write changes, and Outcomes says, per
	// group, whether it applied or was held.
	Changed  []string          `json:"changed"`
	Outcomes map[string]string `json:"outcomes"`
	// Held carries the incoming value of every held group, so the change is
	// kept even though it was not applied.
	Held map[string]json.RawMessage `json:"held,omitempty"`
	// Reason says why changes were held other than by their stamps, when one
	// rule did that (MergeReport.HoldReason): "older revision of this board"
	// for a running write the same board had already followed with a newer
	// one (the running rev guard), an engi finish held whole, or a change
	// that would leave a finished match without the winner it needs. Empty
	// when each group was judged by its own stamp.
	Reason string `json:"reason,omitempty"`
	// ClearedWithdrawal is the result a later scoring change cleared (R2).
	ClearedWithdrawal json.RawMessage `json:"clearedWithdrawal,omitempty"`
}

// matchHistoryFile is the file name of a match's history: the match id
// path-escaped, so a "/" or ".." in an id cannot leave the directory.
func matchHistoryFile(matchID string) string {
	return url.PathEscape(matchID) + ".jsonl"
}

func (s *Store) matchHistoryPath(compID, matchID string) string {
	return s.compPath(compID, matchHistoryDir, matchHistoryFile(matchID))
}

// ensureMatchHistoryDirLocked creates the history directory, but ONLY under a
// competition directory that already exists: creating the competition
// directory is competition creation's job (saveCompetitionChangedLocked), and
// a write landing after DeleteCompetition must fail rather than resurrect it.
func (s *Store) ensureMatchHistoryDirLocked(compID string) error {
	compDir := s.compPath(compID)
	info, err := os.Stat(compDir)
	if err != nil {
		return fmt.Errorf("match history: competition %s: %w", compID, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("match history: competition %s is not a directory", compID)
	}
	return os.Mkdir(s.compPath(compID, matchHistoryDir), 0o700)
}

func (s *Store) readMatchHistoryBytesLocked(compID, matchID string) ([]byte, error) {
	b, err := os.ReadFile(s.matchHistoryPath(compID, matchID))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// appendMatchHistoryLocked appends entry to existing (the file's current
// bytes, staged or on disk) and writes the whole file through write.
func (s *Store) appendMatchHistoryLocked(compID string, existing []byte, entry MatchHistoryEntry, write writeFn) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	if entry.MatchID == "" {
		return fmt.Errorf("match history: entry without a match id")
	}
	if err := s.ensureMatchHistoryDirLocked(compID); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("match history: %w", err)
	}
	// A copy, never existing itself: it may be the staged bytes another
	// read still holds. No capacity is computed up front, so no size sum
	// can overflow; append grows the copy as it needs to.
	buf := append([]byte(nil), existing...)
	if len(buf) > 0 && buf[len(buf)-1] != '\n' {
		buf = append(buf, '\n')
	}
	buf = append(buf, line...)
	buf = append(buf, '\n')
	return write(s.matchHistoryPath(compID, entry.MatchID), buf, 0o600)
}

// parseMatchHistory reads a history file's lines. A line that does not parse
// is skipped and logged rather than failing the read: the history is a record
// to consult, and one damaged line must not hide the rest.
func parseMatchHistory(compID, matchID string, raw []byte) []MatchHistoryEntry {
	var out []MatchHistoryEntry
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e MatchHistoryEntry
		if err := json.Unmarshal(line, &e); err != nil {
			log.Printf("state: match history %s/%s: a line could not be read and is skipped: %v", compID, matchID, err)
			continue
		}
		out = append(out, e)
	}
	return out
}

// AppendMatchHistory appends one entry to a match's history, outside a
// transaction. A writer inside one uses StoreTx.AppendMatchHistory.
func (s *Store) AppendMatchHistory(compID string, entry MatchHistoryEntry) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	existing, err := s.readMatchHistoryBytesLocked(compID, entry.MatchID)
	if err != nil {
		return err
	}
	return s.appendMatchHistoryLocked(compID, existing, entry, s.directWrite)
}

// LoadMatchHistory returns a match's history in the order it was written
// (arrival order; each entry carries its own stamp). An unknown match or one
// with no history returns nil.
func (s *Store) LoadMatchHistory(compID, matchID string) ([]MatchHistoryEntry, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	mu := s.getCompLock(compID)
	mu.RLock()
	defer mu.RUnlock()
	raw, err := s.readMatchHistoryBytesLocked(compID, matchID)
	if err != nil {
		return nil, err
	}
	return parseMatchHistory(compID, matchID, raw), nil
}

// DeleteMatchHistory removes every match history of a competition. A
// discarded draw calls it: generating the draw again reuses the match ids, and
// the new matches must not inherit the old ones' history.
func (s *Store) DeleteMatchHistory(compID string) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	dir := s.compPath(compID, matchHistoryDir)
	if filepath.Dir(dir) != s.compPath(compID) {
		return fmt.Errorf("match history: refusing to remove %q", dir)
	}
	return os.RemoveAll(dir)
}

func (t *storeTx) AppendMatchHistory(compID string, entry MatchHistoryEntry) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	existing, ok := t.pendingFor(filepath.Join(matchHistoryDir, matchHistoryFile(entry.MatchID)))
	if !ok {
		var err error
		if existing, err = t.store.readMatchHistoryBytesLocked(compID, entry.MatchID); err != nil {
			return err
		}
	}
	return t.store.appendMatchHistoryLocked(compID, existing, entry, t.txWriteFn())
}

func (t *storeTx) LoadMatchHistory(compID, matchID string) ([]MatchHistoryEntry, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	if pending, ok := t.pendingFor(filepath.Join(matchHistoryDir, matchHistoryFile(matchID))); ok {
		return parseMatchHistory(compID, matchID, pending), nil
	}
	raw, err := t.store.readMatchHistoryBytesLocked(compID, matchID)
	if err != nil {
		return nil, err
	}
	return parseMatchHistory(compID, matchID, raw), nil
}
