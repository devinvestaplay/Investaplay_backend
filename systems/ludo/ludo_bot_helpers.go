package ludo

import (
	"context"
	"encoding/json"
	"fmt"
	"game-server/utils"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
)

func newLudoHumanPlayer(userID string, seat int, color string) *LudoPlayer {
	return &LudoPlayer{ID: userID, UserID: userID, Name: "Player", AvatarID: ludoBotDefaultAvatarID, Level: ludoBotDefaultHumanLevel, Country: ludoBotDefaultCountry, Seat: seat, Color: color, IsBot: false, Tokens: newLudoTokens()}
}

func newLudoBotPlayer(matchID string, seat int, color string, difficulty BotDifficulty) *LudoPlayer {
	template := selectLudoBotTemplate(seat)
	id := fmt.Sprintf("bot_%s_%d", sanitizeLudoBotIDPart(matchID), seat)
	return &LudoPlayer{ID: id, Name: template.Name, AvatarID: template.AvatarID, Level: randomBotLevel(ludoBotDefaultHumanLevel), Country: template.Country, Seat: seat, Color: color, IsBot: true, BotDifficulty: difficulty, Tokens: newLudoTokens()}
}

func populateLudoBotMatchPlayers(state *LudoMatchState, config ludoBotMatchConfig) {
	human := newLudoHumanPlayer(config.HumanUserID, 0, ludoBotPlayerColor(0))
	addLudoPlayerToState(state, human)

	for seat := 1; seat < ludoBotPlayerCount(config.Mode); seat++ {
		bot := newLudoBotPlayer(config.MatchID, seat, ludoBotPlayerColor(seat), config.BotDifficulty)
		addLudoPlayerToState(state, bot)
	}
}

func addLudoPlayerToState(state *LudoMatchState, player *LudoPlayer) {
	state.Players[player.ID] = player
	state.PlayerOrder = append(state.PlayerOrder, player.ID)
}

func ludoBotPlayerColor(seat int) string {
	colors := []string{"red", "yellow", "green", "blue"}
	if seat < 0 || seat >= len(colors) {
		return colors[0]
	}
	return colors[seat]
}

func newLudoTokens() []LudoToken {
	tokens := make([]LudoToken, 0, ludoBotTokensPerPlayer)
	for i := 0; i < ludoBotTokensPerPlayer; i++ {
		tokens = append(tokens, LudoToken{ID: i, Position: ludoBotBasePosition})
	}
	return tokens
}

func selectLudoBotTemplate(seat int) BotTemplate {
	if len(ludoBotTemplates) == 0 {
		return BotTemplate{Name: "Pooja", AvatarID: ludoBotDefaultAvatarID, Country: ludoBotDefaultCountry}
	}
	idx, err := secureRandomInt(len(ludoBotTemplates))
	if err != nil {
		idx = seat % len(ludoBotTemplates)
	}
	return ludoBotTemplates[idx]
}

func updateHumanPlayerProfile(ctx context.Context, nk runtime.NakamaModule, presence runtime.Presence, player *LudoPlayer) {
	player.Name = presence.GetUsername()
	account, err := nk.AccountGetId(ctx, presence.GetUserId())
	if err != nil || account == nil || account.User == nil {
		return
	}
	if strings.TrimSpace(account.User.DisplayName) != "" {
		player.Name = account.User.DisplayName
	}
	player.AvatarID = avatarIDFromString(account.User.AvatarUrl)
	var metadata map[string]interface{}
	if err := utils.DeserializeObjectFromStringByRefsToMap(&account.User.Metadata, &metadata); err == nil {
		player.Level = intFromMetadata(metadata, "level", player.Level)
		player.Country = stringFromMetadata(metadata, "country", player.Country)
	}
}

func broadcastMatchStart(dispatcher runtime.MatchDispatcher, state *LudoMatchState) {
	payload := ludoMatchStartPayload{MatchID: state.MatchID, Mode: state.Mode, Players: ludoStartPlayers(state), CurrentPlayerID: state.CurrentPlayerID, TurnNumber: state.TurnNumber}
	broadcastPayload(dispatcher, OpMatchStart, payload)
}

func broadcastTurnStart(dispatcher runtime.MatchDispatcher, state *LudoMatchState) {
	broadcastPayload(dispatcher, OpTurnStart, map[string]interface{}{"current_player_id": state.CurrentPlayerID, "turn_number": state.TurnNumber, "phase": state.Phase})
}

