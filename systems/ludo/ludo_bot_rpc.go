package ludo

import (
	"context"
	"database/sql"
	"fmt"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"strings"

	"github.com/heroiclabs/nakama-common/runtime"
)

type ludoMatchStartPayload struct {
	MatchID         string             `json:"match_id"`
	Mode            string             `json:"mode"`
	Players         []*LudoStartPlayer `json:"players"`
	CurrentPlayerID string             `json:"current_player_id"`
	TurnNumber      int                `json:"turn_number"`
}

type LudoStartPlayer struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	AvatarID int    `json:"avatar_id"`
	Level    int    `json:"level"`
	Country  string `json:"country"`
	Seat     int    `json:"seat"`
	Color    string `json:"color"`
	IsBot    bool   `json:"is_bot"`
}

type ludoDiceResultPayload struct {
	PlayerID      string `json:"player_id"`
	DiceValue     int    `json:"dice_value"`
	MovableTokens []int  `json:"movable_tokens"`
}

type ludoSelectTokenPayload struct {
	TokenID int `json:"token_id"`
}

type ludoTokenMovedPayload struct {
	PlayerID         string `json:"player_id"`
	TokenID          int    `json:"token_id"`
	FromPosition     int    `json:"from_position"`
	ToPosition       int    `json:"to_position"`
	Path             []int  `json:"path"`
	Captured         bool   `json:"captured"`
	CapturedPlayerID string `json:"captured_player_id"`
	CapturedTokenID  int    `json:"captured_token_id"`
	ReachedSafe      bool   `json:"reached_safe"`
	FinishedToken    bool   `json:"finished_token"`
	ExtraTurn        bool   `json:"extra_turn"`
	NextPlayerID     string `json:"next_player_id"`
}

type ludoActionErrorPayload struct {
	Success bool   `json:"success"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ludoMatchFinishedPayload struct {
	MatchID string                 `json:"match_id"`
	Mode    string                 `json:"mode"`
	Ranks   []string               `json:"ranks"`
	Players map[string]*LudoPlayer `json:"players"`
}

func ludoCreateBotMatch(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", runtime.NewError("authenticated user required", 16)
	}

	var req LudoBotMatchCreateRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return "", runtime.NewError("invalid ludo_create_bot_match payload", 3)
	}
	req.Mode = strings.TrimSpace(req.Mode)
	req.Difficulty = strings.TrimSpace(req.Difficulty)
	req.RequestID = strings.TrimSpace(req.RequestID)
	if !isSupportedLudoBotMode(req.Mode) {
		return "", runtime.NewError("unsupported ludo bot mode", 3)
	}
	if !isSupportedBotDifficulty(BotDifficulty(req.Difficulty)) {
		return "", runtime.NewError("unsupported bot difficulty", 3)
	}
	if req.RequestID == "" {
		return "", runtime.NewError("request_id is required", 3)
	}

	storageKey := ludoBotRequestStorageKey(userID, req.RequestID)
	if record, found, err := readLudoBotRequestRecord(ctx, nk, storageKey); err != nil {
		return "", runtime.NewError("failed to read bot match request", 13)
	} else if found {
		return serializeLudoBotResponse(LudoBotMatchCreateResponse{Success: true, MatchID: record.MatchID, HasBot: true, Mode: record.Mode})
	}

	matchID, err := nk.MatchCreate(ctx, ludoBotMatchModule, map[string]interface{}{
		"mode":           req.Mode,
		"human_user_id":  userID,
		"include_bot":    true,
		"bot_difficulty": req.Difficulty,
		"request_id":     req.RequestID,
	})
	if err != nil {
		logger.Error("failed to create ludo bot match for user %s: %v", userID, err)
		return "", runtime.NewError("match creation failed", 13)
	}

	record := ludoBotMatchRequestRecord{MatchID: matchID, Mode: req.Mode, Difficulty: req.Difficulty}
	if err := writeLudoBotRequestRecord(ctx, nk, storageKey, record); err != nil {
		logger.Error("failed to store ludo bot request %s for user %s: %v", req.RequestID, userID, err)
	}
	return serializeLudoBotResponse(LudoBotMatchCreateResponse{Success: true, MatchID: matchID, HasBot: true, Mode: req.Mode})
}

func serializeLudoBotResponse(resp LudoBotMatchCreateResponse) (string, error) {
	return utils.SerializeObjectToString(&resp)
}

func readLudoBotRequestRecord(ctx context.Context, nk runtime.NakamaModule, key string) (ludoBotMatchRequestRecord, bool, error) {
	records, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: shared_constants.LudoCollectionName, Key: key, UserID: shared_constants.ServerSystemUserId}})
	if err != nil || len(records) == 0 {
		return ludoBotMatchRequestRecord{}, false, err
	}
	var record ludoBotMatchRequestRecord
	if err := utils.DeserializeObjectFromStringByRefs(&records[0].Value, &record); err != nil {
		return ludoBotMatchRequestRecord{}, false, err
	}
	return record, true, nil
}

func writeLudoBotRequestRecord(ctx context.Context, nk runtime.NakamaModule, key string, record ludoBotMatchRequestRecord) error {
	value, err := utils.SerializeObjectToString(&record)
	if err != nil {
		return err
	}
	_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: shared_constants.LudoCollectionName, Key: key, UserID: shared_constants.ServerSystemUserId, Value: value, PermissionRead: 0, PermissionWrite: 0}})
	return err
}

func isSupportedLudoBotMode(mode string) bool {
	return mode == "ludo_2p" || mode == "ludo_4p"
}

func isSupportedBotDifficulty(difficulty BotDifficulty) bool {
	return difficulty == BotEasy || difficulty == BotMedium || difficulty == BotHard
}

func ludoBotPlayerCount(mode string) int {
	if mode == "ludo_4p" {
		return 4
	}
	return 2
}

func ludoBotRequestStorageKey(userID string, requestID string) string {
	return ludoBotMatchRequestStorageKey + sanitizeLudoBotIDPart(userID) + "_" + sanitizeLudoBotIDPart(requestID)
}

func sanitizeLudoBotIDPart(value string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "-", "_")
	value = replacer.Replace(strings.TrimSpace(value))
	if value == "" {
		return "unknown"
	}
	return value
}

func stringFromParam(value interface{}) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func boolFromParam(value interface{}) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	default:
		return false
	}
}

func stringFromContext(ctx context.Context, key string) string {
	value, _ := ctx.Value(key).(string)
	return value
}
