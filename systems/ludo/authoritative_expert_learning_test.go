package ludo

import (
	"context"
	"testing"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
)

func expertEvaluatedMoves(game *authGame) []authoritativeBotMove {
	player := game.player(game.Current)
	moves := getAuthoritativeLegalMoves(game)
	for index := range moves {
		moves[index] = evaluateExpertAuthoritativeMove(game, player, moves[index])
	}
	return moves
}

func TestExpertEvaluatesWholeBoardOnSix(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{6}
	putAuthPiece(game, 0, 1, 51) // Can finish.
	putAuthPiece(game, 0, 2, 2)  // Can capture.
	putAuthPiece(game, 0, 3, 20) // Can progress.
	putAuthPiece(game, 2, 0, 34)

	moves := getAuthoritativeLegalMoves(game)
	seen := map[int]bool{}
	for _, move := range moves {
		seen[move.PieceID] = true
	}
	if len(seen) != 4 {
		t.Fatalf("Expert did not consider every token on six: %+v", moves)
	}
	selected, ok := selectAuthoritativeBotMove(game, nil)
	if !ok || selected.PieceID != 1 || !selected.ReachHome {
		t.Fatalf("Expert ignored clearly superior finish: %+v", selected)
	}
}

func TestExpertReevaluatesAllTokensAfterSpawn(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{6}
	putAuthPiece(game, 0, 0, 1)  // Previously spawned token.
	putAuthPiece(game, 0, 1, 2)  // Capture option.
	putAuthPiece(game, 0, 2, 51) // Finish option.
	putAuthPiece(game, 0, 3, 20)
	putAuthPiece(game, 2, 0, 34)

	selected, ok := selectAuthoritativeBotMove(game, nil)
	if !ok || selected.PieceID != 2 || !selected.ReachHome {
		t.Fatalf("Expert followed spawned token instead of reevaluating board: %+v", selected)
	}
}

func TestExpertCaptureCanPreventOpponentFinish(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{6}
	putAuthPiece(game, 0, 0, 19) // Lands on global 24.
	putAuthPiece(game, 2, 0, 51) // Global 24 and one six from finish.
	moves := expertEvaluatedMoves(game)
	found := false
	for _, move := range moves {
		if move.PieceID == 0 && move.Capture {
			found = true
			if move.PreventedFinish <= 0 {
				t.Fatalf("capture did not recognize prevented finish: %+v", move)
			}
		}
	}
	if !found {
		t.Fatalf("expected strategic capture candidate: %+v", moves)
	}
}

func TestLearnedAggressionIncreasesExpertRiskPenalty(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 2)  // Lands on global 2.
	putAuthPiece(game, 2, 0, 28) // Can capture global 2 with one.
	for pieceID := 1; pieceID < 4; pieceID++ {
		putAuthPiece(game, 0, pieceID, 57)
	}

	baseline := expertEvaluatedMoves(game)[0]
	game.HumanModels[2] = authHumanBehavior{Schema: 1, Moves: 100, Captures: 55}
	learned := expertEvaluatedMoves(game)[0]
	if learned.CaptureRisk <= baseline.CaptureRisk || learned.Score >= baseline.Score {
		t.Fatalf("learned aggression did not increase caution: baseline=%+v learned=%+v", baseline, learned)
	}
}

func TestHumanInteractionUpdatesAggregateModel(t *testing.T) {
	before := authTestGame(2)
	before.Phase = "move"
	before.Rolls = []int{1}
	putAuthPiece(before, 0, 0, 2)
	putAuthPiece(before, 2, 0, 29)
	after := before.clone()
	result, err := after.moveRequested(0, 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	after.observeHumanMove(before, 0, result)
	model := after.HumanModels[0]
	if model.Moves != 1 || model.Captures != 1 {
		t.Fatalf("human interaction was not learned: %+v", model)
	}
}

type authLearningFake struct {
	runtime.NakamaModule
	records map[string]string
}

func (fake *authLearningFake) StorageRead(ctx context.Context, reads []*runtime.StorageRead) ([]*api.StorageObject, error) {
	objects := []*api.StorageObject{}
	for _, read := range reads {
		if value, found := fake.records[read.Key]; found {
			objects = append(objects, &api.StorageObject{Collection: read.Collection, Key: read.Key, Value: value})
		}
	}
	return objects, nil
}

func (fake *authLearningFake) StorageWrite(ctx context.Context, writes []*runtime.StorageWrite) ([]*api.StorageObjectAck, error) {
	if fake.records == nil {
		fake.records = map[string]string{}
	}
	for _, write := range writes {
		fake.records[write.Key] = write.Value
	}
	return nil, nil
}

func TestHumanLearningPersistsAcrossMatches(t *testing.T) {
	fake := &authLearningFake{records: map[string]string{}}
	first := authTestGame(2)
	first.HumanModels[0] = authHumanBehavior{Schema: 1, Moves: 12, Captures: 4, SafeMoves: 3}
	if err := persistAuthoritativeHumanModels(context.Background(), fake, first, nil); err != nil {
		t.Fatal(err)
	}

	second := authTestGame(2)
	loadAuthoritativeHumanModels(context.Background(), fake, second, nil)
	loaded := second.HumanModels[0]
	if loaded.Moves != 12 || loaded.Captures != 4 || loaded.Games != 1 {
		t.Fatalf("learning profile was not restored: %+v", loaded)
	}
	if key := authoritativeLearningKey("0"); key == "" || key == "0" {
		t.Fatal("learning storage key must be stable and pseudonymous")
	}
}

func TestDifficultyPoliciesRemainSeparated(t *testing.T) {
	move := authoritativeBotMove{FromPassed: 10, ToPassed: 16, Capture: true, CapturedProgress: 30}
	medium := evaluateMediumAuthoritativeMove(move)
	hard := evaluateAuthoritativeMove(nil, nil, move, authoritativeBotWeightsFor(BotBalanced))
	if medium.Score == hard.Score || medium.Reason == hard.Reason {
		t.Fatalf("Medium and Hard collapsed to one policy: medium=%+v hard=%+v", medium, hard)
	}
}
