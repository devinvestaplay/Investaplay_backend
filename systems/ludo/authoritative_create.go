package ludo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/heroiclabs/nakama-common/runtime"
	"sort"
)

// Invitations identify participants only. Each human must join and send Ready
// before the server atomically collects entry fees and starts the game.
func authoritativeCreate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	user, err := ludoAuthenticatedUserID(ctx)
	if err != nil {
		return "", err
	}
	var req struct {
		Arena    string                   `json:"arena_name"`
		Profiles []map[string]interface{} `json:"profiles"`
	}
	if len(payload) > 32768 || json.Unmarshal([]byte(payload), &req) != nil {
		return "", runtime.NewError("invalid match request", 3)
	}
	if _, err = authArena(req.Arena, len(req.Profiles)); err != nil {
		return "", err
	}
	ids := []string{}
	profiles := map[string]map[string]interface{}{}
	self := false
	for _, profile := range req.Profiles {
		id, _ := profile["userID"].(string)
		if id == "" || profiles[id] != nil {
			return "", runtime.NewError("duplicate or missing participant", 3)
		}
		if id == user {
			self = true
		}
		ids = append(ids, id)
		profiles[id] = profile
	}
	if !self {
		return "", runtime.NewError("caller must participate", 7)
	}
	users, err := nk.UsersGetId(ctx, ids, nil)
	if err != nil {
		return "", err
	}
	if len(users) != len(ids) {
		return "", runtime.NewError("participant not found", 5)
	}
	for _, u := range users {
		profiles[u.Id]["username"] = u.Username
		profiles[u.Id]["userID"] = u.Id
	}
	sort.Strings(ids)
	players := []*authPlayer{}
	for i, id := range ids {
		seat := i
		if len(ids) == 2 {
			seat = i * 2
		}
		players = append(players, &authPlayer{ID: seat, UserID: id, Profile: profiles[id]})
	}
	encoded, _ := json.Marshal(players)
	matchID, err := nk.MatchCreate(ctx, authoritativeModule, map[string]interface{}{"players": string(encoded), "arena_name": req.Arena})
	if err != nil {
		return "", err
	}
	result, _ := json.Marshal(map[string]interface{}{"match_id": matchID, "protocol": 2})
	return string(result), nil
}

func authoritativeBotCreate(ctx context.Context, nk runtime.NakamaModule, user string, req LudoOnlineBotMatchCreateRequest) (string, error) {
	if len(req.RequestID) > 256 {
		return "", runtime.NewError("request_id too long", 3)
	}
	key := fmt.Sprintf("bot_request_%x", sha256.Sum256([]byte(user+"|"+req.RequestID)))
	readCached := func() (string, error) {
		records, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: authRecords, Key: key}})
		if err != nil {
			return "", err
		}
		if len(records) == 0 {
			return "", nil
		}
		var response LudoOnlineBotMatchCreateResponse
		if json.Unmarshal([]byte(records[0].Value), &response) != nil || response.ArenaName != req.ArenaName || len(response.Players) != req.PlayerCount {
			return "", runtime.NewError("request_id reused with different parameters", 3)
		}
		return records[0].Value, nil
	}
	if cached, err := readCached(); err != nil || cached != "" {
		return cached, err
	}
	account, err := nk.AccountGetId(ctx, user)
	if err != nil {
		return "", err
	}
	players := []*authPlayer{}
	responsePlayers := []LudoOnlineBotMatchPlayer{}
	for i := 0; i < req.PlayerCount; i++ {
		seat := i
		if req.PlayerCount == 2 {
			seat = i * 2
		}
		id := fmt.Sprintf("bot_%s_%d", user, seat)
		name := fmt.Sprintf("Bot %d", i)
		username := id
		avatar := "avatar_1"
		if i == 0 {
			id = user
			username = account.User.Username
			name = account.User.DisplayName
			if name == "" {
				name = username
			}
			avatar = ludoNormalizeAvatarAddressableKey(account.User.AvatarUrl)
		}
		profile := map[string]interface{}{"userID": id, "username": username, "displayName": name, "currentAvatar": avatar, "currentDice": 1, "currentBoard": 1, "currentPiece": 1}
		players = append(players, &authPlayer{ID: seat, UserID: id, Bot: i != 0, Profile: profile})
		responsePlayers = append(responsePlayers, LudoOnlineBotMatchPlayer{UserID: id, DisplayName: name, Avatar: avatar, PlayerID: seat, Seat: i, IsBot: i != 0})
	}
	data, _ := json.Marshal(players)
	matchID, err := nk.MatchCreate(ctx, authoritativeModule, map[string]interface{}{"players": string(data), "arena_name": req.ArenaName})
	if err != nil {
		return "", err
	}
	response := LudoOnlineBotMatchCreateResponse{Success: true, MatchID: matchID, ArenaName: req.ArenaName, Players: responsePlayers}
	result, err := serializeLudoOnlineBotResponse(response)
	if err != nil {
		return "", err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: authRecords, Key: key, Value: result, Version: "*", PermissionRead: 0, PermissionWrite: 0}})
	if err != nil {
		if cached, readErr := readCached(); readErr == nil && cached != "" {
			return cached, nil
		}
		return "", err
	}
	return result, nil
}
