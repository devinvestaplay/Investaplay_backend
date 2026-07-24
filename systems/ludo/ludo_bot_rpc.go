package ludo

import (
	"context"
	"database/sql"
	"fmt"
	"game-server/systems/arena"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"strings"

	"github.com/heroiclabs/nakama-common/api"
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
	userID, err := ludoAuthenticatedUserID(ctx)
	if err != nil {
		return "", err
	}

	req, err := parseLudoBotMatchCreateRequest(payload)
	if err != nil {
		return "", err
	}

	options, err := newLudoBotMatchCreateOptions(userID, req)
	if err != nil {
		return "", err
	}

	result, err := createOrGetLudoBotMatch(ctx, logger, nk, options)
	if err != nil {
		return "", err
	}

	return serializeLudoBotResponse(newLudoBotMatchCreateResponse(result))
}

func ludoOnlineBotMatchCreate(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, err := ludoAuthenticatedUserID(ctx)
	if err != nil {
		return "", err
	}

	req, err := parseLudoOnlineBotMatchCreateRequest(payload)
	if err != nil {
		return "", err
	}

	options, matchArena, err := newLudoOnlineBotMatchCreateOptions(userID, req)
	if err != nil {
		return "", err
	}

	result, err := createOrGetLudoBotMatch(ctx, logger, nk, options)
	if err != nil {
		return "", err
	}

	matchState, err := readLudoBotMatchState(ctx, nk, result.MatchID)
	if err != nil {
		logger.Error("failed to read ludo online bot match state %s: %v", result.MatchID, err)
		return "", runtime.NewError("failed to read bot match state", 13)
	}

	players, err := ludoOnlineBotMatchPlayers(ctx, nk, userID, matchState)
	if err != nil {
		logger.Error("failed to build ludo online bot players for match %s: %v", result.MatchID, err)
		return "", runtime.NewError("failed to build bot match players", 13)
	}

	resp := LudoOnlineBotMatchCreateResponse{
		Success:          true,
		MatchID:          result.MatchID,
		ArenaName:        req.ArenaName,
		EntryFeeRequired: matchArena.FeeCurrencyData.Amount > 0,
		EntryFeeAmount:   matchArena.FeeCurrencyData.Amount,
		Players:          players,
	}
	return serializeLudoOnlineBotResponse(resp)
}

func parseLudoOnlineBotMatchCreateRequest(payload string) (LudoOnlineBotMatchCreateRequest, error) {
	var req LudoOnlineBotMatchCreateRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return LudoOnlineBotMatchCreateRequest{}, runtime.NewError("invalid ludo_online_bot_match_create payload", 3)
	}
	req.ArenaName = strings.TrimSpace(req.ArenaName)
	req.Difficulty = strings.TrimSpace(req.Difficulty)
	req.RequestID = strings.TrimSpace(req.RequestID)
	return req, nil
}

func newLudoOnlineBotMatchCreateOptions(userID string, req LudoOnlineBotMatchCreateRequest) (ludoBotMatchCreateOptions, arena.LudoArenaItemData, error) {
	matchArena, ok := arena.LudoArena.Arenas[req.ArenaName]
	if !ok {
		return ludoBotMatchCreateOptions{}, arena.LudoArenaItemData{}, runtime.NewError("arena not found", 5)
	}
	if !matchArena.Enabled {
		return ludoBotMatchCreateOptions{}, arena.LudoArenaItemData{}, runtime.NewError("arena disabled", 7)
	}

	mode, err := ludoBotModeForOnlineArena(matchArena.Mode, req.PlayerCount)
	if err != nil {
		return ludoBotMatchCreateOptions{}, arena.LudoArenaItemData{}, err
	}

	difficulty := BotDifficulty(req.Difficulty)
	if difficulty == "" {
		difficulty = BotMedium
	}
	if !isSupportedBotDifficulty(difficulty) {
		return ludoBotMatchCreateOptions{}, arena.LudoArenaItemData{}, runtime.NewError("unsupported bot difficulty", 3)
	}
	if req.RequestID == "" {
		return ludoBotMatchCreateOptions{}, arena.LudoArenaItemData{}, runtime.NewError("request_id is required", 3)
	}

	return ludoBotMatchCreateOptions{
		HumanUserID: userID,
		Mode:        mode,
		Difficulty:  difficulty,
		RequestID:   req.RequestID,
		IncludeBot:  true,
		StorageKey:  ludoOnlineBotRequestStorageKey(userID, req.ArenaName, req.PlayerCount, req.RequestID),
		StorageRecord: ludoBotMatchRequestRecord{
			Mode:        mode,
			Difficulty:  string(difficulty),
			ArenaName:   req.ArenaName,
			PlayerCount: req.PlayerCount,
		},
	}, matchArena, nil
}

