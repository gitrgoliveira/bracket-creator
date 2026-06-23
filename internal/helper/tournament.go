package helper

import (
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// cellCoord holds an Excel workbook cell address; used only during workbook
// generation and never serialised or stored on domain types.
type cellCoord struct {
	sheetName string
	cell      string
}

// playerCellCoord extends cellCoord with an optional player-number cell.
type playerCellCoord struct {
	cellCoord
	numberCell string // non-empty only when the player has a Number field
}

type Pool struct {
	PoolName string   `json:"poolName"`
	Players  []Player `json:"players"`
	Matches  []Match  `json:"matches,omitempty"`
}

// Player is a type alias for domain.Player. The helper package used to
// own a parallel struct during the NFR-007 migration; it was collapsed
// to an alias once the two were proven field-identical (the converters
// were copying fields 1:1 with no translation). The helper name is kept
// for rendering-side ergonomics inside this package.
type Player = domain.Player

// MatchWinner records the Excel cell that contains a pool or elimination match
// winner's name; used to build cross-sheet formula references in bracket trees.
type MatchWinner struct {
	cellCoord
}

type Match struct {
	SideA *Player `json:"sideA"`
	SideB *Player `json:"sideB"`
	Round int     `json:"round"`
}

func CreatePlayers(entries []string, withZekkenName bool) ([]Player, error) {
	records := make([][]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, `"`) {
			records = append(records, strings.Split(entry, ","))
			continue
		}
		r := csv.NewReader(strings.NewReader(entry))
		r.LazyQuotes = true
		r.TrimLeadingSpace = true
		fields, err := r.Read()
		if err != nil {
			records = append(records, strings.Split(entry, ","))
			continue
		}
		records = append(records, fields)
	}
	return CreatePlayersFromRecords(records, withZekkenName)
}

