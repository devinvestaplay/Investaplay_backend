package ludo

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	authoritativeBotRollDelayMinTicks = 24 // 0.8 seconds at 30 Hz.
	authoritativeBotRollDelayMaxTicks = 54 // 1.8 seconds at 30 Hz.
	authoritativeBotMoveDelayMinTicks = 36 // Allow the dice animation to finish.
	authoritativeBotMoveDelayMaxTicks = 54 // 1.2-1.8 seconds after the roll.
)

type authoritativeBotWeights struct {
	Progress         float64
	Capture          float64
	Escape           float64
	Safety           float64
	HomeEntry        float64
	ReachHome        float64
	OpponentPressure float64
	CaptureRisk      float64
	Exposure         float64
	Future           float64
}

type authoritativeBotMove struct {
	PieceID          int
	Dice             int
	FromPassed       int
	ToPassed         int
	FromPosition     int
	ToPosition       int
	Capture          bool
	CapturedProgress int
	EscapesDanger    bool
	Safe             bool
	Spawn            bool
	HomeEntry        bool
	ReachHome        bool
	CaptureRisk      float64
	Pressure         float64
	FutureValue      float64
	Score            float64
	Reason           string
}

func selectUniqueAuthoritativeBotTemplate(seat int, used map[int]bool) BotTemplate {
	if len(ludoBotTemplates) == 0 {
		return BotTemplate{Name: "Pooja", AvatarID: ludoBotDefaultAvatarID, Country: ludoBotDefaultCountry}
	}
	start, err := secureRandomInt(len(ludoBotTemplates))
	if err != nil {
		start = seat % len(ludoBotTemplates)
	}
	for offset := 0; offset < len(ludoBotTemplates); offset++ {
		index := (start + offset) % len(ludoBotTemplates)
		if !used[index] {
			used[index] = true
			return ludoBotTemplates[index]
		}
	}
	return ludoBotTemplates[start]
}

func randomAuthoritativeBotPersonality() BotPersonality {
	personalities := []BotPersonality{BotAggressive, BotDefensive, BotBalanced, BotStrategic}
	index, err := secureRandomInt(len(personalities))
	if err != nil {
		return BotStrategic
	}
	return personalities[index]
}

func authoritativeBotDifficulty(player *authPlayer) BotDifficulty {
	if player != nil && player.Profile != nil {
		if value, ok := player.Profile["botDifficulty"].(string); ok {
			switch BotDifficulty(strings.ToLower(value)) {
			case BotEasy, BotMedium, BotHard, BotExpert:
				return BotDifficulty(strings.ToLower(value))
			}
		}
	}
	return BotExpert
}

func authoritativeBotPersonality(player *authPlayer) BotPersonality {
	if player != nil && player.Profile != nil {
		if value, ok := player.Profile["botPersonality"].(string); ok {
			switch BotPersonality(strings.ToLower(value)) {
			case BotAggressive, BotDefensive, BotBalanced, BotStrategic:
				return BotPersonality(strings.ToLower(value))
			}
		}
	}
	return BotStrategic
}

func authoritativeBotWeightsFor(personality BotPersonality) authoritativeBotWeights {
	weights := authoritativeBotWeights{
		Progress:         12,
		Capture:          5000,
		Escape:           1500,
		Safety:           700,
		HomeEntry:        1000,
		ReachHome:        3000,
		OpponentPressure: 120,
		CaptureRisk:      620,
		Exposure:         180,
		Future:           0.70,
	}
	switch personality {
	case BotAggressive:
		weights.Capture *= 1.30
		weights.OpponentPressure *= 1.35
		weights.Safety *= 0.80
		weights.CaptureRisk *= 0.82
	case BotDefensive:
		weights.Safety *= 1.35
		weights.CaptureRisk *= 1.35
		weights.Exposure *= 1.25
		weights.Capture *= 0.90
	case BotStrategic:
		weights.Future *= 1.45
		weights.OpponentPressure *= 1.15
	}
	return weights
}

