package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"game-server/systems/arena"
	"sort"
	"strconv"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

type authRequest struct {
	ID      string `json:"requestId"`
	Version int    `json:"version"`
	Piece   int    `json:"pieceId"`
	Dice    int    `json:"moveAmount"`
}
type authReply struct {
	Version          int           `json:"version"`
	Commands         []authCommand `json:"commands"`
	Phase            string        `json:"phase"`
	Deadline         int64         `json:"deadlineUnixMs"`
	RequestID        string        `json:"requestId"`
	ProcessingMicros int64         `json:"processingMicros"`
	RemainingMs      int64         `json:"remainingMs"`
}
type authMatchState struct {
	Arena         arena.LudoArenaItemData
	History       [][]byte
	Game          *authGame
	MatchID       string
	Presences     map[string]runtime.Presence
	Ready         map[string]bool
	Responses     map[string]map[string][]byte
	ResponseOrder map[string][]string
	CreatedTick   int64
	FinishedTick  int64
	Persisted     bool
	EntryPaid     bool
}
type AuthoritativeLudoMatch struct{}

func registerAuthoritativeLudo(initializer runtime.Initializer) error {
	if err := initializer.RegisterRpc("ludo_authoritative_create", authoritativeCreate); err != nil {
		return err
	}
	if err := initializer.RegisterMatch(authoritativeModule, func(context.Context, runtime.Logger, *sql.DB, runtime.NakamaModule) (runtime.Match, error) {
		return &AuthoritativeLudoMatch{}, nil
	}); err != nil {
		return err
	}
	return initializer.RegisterMatchmakerMatched(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, entries []runtime.MatchmakerEntry) (string, error) {
		// Other games and older clients retain their existing matchmaking behavior.
		count := 0
		for _, e := range entries {
			if e.GetProperties()["ludo_protocol"] == "2" {
				count++
			}
		}
		if count == 0 {
			return "", nil
		}
		if count != len(entries) || (count != 2 && count != 4) {
			return "", errors.New("incompatible authoritative Ludo players")
		}
		arenaName, _ := entries[0].GetProperties()["arena_name"].(string)
		if _, err := authArena(arenaName, count); err != nil {
			return "", err
		}
		for _, e := range entries {
			if e.GetProperties()["arena_name"] != arenaName {
				return "", errors.New("arena mismatch")
			}
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].GetPresence().GetUserId() < entries[j].GetPresence().GetUserId()
		})
		players := []*authPlayer{}
		for i, e := range entries {
			pr := e.GetPresence()
			profile := map[string]interface{}{}
			if raw, ok := e.GetProperties()["BlackTech_"+pr.GetUsername()].(string); ok {
				_ = json.Unmarshal([]byte(raw), &profile)
			}
			if profile == nil {
				profile = map[string]interface{}{}
			}
			profile["userID"] = pr.GetUserId()
			profile["username"] = pr.GetUsername()
			id := i
			if count == 2 {
				id = i * 2
			}
			players = append(players, &authPlayer{ID: id, UserID: pr.GetUserId(), Profile: profile})
		}
		data, err := json.Marshal(players)
		if err != nil {
			return "", err
		}
		return nk.MatchCreate(ctx, authoritativeModule, map[string]interface{}{"players": string(data), "arena_name": arenaName})
	})
}
func (m *AuthoritativeLudoMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	var players []*authPlayer
	raw, _ := params["players"].(string)
	if json.Unmarshal([]byte(raw), &players) != nil || (len(players) != 2 && len(players) != 4) {
		return nil, 0, ""
	}
	a, err := authArena(stringFromParam(params["arena_name"]), len(players))
	if err != nil {
		return nil, 0, ""
	}
	state := &authMatchState{Game: newAuthGame(players), MatchID: stringFromContext(ctx, runtime.RUNTIME_CTX_MATCH_ID), Presences: map[string]runtime.Presence{}, Ready: map[string]bool{}, Responses: map[string]map[string][]byte{}, ResponseOrder: map[string][]string{}}
	state.Arena = a
	for _, p := range players {
		if p.Bot {
			state.Ready[p.UserID] = true
		}
	}
	label, _ := json.Marshal(map[string]interface{}{"ludo_protocol": 2, "players": len(players)})
	return state, authoritativeTickRate, string(label)
}
func authMember(s *authMatchState, user string) *authPlayer {
	for _, p := range s.Game.Players {
		if p.UserID == user {
			return p
		}
	}
	return nil
}
func (m *AuthoritativeLudoMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	s := state.(*authMatchState)
	if authMember(s, presence.GetUserId()) == nil {
		return state, false, "not a match participant"
	}
	return state, true, ""
}
func (m *AuthoritativeLudoMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*authMatchState)
	for _, p := range presences {
		if old := s.Presences[p.GetUserId()]; old != nil && old.GetSessionId() != p.GetSessionId() {
			_ = d.MatchKick([]runtime.Presence{old})
		}
		s.Presences[p.GetUserId()] = p
	}
	return state
}
func (m *AuthoritativeLudoMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	s := state.(*authMatchState)
	for _, p := range presences {
		if old := s.Presences[p.GetUserId()]; old != nil && old.GetSessionId() == p.GetSessionId() {
			delete(s.Presences, p.GetUserId())
			if s.Game.Phase == "ready" {
				delete(s.Ready, p.GetUserId())
			}
		}
	}
	return state
}
func authSend(d runtime.MatchDispatcher, logger runtime.Logger, op int64, data []byte, to []runtime.Presence) {
	if err := d.BroadcastMessage(op, data, to, nil, true); err != nil {
		logger.Error("authoritative Ludo broadcast: %v", err)
	}
}
func (s *authMatchState) flush(d runtime.MatchDispatcher, logger runtime.Logger, tick int64, id string, started time.Time) []byte {
	s.Game.Version++
	remaining := s.Game.Deadline - tick
	if remaining < 0 {
		remaining = 0
	}
	reply := authReply{s.Game.Version, s.Game.Commands, s.Game.Phase, time.Now().UnixMilli() + remaining*1000/authoritativeTickRate, id, time.Since(started).Microseconds(), remaining * 1000 / authoritativeTickRate}
	data, _ := json.Marshal(reply)
	s.Game.Commands = nil
	s.History = append(s.History, data)
	authSend(d, logger, authBatch, data, nil)
	return data
}
func (m *AuthoritativeLudoMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	s := state.(*authMatchState)
	if s.Game.Phase == "ready" && tick-s.CreatedTick > 120*authoritativeTickRate {
		data, _ := json.Marshal(map[string]interface{}{"error": "Match cancelled because not all players became ready.", "fatal": true})
		authSend(d, logger, authError, data, nil)
		return nil
	}
	for _, msg := range messages {
		started := time.Now()
		user := msg.GetUserId()
		p := authMember(s, user)
		presence := s.Presences[user]
		if p == nil || presence == nil || presence.GetSessionId() != msg.GetSessionId() {
			continue
		}
		if len(msg.GetData()) > 32768 {
			continue
		}
		if msg.GetOpCode() == 116 || msg.GetOpCode() == 117 {
			var chat map[string]interface{}
			if len(msg.GetData()) <= 1024 && json.Unmarshal(msg.GetData(), &chat) == nil {
				chat["fromPlayer"] = strconv.Itoa(p.ID)
				data, _ := json.Marshal(chat)
				recipients := []runtime.Presence{}
				for id, pr := range s.Presences {
					if id != user {
						recipients = append(recipients, pr)
					}
				}
				if len(recipients) > 0 {
					authSend(d, logger, msg.GetOpCode(), data, recipients)
				}
			}
			continue
		}
		if msg.GetOpCode() == authSync {
			var sync struct {
				AfterVersion *int `json:"afterVersion"`
			}
			_ = json.Unmarshal(msg.GetData(), &sync)
			if sync.AfterVersion != nil && *sync.AfterVersion >= 0 && *sync.AfterVersion < s.Game.Version {
				for _, packet := range s.History[*sync.AfterVersion:] {
					var replay authReply
					_ = json.Unmarshal(packet, &replay)
					replay.RemainingMs = 0
					if replay.Version == s.Game.Version {
						replay.RemainingMs = max(int64(0), s.Game.Deadline-tick) * 1000 / authoritativeTickRate
					}
					data, _ := json.Marshal(replay)
					authSend(d, logger, authBatch, data, []runtime.Presence{msg})
				}
			} else {
				s.sendSnapshot(d, logger, msg, tick)
			}
			continue
		}
		if msg.GetOpCode() == authReady {
			// Cosmetic display data only; IDs, seats, dice and rules stay server-owned.
			if s.Game.Phase == "ready" {
				var profile map[string]interface{}
				if json.Unmarshal(msg.GetData(), &profile) == nil {
					if p.Profile == nil {
						p.Profile = map[string]interface{}{}
					}
					for _, key := range []string{"displayName", "currentAvatar", "currentBoard", "currentDice", "currentPiece"} {
						if value, ok := profile[key]; ok {
							p.Profile[key] = value
						}
					}
				}
			}
			s.Ready[user] = true
			if s.Game.Phase == "ready" && len(s.Ready) == len(s.Game.Players) {
				if err := s.charge(ctx, nk); err != nil {
					data, _ := json.Marshal(map[string]interface{}{"error": "Cannot start: entry payment could not be confirmed. Check your balance before trying again.", "fatal": true})
					authSend(d, logger, authError, data, nil)
					return nil
				}
				s.Game.start(tick)
				s.flush(d, logger, tick, "", started)
			}
			continue
		}
		var req authRequest
		if len(msg.GetData()) > 1024 || json.Unmarshal(msg.GetData(), &req) != nil || len(req.ID) == 0 || len(req.ID) > 64 {
			continue
		}
		if cached := s.Responses[user][req.ID]; cached != nil {
			authSend(d, logger, authBatch, cached, []runtime.Presence{msg})
			continue
		}
		var err error
		if req.Version != s.Game.Version {
			err = errors.New("stale match version")
		} else if s.Game.Phase == "ready" {
			err = errors.New("match has not started")
		} else if msg.GetOpCode() == authSurrender && (s.Game.Phase == "finished" || s.Game.Ranks[p.ID] > 0) {
			data, _ := json.Marshal(authReply{Version: s.Game.Version, Commands: []authCommand{}, Phase: s.Game.Phase, RequestID: req.ID})
			authSend(d, logger, authBatch, data, []runtime.Presence{msg})
			continue
		} else if msg.GetOpCode() == authSurrender {
			s.Game.surrender(p.ID, tick)
		} else if p.ID != s.Game.Current || s.Game.Left[p.ID] || s.Game.Phase == "finished" {
			err = errors.New("not your turn")
		} else {
			switch msg.GetOpCode() {
			case authRoll:
				if s.Game.Phase != "roll" {
					err = errors.New("not waiting for roll")
				} else {
					var dice int
					dice, err = rollDice()
					if err == nil {
						err = s.Game.roll(dice, tick)
					}
				}
			case authMove:
				err = s.Game.move(req.Piece, req.Dice, tick)
			default:
				err = errors.New("unsupported action")
			}
		}
		if err != nil {
			data, _ := json.Marshal(map[string]interface{}{"requestId": req.ID, "error": err.Error(), "version": s.Game.Version})
			authSend(d, logger, authError, data, []runtime.Presence{msg})
			continue
		}
		data := s.flush(d, logger, tick, req.ID, started)
		if s.Responses[user] == nil {
			s.Responses[user] = map[string][]byte{}
		}
		s.Responses[user][req.ID] = data
		s.ResponseOrder[user] = append(s.ResponseOrder[user], req.ID)
		if len(s.ResponseOrder[user]) > 64 {
			old := s.ResponseOrder[user][0]
			s.ResponseOrder[user] = s.ResponseOrder[user][1:]
			delete(s.Responses[user], old)
		}
	}
	if s.Game.Phase != "ready" && s.Game.Phase != "finished" {
		current := s.Game.player(s.Game.Current)
		if current != nil && current.Bot && s.Game.BotActionTick > 0 && tick >= s.Game.BotActionTick {
			started := time.Now()
			s.Game.BotActionTick = 0
			if err := performAuthoritativeBotAction(s.Game, logger, tick); err != nil {
				if logger != nil {
					logger.Error("authoritative Ludo bot action: %v", err)
				}
			} else {
				s.flush(d, logger, tick, "", started)
			}
		}
	}
	if s.Game.Phase != "ready" && s.Game.Phase != "finished" && tick >= s.Game.Deadline {
		started := time.Now()
		current := s.Game.player(s.Game.Current)
		var err error
		if current != nil && current.Bot {
			err = performAuthoritativeBotAction(s.Game, logger, tick)
		} else {
			err = s.Game.timeout(tick)
		}
		if err != nil {
			if logger != nil {
				logger.Error("authoritative Ludo timeout: %v", err)
			}
		} else {
			s.flush(d, logger, tick, "", started)
		}
	}
	if s.Game.Phase == "finished" {
		if s.FinishedTick == 0 {
			s.FinishedTick = tick
		}
		if !s.Persisted && (s.FinishedTick == tick || tick%authoritativeTickRate == 0) {
			if err := s.settle(ctx, nk); err != nil {
				logger.Error("Ludo settlement pending match %s: %v", s.MatchID, err)
			}
		}
		if s.Persisted && tick-s.FinishedTick > 300*authoritativeTickRate {
			return nil
		}
	}
	return s
}
func (s *authMatchState) sendSnapshot(d runtime.MatchDispatcher, logger runtime.Logger, to runtime.Presence, tick int64) {
	states := map[int]int{}
	profiles := map[int]interface{}{}
	for _, p := range s.Game.Players {
		states[p.ID] = 0
		if s.Presences[p.UserID] == nil {
			states[p.ID] = 3
		}
		if s.Game.Ranks[p.ID] > 0 {
			states[p.ID] = 1
		}
		if s.Game.Left[p.ID] {
			states[p.ID] = 2
		}
		profiles[p.ID] = p.Profile
	}
	rejoin := map[string]interface{}{"players": s.Game.Players, "currentPlayer": s.Game.Current, "currentPlayerDiceRolls": s.Game.Rolls, "currentPlayerMaxRolled": s.Game.Sixes, "currentPlayerRollsAvaiable": s.Game.Available, "arenaType": 0, "isWatingRollDice": s.Game.Phase == "roll", "isWaitingMove": s.Game.Phase == "move", "matchConfig": map[string]interface{}{"PieceFinishToWinCount": 4, "isQuick": false}, "playerRanking": s.Game.Ranks, "playerStates": states, "allPlayerUserSmallData": profiles}
	data, _ := json.Marshal(map[string]interface{}{"version": s.Game.Version, "commandNumber": s.Game.Number, "phase": s.Game.Phase, "state": rejoin, "moves": s.Game.moves(), "remainingMs": max(int64(0), s.Game.Deadline-tick) * 1000 / authoritativeTickRate})
	authSend(d, logger, authSnapshot, data, []runtime.Presence{to})
}
func (m *AuthoritativeLudoMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, grace int) interface{} {
	return state
}
func (m *AuthoritativeLudoMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	s := state.(*authMatchState)
	encoded, _ := json.Marshal(map[string]interface{}{"protocol": 2, "finished": s.Game.Phase == "finished", "players": s.Game.Players, "ranks": s.Game.Ranks})
	return state, string(encoded)
}
