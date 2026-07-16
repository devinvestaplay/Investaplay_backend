package ludo

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/heroiclabs/nakama-common/runtime"
)

func rollForCurrentPlayer(dispatcher runtime.MatchDispatcher, state *LudoMatchState) error {
	player := state.Players[state.CurrentPlayerID]
	if player == nil {
		return errors.New("current player not found")
	}
	dice, err := rollDice()
	if err != nil {
		return err
	}
	state.CurrentDice = dice
	if dice == 6 {
		state.ConsecutiveSixes[player.ID]++
	} else {
		state.ConsecutiveSixes[player.ID] = 0
	}
	if state.ConsecutiveSixes[player.ID] >= ludoBotMaxConsecutiveSixes {
		state.LegalMoves = nil
		state.MovableTokens = nil
		broadcastDiceResult(dispatcher, state)
		state.ConsecutiveSixes[player.ID] = 0
		endTurn(dispatcher, state, false)
		return nil
	}
	state.LegalMoves = getLegalMoves(state, player, dice)
	state.MovableTokens = movableTokensFromMoves(state.LegalMoves)
	broadcastDiceResult(dispatcher, state)
	if len(state.LegalMoves) == 0 {
		endTurn(dispatcher, state, false)
		return nil
	}
	state.Phase = PhaseWaitingForMove
	state.BotPendingMove = player.IsBot
	if player.IsBot {
		state.BotActionTick = state.LastTick + ludoBotDiceAnimationDelayTicks
	}
	return nil
}

func applyMoveAndAdvance(dispatcher runtime.MatchDispatcher, state *LudoMatchState, move LegalMove) {
	player := state.Players[state.CurrentPlayerID]
	if player == nil || move.TokenID < 0 || move.TokenID >= len(player.Tokens) {
		return
	}
	state.Phase = PhaseAnimatingMove
	token := &player.Tokens[move.TokenID]
	token.Position = move.ToPosition
	token.Finished = move.FinishesToken
	if move.Captures {
		if capturedPlayer := state.Players[move.CapturedPlayerID]; capturedPlayer != nil && move.CapturedTokenID >= 0 && move.CapturedTokenID < len(capturedPlayer.Tokens) {
			capturedPlayer.Tokens[move.CapturedTokenID].Position = ludoBotBasePosition
			capturedPlayer.Tokens[move.CapturedTokenID].Finished = false
			broadcastPayload(dispatcher, OpTokenCaptured, move)
		}
	}
	if allTokensFinished(player) && player.Rank == 0 {
		player.Rank = len(state.Ranks) + 1
		state.Ranks = append(state.Ranks, player.ID)
	}
	extraTurn := !state.MatchFinished && (state.CurrentDice == 6 || move.Captures || move.FinishesToken) && player.Rank == 0
	if player.Rank > 0 {
		extraTurn = false
	}
	nextID := nextPlayerID(state, extraTurn)
	broadcastPayload(dispatcher, OpTokenMoved, ludoTokenMovedPayload{PlayerID: player.ID, TokenID: move.TokenID, FromPosition: move.FromPosition, ToPosition: move.ToPosition, Path: move.Path, Captured: move.Captures, CapturedPlayerID: move.CapturedPlayerID, CapturedTokenID: move.CapturedTokenID, ReachedSafe: move.ReachesSafe, FinishedToken: move.FinishesToken, ExtraTurn: extraTurn, NextPlayerID: nextID})
	if extraTurn {
		broadcastPayload(dispatcher, OpExtraTurn, map[string]interface{}{"player_id": player.ID})
	}
	if shouldFinishMatch(state) {
		finishBotMatch(dispatcher, state)
		return
	}
	endTurn(dispatcher, state, extraTurn)
}

func endTurn(dispatcher runtime.MatchDispatcher, state *LudoMatchState, samePlayer bool) {
	state.CurrentDice = 0
	state.LegalMoves = nil
	state.MovableTokens = nil
	state.BotPendingMove = false
	if !samePlayer {
		state.CurrentPlayerID = nextPlayerID(state, false)
		state.TurnNumber++
	}
	state.Phase = PhaseWaitingForRoll
	broadcastTurnStart(dispatcher, state)
	if current := state.Players[state.CurrentPlayerID]; current != nil && current.IsBot {
		state.BotActionTick = state.LastTick + randomDelayTicks(8, 18)
	}
}