func ludoBotModeForOnlineArena(mode arena.ArenaMode, playerCount int) (string, error) {
	switch playerCount {
	case 2:
		if mode != arena.Mode2POnline {
			return "", runtime.NewError("arena is not a 2 player online arena", 3)
		}
		return "ludo_2p", nil
	case 4:
		if mode != arena.Mode4POnline {
			return "", runtime.NewError("arena is not a 4 player online arena", 3)
		}
		return "ludo_4p", nil
	default:
		return "", runtime.NewError("player_count must be 2 or 4", 3)
	}
}

func readLudoBotMatchState(ctx context.Context, nk runtime.NakamaModule, matchID string) (*LudoMatchState, error) {
	payload, err := nk.MatchSignal(ctx, matchID, "state")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(payload) == "" {
		return nil, fmt.Errorf("empty bot match state")
	}
	var state LudoMatchState
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func ludoOnlineBotMatchPlayers(ctx context.Context, nk runtime.NakamaModule, humanUserID string, state *LudoMatchState) ([]LudoOnlineBotMatchPlayer, error) {
	account, err := nk.AccountGetId(ctx, humanUserID)
	if err != nil {
		return nil, err
	}

	players := make([]LudoOnlineBotMatchPlayer, 0, len(state.PlayerOrder))
	for _, playerID := range state.PlayerOrder {
		player := state.Players[playerID]
		if player == nil {
			continue
		}
		players = append(players, ludoOnlineBotMatchPlayer(player, humanUserID, account))
	}
	return players, nil
}

func ludoOnlineBotMatchPlayer(player *LudoPlayer, humanUserID string, account *api.Account) LudoOnlineBotMatchPlayer {
	userID := player.ID
	displayName := player.Name
	avatar := fmt.Sprint(player.AvatarID)

	if !player.IsBot {
		if strings.TrimSpace(player.UserID) != "" {
			userID = player.UserID
		} else {
			userID = humanUserID
		}
		if account != nil && account.User != nil {
			if strings.TrimSpace(account.User.DisplayName) != "" {
				displayName = account.User.DisplayName
			} else if strings.TrimSpace(account.User.Username) != "" {
				displayName = account.User.Username
			}
			if strings.TrimSpace(account.User.AvatarUrl) != "" {
				avatar = account.User.AvatarUrl
			}
		}
	}

	return LudoOnlineBotMatchPlayer{
		UserID:      userID,
		DisplayName: displayName,
		Avatar:      avatar,
		PlayerID:    ludoUnityPlayerIDForSeat(player.Seat),
		Seat:        player.Seat,
		Color:       player.Color,
		IsBot:       player.IsBot,
	}
}
func ludoAuthenticatedUserID(ctx context.Context) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok || strings.TrimSpace(userID) == "" {
		return "", runtime.NewError("authenticated user required", 16)
	}
	return strings.TrimSpace(userID), nil
}

func parseLudoBotMatchCreateRequest(payload string) (LudoBotMatchCreateRequest, error) {
	var req LudoBotMatchCreateRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return LudoBotMatchCreateRequest{}, runtime.NewError("invalid ludo_create_bot_match payload", 3)
	}
	return req, nil
}

