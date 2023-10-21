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

	PoolPosition int

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
	players := make([]Player, 0)
	c := cases.Title(language.Und, cases.NoLower)

	for _, entry := range entries {
		line := strings.Split(entry, ",")
		fmt.Println(c.String(strings.TrimSpace(line[0])))
		player := Player{
			Name:        c.String(strings.TrimSpace(line[0])),
			Dojo:        strings.TrimSpace(line[1]),
			DisplayName: sanatizeName(line[0]),
		}
		players = append(players, player)
	}

	return players
}

func sanatizeName(name string) string {
	//removing extra spaces
	name = strings.TrimSpace(name)

	// return only first and last name
	fullName := strings.Split(name, " ")

	// capitalize first letter
	// firstName := strings.ToUpper(fullName[0][0:1]) + fullName[0][1:]
	firstName := strings.ToTitle(fullName[0])

	// Last Name all caps
	lastName := strings.ToUpper(fullName[len(fullName)-1])

	return fmt.Sprintf("%c. %s", firstName[0], lastName)
}
func CreatePools(players []Player, poolSize int) []Pool {
	totalPools := len(players) / poolSize
	// remainingEntries := len(players) % poolSize

	pools := make([]Pool, totalPools)

	for _, player := range players {
		poolN := discoverPool(pools, player, poolSize)
		if poolN < 0 {
			poolN = forcePoolSize(pools, player, poolSize)
			fmt.Printf("Added extra player to pool %d\n", poolN)
		}
		player.PoolPosition = len(pools[poolN].Players) + 1
		pools[poolN].Players = append(pools[poolN].Players, player)
	}

	for i := 0; i < len(pools); i++ {
		char := string('A' + i%26)
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

		sameDojo := false
		for _, assignedPlayers := range pool.Players {
			// try make sure that there aren't other players of the same dojo
			if assignedPlayers.Dojo == player.Dojo ||
				assignedPlayers.Name == player.Name {
				sameDojo = true
			}
		}

		if !sameDojo {
			return i
		}

	}
	return -1
}

func forcePoolSize(pools []Pool, player Player, poolSize int) int {

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

		matchIndex := 0
		var previousA *Player
		var previousB *Player
		for i := 0; i < size-1; i++ {

			for j := i + 1; j < size; j++ {
				matchIndex++
				sideA := &pools[poolN].Players[i]
				sideB := &pools[poolN].Players[j]

				if matchIndex%(size-1) == 0 {
					sideA, sideB = sideB, sideA
				}

				// restore if it was not time to change sides. This can happen with pools >4
				if previousA == sideB || previousB == sideA {
					sideA, sideB = sideB, sideA
				}
				pools[poolN].Matches = append(pools[poolN].Matches, Match{
					SideA: sideA,
					SideB: sideB,
				})

				previousA = sideA
				previousB = sideB
			}
		}

	}

}
