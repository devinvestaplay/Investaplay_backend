package ludo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"game-server/systems/arena"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	ludoCustomRoomMatchModule = "ludo_custom_room"
	ludoRoomStorageKeyPrefix  = "custom_room_"
	ludoRoomCodeLength        = 6

	ludoRoomStatusOpen    = "open"
	ludoRoomStatusFull    = "full"
	ludoRoomStatusClosed  = "closed"
	ludoRoomStatusPlaying = "playing"

	ludoCustomRoomStatusWaiting = "waiting_for_players"
	ludoCustomRoomStatusPlaying = "playing"
	ludoCustomRoomSignalStart   = "start"
	ludoCustomRoomStartOpCode   = 200
)

type LudoRoomCreateRequest struct {
	ArenaName  string `json:"arena_name"`
	RoomName   string `json:"room_name"`
	MaxPlayers int    `json:"max_players"`
}

type LudoRoomJoinRequest struct {
	RoomCode string `json:"room_code"`
}

type LudoRoomGetRequest struct {
	RoomCode string `json:"room_code"`
}

type LudoRoomLeaveRequest struct {
	RoomCode string `json:"room_code"`
}

type LudoRoomPlayer struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	JoinedAt int64  `json:"joined_at"`
	IsHost   bool   `json:"is_host"`
}

type LudoRoomData struct {
	RoomCode  string                    `json:"room_code"`
	RoomName  string                    `json:"room_name,omitempty"`
	MatchID   string                    `json:"match_id"`
	ArenaName string                    `json:"arena_name"`
	Mode      arena.ArenaMode           `json:"mode"`
	HostID    string                    `json:"host_id"`
	MaxPlayers int                      `json:"max_players"`
	Status    string                    `json:"status"`
	Players   map[string]LudoRoomPlayer `json:"players"`
	CreatedAt int64                     `json:"created_at"`
	UpdatedAt int64                     `json:"updated_at"`
}

type LudoRoomResponse struct {
	Status     bool                     `json:"status"`
	RoomCode   string                   `json:"room_code"`
	MatchID    string                   `json:"match_id"`
	ArenaName  string                   `json:"arena_name"`
	Mode       arena.ArenaMode          `json:"mode"`
	HostID     string                   `json:"host_id"`
	MaxPlayers int                      `json:"max_players"`
	RoomStatus string                   `json:"room_status"`
	Players    []LudoRoomPlayer         `json:"players"`
}