// CreatePlayersFromRecords builds players from pre-parsed CSV records
// (each record is a slice of fields). Use this when the CSV has already
// been parsed by encoding/csv so that quoted commas are handled correctly.
func CreatePlayersFromRecords(records [][]string, withZekkenName bool) ([]Player, error) {
	players := make([]Player, 0, len(records))
	var errors []string
	seenNames := make(map[string]int)
	c := cases.Title(language.Und, cases.NoLower)

	for i, line := range records {
		allEmpty := true
		for _, f := range line {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}
		for j := range line {
			line[j] = strings.TrimSpace(line[j])
		}

		player := Player{
			PoolPosition: int64(len(players)),
		}

		if withZekkenName {
			if len(line) < 2 {
				errors = append(errors, fmt.Sprintf("entry %d: invalid format: expected 'Name, Dojo' or 'Name, DisplayName, Dojo'", i+1))
				continue
			}
			player.Name = c.String(line[0])
			if len(line) == 2 {
				player.DisplayName = SanitizeName(line[0])
				player.Dojo = line[1]
				if player.Dojo == "" {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
			} else {
				if line[2] == "" {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
				player.DisplayName = line[1]
				if player.DisplayName == "" {
					player.DisplayName = SanitizeName(line[0])
				}
				player.Dojo = line[2]
				if len(line) > 3 {
					meta := line[3:]
					if len(meta) > 0 && IsRegistrationSource(meta[len(meta)-1]) {
						player.Source = CanonicalRegistrationSource(meta[len(meta)-1])
						meta = meta[:len(meta)-1]
					}
					if len(meta) > 0 {
						player.Metadata = meta
					}
				}
			}
		} else {
			player.Name = c.String(line[0])
			player.DisplayName = SanitizeName(line[0])
			player.Dojo = "NA"
			if len(line) >= 2 {
				player.Dojo = line[1]
			}
			if len(line) > 2 {
				meta := line[2:]
				if len(meta) > 0 && IsRegistrationSource(meta[len(meta)-1]) {
					player.Source = CanonicalRegistrationSource(meta[len(meta)-1])
					meta = meta[:len(meta)-1]
				}
				if len(meta) > 0 {
					player.Metadata = meta
				}
			}
		}
		key := fmt.Sprintf("%s|%s|%s", player.Name, player.DisplayName, player.Dojo)
		if lineNo, seen := seenNames[key]; seen {
			errors = append(errors, fmt.Sprintf("entry %d: duplicate participant '%s' from '%s' (display name: '%s', originally at entry %d)", i+1, player.Name, player.Dojo, player.DisplayName, lineNo))
			continue
		}
		seenNames[key] = i + 1
		players = append(players, player)
	}

	if len(errors) > 0 {
		return nil, fmt.Errorf("participant validation failed:\n%s", strings.Join(errors, "\n"))
	}

	return players, nil
}

// IsRegistrationSource reports whether s is a recognised participant
// registration source (case-insensitive): manual / registered / transfer.
// Exported so the API boundary validator can reject unknown values before they
// are persisted — the CSV loader only recognises these tokens, so an unexpected
// value would otherwise shift into Metadata on reload.
func IsRegistrationSource(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "manual", "registered", "transfer":
		return true
	}
	return false
}

// CanonicalRegistrationSource returns the canonical stored form of a
// registration source: trimmed + lower-case. Keeps filter buckets from
// splitting on whitespace/casing ("Manual" vs "manual").
func CanonicalRegistrationSource(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// TitleCaseName applies the same Unicode Title-casing that CreatePlayers uses
// so names stored to participants.csv (and seeds.csv) match what is read back
// on the next load, avoiding seed-merge mismatches. TrimSpace is applied first
// to match CreatePlayers' per-column trim before title-casing.
func TitleCaseName(name string) string {
	return cases.Title(language.Und, cases.NoLower).String(strings.TrimSpace(name))
}

// SanitizeName returns the canonical display form derived from a participant
// name: a single token uppercased ("KAZUKI") or "F. LAST" for multi-token
// names. Exported so state.SaveParticipants can detect display names that
// match the auto-derived form and avoid round-trip data corruption (a 3-column
// row whose DisplayName equals SanitizeName(Name) carries no extra information
// and must not be written for non-zekken competitions — see
// internal/state/participants.go).
func SanitizeName(name string) string {
	//removing extra spaces
	name = strings.TrimSpace(name)

	// return only first and last name
	fullName := strings.Split(name, " ")

	if len(fullName) == 1 {
		return strings.ToUpper(fullName[0])
	}

	// First Name all caps
	firstName := strings.ToUpper(fullName[0])

	// Last Name all caps
	lastName := strings.ToUpper(fullName[len(fullName)-1])

	return fmt.Sprintf("%c. %s", firstName[0], lastName)
}

func CreatePools(players []Player, poolSize int, isMax bool) ([]Pool, error) {
	// Guard before the division below: poolSize is the divisor in both the
	// "max" and fixed-size branches, so a zero/negative value panics with an
	// integer divide-by-zero. Reject it here — the lowest shared point — so
	// every caller (engine draw, schedule estimator, CLI) is panic-proof
	// regardless of how PoolSize reached it. (mp-ebgz)
	if poolSize <= 0 {
		return nil, fmt.Errorf("cannot create pools: pool size must be at least 1, got %d", poolSize)
	}
	var totalPools int
	if isMax {
		totalPools = (len(players) + poolSize - 1) / poolSize
	} else {
		totalPools = len(players) / poolSize
	}

	if totalPools == 0 && len(players) > 0 {
		return nil, fmt.Errorf("cannot create pools: player count (%d) is less than pool size (%d)", len(players), poolSize)
	}

	pools := make([]Pool, totalPools)

	targetSizes := make([]int, totalPools)
	if isMax && totalPools > 0 {
		base := len(players) / totalPools
		rem := len(players) % totalPools
		for i := 0; i < totalPools; i++ {
			if i < rem {
				targetSizes[i] = base + 1
			} else {
				targetSizes[i] = base
			}
		}
	} else {
		for i := 0; i < totalPools; i++ {
			targetSizes[i] = poolSize
		}
	}

	// Per-pool sets for O(1) dojo-conflict and duplicate-name detection.
	dojoSets := make([]map[string]bool, totalPools)
	nameSets := make([]map[string]bool, totalPools)
	for i := range dojoSets {
		dojoSets[i] = make(map[string]bool)
		nameSets[i] = make(map[string]bool)
	}

	for i, player := range players {
		poolN := discoverPool(pools, dojoSets, nameSets, player, targetSizes, i%totalPools)
		// try and force same dojo
		if poolN < 0 {
			poolN = forceSameDojo(pools, targetSizes)
		}

		// try and force pool size
		if poolN < 0 {
			poolN = forcePoolSize(pools, targetSizes)
		}
		player.PoolPosition = int64(len(pools[poolN].Players) + 1)
		pools[poolN].Players = append(pools[poolN].Players, player)
		dojoSets[poolN][player.Dojo] = true
		nameSets[poolN][player.Name] = true
	}

	for i := 0; i < len(pools); i++ {
		char := string(rune('A' + i%26))
		if i > 25 {
			char = char + char
		}
		pools[i].PoolName = fmt.Sprintf("Pool %s", char)
	}

	return pools, nil
}

func discoverPool(pools []Pool, dojoSets, nameSets []map[string]bool, player Player, targetSizes []int, startIndex int) int {
	totalPools := len(pools)
	if totalPools == 0 {
		return -1
	}

	for i := 0; i < totalPools; i++ {
		curr := (startIndex + i) % totalPools

		// making sure there's space first
		if len(pools[curr].Players) >= targetSizes[curr] {
			continue
		}

		// O(1): reject if dojo or name already present in this pool
		if dojoSets[curr][player.Dojo] || nameSets[curr][player.Name] {
			continue
		}

		return curr
	}

	// If no suitable pool is found, return -1
	return -1
}

func forceSameDojo(pools []Pool, targetSizes []int) int {
	for i, pool := range pools {
		if len(pool.Players) < targetSizes[i] {
			return i
		}
	}
	return -1
}

func forcePoolSize(pools []Pool, targetSizes []int) int {

	for i, j := 0, len(pools)-1; i <= j; i, j = i+1, j-1 {
		if len(pools[i].Players) < targetSizes[i]+1 {
			return i
		}
		if i != j {
			if len(pools[j].Players) < targetSizes[j]+1 {
				return j
			}
		}
	}
	return 0
}

func CreatePoolMatches(pools []Pool) {
	for i := range pools {
		pool := &pools[i]
		players := pool.Players

		// Special case: pool of 2 only needs 1 match
		if len(players) == 2 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[0],
				SideB: &players[1],
			})
			continue
		}

		switch len(players) {
		case 0:
			continue
		case 1:
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[0],
				SideB: &players[0],
			})
			continue
		case 3:
			pool.Matches = append(pool.Matches,
				Match{SideA: &players[0], SideB: &players[1]},
				Match{SideA: &players[0], SideB: &players[2]},
				Match{SideA: &players[1], SideB: &players[2]},
			)
			continue
		case 4:
			pool.Matches = append(pool.Matches,
				Match{SideA: &players[0], SideB: &players[1]},
				Match{SideA: &players[2], SideB: &players[1]},
				Match{SideA: &players[2], SideB: &players[3]},
				Match{SideA: &players[0], SideB: &players[3]},
			)
			continue
		}

		for i := 0; i+1 < len(players); i += 2 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[i],
				SideB: &players[i+1],
			})
			next := (i + 2) % len(players)
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[next],
				SideB: &players[i+1],
			})
		}
		if len(players)%2 != 0 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[len(players)-1],
				SideB: &players[0],
			})
		}

	}
}

