package ludo

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"
)

func authTestGame(n int) *authGame {
	players := []*authPlayer{}
	for i := 0; i < n; i++ {
		id := i
		if n == 2 {
			id = i * 2
		}
		players = append(players, &authPlayer{ID: id, UserID: fmt.Sprint(id)})
	}
	g := newAuthGame(players)
	g.start(0)
	g.Commands = nil
	return g
}
func putAuthPiece(g *authGame, id, piece, passed int) {
	p := g.player(id)
	pc := &p.Pieces[piece]
	pc.Passed = passed
	pc.OnHomeColumn = passed >= 52
	if passed == 0 {
		pc.Position = -1
	} else if passed >= 52 {
		pc.Position = passed - 52
	} else {
		pc.Position = (p.Start + passed - 1) % 52
	}
}
func TestAuthoritativeManualStart(t *testing.T) {
	g := authTestGame(2)
	if g.Current != 0 || g.Phase != "roll" || len(g.Rolls) != 0 || g.Available != 1 {
		t.Fatalf("start must wait for a tap: %+v", g)
	}
}
func TestAuthoritativeBaseEntryAndBonus(t *testing.T) {
	g := authTestGame(2)
	if err := g.roll(6, 1); err != nil {
		t.Fatal(err)
	}
	if g.Phase != "move" || len(g.moves()) != 4 {
		t.Fatal("six must offer base pieces before bonus roll")
	}
	if err := g.move(0, 6, 2); err != nil {
		t.Fatal(err)
	}
	pc := g.player(0).Pieces[0]
	if pc.Passed != 1 || pc.Position != 0 || g.Phase != "roll" || g.Current != 0 {
		t.Fatalf("entry/bonus mismatch: %+v", g)
	}
}
func TestAuthoritativeNoMoveAdvancesInSameBatch(t *testing.T) {
	g := authTestGame(2)
	_ = g.roll(1, 1)
	names := []string{}
	for _, c := range g.Commands {
		names = append(names, c["commandName"].(string))
	}
	if !reflect.DeepEqual(names, []string{"RollDice", "SelectPlayerTurn", "WaitForRollDice"}) || g.Current != 2 {
		t.Fatalf("unexpected batch: %v", names)
	}
}
func TestAuthoritativeThreeSixesEndsTurn(t *testing.T) {
	g := authTestGame(2)
	for i := 0; i < 3; i++ {
		if err := g.roll(6, int64(i)); err != nil {
			t.Fatal(err)
		}
		if i < 2 {
			if err := g.move(i, 6, int64(i)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if g.Current != 2 || len(g.Rolls) != 0 {
		t.Fatal("third six must discard remaining moves and change turn")
	}
}
func TestAuthoritativeRejectIllegalMovesWithoutMutation(t *testing.T) {
	for _, move := range [][2]int{{-1, 6}, {4, 6}, {0, 0}, {0, 7}, {0, 5}} {
		g := authTestGame(2)
		_ = g.roll(6, 1)
		before, _ := json.Marshal(g)
		if g.move(move[0], move[1], 2) == nil {
			t.Fatalf("accepted %v", move)
		}
		after, _ := json.Marshal(g)
		if string(before) != string(after) {
			t.Fatalf("rejected move mutated state: %v", move)
		}
	}
}
func TestAuthoritativeCaptureUsesGlobalPosition(t *testing.T) {
	g := authTestGame(2)
	putAuthPiece(g, 0, 0, 2)
	putAuthPiece(g, 2, 0, 29) // global 1 -> 2, opponent global 2
	_ = g.roll(1, 1)
	if err := g.move(0, 1, 2); err != nil {
		t.Fatal(err)
	}
	if g.player(2).Pieces[0].Passed != 0 || g.player(0).Kills != 1 || g.player(2).Deaths != 1 || g.Available != 1 || g.Current != 0 {
		t.Fatal("capture did not return opponent to base and award roll")
	}
	found := false
	for _, c := range g.Commands {
		if c["commandName"] == "CapturePiece" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing capture animation")
	}
}
func TestAuthoritativeSafeAndBlockedCells(t *testing.T) {
	g := authTestGame(2)
	putAuthPiece(g, 0, 0, 2)
	putAuthPiece(g, 2, 0, 29)
	putAuthPiece(g, 2, 1, 29)
	if g.legal(0, 1) {
		t.Fatal("crossed opposing blockade")
	}
	putAuthPiece(g, 0, 0, 8)
	putAuthPiece(g, 2, 0, 35)
	putAuthPiece(g, 2, 1, 35) // global safe 8
	if !g.legal(0, 1) {
		t.Fatal("safe cell incorrectly blocked")
	}
	_ = g.roll(1, 1)
	_ = g.move(0, 1, 2)
	if g.player(2).Pieces[0].Passed != 35 {
		t.Fatal("captured on safe cell")
	}
}
func TestAuthoritativePrivateHomeAndExactFinish(t *testing.T) {
	g := authTestGame(2)
	putAuthPiece(g, 0, 0, 51)
	putAuthPiece(g, 2, 0, 27)
	putAuthPiece(g, 2, 1, 27)
	if !g.legal(0, 1) {
		t.Fatal("shared-track stack blocked private home entry")
	}
	_ = g.roll(1, 1)
	if err := g.move(0, 1, 2); err != nil {
		t.Fatal(err)
	}
	if pc := g.player(0).Pieces[0]; !pc.OnHomeColumn || pc.Position != 0 || pc.Passed != 52 {
		t.Fatalf("bad home entry: %+v", pc)
	}
	g.Current = 0
	g.Phase = "move"
	g.Rolls = []int{1, 2}
	putAuthPiece(g, 0, 0, 56)
	if g.legal(0, 2) {
		t.Fatal("allowed home overshoot")
	}
	if err := g.move(0, 1, 3); err != nil {
		t.Fatal(err)
	}
	if g.player(0).Finished != 1 || g.player(0).Pieces[0].Passed != 57 {
		t.Fatal("exact finish not counted")
	}
}
func TestAuthoritativeRankingAfterSurrender(t *testing.T) {
	g := authTestGame(4)
	g.surrender(3, 1)
	p := g.player(0)
	p.Finished = 3
	for i := 0; i < 3; i++ {
		putAuthPiece(g, 0, i, 57)
	}
	putAuthPiece(g, 0, 3, 56)
	g.Phase = "move"
	g.Rolls = []int{1}
	if err := g.move(3, 1, 2); err != nil {
		t.Fatal(err)
	}
	if g.Ranks[0] != 1 || g.Ranks[3] != 5 || g.Phase == "finished" {
		t.Fatalf("invalid ranks: %v", g.Ranks)
	}
	g.surrender(2, 3)
	if g.Phase != "finished" || g.Ranks[1] != 2 {
		t.Fatalf("remaining finisher rank: %v", g.Ranks)
	}
}
func TestAuthoritativeTwoPlayerFinishRanks(t *testing.T) {
	g := authTestGame(2)
	p := g.player(0)
	p.Finished = 3
	for i := 0; i < 3; i++ {
		putAuthPiece(g, 0, i, 57)
	}
	putAuthPiece(g, 0, 3, 56)
	g.Phase = "move"
	g.Rolls = []int{1}
	_ = g.move(3, 1, 1)
	if g.Phase != "finished" || g.Ranks[0] != 1 || g.Ranks[2] != 2 {
		t.Fatalf("bad finish ranks: %v", g.Ranks)
	}
}

type authTestDispatcher struct {
	runtime.MatchDispatcher
	ops     []int64
	packets [][]byte
	senders []runtime.Presence
}

func (d *authTestDispatcher) BroadcastMessage(op int64, data []byte, to []runtime.Presence, sender runtime.Presence, reliable bool) error {
	d.ops = append(d.ops, op)
	d.packets = append(d.packets, append([]byte(nil), data...))
	d.senders = append(d.senders, sender)
	return nil
}
func authTestState() *authMatchState {
	g := authTestGame(2)
	g.Version = 1
	return &authMatchState{Game: g, Presences: map[string]runtime.Presence{"0": relayTestPresence{user: "0", session: "s0"}, "2": relayTestPresence{user: "2", session: "s2"}}, Ready: map[string]bool{}, Responses: map[string]map[string]authCachedResponse{}, ResponseOrder: map[string][]string{}}
}
func authTestIntent(user, session, id string, version int, op int64) runtime.MatchData {
	data, _ := json.Marshal(authRequest{ID: id, Version: version, Piece: 0, Dice: 6})
	return relayTestMessage{relayTestPresence: relayTestPresence{user: user, session: session}, op: op, data: data}
}
func TestAuthoritativeDuplicateRollReturnsSameOutcome(t *testing.T) {
	s := authTestState()
	d := &authTestDispatcher{}
	m := &AuthoritativeLudoMatch{}
	msg := authTestIntent("0", "s0", "roll", 1, authRoll)
	m.MatchLoop(context.Background(), nil, nil, nil, d, 1, s, []runtime.MatchData{msg, msg})
	if s.Game.Version != 2 || len(d.packets) != 2 || string(d.packets[0]) != string(d.packets[1]) {
		t.Fatal("retry rolled again or changed outcome")
	}
	for _, sender := range d.senders {
		if sender != nil {
			t.Fatal("result must originate from server")
		}
	}
}
func TestAuthoritativeRejectsWrongTurnStaleAndSpoofedSession(t *testing.T) {
	for _, test := range []struct {
		name, user, session string
		version             int
		reply               bool
		recovery            bool
	}{{"wrong turn", "2", "s2", 1, true, false}, {"stale", "0", "s0", 0, true, true}, {"session spoof", "0", "fake", 1, false, false}, {"non member", "9", "s9", 1, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			s := authTestState()
			d := &authTestDispatcher{}
			before, _ := json.Marshal(s.Game)
			(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, nil, d, 1, s, []runtime.MatchData{authTestIntent(test.user, test.session, "r", test.version, authRoll)})
			after, _ := json.Marshal(s.Game)
			if string(before) != string(after) {
				t.Fatal("invalid request changed state")
			}
			expectedOpcode := authError
			if test.recovery {
				expectedOpcode = authSnapshot
			}
			if test.reply && (len(d.ops) != 1 || d.ops[0] != expectedOpcode) {
				t.Fatal("missing rejection")
			}
			if !test.reply && len(d.ops) != 0 {
				t.Fatal("spoofed presence accepted")
			}
		})
	}
}
func TestAuthoritativeCannotSurrenderBeforeEntry(t *testing.T) {
	s := authTestState()
	s.Game.Phase = "ready"
	d := &authTestDispatcher{}
	(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, nil, d, 1, s, []runtime.MatchData{authTestIntent("0", "s0", "s", 1, authSurrender)})
	if len(s.Game.Ranks) != 0 || s.Game.Phase != "ready" || d.ops[0] != authError {
		t.Fatal("unpaid match could award ranks")
	}
}
func TestAuthoritativeTimeoutIsServerDriven(t *testing.T) {
	s := authTestState()
	d := &authTestDispatcher{}
	(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, nil, d, s.Game.Deadline, s, nil)
	if s.Game.Version != 2 || len(d.packets) != 1 {
		t.Fatal("server timeout did not resolve turn")
	}
}
func TestAuthoritativeReplayContainsAllMissedBatches(t *testing.T) {
	s := authTestState()
	s.Game.Version = 0
	s.Game.Commands = []authCommand{{"commandName": "WaitForRollDice"}}
	d := &authTestDispatcher{}
	s.flush(d, nil, 0, "", time.Now())
	_ = s.Game.roll(1, 1)
	s.flush(d, nil, 1, "r", time.Now())
	d = &authTestDispatcher{}
	msg := relayTestMessage{relayTestPresence: relayTestPresence{user: "0", session: "s0"}, op: authSync, data: []byte(`{"afterVersion":0}`)}
	(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, nil, d, 2, s, []runtime.MatchData{msg})
	if len(d.packets) != 2 {
		t.Fatal("resync lost batches")
	}
	for i, packet := range d.packets {
		var got, original authReply
		_ = json.Unmarshal(packet, &got)
		_ = json.Unmarshal(s.History[i], &original)
		if got.Version != original.Version || !reflect.DeepEqual(got.Commands, original.Commands) {
			t.Fatal("resync lost commands")
		}
		if i == 0 && got.RemainingMs != 0 {
			t.Fatal("replay resurrected an expired turn timer")
		}
	}
}
func TestAuthoritativeCustomRoomDoesNotRelayHostDice(t *testing.T) {
	s := &LudoCustomRoomMatchState{Protocol: 2, Authority: authTestState()}
	d := &authTestDispatcher{}
	(&LudoCustomRoomMatch{}).MatchLoop(context.Background(), nil, nil, nil, d, 1, s, []runtime.MatchData{authTestIntent("0", "s0", "host", 1, 4)})
	if s.Authority.Game.Version != 1 || len(d.ops) != 1 || d.ops[0] != authError {
		t.Fatal("legacy host opcode was relayed")
	}
}
func TestAuthoritativeRejectsClientRankings(t *testing.T) {
	if _, err := ludoMatchFinish(context.Background(), nil, nil, nil, `{"ranking":{"attacker":{"rank":1}}}`); err == nil {
		t.Fatal("client-supplied results accepted")
	}
}

func TestAuthoritativeCompleteGamesPreserveBoardInvariants(t *testing.T) {
	for _, count := range []int{2, 4} {
		for seed := int64(1); seed <= 10; seed++ {
			g := authTestGame(count)
			rng := rand.New(rand.NewSource(seed))
			for action := 0; g.Phase != "finished" && action < 20000; action++ {
				var err error
				if g.Phase == "roll" {
					err = g.roll(1+rng.Intn(6), int64(action))
				} else {
					moves := g.moves()
					ids := []int{}
					for id := range moves {
						ids = append(ids, id)
					}
					sort.Ints(ids)
					if len(ids) == 0 {
						t.Fatal("move phase has no legal move")
					}
					id := ids[rng.Intn(len(ids))]
					options := moves[id]
					err = g.move(id, options[rng.Intn(len(options))].Dice, int64(action))
				}
				if err != nil {
					t.Fatal(err)
				}
				for _, p := range g.Players {
					finished := 0
					for _, pc := range p.Pieces {
						if pc.Passed < 0 || pc.Passed > 57 {
							t.Fatal("piece escaped board")
						}
						if pc.Passed == 57 {
							finished++
						}
						expected := -1
						if pc.Passed >= 52 {
							expected = pc.Passed - 52
						} else if pc.Passed > 0 {
							expected = (p.Start + pc.Passed - 1) % 52
						}
						if pc.Position != expected || pc.OnHomeColumn != (pc.Passed >= 52) {
							t.Fatalf("board desync: %+v", pc)
						}
					}
					if p.Finished != finished {
						t.Fatal("finished count mismatch")
					}
				}
				g.Commands = nil
			}
			if g.Phase != "finished" || len(g.Ranks) != count {
				t.Fatalf("game did not finish: players %d seed %d", count, seed)
			}
		}
	}
}