func scheduleAuthoritativeBotAction(game *authGame, tick int64) {
	game.BotActionTick = 0
	player := game.player(game.Current)
	if player == nil || !player.Bot || game.Phase == "ready" || game.Phase == "finished" {
		return
	}
	minTicks, maxTicks := authoritativeBotRollDelayMinTicks, authoritativeBotRollDelayMaxTicks
	if game.Phase == "move" {
		minTicks, maxTicks = authoritativeBotMoveDelayMinTicks, authoritativeBotMoveDelayMaxTicks
	}
	game.BotActionTick = tick + randomDelayTicks(minTicks, maxTicks)
}

func getAuthoritativeLegalMoves(game *authGame) []authoritativeBotMove {
	player := game.player(game.Current)
	if player == nil {
		return nil
	}
	rawMoves := game.moves()
	pieceIDs := make([]int, 0, len(rawMoves))
	for pieceID := range rawMoves {
		pieceIDs = append(pieceIDs, pieceID)
	}
	sort.Ints(pieceIDs)
	moves := make([]authoritativeBotMove, 0)
	for _, pieceID := range pieceIDs {
		options := append([]authMoveData(nil), rawMoves[pieceID]...)
		sort.Slice(options, func(i, j int) bool { return options[i].Dice < options[j].Dice })
		for _, option := range options {
			piece := player.Pieces[pieceID]
			toPassed, toPosition := game.destination(player, piece, option.Dice)
			move := authoritativeBotMove{
				PieceID:      pieceID,
				Dice:         option.Dice,
				FromPassed:   piece.Passed,
				ToPassed:     toPassed,
				FromPosition: piece.Position,
				ToPosition:   toPosition,
				Safe:         toPassed >= 52 || authSafe(toPosition),
				Spawn:        piece.Passed == 0,
				HomeEntry:    piece.Passed < 52 && toPassed >= 52,
				ReachHome:    toPassed == 57,
			}
			if toPassed < 52 {
				if victim, victimPiece := game.victim(player.ID, toPosition); victim != nil {
					move.Capture = true
					move.CapturedProgress = victim.Pieces[victimPiece].Passed
				}
			}
			move.CaptureRisk = calculateAuthoritativeCaptureRisk(game, player.ID, toPassed, toPosition)
			currentRisk := calculateAuthoritativeCaptureRisk(game, player.ID, piece.Passed, piece.Position)
			move.EscapesDanger = currentRisk > 0 && move.CaptureRisk < currentRisk
			move.Pressure = calculateAuthoritativeOpponentPressure(game, player.ID, toPassed, toPosition)
			move.FutureValue = predictAuthoritativeFutureValue(game, player, move)
			moves = append(moves, move)
		}
	}
	return moves
}

func calculateAuthoritativeCaptureRisk(game *authGame, playerID, passed, position int) float64 {
	if passed <= 0 || passed >= 52 || authSafe(position) {
		return 0
	}
	survival := 1.0
	for _, opponent := range game.Players {
		if opponent.ID == playerID || game.Left[opponent.ID] || game.Ranks[opponent.ID] > 0 {
			continue
		}
		hits := 0
		for _, piece := range opponent.Pieces {
			for dice := 1; dice <= 6; dice++ {
				if authoritativeOpponentCanLand(game, opponent, piece, dice, position) {
					hits++
				}
			}
		}
		probability := math.Min(1, float64(hits)/6.0)
		survival *= 1 - probability
	}
	return 1 - survival
}

func authoritativeOpponentCanLand(game *authGame, player *authPlayer, piece authPiece, dice, targetPosition int) bool {
	if piece.Passed <= 0 || piece.Passed+dice > 57 {
		return false
	}
	passed := piece.Passed + dice
	if passed >= 52 {
		return false
	}
	for step := 0; step <= dice && piece.Passed+step < 52; step++ {
		position := (player.Start + piece.Passed + step - 1) % 52
		if game.blocked(player.ID, position) {
			return false
		}
	}
	destinationPosition := (player.Start + passed - 1) % 52
	return destinationPosition == targetPosition && game.canLandOnSquare(player.ID, piece.PieceID, authDestination{Passed: passed, Position: destinationPosition})
}

