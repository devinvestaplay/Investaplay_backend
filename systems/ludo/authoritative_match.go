package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
type authCachedResponse struct {
	OpCode      int64
	Data        []byte
	Action      int64
	Client      int
	Dice        int
	PieceID     int
	OldPosition int
	NewPosition int
	Accepted    bool
	Reason      string
}
type authMatchState struct {
	Arena         arena.LudoArenaItemData
	History       [][]byte
	Game          *authGame
	MatchID       string
	Presences     map[string]runtime.Presence
	Ready         map[string]bool
	Responses     map[string]map[string]authCachedResponse
	ResponseOrder map[string][]string
	CreatedTick   int64
	FinishedTick  int64
	Persisted     bool
	EntryPaid     bool
}
type AuthoritativeLudoMatch struct{}

const authResponseCacheLimit = 64

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
	state := &authMatchState{Game: newAuthGame(players), MatchID: stringFromContext(ctx, runtime.RUNTIME_CTX_MATCH_ID), Presences: map[string]runtime.Presence{}, Ready: map[string]bool{}, Responses: map[string]map[string]authCachedResponse{}, ResponseOrder: map[string][]string{}}
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
		if logger != nil {
			logger.Error("authoritative Ludo broadcast: %v", err)
		}
	}
}

func (s *authMatchState) cacheResponse(userID, requestID string, opCode int64, data []byte) {
	if s.Responses == nil {
		s.Responses = map[string]map[string]authCachedResponse{}
	}
	if s.ResponseOrder == nil {
		s.ResponseOrder = map[string][]string{}
	}
	if s.Responses[userID] == nil {
		s.Responses[userID] = map[string]authCachedResponse{}
	}
	if _, exists := s.Responses[userID][requestID]; exists {
		return
	}
	s.Responses[userID][requestID] = authCachedResponse{OpCode: opCode, Data: append([]byte(nil), data...)}
	s.ResponseOrder[userID] = append(s.ResponseOrder[userID], requestID)
	if len(s.ResponseOrder[userID]) > authResponseCacheLimit {
		oldest := s.ResponseOrder[userID][0]
		s.ResponseOrder[userID] = s.ResponseOrder[userID][1:]
		delete(s.Responses[userID], oldest)
	}
}

func (s *authMatchState) annotateCachedResponse(userID, requestID string, request authRequest, action int64, dice, pieceID, oldPosition, newPosition int, accepted bool, reason string) {
	cached, found := s.Responses[userID][requestID]
	if !found {
		return
	}
	cached.Action = action
	cached.Client = request.Version
	cached.Dice = dice
	cached.PieceID = pieceID
	cached.OldPosition = oldPosition
	cached.NewPosition = newPosition
	cached.Accepted = accepted
	cached.Reason = reason
	s.Responses[userID][requestID] = cached
}

func (s *authMatchState) applyTransition(apply func(*authGame) error) error {
	if err := s.Game.validateCanonicalState(); err != nil {
		return fmt.Errorf("invalid authoritative state before action: %w", err)
	}
	next := s.Game.clone()
	if err := apply(next); err != nil {
		return err
	}
	if err := next.validateCanonicalState(); err != nil {
		return fmt.Errorf("invalid authoritative state after action: %w", err)
	}
	s.Game = next
	return nil
}

func authActionName(opCode int64) string {
	switch opCode {
	case authRoll:
		return "roll"
	case authMove:
		return "move"
	case authSync:
		return "resync"
	case authSurrender:
		return "surrender"
	default:
		return fmt.Sprintf("opcode_%d", opCode)
	}
}

func (s *authMatchState) logAction(logger runtime.Logger, player *authPlayer, request authRequest, opCode int64, serverVersion, dice, pieceID, oldPosition, newPosition int, accepted, duplicate bool, reason string) {
	if logger == nil {
		return
	}
	userID := ""
	playerID := -1
	if player != nil {
		userID = player.UserID
		playerID = player.ID
	}
	logger.Info("[LudoAction] matchId=%s userId=%s playerId=%d requestId=%s clientVersion=%d serverVersion=%d action=%s dice=%d pieceId=%d oldPosition=%d newPosition=%d accepted=%t rejected=%t rejectionReason=%q duplicate=%t", s.MatchID, userID, playerID, request.ID, request.Version, serverVersion, authActionName(opCode), dice, pieceID, oldPosition, newPosition, accepted, !accepted, reason, duplicate)
}

