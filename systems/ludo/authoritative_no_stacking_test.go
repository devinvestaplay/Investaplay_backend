package ludo

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

func ownCollisionGame(dice int) *authGame {
	game := authTestGame(2)
	game.Phase = "move"
	game.Rolls = []int{dice}
	putAuthPiece(game, 0, 0, 4)
	putAuthPiece(game, 0, 1, 2)
	return game
}

func containsDice(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestHumanOwnTokenLandingIsRejectedWithoutMutation(t *testing.T) {
	state := authTestState()
	state.Game = ownCollisionGame(2)
	state.Game.Version = 1
	before, _ := json.Marshal(state.Game)
	dispatcher := runAuthMessages(state, authHardeningIntent("own-collision", 1, authMove, 1, 2))
	after, _ := json.Marshal(state.Game)

	if string(before) != string(after) || state.Game.Version != 1 {
		t.Fatal("own-token collision mutated authoritative state")
	}
	if len(dispatcher.packets) != 1 || dispatcher.ops[0] != authError {
		t.Fatalf("own-token collision was not rejected: %v", dispatcher.ops)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(dispatcher.packets[0], &response); err != nil || response["error"] != "OWN_TOKEN_OCCUPIED" {
		t.Fatalf("wrong collision rejection: %s", dispatcher.packets[0])
	}
}

func TestBotOwnTokenLandingIsRejected(t *testing.T) {
	game := ownCollisionGame(2)
	game.player(0).Bot = true
	result, err := game.moveRequested(1, 2, 10)
	if !errors.Is(err, errOwnTokenOccupied) {
		t.Fatalf("bot collision returned %v, result=%+v", err, result)
	}
	if game.player(0).Pieces[1].Passed != 2 {
		t.Fatal("rejected bot move changed its token")
	}
}

func TestOwnBlockedTokenIsRemovedFromHumanSelectableMoves(t *testing.T) {
	game := ownCollisionGame(2)
	moves := game.moves()
	if _, selectable := moves[1]; selectable {
		t.Fatalf("blocked token was selectable: %+v", moves)
	}
	if _, selectable := moves[0]; !selectable {
		t.Fatalf("valid token was removed: %+v", moves)
	}
}

func TestWaitForMoveBroadcastUsesSameSelectableRule(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 4)
	putAuthPiece(game, 0, 1, 2)
	game.Commands = nil
	game.Phase = "roll"
	game.Rolls = nil
	game.Available = 1
	if err := game.roll(2, 10); err != nil {
		t.Fatal(err)
	}
	last := game.Commands[len(game.Commands)-1]
	selectable, ok := last["moveablePieces"].(map[int][]authMoveData)
	if !ok {
		t.Fatalf("missing authoritative selectable-token payload: %+v", last)
	}
	if _, blocked := selectable[1]; blocked {
		t.Fatalf("Unity payload exposed own-collision token: %+v", selectable)
	}
	if _, valid := selectable[0]; !valid {
		t.Fatalf("Unity payload omitted valid token: %+v", selectable)
	}
}

func TestDiceRejectedWhenOneTokenCollidesDespiteAnotherValidMove(t *testing.T) {
	game := ownCollisionGame(2)
	game.player(0).Bot = true
	game.Phase = "roll"
	game.Rolls = nil
	pool := authoritativeBotDicePool(game, nil)
	if containsDice(pool, 2) {
		t.Fatalf("dice 2 remained available despite an own-token collision: %v", pool)
	}
}

func TestHumanAndBotUseSameOwnCollisionDicePool(t *testing.T) {
	game := ownCollisionGame(2)
	game.Phase = "roll"
	game.Rolls = nil
	humanPool, _ := game.authoritativeDicePool()
	game.player(0).Bot = true
	botPool := authoritativeBotDicePool(game, nil)
	if !reflect.DeepEqual(humanPool, botPool) {
		t.Fatalf("human and bot dice pools differ: human=%v bot=%v", humanPool, botPool)
	}
}

func TestAllOwnBlockedMovesUseExistingPassTurn(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 52)
	putAuthPiece(game, 0, 1, 54)
	putAuthPiece(game, 0, 2, 56)
	game.Phase = "roll"
	game.Rolls = nil
	game.Available = 1
	if err := game.roll(2, 10); err != nil {
		t.Fatal(err)
	}
	if game.Current != 2 || game.Phase != "roll" {
		t.Fatalf("no-move roll did not pass the turn: current=%d phase=%s", game.Current, game.Phase)
	}
}

