package helper

import (
	"fmt"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

type Pool struct {
	PoolName string
	Players  []Player
	Matches  []Match

	// Excel coordinates
	sheetName string
	cell      string
}

type Player struct {
	Name        string
	DisplayName string
	Dojo        string

	PoolPosition int64

	// Excel coordinates
	sheetName string
	cell      string
}
type MatchWinner struct {
	// Excel coordinates
	sheetName string
	cell      string
}

type Match struct {
	SideA *Player
	SideB *Player
}

func CreatePlayers(entries []string) []Player {
	players := make([]Player, len(entries))
	c := cases.Title(language.Und, cases.NoLower)

	for i, entry := range entries {
		line := strings.Split(entry, ",")

		player := Player{
			Name:         c.String(strings.TrimSpace(line[0])),
			Dojo:         "NA",
			DisplayName:  sanitizeName(line[0]),
			PoolPosition: int64(i),
		}

		if len(line) >= 2 {
			player.Dojo = strings.TrimSpace(line[1])
		}

		players[i] = player
	}

	return players
}

func sanitizeName(name string) string {
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
func CreatePools(players []Player, poolSize int) []Pool {
	totalPools := len(players) / poolSize
	pools := make([]Pool, totalPools)

	for _, player := range players {
		poolN := discoverPool(pools, player, poolSize)
		// try and force same dojo
		if poolN < 0 {
			poolN = forceSameDojo(pools, poolSize)
		}

		// try and force pool size
		if poolN < 0 {
			poolN = forcePoolSize(pools, poolSize)
			fmt.Printf("Added extra player to pool %d\n", poolN)
		}
		player.PoolPosition = int64(len(pools[poolN].Players) + 1)
		pools[poolN].Players = append(pools[poolN].Players, player)
	}

	for i := 0; i < len(pools); i++ {
		char := string(rune('A' + i%26))
		if i > 25 {
			char = char + char
		}
		pools[i].PoolName = fmt.Sprintf("Pool %s", string(char))
	}

	return pools
}

func discoverPool(pools []Pool, player Player, poolSize int) int {

	for i, pool := range pools {

		// making sure there's space first
		if len(pool.Players) >= poolSize {
			continue
		}

		canAddToPool := true
		for _, assignedPlayers := range pool.Players {
			// try make sure that there aren't other players of the same dojo
			if assignedPlayers.Dojo == player.Dojo ||
				assignedPlayers.Name == player.Name {
				canAddToPool = false
				break
			}
		}

		// If the player can be added, return the pool index
		if canAddToPool {
			return i
		}

	}

	// If no suitable pool is found, return -1
	return -1
}
func forceSameDojo(pools []Pool, poolSize int) int {
	for i, pool := range pools {
		if len(pool.Players) < poolSize {
			return i
		}
	}
	return -1
}

func forcePoolSize(pools []Pool, poolSize int) int {

	for i, j := 0, len(pools)-1; i <= j; i, j = i+1, j-1 {
		if len(pools[i].Players) < poolSize+1 {
			return i
		}
		if i != j {
			if len(pools[j].Players) < poolSize+1 {
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
		for j := range players {
			sideA := &players[j]
			sideB := &players[0]
			if j != len(players)-1 {
				sideB = &players[j+1]
			}
			if j%2 != 0 {
				sideA, sideB = sideB, sideA
			}
			pool.Matches = append(pool.Matches, Match{
				SideA: sideA,
				SideB: sideB,
			})
		}

	}
}

func CreatePoolRoundRobinMatches(pools []Pool) {

	for poolN, pool := range pools {
		size := len(pool.Players)

		for i := 1; i < size; i++ {
			for k, j := i, 0; j < size-i; j, k = j+1, k+1 {
				sideA := &pools[poolN].Players[j]
				sideB := &pools[poolN].Players[k]

				if j%2 != 0 {
					sideA, sideB = sideB, sideA
				}

				pools[poolN].Matches = append(pools[poolN].Matches, Match{
					SideA: sideA,
					SideB: sideB,
				})
			}
		}

		// handle the special case for pools of 4
		if size == 4 {
			// swap the second last and third last round
			secondLastRound := pools[poolN].Matches[len(pools[poolN].Matches)-2]
			thirdLastRound := pools[poolN].Matches[len(pools[poolN].Matches)-3]
			// swap the sides
			secondLastRound.SideA, secondLastRound.SideB = secondLastRound.SideB, secondLastRound.SideA

			pools[poolN].Matches[len(pools[poolN].Matches)-2] = thirdLastRound
			pools[poolN].Matches[len(pools[poolN].Matches)-3] = secondLastRound

		} else {
			// last match always needs to swap sides
			lastRound := &pools[poolN].Matches[len(pools[poolN].Matches)-1]
			lastRound.SideA, lastRound.SideB = lastRound.SideB, lastRound.SideA
		}

	}

}

func ConvertPlayersToWinners(players []Player, sanitized bool) map[string]MatchWinner {
	matchWinners := make(map[string]MatchWinner, len(players))

	if sanitized {
		for _, player := range players {
			matchWinners[player.DisplayName] = MatchWinner{
				sheetName: player.sheetName,
				cell:      player.cell,
			}
		}

	} else {
		for _, player := range players {
			matchWinners[player.Name] = MatchWinner{
				sheetName: player.sheetName,
				cell:      player.cell,
			}
		}
	}

	return matchWinners
}
