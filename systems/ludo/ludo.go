package ludo

import (
	"context"
	"database/sql"
	"errors"
	"game-server/systems/arena"
	"game-server/systems/leaderboard"
	"game-server/systems/wallet"
	"game-server/utils"
	"math"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

// TODO add start/finish match rpc for wallet (fee and reward)

const (
	rpcIdLudoMatchCheckBalance    = "ludo_match_check_balance"
	rpcIdLudoMatchStart           = "ludo_match_start"
	rpcIdLudoMatchFinish          = "ludo_match_finish"
	rpcIdLudoGetSkill             = "ludo_get_skill"
	rpcIdLudoRoomCreate           = "ludo_room_create"
	rpcIdLudoRoomJoin             = "ludo_room_join"
	rpcIdLudoRoomGet              = "ludo_room_get"
	rpcIdLudoRoomLeave            = "ludo_room_leave"
	rpcIdLudoCreateBotMatch       = "ludo_create_bot_match"
	rpcIdLudoOnlineBotMatchCreate = "ludo_online_bot_match_create"

	ludoMinMatches    = 5
	ludoTargetMatches = 30
	ludoPriorWins     = 3.0
	ludoPriorLosses   = 3.0
	ludoAlpha         = 0.1
)

func InitLudo(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {
	if err := registerAuthoritativeLudo(*initializer); err != nil {
		return err
	}

	if err := (*initializer).RegisterMatch(ludoCustomRoomMatchModule, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
		return &LudoCustomRoomMatch{}, nil
	}); err != nil {
		return err
	}

	if err := (*initializer).RegisterMatch(ludoBotMatchModule, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule) (runtime.Match, error) {
		return &LudoBotMatch{}, nil
	}); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdLudoMatchCheckBalance, ludoMatchCheckBalance); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoMatchStart, ludoMatchStart); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoMatchFinish, ludoMatchFinish); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoGetSkill, ludoGetSkill); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoRoomCreate, ludoRoomCreate); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoRoomJoin, ludoRoomJoin); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoRoomGet, ludoRoomGet); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoRoomLeave, ludoRoomLeave); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdLudoCreateBotMatch, ludoCreateBotMatch); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoOnlineBotMatchCreate, ludoOnlineBotMatchCreate); err != nil {
		return err
	}

	return nil
}

func ludoMatchCheckBalance(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	// ----------------------------- Payload -----------------------------

	var ludoCheck LudoMatchStartData

	err := utils.DeserializeObjectFromStringByRefs(&payload, &ludoCheck)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Arena -----------------------------

	matchArena, ok := arena.LudoArena.Arenas[ludoCheck.ArenaName]
	if !ok {
		err := errors.New("arena not found")
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}
	feeAmount := matchArena.FeeCurrencyData.Amount

	// ----------------------------- Wallet -----------------------------

	acc, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	walletData, err := wallet.DeserializeWalletData(&acc.Wallet)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	if walletData.Coins < feeAmount {
		return utils.CreateStatus(false, http.StatusPaymentRequired, "not enough balance"), nil
	}

	return utils.CreateStatus(true, http.StatusOK, "enough balance"), nil
}

type LudoMatchStartData struct {
	ArenaName string `json:"arena_name"`
	RoomCode  string `json:"room_code,omitempty"`
	MatchID   string `json:"match_id,omitempty"`
}

type LudoPlayerResult struct {
	Rank   arena.Rank `json:"rank"`
	Kills  int        `json:"kills"`
	Deaths int        `json:"deaths"`
}

type LudoMatchFinishData struct {
	ArenaName string                      `json:"arena_name"`
	Ranking   map[string]LudoPlayerResult `json:"ranking"` // key: userID
}

type LudoSkillResponse struct {
	IsEligible  bool    `json:"is_eligible"`
	Skill       float64 `json:"skill"`
	GamesPlayed int     `json:"games_played"`
	Wins        int     `json:"wins"`
	Kills       int     `json:"kills"`
	Deaths      int     `json:"deaths"`
	Rank        int64   `json:"rank"`
}

// ----------------------------- Skill RPC -----------------------------

func ludoGetSkill(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	skillScore, resp, err := ComputeLudoSkillForUser(ctx, nk, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	if resp.IsEligible {
		record, lerr := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardSkillLudoID, userID, "", int64(skillScore*1_000_000), 0, nil, nil)
		if lerr != nil {
			logger.Error("failed to write ludo skill leaderboard for user %s: %v", userID, lerr)
		} else {
			resp.Rank = record.Rank
		}
	}

	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

// ComputeLudoSkillForUser is exported so the global ranking RPC can call it.
func ComputeLudoSkillForUser(ctx context.Context, nk runtime.NakamaModule, userID string) (float64, LudoSkillResponse, error) {
	acc, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return 0, LudoSkillResponse{}, err
	}

	var meta map[string]interface{}
	if err := utils.DeserializeObjectFromStringByRefsToMap(&acc.User.Metadata, &meta); err != nil {
		return 0, LudoSkillResponse{}, err
	}

	N := int(getMetaFloat(meta, "ludo_played_match"))
	W := int(getMetaFloat(meta, "ludo_win_match"))
	K := int(getMetaFloat(meta, "ludo_kills"))
	D := int(getMetaFloat(meta, "ludo_deaths"))

	resp := LudoSkillResponse{
		GamesPlayed: N,
		Wins:        W,
		Kills:       K,
		Deaths:      D,
	}

	if N < ludoMinMatches {
		resp.IsEligible = false
		return 0, resp, nil
	}

	skill := computeLudoSkill(N, W, K, D)
	resp.IsEligible = true
	resp.Skill = skill
	return skill, resp, nil
}

func computeLudoSkill(N, W, K, D int) float64 {
	bwr := (float64(W) + ludoPriorWins) / (float64(N) + ludoPriorWins + ludoPriorLosses)
	combatRaw := float64(K-D) / float64(N)
	combatScore := math.Max(-1, math.Min(1, combatRaw))
	combatMult := 1 + (ludoAlpha * combatScore)
	confidence := math.Min(1, math.Sqrt(float64(N)/ludoTargetMatches))
	return bwr * combatMult * confidence
}

func getMetaFloat(meta map[string]interface{}, key string) float64 {
	if v, ok := meta[key]; ok {
		if f, ok := v.(float64); ok {
			return f
		}
	}
	return 0
}