func newLudoBotMatchCreateOptions(userID string, req LudoBotMatchCreateRequest) (ludoBotMatchCreateOptions, error) {
	req.Mode = strings.TrimSpace(req.Mode)
	req.Difficulty = strings.TrimSpace(req.Difficulty)
	req.RequestID = strings.TrimSpace(req.RequestID)

	if !isSupportedLudoBotMode(req.Mode) {
		return ludoBotMatchCreateOptions{}, runtime.NewError("unsupported ludo bot mode", 3)
	}
	difficulty := BotDifficulty(req.Difficulty)
	if !isSupportedBotDifficulty(difficulty) {
		return ludoBotMatchCreateOptions{}, runtime.NewError("unsupported bot difficulty", 3)
	}
	if req.RequestID == "" {
		return ludoBotMatchCreateOptions{}, runtime.NewError("request_id is required", 3)
	}

	return ludoBotMatchCreateOptions{
		HumanUserID: userID,
		Mode:        req.Mode,
		Difficulty:  difficulty,
		RequestID:   req.RequestID,
		IncludeBot:  true,
		StorageKey:  ludoBotRequestStorageKey(userID, req.RequestID),
		StorageRecord: ludoBotMatchRequestRecord{
			Mode:       req.Mode,
			Difficulty: req.Difficulty,
		},
	}, nil
}

func createOrGetLudoBotMatch(ctx context.Context, logger runtime.Logger, nk runtime.NakamaModule, options ludoBotMatchCreateOptions) (ludoBotMatchCreateResult, error) {
	if record, found, err := readLudoBotRequestRecord(ctx, nk, options.StorageKey); err != nil {
		return ludoBotMatchCreateResult{}, runtime.NewError("failed to read bot match request", 13)
	} else if found {
		return ludoBotMatchCreateResult{MatchID: record.MatchID, Mode: record.Mode, Difficulty: record.Difficulty}, nil
	}

	matchID, err := createLudoBotMatch(ctx, nk, options)
	if err != nil {
		logger.Error("failed to create ludo bot match for user %s: %v", options.HumanUserID, err)
		return ludoBotMatchCreateResult{}, runtime.NewError("match creation failed", 13)
	}

	record := options.StorageRecord
	record.MatchID = matchID
	if err := writeLudoBotRequestRecord(ctx, nk, options.StorageKey, record); err != nil {
		logger.Error("failed to store ludo bot request %s for user %s: %v", options.RequestID, options.HumanUserID, err)
	}

	return ludoBotMatchCreateResult{MatchID: matchID, Mode: options.Mode, Difficulty: string(options.Difficulty)}, nil
}

func createLudoBotMatch(ctx context.Context, nk runtime.NakamaModule, options ludoBotMatchCreateOptions) (string, error) {
	return nk.MatchCreate(ctx, ludoBotMatchModule, ludoBotMatchCreateParams(options))
}

func ludoBotMatchCreateParams(options ludoBotMatchCreateOptions) map[string]interface{} {
	return map[string]interface{}{
		"mode":           options.Mode,
		"human_user_id":  options.HumanUserID,
		"include_bot":    options.IncludeBot,
		"bot_difficulty": string(options.Difficulty),
		"request_id":     options.RequestID,
	}
}

func newLudoBotMatchCreateResponse(result ludoBotMatchCreateResult) LudoBotMatchCreateResponse {
	return LudoBotMatchCreateResponse{Success: true, MatchID: result.MatchID, HasBot: true, Mode: result.Mode}
}

func serializeLudoBotResponse(resp LudoBotMatchCreateResponse) (string, error) {
	return utils.SerializeObjectToString(&resp)
}

func serializeLudoOnlineBotResponse(resp LudoOnlineBotMatchCreateResponse) (string, error) {
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

func ludoOnlineBotRequestStorageKey(userID string, arenaName string, playerCount int, requestID string) string {
	return ludoOnlineBotMatchRequestStorageKey + sanitizeLudoBotIDPart(userID) + "_" + sanitizeLudoBotIDPart(arenaName) + "_" + fmt.Sprint(playerCount) + "_" + sanitizeLudoBotIDPart(requestID)
}

func ludoUnityPlayerIDForSeat(seat int) int {
	switch seat {
	case 0:
		return 0
	case 1:
		return 2
	case 2:
		return 1
	case 3:
		return 3
	default:
		return seat
	}
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
