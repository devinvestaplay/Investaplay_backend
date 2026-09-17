package ludo

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/heroiclabs/nakama-common/runtime"
)

func authHardeningIntent(id string, version int, op int64, pieceID, claimedDice int) runtime.MatchData {
	data, _ := json.Marshal(authRequest{ID: id, Version: version, Piece: pieceID, Dice: claimedDice})
	return relayTestMessage{relayTestPresence: relayTestPresence{user: "0", session: "s0"}, op: op, data: data}
}

func runAuthMessages(state *authMatchState, messages ...runtime.MatchData) *authTestDispatcher {
	dispatcher := &authTestDispatcher{}
	(&AuthoritativeLudoMatch{}).MatchLoop(context.Background(), nil, nil, nil, dispatcher, 10, state, messages)
	return dispatcher
}

func TestAuthoritativeDuplicateMoveAppliedExactlyOnce(t *testing.T) {
	state := authTestState()
	state.Game.Phase = "move"
	state.Game.Rolls = []int{6}
	message := authHardeningIntent("move-once", 1, authMove, 0, 6)
	dispatcher := runAuthMessages(state, message, message, message)

	piece := state.Game.player(0).Pieces[0]
	if state.Game.Version != 2 || piece.Passed != 1 || piece.Position != 0 {
		t.Fatalf("duplicate move mutated state more than once: version=%d piece=%+v", state.Game.Version, piece)
	}
	if len(dispatcher.packets) != 3 || string(dispatcher.packets[0]) != string(dispatcher.packets[1]) || string(dispatcher.packets[1]) != string(dispatcher.packets[2]) {
		t.Fatal("duplicate move did not return the cached authoritative result")
	}
}

func TestAuthoritativeRejectedRequestIsIdempotent(t *testing.T) {
	state := authTestState()
	state.Game.Phase = "move"
	state.Game.Rolls = []int{6}
	message := authHardeningIntent("bad-move", 1, authMove, 0, 5)
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, message, message, message)
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || state.Game.Version != 1 {
		t.Fatal("repeated rejected action mutated authoritative state")
	}
	if len(dispatcher.ops) != 3 || dispatcher.ops[0] != authError || dispatcher.ops[1] != authError || dispatcher.ops[2] != authError {
		t.Fatalf("unexpected duplicate rejection responses: %v", dispatcher.ops)
	}
}

func TestAuthoritativeFutureVersionReturnsRecoverySnapshot(t *testing.T) {
	state := authTestState()
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, authHardeningIntent("future", 9, authRoll, 0, 0))
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || state.Game.Version != 1 {
		t.Fatal("future-version action mutated state")
	}
	if len(dispatcher.ops) != 1 || dispatcher.ops[0] != authSnapshot {
		t.Fatalf("future version did not receive a recovery snapshot: %v", dispatcher.ops)
	}
}

func TestAuthoritativeMoveBeforeRollRejected(t *testing.T) {
	state := authTestState()
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, authHardeningIntent("early-move", 1, authMove, 0, 0))
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || len(dispatcher.ops) != 1 || dispatcher.ops[0] != authError {
		t.Fatal("move before roll was not rejected without mutation")
	}
}

func TestAuthoritativeSecondRollBeforeMoveRejected(t *testing.T) {
	state := authTestState()
	state.Game.Phase = "move"
	state.Game.Rolls = []int{6}
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, authHardeningIntent("double-roll", 1, authRoll, 0, 0))
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || len(dispatcher.ops) != 1 || dispatcher.ops[0] != authError {
		t.Fatal("second roll was accepted while a move was required")
	}
}

func TestAuthoritativeIllegalTokenAndMoveAmountRejected(t *testing.T) {
	for _, test := range []struct {
		name  string
		piece int
		dice  int
	}{
		{name: "illegal token", piece: 9, dice: 6},
		{name: "incorrect amount", piece: 0, dice: 5},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := authTestState()
			state.Game.Phase = "move"
			state.Game.Rolls = []int{6}
			before, _ := json.Marshal(state.Game)
			dispatcher := runAuthMessages(state, authHardeningIntent(test.name, 1, authMove, test.piece, test.dice))
			after, _ := json.Marshal(state.Game)
			if string(before) != string(after) || len(dispatcher.ops) != 1 || dispatcher.ops[0] != authError {
				t.Fatalf("invalid move mutated state: piece=%d dice=%d", test.piece, test.dice)
			}
		})
	}
}