func calculateAuthoritativeOpponentPressure(game *authGame, playerID, passed, position int) float64 {
	if passed <= 0 || passed >= 52 {
		return 0
	}
	pressure := 0.0
	for _, opponent := range game.Players {
		if opponent.ID == playerID || game.Left[opponent.ID] || game.Ranks[opponent.ID] > 0 {
			continue
		}
		for _, piece := range opponent.Pieces {
			if piece.Passed <= 0 || piece.Passed >= 52 {
				continue
			}
			distance := (piece.Position - position + 52) % 52
			if distance >= 1 && distance <= 6 {
				pressure += float64(7-distance) / 6.0
			}
			if piece.Passed >= 45 {
				pressure += 0.35
			}
		}
	}
	return pressure
}

func predictAuthoritativeFutureValue(game *authGame, player *authPlayer, move authoritativeBotMove) float64 {
	total := 0.0
	// Lightweight two-roll expectation. It only uses public board state and fair
	// uniform dice probabilities; it never changes the actual dice result.
	for firstDice := 1; firstDice <= 6; firstDice++ {
		firstPassed := move.ToPassed + firstDice
		if firstPassed > 57 {
			continue
		}
		firstValue := float64(firstDice * 4)
		if firstPassed == 57 {
			firstValue += 220
		} else if firstPassed >= 52 {
			firstValue += 70
		}
		for secondDice := 1; secondDice <= 6; secondDice++ {
			secondPassed := firstPassed + secondDice
			secondValue := firstValue
			if secondPassed <= 57 {
				secondValue += float64(secondDice * 2)
				if secondPassed == 57 {
					secondValue += 120
				}
			}
			total += secondValue / 36.0
		}
	}
	return total * (1 - move.CaptureRisk)
}

func evaluateAuthoritativeMove(game *authGame, player *authPlayer, move authoritativeBotMove, weights authoritativeBotWeights) authoritativeBotMove {
	progress := move.ToPassed - move.FromPassed
	if move.FromPassed == 0 {
		progress = 1
	}
	move.Score = float64(progress)*weights.Progress + move.Pressure*weights.OpponentPressure + move.FutureValue*weights.Future
	move.Reason = "PROGRESS"
	if move.Capture {
		move.Score += weights.Capture + float64(move.CapturedProgress)*3
		move.Reason = "CAPTURE_HIGH_VALUE"
	}
	if move.ReachHome {
		move.Score += weights.ReachHome
		move.Reason = "REACH_HOME"
	}
	if move.EscapesDanger {
		move.Score += weights.Escape
		if move.Reason == "PROGRESS" {
			move.Reason = "ESCAPE_DANGER"
		}
	}
	if move.Safe && !move.Spawn {
		move.Score += weights.Safety
		if move.Reason == "PROGRESS" {
			move.Reason = "SAFE_POSITION"
		}
	}
	if move.HomeEntry {
		move.Score += weights.HomeEntry
		move.Reason = "ENTER_HOME"
	}
	if move.Spawn && move.Reason == "PROGRESS" {
		move.Reason = "SPAWN"
	}
	move.Score -= move.CaptureRisk * weights.CaptureRisk
	if move.CaptureRisk > 0 {
		move.Score -= weights.Exposure
	}
	return move
}

func authoritativeBotDicePool(game *authGame, logger runtime.Logger) []int {
	player := game.player(game.Current)
	if player == nil {
		return nil
	}
	allowed := make([]int, 0, 6)
	for dice := 1; dice <= 6; dice++ {
		hasMoveIgnoringOwnToken := false
		hasLegalMove := false
		for pieceID := range player.Pieces {
			if game.legalWithoutOwnOccupancy(pieceID, dice) {
				hasMoveIgnoringOwnToken = true
			}
			if game.legal(pieceID, dice) {
				hasLegalMove = true
			}
		}
		if hasMoveIgnoringOwnToken && !hasLegalMove {
			if logger != nil {
				logger.Info("BOT_DICE_REJECTED dice=%d reason=NO_VALID_DESTINATION", dice)
			}
			continue
		}
		allowed = append(allowed, dice)
	}
	return allowed
}