func (s *authMatchState) rejectAction(d runtime.MatchDispatcher, logger runtime.Logger, presence runtime.Presence, player *authPlayer, request authRequest, opCode int64, tick int64, reason string, recoverySnapshot bool) {
	if recoverySnapshot {
		if stateErr := s.Game.validateCanonicalState(); stateErr != nil {
			data, _ := json.Marshal(map[string]interface{}{"requestId": request.ID, "error": "authoritative state is invalid", "fatal": true, "version": s.Game.Version})
			s.cacheResponse(player.UserID, request.ID, authError, data)
			s.annotateCachedResponse(player.UserID, request.ID, request, opCode, request.Dice, request.Piece, -1, -1, false, stateErr.Error())
			authSend(d, logger, authError, data, []runtime.Presence{presence})
			s.logAction(logger, player, request, opCode, s.Game.Version, request.Dice, request.Piece, -1, -1, false, false, stateErr.Error())
			return
		}
		data := s.snapshotData(tick, request.ID)
		s.cacheResponse(player.UserID, request.ID, authSnapshot, data)
		s.annotateCachedResponse(player.UserID, request.ID, request, opCode, request.Dice, request.Piece, -1, -1, false, reason)
		authSend(d, logger, authSnapshot, data, []runtime.Presence{presence})
		s.logAction(logger, player, request, opCode, s.Game.Version, request.Dice, request.Piece, -1, -1, false, false, reason)
		return
	}
	data, _ := json.Marshal(map[string]interface{}{"requestId": request.ID, "error": reason, "version": s.Game.Version})
	s.cacheResponse(player.UserID, request.ID, authError, data)
	s.annotateCachedResponse(player.UserID, request.ID, request, opCode, request.Dice, request.Piece, -1, -1, false, reason)
	authSend(d, logger, authError, data, []runtime.Presence{presence})
	s.logAction(logger, player, request, opCode, s.Game.Version, request.Dice, request.Piece, -1, -1, false, false, reason)
}