func TestAuthoritativeMoveCanOmitClientDice(t *testing.T) {
	state := authTestState()
	state.Game.Phase = "move"
	state.Game.Rolls = []int{2}
	putAuthPiece(state.Game, 0, 0, 10)
	runAuthMessages(state, authHardeningIntent("piece-only", 1, authMove, 0, 0))

	if piece := state.Game.player(0).Pieces[0]; piece.Passed != 12 {
		t.Fatalf("server did not apply its authoritative dice: %+v", piece)
	}
}

func TestAuthoritativeDuplicateResyncIsReadOnly(t *testing.T) {
	state := authTestState()
	data := []byte(`{"afterVersion":1}`)
	message := relayTestMessage{relayTestPresence: relayTestPresence{user: "0", session: "s0"}, op: authSync, data: data}
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, message, message, message)
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || state.Game.Version != 1 {
		t.Fatal("duplicate resync mutated gameplay state")
	}
	if len(dispatcher.ops) != 3 {
		t.Fatalf("expected one read-only response per resync, got %v", dispatcher.ops)
	}
	for _, opCode := range dispatcher.ops {
		if opCode != authSnapshot {
			t.Fatalf("resync returned gameplay opcode %d", opCode)
		}
	}
}

func TestAuthoritativeTokenRemainsCanonicalAfterMove(t *testing.T) {
	state := authTestState()
	state.Game.Phase = "move"
	state.Game.Rolls = []int{3}
	putAuthPiece(state.Game, 0, 0, 10)
	runAuthMessages(state, authHardeningIntent("canonical", 1, authMove, 0, 0))

	player := state.Game.player(0)
	count := 0
	for index, piece := range player.Pieces {
		if piece.PieceID != index {
			t.Fatalf("piece identity changed: %+v", piece)
		}
		if piece.PieceID == 0 {
			count++
		}
	}
	if count != 1 || len(player.Pieces) != 4 || player.Pieces[0].Passed != 13 || player.Pieces[0].Position != 12 {
		t.Fatalf("token was duplicated or position was not canonical: %+v", player.Pieces)
	}
	if err := state.Game.validateCanonicalState(); err != nil {
		t.Fatal(err)
	}
}

func TestAuthoritativeResponseCacheIsBounded(t *testing.T) {
	state := authTestState()
	for index := 0; index < authResponseCacheLimit+10; index++ {
		requestID := fmt.Sprintf("request-%d", index)
		state.cacheResponse("0", requestID, authError, []byte(requestID))
	}
	if len(state.Responses["0"]) != authResponseCacheLimit || len(state.ResponseOrder["0"]) != authResponseCacheLimit {
		t.Fatalf("response cache is not bounded: responses=%d order=%d", len(state.Responses["0"]), len(state.ResponseOrder["0"]))
	}
	if _, found := state.Responses["0"]["request-0"]; found {
		t.Fatal("oldest cached request was not evicted")
	}
}

func TestAuthoritativeBotTransitionUsesCanonicalRules(t *testing.T) {
	state := authTestState()
	bot := state.Game.player(0)
	bot.Bot = true
	bot.Profile = map[string]interface{}{"botDifficulty": string(BotExpert)}
	state.Game.Phase = "move"
	state.Game.Rolls = []int{1}
	putAuthPiece(state.Game, 0, 0, 10)
	for pieceID := 1; pieceID < 4; pieceID++ {
		putAuthPiece(state.Game, 0, pieceID, 57)
	}

	if err := state.applyTransition(func(game *authGame) error {
		return performAuthoritativeBotAction(game, nil, 20)
	}); err != nil {
		t.Fatal(err)
	}
	if state.Game.player(0).Pieces[0].Passed != 11 || state.Game.Version != 1 {
		t.Fatalf("bot bypassed canonical movement/version boundary: %+v", state.Game.player(0).Pieces[0])
	}
	if err := state.Game.validateCanonicalState(); err != nil {
		t.Fatal(err)
	}
}
