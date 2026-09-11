package ludo

import (
	"context"
	"encoding/json"
	"errors"
	"game-server/systems/arena"
	"game-server/systems/currency"
	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
	"testing"
)

type authEconomyFake struct {
	runtime.NakamaModule
	coins        map[string]int64
	records      map[string]string
	meta         map[string]string
	transactions int
	fail         bool
}

func newAuthEconomyFake() *authEconomyFake {
	return &authEconomyFake{coins: map[string]int64{"0": 1000, "2": 1000}, records: map[string]string{}, meta: map[string]string{"0": "{}", "2": "{}"}}
}
func (n *authEconomyFake) StorageRead(ctx context.Context, reads []*runtime.StorageRead) ([]*api.StorageObject, error) {
	out := []*api.StorageObject{}
	for _, r := range reads {
		if v, ok := n.records[r.Key]; ok {
			out = append(out, &api.StorageObject{Value: v})
		}
	}
	return out, nil
}
func (n *authEconomyFake) AccountGetId(ctx context.Context, id string) (*api.Account, error) {
	return &api.Account{User: &api.User{Id: id, Metadata: n.meta[id]}}, nil
}
func (n *authEconomyFake) MultiUpdate(ctx context.Context, accounts []*runtime.AccountUpdate, writes []*runtime.StorageWrite, deletes []*runtime.StorageDelete, updates []*runtime.WalletUpdate, ledger bool) ([]*api.StorageObjectAck, []*runtime.WalletUpdateResult, error) {
	if n.fail {
		return nil, nil, errors.New("database unavailable")
	}
	for _, w := range writes {
		if _, exists := n.records[w.Key]; exists && w.Version == "*" {
			return nil, nil, errors.New("receipt exists")
		}
	}
	for _, u := range updates {
		if n.coins[u.UserID]+u.Changeset["coins"] < 0 {
			return nil, nil, errors.New("insufficient funds")
		}
	}
	for _, u := range updates {
		n.coins[u.UserID] += u.Changeset["coins"]
	}
	for _, w := range writes {
		n.records[w.Key] = w.Value
	}
	for _, a := range accounts {
		v, _ := json.Marshal(a.Metadata)
		n.meta[a.UserID] = string(v)
	}
	n.transactions++
	return nil, nil, nil
}
func (n *authEconomyFake) LeaderboardRecordWrite(context.Context, string, string, string, int64, int64, map[string]interface{}, *int) (*api.LeaderboardRecord, error) {
	return nil, nil
}
func economyTestState() *authMatchState {
	s := authTestState()
	s.MatchID = "test-match"
	s.Arena = arena.LudoArenaItemData{Name: "test", FeeCurrencyData: currency.VirtualCurrency{Amount: 100}, Rewards: map[arena.Rank][]currency.VirtualCurrency{arena.RankWinner: {{Amount: 160}}, arena.RankSecond: {{Amount: 40}}}}
	return s
}
func TestAuthoritativeFeesAndRewardsAreIdempotent(t *testing.T) {
	n := newAuthEconomyFake()
	s := economyTestState()
	ctx := context.Background()
	if err := s.charge(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.charge(ctx, n); err != nil {
		t.Fatal(err)
	}
	if n.coins["0"] != 900 || n.coins["2"] != 900 || n.transactions != 1 {
		t.Fatal("fee charged twice")
	}
	s.Game.Ranks = map[int]int{0: 1, 2: 2}
	s.Game.player(0).Kills = 2
	s.Game.player(2).Deaths = 2
	if err := s.settle(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.settle(ctx, n); err != nil {
		t.Fatal(err)
	}
	if n.coins["0"] != 1060 || n.coins["2"] != 940 || n.transactions != 2 {
		t.Fatal("reward duplicated")
	}
	var meta map[string]int
	_ = json.Unmarshal([]byte(n.meta["0"]), &meta)
	if meta["ludo_played_match"] != 1 || meta["ludo_win_match"] != 1 || meta["ludo_kills"] != 2 {
		t.Fatalf("invalid server stats: %v", meta)
	}
}
func TestAuthoritativeEntryIsAllOrNothing(t *testing.T) {
	n := newAuthEconomyFake()
	n.coins["2"] = 0
	s := economyTestState()
	if s.charge(context.Background(), n) == nil || s.EntryPaid || n.coins["0"] != 1000 || len(n.records) != 0 {
		t.Fatal("partial entry fee on failed start")
	}
}
func TestAuthoritativeSettlementRetriesAfterFailure(t *testing.T) {
	n := newAuthEconomyFake()
	s := economyTestState()
	ctx := context.Background()
	_ = s.charge(ctx, n)
	s.Game.Ranks = map[int]int{0: 1, 2: 2}
	n.fail = true
	if s.settle(ctx, n) == nil || s.Persisted || n.coins["0"] != 900 {
		t.Fatal("failed settlement marked committed")
	}
	n.fail = false
	if err := s.settle(ctx, n); err != nil {
		t.Fatal(err)
	}
	if n.coins["0"] != 1060 {
		t.Fatal("settlement did not recover")
	}
}
func TestAuthoritativeReadyWaitsForBothHumans(t *testing.T) {
	n := newAuthEconomyFake()
	s := economyTestState()
	s.Game.Phase = "ready"
	s.Game.Version = 0
	d := &authTestDispatcher{}
	m := &AuthoritativeLudoMatch{}
	ready0 := relayTestMessage{relayTestPresence: relayTestPresence{user: "0", session: "s0"}, op: authReady, data: []byte(`{}`)}
	ready2 := relayTestMessage{relayTestPresence: relayTestPresence{user: "2", session: "s2"}, op: authReady, data: []byte(`{}`)}
	m.MatchLoop(context.Background(), nil, nil, n, d, 1, s, []runtime.MatchData{ready0})
	if s.EntryPaid || s.Game.Phase != "ready" {
		t.Fatal("started before both clients ready")
	}
	m.MatchLoop(context.Background(), nil, nil, n, d, 2, s, []runtime.MatchData{ready2, ready0})
	if !s.EntryPaid || s.Game.Phase != "roll" || n.transactions != 1 || s.Game.Version != 1 {
		t.Fatal("ready did not start exactly once")
	}
	var batch authReply
	_ = json.Unmarshal(d.packets[0], &batch)
	if len(batch.Commands) != 3 || batch.Commands[0]["commandName"] != "StartGame" || batch.RemainingMs != 20000 {
		t.Fatalf("incomplete startup packet: %+v", batch)
	}
}
func TestAuthoritativeBotDoesNotNeedPresenceOrWallet(t *testing.T) {
	n := newAuthEconomyFake()
	s := economyTestState()
	s.Game.player(2).Bot = true
	s.Game.Phase = "ready"
	s.Game.Version = 0
	s.Ready["2"] = true
	delete(s.Presences, "2")
	msg := relayTestMessage{relayTestPresence: relayTestPresence{user: "0", session: "s0"}, op: authReady, data: []byte(`{}`)}
	(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, n, &authTestDispatcher{}, 1, s, []runtime.MatchData{msg})
	if !s.EntryPaid || n.coins["0"] != 900 || n.coins["2"] != 1000 || s.Game.Phase != "roll" {
		t.Fatal("bot match could not start or charged a bot")
	}
}
