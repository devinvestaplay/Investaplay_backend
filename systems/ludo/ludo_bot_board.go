package ludo

import (
	"fmt"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
)

func isPositionThreatened(state *LudoMatchState, playerID string, position int) bool {
	if position < 0 || position >= ludoBotHomePosition || isLudoBotSafePosition(position) {
		return false
	}
	for _, opponent := range state.Players {
		if opponent.ID == playerID || opponent.Rank > 0 {
			continue
		}
		for _, token := range opponent.Tokens {
			if token.Position < 0 || token.Finished {
				continue
			}
			for dice := 1; dice <= 6; dice++ {
				if token.Position+dice == position {
					return true
				}
			}
		}
	}
	return false
}

func createsUsefulBlock(player *LudoPlayer, move LegalMove) bool {
	if move.ToPosition < 0 || move.FinishesToken {
		return false
	}
	count := 0
	for _, token := range player.Tokens {
		if token.ID != move.TokenID && token.Position == move.ToPosition {
			count++
		}
	}
	return count > 0
}

func breaksUsefulBlock(player *LudoPlayer, move LegalMove) bool {
	if move.FromPosition < 0 {
		return false
	}
	count := 0
	for _, token := range player.Tokens {
		if token.ID != move.TokenID && token.Position == move.FromPosition {
			count++
		}
	}
	return count > 0
}

func exposesNearOpponent(state *LudoMatchState, playerID string, position int) bool {
	if position < 0 || isLudoBotSafePosition(position) {
		return false
	}
	for _, opponent := range state.Players {
		if opponent.ID == playerID {
			continue
		}
		for _, token := range opponent.Tokens {
			if token.Position >= 0 && absInt(token.Position-position) <= 6 {
				return true
			}
		}
	}
	return false
}

func isLudoBotSafePosition(position int) bool {
	switch position {
	case 0, 8, 13, 21, 26, 34, 39, 47, ludoBotHomePosition:
		return true
	default:
		return false
	}
}

func isCurrentHumanAction(state *LudoMatchState, message runtime.MatchData, phase string) bool {
	player := state.Players[state.CurrentPlayerID]
	return player != nil && !player.IsBot && message.GetUserId() == player.ID && state.Phase == phase
}

func randomBotLevel(humanLevel int) int {
	if humanLevel < 1 {
		humanLevel = 1
	}
	roll, err := secureRandomInt(10)
	if err != nil {
		return humanLevel
	}
	level := humanLevel - 4 + roll
	if level < 1 {
		return 1
	}
	return level
}

func randomDelayTicks(minTicks int, maxTicks int) int64 {
	if maxTicks <= minTicks {
		return int64(minTicks)
	}
	roll, err := secureRandomInt(maxTicks - minTicks + 1)
	if err != nil {
		return int64(minTicks)
	}
	return int64(minTicks + roll)
}

func avatarIDFromString(value string) int {
	value = strings.TrimSpace(strings.TrimPrefix(value, "avatar_"))
	if value == "" {
		return ludoBotDefaultAvatarID
	}
	var avatarID int
	if _, err := fmt.Sscanf(value, "%d", &avatarID); err != nil || avatarID <= 0 {
		return ludoBotDefaultAvatarID
	}
	return avatarID
}

func intFromMetadata(metadata map[string]interface{}, key string, fallback int) int {
	if value, ok := metadata[key]; ok {
		switch v := value.(type) {
		case float64:
			return int(v)
		case int:
			return v
		case string:
			var parsed int
			if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func stringFromMetadata(metadata map[string]interface{}, key string, fallback string) string {
	if value, ok := metadata[key].(string); ok && strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
