package ludo

import (
	"encoding/json"
	"github.com/heroiclabs/nakama-common/runtime"
)

// Separate from both the Unity relay opcodes and the original bot protocol.
const (
	OpRecoverySync int64 = 300
	OpRecoverySnapshot int64 = 301
	OpRecoveryAction int64 = 302
	OpRecoveryError int64 = 303
)

type ludoRecoveryAction struct {
	ActionID string `json:"action_id"`
	Version int64 `json:"expected_version"`
	Kind string `json:"kind"`
	TokenID int `json:"token_id"`
}

func isActiveLudoPresence(state *LudoMatchState, message runtime.MatchData) bool {
	p := state.Presences[message.GetUserId()]
	return p != nil && p.GetSessionId() == message.GetSessionId() && state.Players[message.GetUserId()] != nil
}

func recoverySnapshot(dispatcher runtime.MatchDispatcher, state *LudoMatchState, recipient runtime.Presence) {
	// The legacy bot policy has a deadline only for a disconnected human turn.
	state.TurnDeadlineMs = 0
	if p := state.Players[state.CurrentPlayerID]; p != nil && !p.IsBot && !state.MatchFinished && state.Phase != PhaseWaitingForHuman {
		if state.Presences[p.ID] == nil {
			remaining := ludoBotHumanTurnTimeoutTicks - (state.LastTick - state.TurnStartedTick)
			if remaining < 0 { remaining = 0 }
			state.TurnDeadlineMs = state.ServerTimeMs + remaining*1000/ludoBotMatchTickRate
		}
	}
	broadcastPayloadTo(dispatcher, OpRecoverySnapshot, state, []runtime.Presence{recipient})
}

func handleLudoRecoveryMessage(dispatcher runtime.MatchDispatcher, logger runtime.Logger, state *LudoMatchState, message runtime.MatchData) {
	if message.GetOpCode() == OpRecoverySync {
		recoverySnapshot(dispatcher, state, message)
		logger.Debug("ludo_sync version=%d phase=%s", state.StateVersion, state.Phase)
		return
	}
	var action ludoRecoveryAction
	if len(message.GetData()) > 1024 || json.Unmarshal(message.GetData(), &action) != nil || len(action.ActionID) < 1 || len(action.ActionID) > 64 {
		broadcastPayloadTo(dispatcher, OpRecoveryError, map[string]string{"code":"INVALID_ACTION"}, []runtime.Presence{message})
		return
	}
	key := message.GetUserId() + ":" + action.ActionID
	if _, duplicate := state.AcceptedActions[key]; duplicate {
		recoverySnapshot(dispatcher, state, message)
		return
	}
	if action.Version != state.StateVersion || state.MatchFinished {
		recoverySnapshot(dispatcher, state, message)
		return
	}
	before := state.StateVersion
	switch action.Kind {
	case "roll":
		handleHumanRoll(dispatcher, state, message)
	case "move":
		if isCurrentHumanAction(state, message, PhaseWaitingForMove) {
			if move, ok := legalMoveByToken(state.LegalMoves, action.TokenID); ok {
				applyMoveAndAdvance(dispatcher, state, move)
			}
		}
	}
	if state.StateVersion != before {
		if state.AcceptedActions == nil { state.AcceptedActions = map[string]int64{} }
		state.AcceptedActions[key] = state.StateVersion
		// Old retries still fail the expected-version check after eviction.
		for id, version := range state.AcceptedActions {
			if version < state.StateVersion-256 { delete(state.AcceptedActions, id) }
		}
		logger.Debug("ludo_action version=%d kind=%s", state.StateVersion, action.Kind)
	}
	recoverySnapshot(dispatcher, state, message)
}