func broadcastDiceResult(dispatcher runtime.MatchDispatcher, state *LudoMatchState) {
	payload := ludoDiceResultPayload{PlayerID: state.CurrentPlayerID, DiceValue: state.CurrentDice, MovableTokens: state.MovableTokens}
	broadcastPayload(dispatcher, OpDiceResult, payload)
}

func broadcastStateSync(dispatcher runtime.MatchDispatcher, state *LudoMatchState, presences []runtime.Presence) {
	broadcastPayloadTo(dispatcher, OpStateSync, state, presences)
}

func broadcastActionError(dispatcher runtime.MatchDispatcher, presences []runtime.Presence, code string, message string) {
	broadcastPayloadTo(dispatcher, OpActionError, ludoActionErrorPayload{Success: false, Code: code, Message: message}, presences)
}

func broadcastPayload(dispatcher runtime.MatchDispatcher, opCode int64, payload interface{}) {
	broadcastPayloadTo(dispatcher, opCode, payload, nil)
}

func broadcastPayloadTo(dispatcher runtime.MatchDispatcher, opCode int64, payload interface{}, presences []runtime.Presence) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = dispatcher.BroadcastMessage(opCode, data, presences, nil, true)
}

func finishBotMatch(dispatcher runtime.MatchDispatcher, state *LudoMatchState) {
	if state.MatchFinished {
		return
	}
	state.MatchFinished = true
	state.Phase = PhaseGameFinished
	for _, playerID := range state.PlayerOrder {
		player := state.Players[playerID]
		if player != nil && player.Rank == 0 {
			player.Rank = len(state.Ranks) + 1
			state.Ranks = append(state.Ranks, player.ID)
		}
	}
	broadcastPayload(dispatcher, OpMatchFinished, ludoMatchFinishedPayload{MatchID: state.MatchID, Mode: state.Mode, Ranks: state.Ranks, Players: state.Players})
}

func ludoStartPlayers(state *LudoMatchState) []*LudoStartPlayer {
	players := make([]*LudoStartPlayer, 0, len(state.PlayerOrder))
	for _, playerID := range state.PlayerOrder {
		player := state.Players[playerID]
		if player == nil {
			continue
		}
		players = append(players, &LudoStartPlayer{ID: player.ID, Name: player.Name, AvatarID: player.AvatarID, Level: player.Level, Country: player.Country, Seat: player.Seat, Color: player.Color, IsBot: player.IsBot})
	}
	return players
}

func legalMoveByToken(moves []LegalMove, tokenID int) (LegalMove, bool) {
	for _, move := range moves {
		if move.TokenID == tokenID {
			return move, true
		}
	}
	return LegalMove{}, false
}

func movableTokensFromMoves(moves []LegalMove) []int {
	tokens := make([]int, 0, len(moves))
	seen := map[int]bool{}
	for _, move := range moves {
		if !seen[move.TokenID] {
			seen[move.TokenID] = true
			tokens = append(tokens, move.TokenID)
		}
	}
	return tokens
}

func ludoBotPath(from int, to int) []int {
	path := make([]int, 0)
	for pos := from + 1; pos <= to; pos++ {
		path = append(path, pos)
	}
	return path
}

func nextPlayerID(state *LudoMatchState, samePlayer bool) string {
	if samePlayer {
		return state.CurrentPlayerID
	}
	if len(state.PlayerOrder) == 0 {
		return ""
	}
	currentIndex := 0
	for i, playerID := range state.PlayerOrder {
		if playerID == state.CurrentPlayerID {
			currentIndex = i
			break
		}
	}
	for offset := 1; offset <= len(state.PlayerOrder); offset++ {
		candidate := state.PlayerOrder[(currentIndex+offset)%len(state.PlayerOrder)]
		if player := state.Players[candidate]; player != nil && player.Rank == 0 {
			return candidate
		}
	}
	return state.CurrentPlayerID
}

func shouldFinishMatch(state *LudoMatchState) bool {
	active := 0
	for _, player := range state.Players {
		if player != nil && player.Rank == 0 {
			active++
		}
	}
	return active <= 1
}

func allTokensFinished(player *LudoPlayer) bool {
	for _, token := range player.Tokens {
		if !token.Finished {
			return false
		}
	}
	return true
}

func adjustBotLevelsNearHuman(state *LudoMatchState, humanLevel int) {
	for _, player := range state.Players {
		if player != nil && player.IsBot {
			player.Level = randomBotLevel(humanLevel)
		}
	}
}
