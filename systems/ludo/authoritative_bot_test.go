package ludo

import "testing"

func authoritativeBotTestGame() *authGame {
	game := authTestGame(2)
	bot := game.player(0)
	bot.Bot = true
	bot.Profile = map[string]interface{}{
		"botDifficulty":  string(BotExpert),
		"botPersonality": string(BotStrategic),
	}
	return game
}

func evaluatedAuthoritativeMoves(game *authGame) []authoritativeBotMove {
	player := game.player(game.Current)
	moves := getAuthoritativeLegalMoves(game)
	weights := authoritativeBotWeightsFor(authoritativeBotPersonality(player))
	for i := range moves {
		moves[i] = evaluateAuthoritativeMove(game, player, moves[i], weights)
	}
	return moves
}

func TestAuthoritativeBotUsesUniqueIndianProfiles(t *testing.T) {
	used := map[int]bool{}
	names := map[string]bool{}
	for seat := 1; seat < 4; seat++ {
		template := selectUniqueAuthoritativeBotTemplate(seat, used)
		if template.Country != "IN" || template.Name == "" || template.AvatarID <= 0 {
			t.Fatalf("invalid bot identity: %+v", template)
		}
		if names[template.Name] {
			t.Fatalf("duplicate bot name selected: %s", template.Name)
		}
		names[template.Name] = true
	}
}

func TestAuthoritativeBotDefaultsToExpert(t *testing.T) {
	if got := authoritativeBotDifficulty(&authPlayer{}); got != BotExpert {
		t.Fatalf("default difficulty = %s, want %s", got, BotExpert)
	}
	player := &authPlayer{Profile: map[string]interface{}{"botDifficulty": string(BotExpert)}}
	if got := authoritativeBotDifficulty(player); got != BotExpert {
		t.Fatalf("profile difficulty = %s, want %s", got, BotExpert)
	}
}

func TestAuthoritativeBotKeepsDeadlineAndSchedulesNaturalDelay(t *testing.T) {
	players := []*authPlayer{
		{ID: 0, UserID: "bot", Bot: true, Profile: map[string]interface{}{"botDifficulty": string(BotExpert)}},
		{ID: 2, UserID: "human"},
	}
	game := newAuthGame(players)
	game.start(100)
	if game.Deadline != 100+authoritativeTurnTicks+5*authoritativeTickRate {
		t.Fatalf("normal turn deadline changed: %d", game.Deadline)
	}
	minimum := int64(100 + authoritativeBotRollDelayMinTicks + 5*authoritativeTickRate)
	maximum := int64(100 + authoritativeBotRollDelayMaxTicks + 5*authoritativeTickRate)
	if game.BotActionTick < minimum || game.BotActionTick > maximum || game.BotActionTick >= game.Deadline {
		t.Fatalf("bot action tick %d outside [%d,%d] before deadline %d", game.BotActionTick, minimum, maximum, game.Deadline)
	}
}

func TestAuthoritativeBotOnlyLegalMove(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 10)
	for piece := 1; piece < 4; piece++ {
		putAuthPiece(game, 0, piece, 57)
	}
	move, ok := selectAuthoritativeBotMove(game, nil)
	if !ok || move.PieceID != 0 || move.Dice != 1 {
		t.Fatalf("bot did not select the only legal move: %+v, ok=%t", move, ok)
	}
}

func TestAuthoritativeBotPrioritizesCapture(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 2)
	putAuthPiece(game, 0, 1, 15)
	putAuthPiece(game, 2, 0, 29)
	moves := evaluatedAuthoritativeMoves(game)
	var captureScore, ordinaryScore float64
	for _, move := range moves {
		if move.PieceID == 0 && move.Capture {
			captureScore = move.Score
		}
		if move.PieceID == 1 {
			ordinaryScore = move.Score
		}
	}
	if captureScore == 0 || captureScore <= ordinaryScore {
		t.Fatalf("capture score %.2f did not beat ordinary move %.2f: %+v", captureScore, ordinaryScore, moves)
	}
}

func TestAuthoritativeBotValuesSafetyAndCaptureRisk(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 8)  // Lands on global safe cell 8.
	putAuthPiece(game, 0, 1, 2)  // Lands on threatened global cell 2.
	putAuthPiece(game, 2, 0, 28) // Can reach global cell 2 with a one.
	moves := evaluatedAuthoritativeMoves(game)
	var safeMove, riskyMove *authoritativeBotMove
	for i := range moves {
		if moves[i].PieceID == 0 {
			safeMove = &moves[i]
		}
		if moves[i].PieceID == 1 {
			riskyMove = &moves[i]
		}
	}
	if safeMove == nil || riskyMove == nil || !safeMove.Safe || riskyMove.CaptureRisk <= 0 || safeMove.Score <= riskyMove.Score {
		t.Fatalf("safety/risk scoring mismatch: %+v", moves)
	}
}

func TestAuthoritativeBotPrioritizesExactFinish(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 56)
	putAuthPiece(game, 0, 1, 15)
	moves := evaluatedAuthoritativeMoves(game)
	var finishScore, ordinaryScore float64
	for _, move := range moves {
		if move.PieceID == 0 && move.ReachHome {
			finishScore = move.Score
		}
		if move.PieceID == 1 {
			ordinaryScore = move.Score
		}
	}
	if finishScore == 0 || finishScore <= ordinaryScore {
		t.Fatalf("finish score %.2f did not beat ordinary move %.2f: %+v", finishScore, ordinaryScore, moves)
	}
}

func TestAuthoritativeBotRejectsOwnTokenCollision(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{2}
	putAuthPiece(game, 0, 0, 2)
	putAuthPiece(game, 0, 1, 4)
	moves := getAuthoritativeLegalMoves(game)
	blockedTokenFound := false
	validTokenFound := false
	for _, move := range moves {
		if move.PieceID == 0 {
			blockedTokenFound = true
		}
		if move.PieceID == 1 {
			validTokenFound = true
		}
	}
	if blockedTokenFound || !validTokenFound {
		t.Fatalf("bot legal moves did not enforce own-token occupancy: %+v", moves)
	}
}

func TestAuthoritativeBotHandlesNoLegalMove(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	if moves := getAuthoritativeLegalMoves(game); len(moves) != 0 {
		t.Fatalf("base pieces moved without a six: %+v", moves)
	}
	if _, ok := selectAuthoritativeBotMove(game, nil); ok {
		t.Fatal("bot selected a move when none was legal")
	}
}

func TestAuthoritativeBotPerformsServerValidatedMove(t *testing.T) {
	game := authoritativeBotTestGame()
	game.Phase = "move"
	game.Rolls = []int{1}
	putAuthPiece(game, 0, 0, 10)
	for piece := 1; piece < 4; piece++ {
		putAuthPiece(game, 0, piece, 57)
	}
	if err := performAuthoritativeBotAction(game, nil, 200); err != nil {
		t.Fatal(err)
	}
	if game.player(0).Pieces[0].Passed != 11 {
		t.Fatalf("server did not apply selected legal move: %+v", game.player(0).Pieces[0])
	}
}