func getLegalMoves(state *LudoMatchState, player *LudoPlayer, dice int) []LegalMove {
	moves := make([]LegalMove, 0)
	for i := range player.Tokens {
		token := player.Tokens[i]
		if token.Finished {
			continue
		}
		move := LegalMove{TokenID: token.ID, FromPosition: token.Position, CapturedTokenID: -1}
		if token.Position == ludoBotBasePosition {
			if dice != 6 {
				continue
			}
			move.ToPosition = 0
			move.Path = []int{0}
			move.LeavesBase = true
		} else {
			to := token.Position + dice
			if to > ludoBotHomePosition {
				continue
			}
			move.ToPosition = to
			move.Path = ludoBotPath(token.Position, to)
		}
		move.ReachesSafe = isLudoBotSafePosition(move.ToPosition)
		move.ReachesHome = move.ToPosition == ludoBotHomePosition
		move.FinishesToken = move.ToPosition == ludoBotHomePosition
		if !move.ReachesSafe && !move.FinishesToken {
			for _, opponent := range state.Players {
				if opponent.ID == player.ID || opponent.Rank > 0 {
					continue
				}
				for _, opponentToken := range opponent.Tokens {
					if opponentToken.Position == move.ToPosition && !opponentToken.Finished {
						move.Captures = true
						move.CapturedPlayerID = opponent.ID
						move.CapturedTokenID = opponentToken.ID
					}
				}
			}
		}
		move.ExposedAfter = isPositionThreatened(state, player.ID, move.ToPosition)
		moves = append(moves, move)
	}
	return moves
}

func scoreMove(state *LudoMatchState, player *LudoPlayer, move LegalMove) int {
	score := 0
	if move.FinishesToken {
		score += 1000
	}
	if move.Captures {
		score += 700
	}
	if isPositionThreatened(state, player.ID, move.FromPosition) && !move.ExposedAfter {
		score += 450
	}
	if move.ReachesSafe {
		score += 300
	}
	if createsUsefulBlock(player, move) {
		score += 250
	}
	if move.LeavesBase {
		score += 220
	}
	if move.ToPosition > move.FromPosition {
		score += (move.ToPosition - move.FromPosition) * 10
	}
	if move.ExposedAfter {
		score -= 250
	}
	if breaksUsefulBlock(player, move) {
		score -= 120
	}
	if exposesNearOpponent(state, player.ID, move.ToPosition) {
		score -= 150
	}
	jitter, _ := secureRandomInt(15)
	return score + jitter
}

func selectBotMove(state *LudoMatchState, player *LudoPlayer) (LegalMove, bool) {
	if player == nil || len(state.LegalMoves) == 0 {
		return LegalMove{}, false
	}
	if len(state.LegalMoves) == 1 {
		return state.LegalMoves[0], true
	}
	scored := make([]ScoredMove, 0, len(state.LegalMoves))
	for _, move := range state.LegalMoves {
		scored = append(scored, ScoredMove{LegalMove: move, Score: scoreMove(state, player, move)})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	chance := 70
	fallbackPool := 3
	switch player.BotDifficulty {
	case BotEasy:
		chance = 35
		fallbackPool = len(scored)
	case BotHard:
		chance = 90
		fallbackPool = 2
	}
	roll, _ := secureRandomInt(100)
	if roll < chance {
		return scored[0].LegalMove, true
	}
	if fallbackPool > len(scored) {
		fallbackPool = len(scored)
	}
	idx, _ := secureRandomInt(fallbackPool)
	return scored[idx].LegalMove, true
}

func rollDice() (int, error) {
	value, err := secureRandomInt(6)
	if err != nil {
		return 0, err
	}
	return value + 1, nil
}

func secureRandomInt(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("max must be greater than zero")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