func ludoRoomCreate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	var req LudoRoomCreateRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	matchArena, ok := arena.LudoArena.Arenas[req.ArenaName]
	if !ok {
		err := errors.New("arena not found")
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}
	if !matchArena.Enabled {
		return utils.CreateStatus(false, http.StatusForbidden, "arena disabled"), nil
	}
	if !isLudoCustomRoomMode(matchArena.Mode) {
		return utils.CreateStatus(false, http.StatusBadRequest, "arena is not a with-friends arena"), nil
	}

	maxPlayers := ludoRoomMaxPlayersForMode(matchArena.Mode)
	if req.MaxPlayers > 0 && req.MaxPlayers != maxPlayers {
		return utils.CreateStatus(false, http.StatusBadRequest, fmt.Sprintf("max_players must be %d for this arena", maxPlayers)), nil
	}

	account, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	roomCode, err := newUniqueLudoRoomCode(ctx, nk)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	params := map[string]interface{}{
		"room_code":   roomCode,
		"arena_name":  req.ArenaName,
		"mode":        string(matchArena.Mode),
		"host_id":     userID,
		"max_players": maxPlayers,
	}
	matchID, err := nk.MatchCreate(ctx, ludoCustomRoomMatchModule, params)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	now := time.Now().Unix()
	room := LudoRoomData{
		RoomCode:  roomCode,
		RoomName:  strings.TrimSpace(req.RoomName),
		MatchID:   matchID,
		ArenaName: req.ArenaName,
		Mode:      matchArena.Mode,
		HostID:    userID,
		MaxPlayers: maxPlayers,
		Status:    ludoRoomStatusOpen,
		Players: map[string]LudoRoomPlayer{
			userID: {
				UserID:   userID,
				Username: account.User.Username,
				JoinedAt: now,
				IsHost:   true,
			},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := writeLudoRoom(ctx, nk, room, ""); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	resp := ludoRoomToResponse(room)
	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

func ludoRoomJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	var req LudoRoomJoinRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	roomCode := normalizeLudoRoomCode(req.RoomCode)
	room, version, err := readLudoRoom(ctx, nk, roomCode)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}
	if room.Status == ludoRoomStatusClosed {
		return utils.CreateStatus(false, http.StatusGone, "room is closed"), nil
	}
	if _, ok := arena.LudoArena.Arenas[room.ArenaName]; !ok {
		return utils.CreateStatus(false, http.StatusNotFound, "arena not found"), nil
	}

	if _, exists := room.Players[userID]; exists {
		resp := ludoRoomToResponse(room)
		respJson, err := utils.SerializeObjectToString(&resp)
		if err != nil {
			return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
		}
		return respJson, nil
	}
	if room.Status == ludoRoomStatusPlaying {
		return utils.CreateStatus(false, http.StatusConflict, "room already started"), nil
	}
	if len(room.Players) >= room.MaxPlayers {
		return utils.CreateStatus(false, http.StatusConflict, "room is full"), nil
	}

	account, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	now := time.Now().Unix()
	room.Players[userID] = LudoRoomPlayer{UserID: userID, Username: account.User.Username, JoinedAt: now, IsHost: false}
	room.UpdatedAt = now
	if len(room.Players) >= room.MaxPlayers {
		room.Status = ludoRoomStatusFull
	}

	if err := writeLudoRoom(ctx, nk, room, version); err != nil {
		return utils.CreateStatus(false, http.StatusConflict, "room changed, try again"), err
	}

	resp := ludoRoomToResponse(room)
	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

func ludoRoomGet(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	var req LudoRoomGetRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	room, _, err := readLudoRoom(ctx, nk, normalizeLudoRoomCode(req.RoomCode))
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	resp := ludoRoomToResponse(room)
	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

func ludoRoomLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	var req LudoRoomLeaveRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	room, version, err := readLudoRoom(ctx, nk, normalizeLudoRoomCode(req.RoomCode))
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	player, exists := room.Players[userID]
	if !exists {
		resp := ludoRoomToResponse(room)
		respJson, err := utils.SerializeObjectToString(&resp)
		if err != nil {
			return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
		}
		return respJson, nil
	}

	delete(room.Players, userID)
	room.UpdatedAt = time.Now().Unix()
	if player.IsHost || len(room.Players) == 0 {
		room.Status = ludoRoomStatusClosed
	} else {
		room.Status = ludoRoomStatusOpen
	}

	if err := writeLudoRoom(ctx, nk, room, version); err != nil {
		return utils.CreateStatus(false, http.StatusConflict, "room changed, try again"), err
	}

	resp := ludoRoomToResponse(room)
	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

func isLudoCustomRoomMode(mode arena.ArenaMode) bool {
	return mode == arena.Mode2PWithFriends || mode == arena.Mode4PWithFriends
}

func ludoRoomMaxPlayersForMode(mode arena.ArenaMode) int {
	if mode == arena.Mode4PWithFriends {
		return 4
	}
	return 2
}

func normalizeLudoRoomCode(roomCode string) string {
	return strings.ToUpper(strings.TrimSpace(roomCode))
}

func ludoRoomStorageKey(roomCode string) string {
	return ludoRoomStorageKeyPrefix + normalizeLudoRoomCode(roomCode)
}

func readLudoRoom(ctx context.Context, nk runtime.NakamaModule, roomCode string) (LudoRoomData, string, error) {
	roomCode = normalizeLudoRoomCode(roomCode)
	if roomCode == "" {
		return LudoRoomData{}, "", errors.New("room_code is required")
	}

	records, err := nk.StorageRead(ctx, []*runtime.StorageRead{
		{Collection: shared_constants.LudoCollectionName, Key: ludoRoomStorageKey(roomCode), UserID: shared_constants.ServerSystemUserId},
	})
	if err != nil {
		return LudoRoomData{}, "", err
	}
	if len(records) == 0 {
		return LudoRoomData{}, "", errors.New("room not found")
	}

	var room LudoRoomData
	if err := utils.DeserializeObjectFromStringByRefs(&records[0].Value, &room); err != nil {
		return LudoRoomData{}, "", err
	}
	return room, records[0].Version, nil
}

func writeLudoRoom(ctx context.Context, nk runtime.NakamaModule, room LudoRoomData, version string) error {
	value, err := utils.SerializeObjectToString(&room)
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{
		{
			Collection:      shared_constants.LudoCollectionName,
			Key:             ludoRoomStorageKey(room.RoomCode),
			UserID:          shared_constants.ServerSystemUserId,
			Value:           value,
			Version:         version,
			PermissionRead:  0,
			PermissionWrite: 0,
		},
	})
	return err
}

func newUniqueLudoRoomCode(ctx context.Context, nk runtime.NakamaModule) (string, error) {
	for i := 0; i < 10; i++ {
		code, err := generateLudoRoomCode()
		if err != nil {
			return "", err
		}
		_, _, err = readLudoRoom(ctx, nk, code)
		if err != nil {
			return code, nil
		}
	}
	return "", errors.New("failed to generate room code")
}

func generateLudoRoomCode() (string, error) {
	max := big.NewInt(10)
	var builder strings.Builder
	for i := 0; i < ludoRoomCodeLength; i++ {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", err
		}
		builder.WriteString(n.String())
	}
	return builder.String(), nil
}

