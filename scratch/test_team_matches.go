package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	baseURL := "http://localhost:8080/api"
	password := "testpassword"

	// 1. Create Tournament
	tourneyReq := map[string]interface{}{
		"name":     "Test Tournament",
		"date":     "2026-05-11",
		"venue":    "Test Venue",
		"courts":   []string{"A", "B"},
		"password": password,
	}
	resp, err := post(baseURL+"/tournament", tourneyReq, password)
	if err != nil {
		fmt.Printf("Error creating tournament: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Tournament created: %s\n", resp)

	// 2. Create Team Competition
	compReq := map[string]interface{}{
		"name":      "Team Comp",
		"kind":      "team",
		"format":    "pools",
		"teamSize":  3,
		"poolSize":  3,
		"status":    "setup",
		"startTime": "10:00",
		"players": []map[string]interface{}{
			{"name": "Team A", "dojo": "Dojo A"},
			{"name": "Team B", "dojo": "Dojo B"},
			{"name": "Team C", "dojo": "Dojo C"},
		},
	}
	resp, err = post(baseURL+"/competitions", compReq, password)
	if err != nil {
		fmt.Printf("Error creating competition: %v\n", err)
		os.Exit(1)
	}
	var comp map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &comp); err != nil {
		fmt.Printf("Error parsing competition response: %v\n", err)
		os.Exit(1)
	}
	compID := comp["id"].(string)
	fmt.Printf("Competition created: %s (ID: %s)\n", resp, compID)

	// 3. Start Competition
	resp, err = post(baseURL+"/competitions/"+compID+"/start", nil, password)
	if err != nil {
		fmt.Printf("Error starting competition: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Competition started: %s\n", resp)

	// 4. Check Matches via Viewer API
	resp, err = get(baseURL + "/viewer/competitions/" + compID)
	if err != nil {
		fmt.Printf("Error getting viewer data: %v\n", err)
		os.Exit(1)
	}
	var viewerData map[string]interface{}
	if err := json.Unmarshal([]byte(resp), &viewerData); err != nil {
		fmt.Printf("Error parsing viewer data: %v\n", err)
		os.Exit(1)
	}

	poolMatches := viewerData["poolMatches"]
	fmt.Printf("Pool Matches: %v\n", poolMatches)

	if poolMatches == nil || len(poolMatches.([]interface{})) == 0 {
		fmt.Println("FAIL: poolMatches is empty")
		os.Exit(1)
	}
	matches := poolMatches.([]interface{})
	match0 := matches[0].(map[string]interface{})
	matchID := match0["id"].(string)

	// 5. Record Team Score
	scoreReq := map[string]interface{}{
		"winner": "Team A",
		"status": "completed",
		"score": map[string]interface{}{
			"type":      "ippon",
			"winnerPts": 2,
			"loserPts":  1,
		},
		"subResults": []map[string]interface{}{
			{
				"position": 1,
				"sideA":    "Team A",
				"sideB":    "Team B",
				"ipponsA":  []string{"M", "K"},
				"ipponsB":  []string{},
				"winner":   "Team A",
			},
			{
				"position": 2,
				"sideA":    "Team A",
				"sideB":    "Team B",
				"ipponsA":  []string{},
				"ipponsB":  []string{"D"},
				"winner":   "Team B",
			},
			{
				"position": 3,
				"sideA":    "Team A",
				"sideB":    "Team B",
				"ipponsA":  []string{"T"},
				"ipponsB":  []string{},
				"winner":   "Team A",
			},
		},
	}

	// API.recordScore calls /api/competitions/:id/matches/:mid/score
	resp, err = put(baseURL+"/competitions/"+compID+"/matches/"+matchID+"/score", scoreReq, password)
	if err != nil {
		fmt.Printf("Error recording score: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Score recorded: %s\n", resp)

	// 6. Verify Score via Standings
	resp, err = get(baseURL + "/viewer/competitions/" + compID)
	if err != nil {
		fmt.Printf("Error getting viewer data: %v\n", err)
		os.Exit(1)
	}
	if err := json.Unmarshal([]byte(resp), &viewerData); err != nil {
		fmt.Printf("Error parsing viewer data (standings): %v\n", err)
		os.Exit(1)
	}
	standings := viewerData["standings"].(map[string]interface{})
	poolAStandings := standings["Pool A"].([]interface{})

	fmt.Printf("Pool A Standings: %v\n", poolAStandings)

	teamA := poolAStandings[0].(map[string]interface{})
	if teamA["player"].(map[string]interface{})["name"] == "Team A" && teamA["wins"].(float64) == 1 {
		fmt.Println("SUCCESS: Team A has 1 win")
	} else {
		fmt.Printf("FAIL: Team A standings: %v\n", teamA)
	}
}

func put(url string, body interface{}, password string) (string, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	}
	req, _ := http.NewRequest("PUT", url, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", password)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(data))
	}
	return string(data), nil
}

func post(url string, body interface{}, password string) (string, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	}
	req, _ := http.NewRequest("POST", url, buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", password)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(data))
	}
	return string(data), nil
}

func get(url string) (string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(data))
	}
	return string(data), nil
}
