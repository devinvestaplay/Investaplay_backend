package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"game-server/utils"

	"github.com/heroiclabs/nakama-common/runtime"
)

type LudoBotMatch struct{}

func (m *LudoBotMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	config := newLudoBotMatchConfig(ctx, params)
	state := newLudoBotMatchState(config)
	return state, ludoBotMatchTickRate, ludoBotMatchLabel(state)
}

func newLudoBotMatchConfig(ctx context.Context, params map[string]interface{}) ludoBotMatchConfig {
	mode := stringFromParam(params["mode"])
	if !isSupportedLudoBotMode(mode) {
		mode = "ludo_2p"
	}

	difficulty := BotDifficulty(stringFromParam(params["bot_difficulty"]))
	if !isSupportedBotDifficulty(difficulty) {
		difficulty = BotMedium
	}

	matchID := stringFromContext(ctx, runtime.RUNTIME_CTX_MATCH_ID)
	if matchID == "" {
		matchID = stringFromParam(params["request_id"])
	}

	return ludoBotMatchConfig{
		MatchID:       matchID,
		Mode:          mode,
		HumanUserID:   stringFromParam(params["human_user_id"]),
		IncludeBot:    boolFromParam(params["include_bot"]),
		BotDifficulty: difficulty,
		RequestID:     stringFromParam(params["request_id"]),
	}
}

func newLudoBotMatchState(config ludoBotMatchConfig) *LudoMatchState {
	state := &LudoMatchState{
		MatchID:          config.MatchID,
		Mode:             config.Mode,
		HumanUserID:      config.HumanUserID,
		IncludeBot:       config.IncludeBot,
		BotDifficulty:    config.BotDifficulty,
		RequestID:        config.RequestID,
		Phase:            PhaseWaitingForHuman,
		Players:          map[string]*LudoPlayer{},
		PlayerOrder:      []string{},
		TurnNumber:       1,
		ConsecutiveSixes: map[string]int{},
		Presences:        map[string]runtime.Presence{},
	}
	populateLudoBotMatchPlayers(state, config)
	state.CurrentPlayerID = state.PlayerOrder[0]
	return state
}

func ludoBotMatchLabel(state *LudoMatchState) string {
	label, _ := utils.SerializeObjectToString(&map[string]interface{}{"mode": state.Mode, "include_bot": true, "has_bot": true})
	return label
}

func (m *LudoBotMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	matchState, ok := state.(*LudoMatchState)
	if !ok {
		return state, false, "invalid match state"
	}
	if matchState.MatchFinished {
		return state, false, "match finished"
	}
	if presence.GetUserId() != matchState.HumanUserID {
		return state, false, "only the requesting human player can join this bot match"
	}
	return state, true, ""
}

func (m *LudoBotMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	matchState, ok := state.(*LudoMatchState)
	if !ok {
		return state
	}
	for _, presence := range presences {
		matchState.Presences[presence.GetUserId()] = presence
		if player, exists := matchState.Players[presence.GetUserId()]; exists {
			updateHumanPlayerProfile(ctx, nk, presence, player)
			adjustBotLevelsNearHuman(matchState, player.Level)
		}
	}
	if matchState.Phase == PhaseWaitingForHuman {
		matchState.Phase = PhaseWaitingForRoll
		broadcastMatchStart(dispatcher, matchState)
		broadcastTurnStart(dispatcher, matchState)
	}
	broadcastStateSync(dispatcher, matchState, presences)
	return matchState
}

func (m *LudoBotMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	matchState, ok := state.(*LudoMatchState)
	if !ok {
		return state
	}
	for _, presence := range presences {
		delete(matchState.Presences, presence.GetUserId())
	}
	return matchState
}

func (m *LudoBotMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	matchState, ok := state.(*LudoMatchState)
	if !ok || matchState.MatchFinished {
		return state
	}
	matchState.LastTick = tick
	for _, message := range messages {
		switch message.GetOpCode() {
		case OpRollDice:
			handleHumanRoll(dispatcher, matchState, message)
		case OpSelectToken:
			handleHumanSelectToken(dispatcher, matchState, message)
		case OpStateSync:
			broadcastStateSync(dispatcher, matchState, []runtime.Presence{message})
		default:
			broadcastActionError(dispatcher, []runtime.Presence{message}, "UNKNOWN_OPCODE", "Unsupported Ludo match action.")
		}
	}
	if current := matchState.Players[matchState.CurrentPlayerID]; current != nil && current.IsBot && matchState.Phase != PhaseWaitingForHuman {
		processBotTurn(dispatcher, matchState, tick)
	}
	return matchState
}

func (m *LudoBotMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	return state
}

func (m *LudoBotMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	matchState, ok := state.(*LudoMatchState)
	if !ok {
		return state, ""
	}
	payload, _ := utils.SerializeObjectToString(matchState)
	return state, payload
}

func handleHumanRoll(dispatcher runtime.MatchDispatcher, state *LudoMatchState, message runtime.MatchData) {
	if !isCurrentHumanAction(state, message, PhaseWaitingForRoll) {
		broadcastActionError(dispatcher, []runtime.Presence{message}, "NOT_YOUR_ROLL", "It is not your turn to roll.")
		return
	}
	if err := rollForCurrentPlayer(dispatcher, state); err != nil {
		broadcastActionError(dispatcher, []runtime.Presence{message}, "DICE_ROLL_FAILED", err.Error())
	}
}

func handleHumanSelectToken(dispatcher runtime.MatchDispatcher, state *LudoMatchState, message runtime.MatchData) {
	if !isCurrentHumanAction(state, message, PhaseWaitingForMove) {
		broadcastActionError(dispatcher, []runtime.Presence{message}, "NOT_YOUR_MOVE", "It is not your turn to move.")
		return
	}
	var req ludoSelectTokenPayload
	if err := json.Unmarshal(message.GetData(), &req); err != nil {
		broadcastActionError(dispatcher, []runtime.Presence{message}, "INVALID_PAYLOAD", "Token selection payload is invalid.")
		return
	}
	move, ok := legalMoveByToken(state.LegalMoves, req.TokenID)
	if !ok {
		broadcastActionError(dispatcher, []runtime.Presence{message}, "TOKEN_NOT_MOVABLE", "The selected token cannot move with the current dice value.")
		return
	}
	applyMoveAndAdvance(dispatcher, state, move)
}

func processBotTurn(dispatcher runtime.MatchDispatcher, state *LudoMatchState, tick int64) {
	if state.BotActionTick > tick {
		return
	}
	switch state.Phase {
	case PhaseWaitingForRoll:
		if err := rollForCurrentPlayer(dispatcher, state); err != nil {
			broadcastActionError(dispatcher, nil, "BOT_DICE_ROLL_FAILED", err.Error())
		}
	case PhaseWaitingForMove:
		move, ok := selectBotMove(state, state.Players[state.CurrentPlayerID])
		if !ok {
			endTurn(dispatcher, state, false)
			return
		}
		applyMoveAndAdvance(dispatcher, state, move)
	}
}
