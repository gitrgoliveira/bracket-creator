package helper

import (
	"strings"
	"testing"
)

func TestCreatePlayersWithZekkenName(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, クレスワェル, Tokyo Kendo Club",
		"Yuki Tanaka, 田中, Osaka Dojo",
	}

	players, err := CreatePlayers(entries, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(players) != 2 {
		t.Fatalf("Expected 2 players, got %d", len(players))
	}

	if players[0].Name != "Ricardo Oliveira" {
		t.Errorf("Expected Ricardo Oliveira, got %s", players[0].Name)
	}
	if players[0].DisplayName != "クレスワェル" {
		t.Errorf("Expected クレスワェル, got %s", players[0].DisplayName)
	}
	if players[0].Dojo != "Tokyo Kendo Club" {
		t.Errorf("Expected Tokyo Kendo Club, got %s", players[0].Dojo)
	}

	if players[1].Name != "Yuki Tanaka" {
		t.Errorf("Expected Yuki Tanaka, got %s", players[1].Name)
	}
	if players[1].DisplayName != "田中" {
		t.Errorf("Expected 田中, got %s", players[1].DisplayName)
	}
	if players[1].Dojo != "Osaka Dojo" {
		t.Errorf("Expected Osaka Dojo, got %s", players[1].Dojo)
	}
}

func TestCreatePlayersWithZekkenNameFallback(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, , Tokyo Kendo Club",
	}

	players, err := CreatePlayers(entries, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if players[0].DisplayName != "R. OLIVEIRA" {
		t.Errorf("Expected R. OLIVEIRA, got %s", players[0].DisplayName)
	}
}

func TestCreatePlayersWithoutZekkenName(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, Tokyo Kendo Club",
		"Yuki Tanaka",
	}

	players, err := CreatePlayers(entries, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if players[0].Dojo != "Tokyo Kendo Club" {
		t.Errorf("Expected Tokyo Kendo Club, got %s", players[0].Dojo)
	}
	if players[0].DisplayName != "R. OLIVEIRA" {
		t.Errorf("Expected R. OLIVEIRA, got %s", players[0].DisplayName)
	}

	if players[1].Dojo != "NA" {
		t.Errorf("Expected NA, got %s", players[1].Dojo)
	}
}

func TestCreatePlayersZekkenMissingDojo(t *testing.T) {
	entries := []string{
		"Ricardo Oliveira, クレスワェル",
	}

	_, err := CreatePlayers(entries, true)
	if err == nil {
		t.Fatal("Expected error for missing dojo, got nil")
	}
}

func TestCreatePlayersWithMetadata(t *testing.T) {
	// Case 1: Zekken ON
	entriesOn := []string{
		"Ricardo Oliveira, クレスワェル, Tokyo Kendo Club, Extra1, Extra2",
	}
	playersOn, err := CreatePlayers(entriesOn, true)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(playersOn[0].Metadata) != 2 {
		t.Fatalf("Expected 2 metadata fields, got %d", len(playersOn[0].Metadata))
	}
	if playersOn[0].Metadata[0] != "Extra1" || playersOn[0].Metadata[1] != "Extra2" {
		t.Errorf("Unexpected metadata values: %v", playersOn[0].Metadata)
	}

	// Case 2: Zekken OFF
	entriesOff := []string{
		"Ricardo Oliveira, Tokyo Kendo Club, Extra1, Extra2",
	}
	playersOff, err := CreatePlayers(entriesOff, false)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(playersOff[0].Metadata) != 2 {
		t.Fatalf("Expected 2 metadata fields, got %d", len(playersOff[0].Metadata))
	}
	if playersOff[0].Metadata[0] != "Extra1" || playersOff[0].Metadata[1] != "Extra2" {
		t.Errorf("Unexpected metadata values: %v", playersOff[0].Metadata)
	}
}

func TestCreatePlayersDuplicates(t *testing.T) {
	// Case 1: Identical entries (Duplicate Name + Dojo/Zekken)
	entries := []string{
		"John Doe, Dojo A",
		"Jane Doe, Dojo B",
		"John Doe, Dojo A",
	}
	_, err := CreatePlayers(entries, false)
	if err == nil {
		t.Fatal("Expected error for identical entry, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate entry for participant 'John Doe'") {
		t.Errorf("Unexpected error message: %v", err)
	}

	// Case 2: Same Zekken for different people (ALLOWED)
	entriesSameZekken := []string{
		"John Doe, JD, Dojo A",
		"Jane Doe, JD, Dojo B",
	}
	players, err := CreatePlayers(entriesSameZekken, true)
	if err != nil {
		t.Fatalf("Expected no error for different people with same Zekken, got %v", err)
	}
	if len(players) != 2 {
		t.Errorf("Expected 2 players, got %d", len(players))
	}

	// Case 3: Same Name but different Dojo (ALLOWED)
	entriesSameNameDifferentDojo := []string{
		"John Doe, Dojo A",
		"John Doe, Dojo B",
	}
	players2, err := CreatePlayers(entriesSameNameDifferentDojo, false)
	if err != nil {
		t.Fatalf("Expected no error for same name in different dojos, got %v", err)
	}
	if len(players2) != 2 {
		t.Errorf("Expected 2 players, got %d", len(players2))
	}
}
