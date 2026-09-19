package ludo

import (
	"errors"
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
	OpponentFinish   float64
	PreventedFinish  float64
	FutureMobility   float64
	Score            float64
	Reason           string
}

type authoritativeLookAhead struct {
	CaptureRisk      float64
	OpponentFinish   float64
	FutureMobility   float64
	OpponentMobility float64
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

func simulateAuthoritativeMove(game *authGame, move authoritativeBotMove) (*authGame, error) {
	next := game.clone()
	next.Current = game.Current
	next.Phase = "move"
	next.Rolls = []int{move.Dice}
	next.Commands = nil
	if err := next.move(move.PieceID, move.Dice, 0); err != nil {
		return nil, err
	}
	return next, nil
}

func authoritativeOpponentFinishThreat(game *authGame, playerID int) float64 {
	if game == nil {
		return 0
	}
	probe := game.clone()
	threat := 0.0
	for _, opponent := range probe.Players {
		if opponent.ID == playerID || probe.Left[opponent.ID] || probe.Ranks[opponent.ID] > 0 {
			continue
		}
		probe.Current = opponent.ID
		finishDice := 0
		for dice := 1; dice <= 6; dice++ {
			canFinish := false
			for pieceID, piece := range opponent.Pieces {
				if probe.legal(pieceID, dice) && piece.Passed+dice == 57 {
					canFinish = true
					break
				}
			}
			if canFinish {
				finishDice++
			}
		}
		weight := 1.0
		if opponent.Finished >= 3 {
			weight = 2.25
		} else if opponent.Finished == 2 {
			weight = 1.5
		}
		weight *= learnedOpponentFinishMultiplier(probe.HumanModels[opponent.ID])
		threat += float64(finishDice) / 6.0 * weight
	}
	return threat
}

func authoritativeLookAheadAfterMove(game *authGame, playerID, movedPieceID int) authoritativeLookAhead {
	metrics := authoritativeLookAhead{}
	if game == nil {
		return metrics
	}
	movedPlayer := game.player(playerID)
	if movedPlayer == nil || movedPieceID < 0 || movedPieceID >= len(movedPlayer.Pieces) {
		return metrics
	}
	movedPiece := movedPlayer.Pieces[movedPieceID]
	probe := game.clone()
	survival := 1.0
	for _, opponent := range probe.Players {
		if opponent.ID == playerID || probe.Left[opponent.ID] || probe.Ranks[opponent.ID] > 0 {
			continue
		}
		probe.Current = opponent.ID
		captureDice := 0
		finishDice := 0
		legalOptions := 0
		for dice := 1; dice <= 6; dice++ {
			canCapture := false
			canFinish := false
			for pieceID, piece := range opponent.Pieces {
				if !probe.legal(pieceID, dice) {
					continue
				}
				legalOptions++
				passed, position := probe.destination(opponent, piece, dice)
				if passed == 57 {
					canFinish = true
				}
				if movedPiece.Passed > 0 && movedPiece.Passed < 52 && !authSafe(movedPiece.Position) && passed < 52 && position == movedPiece.Position {
					canCapture = true
				}
			}
			if canCapture {
				captureDice++
			}
			if canFinish {
				finishDice++
			}
		}
		risk := float64(captureDice) / 6.0
		risk *= learnedOpponentRiskMultiplier(probe.HumanModels[opponent.ID])
		risk = math.Min(1, risk)
		survival *= 1 - risk
		finishWeight := 1.0
		if opponent.Finished >= 3 {
			finishWeight = 2.25
		}
		finishWeight *= learnedOpponentFinishMultiplier(probe.HumanModels[opponent.ID])
		metrics.OpponentFinish += float64(finishDice) / 6.0 * finishWeight
		metrics.OpponentMobility += float64(legalOptions) / 24.0
	}
	metrics.CaptureRisk = 1 - survival

	probe.Current = playerID
	legalDice := 0
	for dice := 1; dice <= 6; dice++ {
		for pieceID := range movedPlayer.Pieces {
			if probe.legal(pieceID, dice) {
				legalDice++
				break
			}
		}
	}
	metrics.FutureMobility = float64(legalDice) / 6.0
	return metrics
}

func activeAuthoritativeTokens(player *authPlayer) int {
	active := 0
	if player == nil {
		return active
	}
	for _, piece := range player.Pieces {
		if piece.Passed > 0 && piece.Passed < 57 {
			active++
		}
	}
	return active
}

func evaluateExpertAuthoritativeMove(game *authGame, player *authPlayer, move authoritativeBotMove) authoritativeBotMove {
	next, err := simulateAuthoritativeMove(game, move)
	if err != nil {
		move.Score = math.Inf(-1)
		move.Reason = "INVALID"
		return move
	}
	lookAhead := authoritativeLookAheadAfterMove(next, player.ID, move.PieceID)
	beforeFinishThreat := authoritativeOpponentFinishThreat(game, player.ID)
	move.CaptureRisk = lookAhead.CaptureRisk
	move.OpponentFinish = lookAhead.OpponentFinish
	move.PreventedFinish = math.Max(0, beforeFinishThreat-lookAhead.OpponentFinish)
	move.FutureMobility = lookAhead.FutureMobility
	captureMultiplier := 1.0
	riskMultiplier := 1.0
	safetyMultiplier := 1.0
	mobilityMultiplier := 1.0
	preventMultiplier := 1.0
	switch authoritativeBotPersonality(player) {
	case BotAggressive:
		captureMultiplier = 1.15
		riskMultiplier = 0.85
	case BotDefensive:
		captureMultiplier = 0.95
		riskMultiplier = 1.25
		safetyMultiplier = 1.20
	case BotStrategic:
		mobilityMultiplier = 1.20
		preventMultiplier = 1.20
	}

	progress := move.ToPassed - move.FromPassed
	if move.Spawn {
		progress = 1
	}
	move.Score = float64(progress)*18 + move.FutureMobility*240*mobilityMultiplier - lookAhead.OpponentMobility*90
	move.Reason = "PROGRESS"

	if move.Spawn {
		active := activeAuthoritativeTokens(player)
		switch {
		case active == 0:
			move.Score += 520
		case active == 1:
			move.Score += 320
		case active == 2:
			move.Score += 100
		default:
			move.Score -= 180
		}
		move.Reason = "SPAWN_STRATEGIC"
	}
	if move.Safe && !move.Spawn {
		move.Score += 480 * safetyMultiplier
		move.Reason = "SAFE_POSITION"
	}
	if move.HomeEntry {
		move.Score += 850
		move.Reason = "ENTER_HOME"
	}
	if move.EscapesDanger {
		move.Score += 1150 + float64(move.FromPassed)*12
		move.Reason = "ESCAPE_HIGH_VALUE"
	}
	if move.Capture {
		move.Score += (900 + float64(move.CapturedProgress)*22) * captureMultiplier
		move.Reason = "CAPTURE_STRATEGIC"
	}
	if move.PreventedFinish > 0 {
		move.Score += move.PreventedFinish * 1800 * preventMultiplier
		move.Reason = "PREVENT_OPPONENT_FINISH"
	}
	if move.ReachHome {
		move.Score += 2400
		move.Reason = "FINISH_HIGH_VALUE"
	}

	advancedValue := 450 + float64(move.ToPassed)*34
	move.Score -= move.CaptureRisk * advancedValue * riskMultiplier
	move.Score -= move.OpponentFinish * 700
	return move
}

func evaluateMediumAuthoritativeMove(move authoritativeBotMove) authoritativeBotMove {
	progress := move.ToPassed - move.FromPassed
	if move.Spawn {
		progress = 1
	}
	move.Score = float64(progress) * 15
	move.Reason = "PROGRESS"
	if move.Spawn {
		move.Score += 100
		move.Reason = "SPAWN"
	}
	if move.Capture {
		move.Score += 600 + float64(move.CapturedProgress)*5
		move.Reason = "CAPTURE"
	}
	if move.HomeEntry {
		move.Score += 350
		move.Reason = "ENTER_HOME"
	}
	if move.ReachHome {
		move.Score += 1000
		move.Reason = "FINISH"
	}
	return move
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
	allowed, rejected := game.authoritativeDicePool()
	if logger != nil {
		for _, dice := range rejected {
			logger.Info("BOT_DICE_REJECTED dice=%d reason=OWN_TOKEN_OCCUPIED", dice)
		}
	}
	return allowed
}

func rollAuthoritativeBotDice(game *authGame, logger runtime.Logger) (int, error) {
	allowed := authoritativeBotDicePool(game, logger)
	if len(allowed) == 0 {
		return 0, errNoAllowedDiceValues
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

func selectNearEqualExpertMove(moves []authoritativeBotMove) authoritativeBotMove {
	if len(moves) <= 1 {
		return moves[0]
	}
	threshold := math.Max(30, math.Abs(moves[0].Score)*0.035)
	near := 1
	for near < len(moves) && moves[0].Score-moves[near].Score <= threshold {
		near++
	}
	if near == 1 {
		return moves[0]
	}
	weights := make([]int, near)
	total := 0
	for index := 0; index < near; index++ {
		delta := moves[0].Score - moves[index].Score
		weight := max(1, int(100-delta*2))
		weights[index] = weight
		total += weight
	}
	roll, err := secureRandomInt(total)
	if err != nil {
		return moves[0]
	}
	for index, weight := range weights {
		if roll < weight {
			return moves[index]
		}
		roll -= weight
	}
	return moves[0]
}

func logRejectedExpertCandidates(game *authGame, player *authPlayer, logger runtime.Logger) {
	if logger == nil || player == nil {
		return
	}
	for _, dice := range game.Rolls {
		logger.Debug("EXPERT_DICE=%d", dice)
		for pieceID, piece := range player.Pieces {
			if !game.legalWithoutOwnOccupancy(pieceID, dice) {
				continue
			}
			passed, position := game.destination(player, piece, dice)
			if !game.canLandOnSquare(player.ID, pieceID, authDestination{Passed: passed, Position: position}) {
				logger.Debug("CANDIDATE_REJECTED token=%d reason=OWN_TOKEN_COLLISION destination=%d", pieceID, position)
			}
		}
	}
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
	if difficulty == BotExpert {
		logRejectedExpertCandidates(game, player, logger)
	}
	for i := range legalMoves {
		switch difficulty {
		case BotEasy:
			legalMoves[i].Score = 0
			legalMoves[i].Reason = "RANDOM_LEGAL"
		case BotMedium:
			legalMoves[i] = evaluateMediumAuthoritativeMove(legalMoves[i])
		case BotHard:
			legalMoves[i] = evaluateAuthoritativeMove(game, player, legalMoves[i], weights)
		case BotExpert:
			legalMoves[i] = evaluateExpertAuthoritativeMove(game, player, legalMoves[i])
		}
		if logger != nil && difficulty == BotExpert {
			action := legalMoves[i].Reason
			logger.Debug("CANDIDATE token=%d action=%s from=%d to=%d score=%.2f captureRisk=%.3f finishThreat=%.3f mobility=%.3f", legalMoves[i].PieceID, action, legalMoves[i].FromPosition, legalMoves[i].ToPosition, legalMoves[i].Score, legalMoves[i].CaptureRisk, legalMoves[i].OpponentFinish, legalMoves[i].FutureMobility)
		} else if logger != nil {
			logger.Debug("[BOT] difficulty=%s personality=%s token=%d dice=%d score=%.2f capture=%t safe=%t risk=%.2f", difficulty, personality, legalMoves[i].PieceID, legalMoves[i].Dice, legalMoves[i].Score, legalMoves[i].Capture, legalMoves[i].Safe, legalMoves[i].CaptureRisk)
		}
	}
	sort.SliceStable(legalMoves, func(i, j int) bool { return legalMoves[i].Score > legalMoves[j].Score })
	var selected authoritativeBotMove
	if difficulty == BotEasy {
		index, err := secureRandomInt(len(legalMoves))
		if err != nil {
			index = 0
		}
		selected = legalMoves[index]
	} else if difficulty == BotExpert {
		selected = selectNearEqualExpertMove(legalMoves)
	} else {
		selected = applyAuthoritativeDifficultyNoise(legalMoves, difficulty)
	}
	if logger != nil {
		if difficulty == BotExpert {
			logger.Debug("EXPERT_SELECTED token=%d score=%.2f reason=%s", selected.PieceID, selected.Score, selected.Reason)
		} else {
			logger.Debug("[BOT] selected token=%d dice=%d reason=%s score=%.2f", selected.PieceID, selected.Dice, selected.Reason, selected.Score)
		}
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
		if errors.Is(err, errNoAllowedDiceValues) {
			if logger != nil {
				logger.Info("BOT_DICE_POOL_EMPTY player=%d action=PASS_TURN", player.ID)
			}
			game.next(tick)
			return nil
		}
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
