package ludo

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

// This fixture is also deserialized by Tests/LudoProtocolContract.cs against the
// compiled Unity game assembly. It catches wire-field/type mismatches across Go/C#.
func TestAuthoritativeUnityContract(t *testing.T) {
	s := authTestState()
	g := s.Game
	g.Commands = nil
	g.start(0)
	_ = g.roll(6, 1)
	_ = g.move(0, 6, 2)
	_ = g.roll(1, 3)
	_ = g.move(0, 1, 4)
	d := &authTestDispatcher{}
	s.sendSnapshot(d, nil, s.Presences["0"], 4)
	var snapshot map[string]interface{}
	_ = json.Unmarshal(d.packets[0], &snapshot)
	g.surrender(2, 5)
	fixture, _ := json.MarshalIndent(map[string]interface{}{"commands": g.Commands, "snapshot": snapshot}, "", "  ")
	const path = "testdata/authoritative_contract.json"
	if os.Getenv("UPDATE_LUDO_CONTRACT") == "1" {
		if err := os.MkdirAll("testdata", 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, fixture, 0644); err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(actual), bytes.TrimSpace(fixture)) {
		t.Fatal("wire contract changed; review Unity compatibility before updating fixture")
	}
}