func authTransitionDetails(before, after *authGame, playerID int) (dice, pieceID, oldPosition, newPosition int) {
	pieceID, oldPosition, newPosition = -1, -1, -1
	if before.Phase == "roll" {
		for _, command := range after.Commands[len(before.Commands):] {
			if command["commandName"] == "RollDice" {
				dice, _ = command["diceValue"].(int)
				return dice, pieceID, oldPosition, newPosition
			}
		}
		return dice, pieceID, oldPosition, newPosition
	}
	oldPlayer := before.player(playerID)
	newPlayer := after.player(playerID)
	if oldPlayer == nil || newPlayer == nil {
		return dice, pieceID, oldPosition, newPosition
	}
	for index := range oldPlayer.Pieces {
		if oldPlayer.Pieces[index].Passed != newPlayer.Pieces[index].Passed || oldPlayer.Pieces[index].Position != newPlayer.Pieces[index].Position {
			pieceID = index
			oldPosition = oldPlayer.Pieces[index].Position
			newPosition = newPlayer.Pieces[index].Position
			dice = newPlayer.Pieces[index].Passed - oldPlayer.Pieces[index].Passed
			if oldPlayer.Pieces[index].Passed == 0 && newPlayer.Pieces[index].Passed == 1 {
				dice = 6
			}
			return dice, pieceID, oldPosition, newPosition
		}
	}
	return dice, pieceID, oldPosition, newPosition
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
			clientVersion := s.Game.Version
			if sync.AfterVersion != nil {
				clientVersion = *sync.AfterVersion
			}
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
			s.logAction(logger, p, authRequest{Version: clientVersion}, authSync, s.Game.Version, 0, -1, -1, -1, true, false, "")
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
		if len(msg.GetData()) > 1024 || json.Unmarshal(msg.GetData(), &req) != nil {
			data, _ := json.Marshal(map[string]interface{}{"error": "invalid action payload", "version": s.Game.Version})
			authSend(d, logger, authError, data, []runtime.Presence{msg})
			s.logAction(logger, p, req, msg.GetOpCode(), s.Game.Version, 0, req.Piece, -1, -1, false, false, "invalid action payload")
			continue
		}
		if len(req.ID) == 0 || len(req.ID) > 64 {
			data, _ := json.Marshal(map[string]interface{}{"requestId": req.ID, "error": "requestId is required and must not exceed 64 characters", "version": s.Game.Version})
			authSend(d, logger, authError, data, []runtime.Presence{msg})
			s.logAction(logger, p, req, msg.GetOpCode(), s.Game.Version, 0, req.Piece, -1, -1, false, false, "invalid requestId")
			continue
		}
		if cached, found := s.Responses[user][req.ID]; found {
			authSend(d, logger, cached.OpCode, cached.Data, []runtime.Presence{msg})
			action := cached.Action
			if action == 0 {
				action = msg.GetOpCode()
			}
			cachedRequest := authRequest{ID: req.ID, Version: cached.Client, Piece: cached.PieceID}
			s.logAction(logger, p, cachedRequest, action, s.Game.Version, cached.Dice, cached.PieceID, cached.OldPosition, cached.NewPosition, cached.Accepted, true, cached.Reason)
			continue
		}
		if req.Version < s.Game.Version {
			s.rejectAction(d, logger, msg, p, req, msg.GetOpCode(), tick, "stale match version", true)
			continue
		}
		if req.Version > s.Game.Version {
			s.rejectAction(d, logger, msg, p, req, msg.GetOpCode(), tick, "future match version", true)
			continue
		}
		if s.Game.Phase == "ready" {
			s.rejectAction(d, logger, msg, p, req, msg.GetOpCode(), tick, "match has not started", false)
			continue
		}
		if msg.GetOpCode() == authSurrender && (s.Game.Phase == "finished" || s.Game.Ranks[p.ID] > 0) {
			data, _ := json.Marshal(authReply{Version: s.Game.Version, Commands: []authCommand{}, Phase: s.Game.Phase, RequestID: req.ID})
			authSend(d, logger, authBatch, data, []runtime.Presence{msg})
			s.cacheResponse(user, req.ID, authBatch, data)
			s.annotateCachedResponse(user, req.ID, req, msg.GetOpCode(), 0, -1, -1, -1, true, "")
			s.logAction(logger, p, req, msg.GetOpCode(), s.Game.Version, 0, -1, -1, -1, true, false, "")
			continue
		}
		if msg.GetOpCode() != authSurrender && (p.ID != s.Game.Current || s.Game.Left[p.ID] || s.Game.Phase == "finished") {
			s.rejectAction(d, logger, msg, p, req, msg.GetOpCode(), tick, "not your turn", false)
			continue
		}

		var transitionErr error
		diceResult := 0
		moveResult := authMoveResult{PieceID: req.Piece, OldPosition: -1, NewPosition: -1}
		switch msg.GetOpCode() {
		case authSurrender:
			transitionErr = s.applyTransition(func(game *authGame) error {
				game.surrender(p.ID, tick)
				return nil
			})
		case authRoll:
			transitionErr = s.applyTransition(func(game *authGame) error {
				if game.Phase != "roll" {
					return errors.New("not waiting for a roll")
				}
				var err error
				diceResult, err = rollDice()
				if err != nil {
					return err
				}
				return game.roll(diceResult, tick)
			})
		case authMove:
			transitionErr = s.applyTransition(func(game *authGame) error {
				var err error
				moveResult, err = game.moveRequested(req.Piece, req.Dice, tick)
				return err
			})
			diceResult = moveResult.Dice
		default:
			transitionErr = errors.New("unsupported action")
		}
		if transitionErr != nil {
			if errors.Is(transitionErr, errOwnTokenOccupied) && logger != nil {
				logger.Info("MOVE_REJECTED reason=OWN_TOKEN_OCCUPIED player=%d token=%d from=%d destination=%d dice=%d", p.ID, moveResult.PieceID, moveResult.OldPosition, moveResult.NewPosition, moveResult.Dice)
			}
			s.rejectAction(d, logger, msg, p, req, msg.GetOpCode(), tick, transitionErr.Error(), false)
			continue
		}
		data := s.flush(d, logger, tick, req.ID, started)
		s.cacheResponse(user, req.ID, authBatch, data)
		s.annotateCachedResponse(user, req.ID, req, msg.GetOpCode(), diceResult, moveResult.PieceID, moveResult.OldPosition, moveResult.NewPosition, true, "")
		s.logAction(logger, p, req, msg.GetOpCode(), s.Game.Version, diceResult, moveResult.PieceID, moveResult.OldPosition, moveResult.NewPosition, true, false, "")
	}
	if s.Game.Phase != "ready" && s.Game.Phase != "finished" {
		current := s.Game.player(s.Game.Current)
		if current != nil && current.Bot && s.Game.BotActionTick > 0 && tick >= s.Game.BotActionTick {
			started := time.Now()
			before := s.Game
			request := authRequest{ID: fmt.Sprintf("bot-%d-%d", tick, before.Version), Version: before.Version, Piece: -1}
			opCode := authRoll
			if before.Phase == "move" {
				opCode = authMove
			}
			err := s.applyTransition(func(game *authGame) error {
				game.BotActionTick = 0
				return performAuthoritativeBotAction(game, logger, tick)
			})
			if err != nil {
				if logger != nil {
					logger.Error("authoritative Ludo bot action: %v", err)
				}
				s.logAction(logger, current, request, opCode, before.Version, 0, -1, -1, -1, false, false, err.Error())
			} else {
				dice, pieceID, oldPosition, newPosition := authTransitionDetails(before, s.Game, current.ID)
				s.flush(d, logger, tick, "", started)
				s.logAction(logger, current, request, opCode, s.Game.Version, dice, pieceID, oldPosition, newPosition, true, false, "")
			}
		}
	}
	if s.Game.Phase != "ready" && s.Game.Phase != "finished" && tick >= s.Game.Deadline {
		started := time.Now()
		current := s.Game.player(s.Game.Current)
		before := s.Game
		request := authRequest{ID: fmt.Sprintf("timeout-%d-%d", tick, before.Version), Version: before.Version, Piece: -1}
		opCode := authRoll
		if before.Phase == "move" {
			opCode = authMove
		}
		err := s.applyTransition(func(game *authGame) error {
			if current != nil && current.Bot {
				return performAuthoritativeBotAction(game, logger, tick)
			}
			return game.timeout(tick)
		})
		if err != nil {
			if logger != nil {
				logger.Error("authoritative Ludo timeout: %v", err)
			}
			s.logAction(logger, current, request, opCode, before.Version, 0, -1, -1, -1, false, false, err.Error())
		} else {
			dice, pieceID, oldPosition, newPosition := authTransitionDetails(before, s.Game, current.ID)
			s.flush(d, logger, tick, "", started)
			s.logAction(logger, current, request, opCode, s.Game.Version, dice, pieceID, oldPosition, newPosition, true, false, "")
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
func (s *authMatchState) snapshotData(tick int64, requestID string) []byte {
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
	payload := map[string]interface{}{"version": s.Game.Version, "commandNumber": s.Game.Number, "phase": s.Game.Phase, "state": rejoin, "moves": s.Game.moves(), "remainingMs": max(int64(0), s.Game.Deadline-tick) * 1000 / authoritativeTickRate}
	if requestID != "" {
		payload["requestId"] = requestID
	}
	data, _ := json.Marshal(payload)
	return data
}
func (s *authMatchState) sendSnapshot(d runtime.MatchDispatcher, logger runtime.Logger, to runtime.Presence, tick int64) {
	if err := s.Game.validateCanonicalState(); err != nil {
		if logger != nil {
			logger.Error("authoritative Ludo refused invalid snapshot matchId=%s: %v", s.MatchID, err)
		}
		data, _ := json.Marshal(map[string]interface{}{"error": "authoritative state is invalid", "fatal": true, "version": s.Game.Version})
		authSend(d, logger, authError, data, []runtime.Presence{to})
		return
	}
	data := s.snapshotData(tick, "")
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