// playerIndex returns the position of p in players by pointer identity, or -1.
func playerIndex(players []Player, p *Player) int {
	for i := range players {
		if &players[i] == p {
			return i
		}
	}
	return -1
}

// buildRoundLookup converts a CircleMethodRounds (or PathGraphRounds) result
// into a map from normalised IntPair (A < B) to round index.
func buildRoundLookup(rounds [][]IntPair) map[IntPair]int {
	lookup := make(map[IntPair]int)
	for r, pairs := range rounds {
		for _, p := range pairs {
			a, b := p.A, p.B
			if a > b {
				a, b = b, a
			}
			lookup[IntPair{A: a, B: b}] = r
		}
	}
	return lookup
}

func CreatePoolRoundRobinMatches(pools []Pool) {

	for poolN, pool := range pools {
		currentPool := &pools[poolN]
		size := len(pool.Players)

		switch size {
		case 0, 1:
			continue
		case 3:
			currentPool.Matches = append(currentPool.Matches,
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[2]},
				Match{SideA: &currentPool.Players[1], SideB: &currentPool.Players[2]},
			)
		case 4:
			currentPool.Matches = append(currentPool.Matches,
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[2], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[2], SideB: &currentPool.Players[3]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[3]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[2]},
				Match{SideA: &currentPool.Players[1], SideB: &currentPool.Players[3]},
			)
		default:
			for i := 1; i < size; i++ {
				for k, j := i, 0; j < size-i; j, k = j+1, k+1 {
					sideA := &currentPool.Players[j]
					sideB := &currentPool.Players[k]

					if len(currentPool.Matches) > 0 {
						prev := currentPool.Matches[len(currentPool.Matches)-1]
						prevSide := func(match Match, player *Player) int {
							if match.SideA == player {
								return 1
							}
							if match.SideB == player {
								return 2
							}
							return 0
						}

						sideAStatus := prevSide(prev, sideA)
						sideBStatus := prevSide(prev, sideB)
						if sideAStatus == 2 || sideBStatus == 1 {
							sideA, sideB = sideB, sideA
						}
					}

					currentPool.Matches = append(currentPool.Matches, Match{
						SideA: sideA,
						SideB: sideB,
					})
				}
			}
		}

		// Assign Round indices using the circle-method schedule.
		roundLookup := buildRoundLookup(CircleMethodRounds(size))
		for mi := range currentPool.Matches {
			m := &currentPool.Matches[mi]
			idxA := playerIndex(currentPool.Players, m.SideA)
			idxB := playerIndex(currentPool.Players, m.SideB)
			a, b := idxA, idxB
			if a > b {
				a, b = b, a
			}
			if r, ok := roundLookup[IntPair{A: a, B: b}]; ok {
				m.Round = r
			}
		}
	}

}

// playerCoordKey returns the lookup key for a player in a coord map.
// It mirrors the composite uniqueness key enforced by CreatePlayers, so two
// players with the same name but different dojos get distinct entries.
func playerCoordKey(p Player) string {
	return p.Name + "|" + p.DisplayName + "|" + p.Dojo
}

func ConvertPlayersToWinners(players []Player, sanitized bool, pCoords map[string]playerCellCoord) map[string]MatchWinner {
	matchWinners := make(map[string]MatchWinner, len(players))
	for _, player := range players {
		coord, ok := pCoords[playerCoordKey(player)]
		if !ok {
			continue
		}
		key := player.Name
		if sanitized && player.DisplayName != "" {
			key = player.DisplayName
		}
		matchWinners[key] = MatchWinner{cellCoord: coord.cellCoord}
	}
	return matchWinners
}