func ludoRoomToResponse(room LudoRoomData) LudoRoomResponse {
	players := make([]LudoRoomPlayer, 0, len(room.Players))
	for _, player := range room.Players {
		players = append(players, player)
	}
	return LudoRoomResponse{
		Status:     true,
		RoomCode:   room.RoomCode,
		MatchID:    room.MatchID,
		ArenaName:  room.ArenaName,
		Mode:       room.Mode,
		HostID:     room.HostID,
		MaxPlayers: room.MaxPlayers,
		RoomStatus: room.Status,
		Players:    players,
	}
}

type LudoCustomRoomMatch struct{}

type LudoCustomRoomMatchState struct {
	RoomCode   string                      `json:"room_code"`
	ArenaName  string                      `json:"arena_name"`
	Mode       arena.ArenaMode             `json:"mode"`
	HostID     string                      `json:"host_id"`
	MaxPlayers int                         `json:"max_players"`
	Status     string                      `json:"status"`
	Presences  map[string]runtime.Presence `json:"-"`
}

func (m *LudoCustomRoomMatch) MatchInit(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, params map[string]interface{}) (interface{}, int, string) {
	state := &LudoCustomRoomMatchState{
		RoomCode:   fmt.Sprint(params["room_code"]),
		ArenaName:  fmt.Sprint(params["arena_name"]),
		Mode:       arena.ArenaMode(fmt.Sprint(params["mode"])),
		HostID:     fmt.Sprint(params["host_id"]),
		MaxPlayers: intFromParam(params["max_players"], 2),
		Status:     ludoCustomRoomStatusWaiting,
		Presences:  map[string]runtime.Presence{},
	}
	label, _ := utils.SerializeObjectToString(state)
	return state, 1, label
}

func (m *LudoCustomRoomMatch) MatchJoinAttempt(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presence runtime.Presence, metadata map[string]string) (interface{}, bool, string) {
	matchState, ok := state.(*LudoCustomRoomMatchState)
	if !ok {
		return state, false, "invalid match state"
	}
	room, _, err := readLudoRoom(ctx, nk, matchState.RoomCode)
	if err != nil {
		return state, false, err.Error()
	}
	if room.Status == ludoRoomStatusClosed {
		return state, false, "room is closed"
	}
	if _, exists := room.Players[presence.GetUserId()]; !exists {
		return state, false, "join room rpc first"
	}
	return state, true, ""
}