func rollAuthoritativeBotDice(game *authGame, logger runtime.Logger) (int, error) {
	allowed := authoritativeBotDicePool(game, logger)
	if len(allowed) == 0 {
		dice, err := rollDice()
		if err == nil && logger != nil {
			logger.Info("BOT_DICE_SELECTED dice=%d", dice)
		}
		return dice, err
	}
	index, err := secureRandomInt(len(allowed))
	if err != nil {
		return 0, err
	}
	dice := allowed[index]
	if logger != nil {
		logger.Info("BOT_DICE_SELECTED dice=%d", dice)
	}
	return dice, nil
}

func applyAuthoritativeDifficultyNoise(moves []authoritativeBotMove, difficulty BotDifficulty) authoritativeBotMove {
	if len(moves) == 1 {
		return moves[0]
	}
	bestChance := 95
	fallbackPool := 2
	switch difficulty {
	case BotEasy:
		bestChance, fallbackPool = 55, len(moves)
	case BotMedium:
		bestChance, fallbackPool = 75, min(3, len(moves))
	case BotHard:
		bestChance, fallbackPool = 89, min(2, len(moves))
	case BotExpert:
		bestChance, fallbackPool = 95, min(2, len(moves))
	}
	roll, err := secureRandomInt(100)
	if err != nil || roll < bestChance {
		return moves[0]
	}
	index, err := secureRandomInt(fallbackPool)
	if err != nil {
		return moves[0]
	}
	return moves[index]
}

func selectAuthoritativeBotMove(game *authGame, logger runtime.Logger) (authoritativeBotMove, bool) {
	player := game.player(game.Current)
	legalMoves := getAuthoritativeLegalMoves(game)
	if player == nil || len(legalMoves) == 0 {
		return authoritativeBotMove{}, false
	}
	difficulty := authoritativeBotDifficulty(player)
	personality := authoritativeBotPersonality(player)
	weights := authoritativeBotWeightsFor(personality)
	for i := range legalMoves {
		legalMoves[i] = evaluateAuthoritativeMove(game, player, legalMoves[i], weights)
		jitter, err := secureRandomInt(11)
		if err == nil {
			legalMoves[i].Score += float64(jitter - 5)
		}
		if logger != nil {
			logger.Debug("[BOT] difficulty=%s personality=%s token=%d dice=%d score=%.2f capture=%t safe=%t risk=%.2f", difficulty, personality, legalMoves[i].PieceID, legalMoves[i].Dice, legalMoves[i].Score, legalMoves[i].Capture, legalMoves[i].Safe, legalMoves[i].CaptureRisk)
		}
	}
	sort.SliceStable(legalMoves, func(i, j int) bool { return legalMoves[i].Score > legalMoves[j].Score })
	selected := applyAuthoritativeDifficultyNoise(legalMoves, difficulty)
	if logger != nil {
		logger.Debug("[BOT] selected token=%d dice=%d reason=%s score=%.2f", selected.PieceID, selected.Dice, selected.Reason, selected.Score)
	}
	return selected, true
}

func performAuthoritativeBotAction(game *authGame, logger runtime.Logger, tick int64) error {
	player := game.player(game.Current)
	if player == nil || !player.Bot {
		return fmt.Errorf("current player is not a bot")
	}
	difficulty := authoritativeBotDifficulty(player)
	personality := authoritativeBotPersonality(player)
	switch game.Phase {
	case "roll":
		dice, err := rollAuthoritativeBotDice(game, logger)
		if err != nil {
			return err
		}
		if logger != nil {
			logger.Debug("[BOT] difficulty=%s personality=%s action=roll dice=%d", difficulty, personality, dice)
		}
		return game.roll(dice, tick)
	case "move":
		move, ok := selectAuthoritativeBotMove(game, logger)
		if !ok {
			return fmt.Errorf("bot has no legal move in move phase")
		}
		return game.move(move.PieceID, move.Dice, tick)
	default:
		return fmt.Errorf("bot action is not valid in phase %s", game.Phase)
	}
}