func TestBotFiltersDiceWhenEveryOtherwiseLegalDestinationIsOwnOccupied(t *testing.T) {
	game := authTestGame(2)
	game.player(0).Bot = true
	putAuthPiece(game, 0, 0, 52)
	putAuthPiece(game, 0, 1, 54)
	putAuthPiece(game, 0, 2, 56)
	pool := authoritativeBotDicePool(game, nil)
	if containsDice(pool, 2) {
		t.Fatalf("dice 2 should have no valid destination: %v", pool)
	}
}

func TestOpponentDestinationStillCaptures(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 2)
	putAuthPiece(game, 2, 0, 29)
	game.Phase = "move"
	game.Rolls = []int{1}
	if err := game.move(0, 1, 10); err != nil {
		t.Fatal(err)
	}
	if game.player(2).Pieces[0].Passed != 0 || game.player(0).Kills != 1 {
		t.Fatal("opponent capture behavior changed")
	}
}

func TestRollSixCanSpawnOntoOwnTokenOnSafeStart(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 1)
	game.Phase = "move"
	game.Rolls = []int{6}
	moves := game.moves()
	if _, valid := moves[0]; !valid {
		t.Fatal("existing token should retain its valid six move")
	}
	for pieceID := 1; pieceID < 4; pieceID++ {
		if _, valid := moves[pieceID]; !valid {
			t.Fatalf("base token %d could not stack on its safe start square", pieceID)
		}
	}
}

func TestSafeSquareStackDoesNotFilterDice(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 1)
	game.Phase = "roll"
	game.Rolls = nil
	pool, rejected := game.authoritativeDicePool()
	if !containsDice(pool, 6) || containsDice(rejected, 6) {
		t.Fatalf("safe start stack incorrectly filtered dice 6: allowed=%v rejected=%v", pool, rejected)
	}
}

func TestSameColorTokensCanStackOnSafeSharedSquare(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 9) // Player 0 global position 8 is safe.
	putAuthPiece(game, 0, 1, 7)
	game.Phase = "move"
	game.Rolls = []int{2}
	if !game.legal(1, 2) {
		t.Fatal("same-color landing on a safe shared square was rejected")
	}
	if err := game.move(1, 2, 10); err != nil {
		t.Fatalf("same-color safe-square stack failed: %v", err)
	}
	if err := game.validateCanonicalState(); err != nil {
		t.Fatalf("canonical state rejected safe-square stack: %v", err)
	}
}

func TestHomePathAndCompletedTokensPreserveExceptions(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 1)  // Shared-track position 0.
	putAuthPiece(game, 0, 1, 52) // Private-home position 0: a different square.
	putAuthPiece(game, 0, 2, 57) // Completed tokens are not active squares.
	putAuthPiece(game, 0, 3, 56)
	if err := game.validateCanonicalState(); err != nil {
		t.Fatalf("valid shared/home representation rejected: %v", err)
	}
	game.Phase = "move"
	game.Rolls = []int{1}
	if !game.legal(1, 1) || !game.legal(3, 1) {
		t.Fatal("private-home or completed-home movement was incorrectly blocked")
	}
}

func TestCanonicalStateRejectsSameColorOverlapBeforeResync(t *testing.T) {
	game := authTestGame(2)
	putAuthPiece(game, 0, 0, 10)
	putAuthPiece(game, 0, 1, 10)
	if err := game.validateCanonicalState(); err == nil {
		t.Fatal("canonical state accepted two active same-color tokens on one square")
	}
}

func TestResyncRefusesOverlappingSameColorState(t *testing.T) {
	state := authTestState()
	putAuthPiece(state.Game, 0, 0, 10)
	putAuthPiece(state.Game, 0, 1, 10)
	dispatcher := &authTestDispatcher{}
	state.sendSnapshot(dispatcher, nil, state.Presences["0"], 10)
	if len(dispatcher.ops) != 1 || dispatcher.ops[0] != authError {
		t.Fatalf("invalid overlapping state was sent as a snapshot: %v", dispatcher.ops)
	}
}

func TestBotPrefersCaptureOverSpawningOnSix(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{6}
	putAuthPiece(game, 0, 0, 2)
	putAuthPiece(game, 2, 0, 34) // Global position 7, captured by token 0 with six.
	moves := evaluatedAuthoritativeMoves(game)
	var captureScore, spawnScore float64
	for _, move := range moves {
		if move.PieceID == 0 && move.Capture {
			captureScore = move.Score
		}
		if move.Spawn {
			spawnScore = move.Score
		}
	}
	if captureScore == 0 || captureScore <= spawnScore {
		t.Fatalf("capture %.2f did not outrank spawn %.2f: %+v", captureScore, spawnScore, moves)
	}
}