func (m *LudoCustomRoomMatch) MatchJoin(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	matchState, ok := state.(*LudoCustomRoomMatchState)
	if !ok {
		return state
	}
	if matchState.Presences == nil {
		matchState.Presences = map[string]runtime.Presence{}
	}
	for _, presence := range presences {
		matchState.Presences[presence.GetSessionId()] = presence
	}
	return matchState
}

func (m *LudoCustomRoomMatch) MatchLeave(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, presences []runtime.Presence) interface{} {
	matchState, ok := state.(*LudoCustomRoomMatchState)
	if !ok {
		return state
	}
	for _, presence := range presences {
		delete(matchState.Presences, presence.GetSessionId())
	}
	return matchState
}

func (m *LudoCustomRoomMatch) MatchLoop(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, messages []runtime.MatchData) interface{} {
	matchState, ok := state.(*LudoCustomRoomMatchState)
	if !ok {
		return state
	}
	for _, message := range messages {
		if matchState.Status != ludoCustomRoomStatusPlaying {
			if message.GetOpCode() != ludoCustomRoomStartOpCode || message.GetUserId() != matchState.HostID {
				continue
			}
			ludoCustomRoomMarkPlaying(ctx, logger, nk, dispatcher, matchState)
		}

		recipients := ludoCustomRoomRecipients(matchState, "")
		if len(recipients) == 0 {
			continue
		}
		if err := dispatcher.BroadcastMessage(message.GetOpCode(), message.GetData(), recipients, message, true); err != nil {
			logger.Error("failed to relay ludo custom room message op %d: %v", message.GetOpCode(), err)
		}
	}
	return matchState
}

func (m *LudoCustomRoomMatch) MatchTerminate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, graceSeconds int) interface{} {
	return state
}

func (m *LudoCustomRoomMatch) MatchSignal(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, tick int64, state interface{}, data string) (interface{}, string) {
	matchState, ok := state.(*LudoCustomRoomMatchState)
	if !ok {
		return state, ""
	}
	if data != ludoCustomRoomSignalStart {
		return matchState, ""
	}
	if matchState.Status == ludoCustomRoomStatusPlaying {
		return matchState, ""
	}

	ludoCustomRoomMarkPlaying(ctx, logger, nk, dispatcher, matchState)
	presences := ludoCustomRoomRecipients(matchState, "")
	if len(presences) > 0 {
		if err := dispatcher.BroadcastMessage(ludoCustomRoomStartOpCode, []byte(""), presences, nil, true); err != nil {
			logger.Error("failed to broadcast ludo custom room start: %v", err)
		}
	}
	return matchState, ""
}

func ludoCustomRoomMarkPlaying(ctx context.Context, logger runtime.Logger, nk runtime.NakamaModule, dispatcher runtime.MatchDispatcher, state *LudoCustomRoomMatchState) {
	state.Status = ludoCustomRoomStatusPlaying
	if room, version, err := readLudoRoom(ctx, nk, state.RoomCode); err != nil {
		logger.Error("failed to read ludo custom room %s on start: %v", state.RoomCode, err)
	} else {
		room.Status = ludoRoomStatusPlaying
		room.UpdatedAt = time.Now().Unix()
		if err := writeLudoRoom(ctx, nk, room, version); err != nil {
			logger.Error("failed to mark ludo custom room %s playing: %v", state.RoomCode, err)
		}
	}
	label, _ := utils.SerializeObjectToString(state)
	if err := dispatcher.MatchLabelUpdate(label); err != nil {
		logger.Error("failed to update ludo custom room match label: %v", err)
	}
}

func ludoCustomRoomRecipients(state *LudoCustomRoomMatchState, excludeSessionID string) []runtime.Presence {
	presences := make([]runtime.Presence, 0, len(state.Presences))
	for sessionID, presence := range state.Presences {
		if sessionID == excludeSessionID {
			continue
		}
		presences = append(presences, presence)
	}
	return presences
}

func intFromParam(value interface{}, fallback int) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return fallback
	}
}
