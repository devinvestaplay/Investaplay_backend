package solitaire

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"game-server/systems/currency"
	"game-server/systems/leaderboard"
	"game-server/systems/wallet"
	"game-server/utils"
	"math"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	SolitaireGameConfigJSONFilePath = "../configs/solitaire-game-config.json"
	rpcIdResetupSolitaireGameConfig = "solitaire_game_config_resetup"
	rpcIdGetSolitaireGameConfig     = "solitaire_game_config_get"

	rpcIdSolitaireHint     = "solitaire_game_hint"
	rpcIdSolitaireUndo     = "solitaire_game_undo"
	rpcIdSolitaireAutoMove = "solitaire_game_auto_move"

	rpcIdSolitaireFinish   = "solitaire_game_finish"
	rpcIdSolitaireGetSkill = "solitaire_get_skill"

	solitaireScoreHistoryCollection = "ScoreHistory"
	solitaireScoreHistoryKey        = "solitaire"
	solitaireTopK                   = 30
	solitaireMinGames               = 5
	solitaireTargetGames            = 30

	SolitaireCollectionName = "Solitaire" // parent of all categories
	SolitaireGameConfigKey  = "solitaire_game_config"
)

// solitaire config
var solitaireGameConfigJson string
var solitaireGameConfig SolitaireGameConfig

func InitSolitaire(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// Solitaire Game Config data
	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, SolitaireCollectionName, SolitaireGameConfigKey, SolitaireGameConfigJSONFilePath); err != nil {
		return err
	} else {
		solitaireGameConfigJson = data
		err := processSolitaireGameConfigJSON(solitaireGameConfigJson)
		if err != nil {
			return err
		}
		(*logger).Info("solitaireGameConfigJson : ", solitaireGameConfigJson)
	}

	if err := (*initializer).RegisterRpc(rpcIdResetupSolitaireGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		solitaireGameConfigJson = payload
		err := processSolitaireGameConfigJSON(solitaireGameConfigJson)
		if err != nil {
			return "", err
		}

		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, SolitaireCollectionName, SolitaireGameConfigKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdGetSolitaireGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return solitaireGameConfigJson, nil
	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdSolitaireHint, hint); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdSolitaireUndo, undo); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdSolitaireAutoMove, autoMove); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdSolitaireFinish, gameFinished); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdSolitaireGetSkill, solitaireGetSkill); err != nil {
		return err
	}

	return nil
}

func hint(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, solitaireGameConfig.LifelineCosts.Hint, "hint")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func undo(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, solitaireGameConfig.LifelineCosts.Undo, "undo")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func autoMove(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, solitaireGameConfig.LifelineCosts.AutoMove, "auto_move")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func chargeLifelineCost(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger, userID string, cost int, lifelineName string) (string, error) {

	acc, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("account get error: %w", err)
	}

	walletData, err := wallet.DeserializeWalletData(&acc.Wallet)
	if err != nil {
		return "", fmt.Errorf("wallet parse error: %w", err)
	}

	if walletData.Coins < cost {
		return "", errors.New("insufficient balance")
	}

	changeset := map[string]int64{"coins": int64(-cost)}

	metadata := map[string]interface{}{
		"cost":     cost,
		"lifeline": lifelineName,
	}

	updatedWallet, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)

	updatedWalletJson, err := utils.SerializeObjectToString(&updatedWallet)

	return updatedWalletJson, err
}

func gameFinished(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("solitaire gameFinished userID: %s", userID)

	// ----------------------------- Payload -----------------------------

	var finishData SolitaireFinishGameData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &finishData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Wallet -----------------------------

	changeset := map[string]int64{"coins": int64(finishData.Coins)}

	metadata := map[string]interface{}{
		"coins":  finishData.Coins,
		"points": finishData.Points,
	}

	_, _, err = nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		logger.Error("failed to update wallet for user %s: %v", userID, err)
	}

	// ----------------------------- Leaderboard Updates -----------------------------

	grecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsGlobalID, userID, "", int64(finishData.Coins), int64(finishData.Points), metadata, nil)
	if err != nil {
		logger.Error("failed to update global leaderboard for user %s: %v", userID, err)
	}
	logger.Info("global leaderboard for user %s: %+v", userID, grecord)

	lrecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsSolitaireID, userID, "", int64(finishData.Coins), int64(finishData.Points), metadata, nil)
	if err != nil {
		logger.Error("failed to update solitaire leaderboard for user %s: %v", userID, err)
	}
	logger.Info("solitaire leaderboard for user %s: %+v", userID, lrecord)

	// ----------------------------- Score History for Skill -----------------------------

	hintCost := solitaireGameConfig.LifelineCosts.Hint
	undoCost := solitaireGameConfig.LifelineCosts.Undo
	adjustedScore := float64(finishData.Points) + float64(finishData.TimeBonus) -
		float64(finishData.HintsUsed*hintCost) - float64(finishData.UndoUsed*undoCost)

	scores, _ := readSolitaireScoreHistory(ctx, nk, userID)
	scores = append(scores, adjustedScore)
	scores = utils.KeepTopNScores(scores, solitaireTopK)
	if err := writeSolitaireScoreHistory(ctx, nk, userID, scores); err != nil {
		logger.Error("failed to write solitaire score history for user %s: %v", userID, err)
	}

	// ----------------------------- Stats Update -----------------------------

	metaDataMap := make(map[string]any)
	metaDataMap["solitaire_total_earned_coins"] = finishData.Coins
	metaDataMap["solitaire_played_match"] = 1
	if finishData.IsWinner {
		metaDataMap["solitaire_win_match"] = 1
	}

	if err := utils.UpdateMetaData(&ctx, &nk, &logger, userID, metaDataMap, false); err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	// ----------------------------- Response -----------------------------

	return utils.CreateStatus(true, http.StatusOK, "match finished and rewards distributed"), nil
}

// -----------------------------------------------------------------------
// -----------------------------------------------------------------------

func processSolitaireGameConfigJSON(jsonData string) error {
	var data SolitaireGameConfig
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		solitaireGameConfig = data
	}
	return nil
}

type SolitaireGameConfig struct {
	RewardConfig                  SolitaireRewardConfig    `json:"reward_config"`
	LifelineCosts                 SolitaireLifelineCosts   `json:"lifeline_costs"`
	DrawCount                     int                      `json:"draw_count"`                         // default 1-card draw
	ChallengeModeTimeMinutes      int                      `json:"challenge_mode_time_minutes"`        // default 10
	WinnerReward                  currency.VirtualCurrency `json:"winner_reward"`                      // default: SolitaireCoin, 100
	TimeBonusCoinsThresholdSecond int                      `json:"time_bonus_coins_threshold_seconds"` // default 300 (5 min)
	TimeBonusCoins                int                      `json:"time_bonus_coins"`                   // default 75
}

type SolitaireRewardConfig struct {
	MoveStockToTableau          int `json:"move_stock_to_tableau"`               // default 5
	MoveStockToFoundation       int `json:"move_stock_to_foundation"`            // default 10
	MoveTableauToFoundation     int `json:"move_tableau_to_foundation"`          // default 10
	MoveFoundationToTableau     int `json:"move_foundation_to_tableau"`          // default -10
	FlipTableauCard             int `json:"flip_tableau_card"`                   // default 5
	RecycleStock                int `json:"recycle_stock"`                       // default 0 (unset)
	CompleteFoundationPile      int `json:"complete_foundation_pile"`            // default 50
	WinningGame                 int `json:"winning_game"`                        // default 200
	TimeBonusPointsThresholdSec int `json:"time_bonus_points_threshold_seconds"` // default 300
	TimeBonusPoints             int `json:"time_bonus_points"`                   // default 100
}

type SolitaireLifelineCosts struct {
	Hint     int `json:"hint"`      // default 5
	Undo     int `json:"undo"`      // default 8
	AutoMove int `json:"auto_move"` // default 15
}

// -----------------------------------------------------------------------
// -----------------------------------------------------------------------

type SolitaireFinishGameData struct {
	IsWinner  bool `json:"is_winner"`
	Coins     int  `json:"coins"`
	Points    int  `json:"points"`
	TimeBonus int  `json:"time_bonus"`
	HintsUsed int  `json:"hints_used"`
	UndoUsed  int  `json:"undo_used"`
}

type SolitaireScoreHistory struct {
	Scores []float64 `json:"scores"`
}

type SolitaireSkillResponse struct {
	IsEligible   bool    `json:"is_eligible"`
	Skill        float64 `json:"skill"`
	Percentile   float64 `json:"percentile"`
	Confidence   float64 `json:"confidence"`
	GamesPlayed  int     `json:"games_played"`
	AverageScore float64 `json:"average_score"`
	Rank         int64   `json:"rank"`
}

// ----------------------------- Score History Helpers -----------------------------

func readSolitaireScoreHistory(ctx context.Context, nk runtime.NakamaModule, userID string) ([]float64, error) {
	raw, err := utils.ReadUserStorageObject(ctx, nk, userID, solitaireScoreHistoryCollection, solitaireScoreHistoryKey)
	if err != nil || raw == "" {
		return []float64{}, nil
	}
	var h SolitaireScoreHistory
	if err := utils.DeserializeObjectFromStringByRefs(&raw, &h); err != nil {
		return []float64{}, nil
	}
	return h.Scores, nil
}

func writeSolitaireScoreHistory(ctx context.Context, nk runtime.NakamaModule, userID string, scores []float64) error {
	h := SolitaireScoreHistory{Scores: scores}
	raw, err := utils.SerializeObjectToString(&h)
	if err != nil {
		return err
	}
	return utils.WriteUserStorageObject(ctx, nk, userID, solitaireScoreHistoryCollection, solitaireScoreHistoryKey, raw)
}

// ----------------------------- Skill RPC -----------------------------

func solitaireGetSkill(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	skillScore, resp, err := ComputeSolitaireSkillForUser(ctx, db, nk, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	_ = skillScore

	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

// ComputeSolitaireSkillForUser is exported so the global ranking RPC can call it.
// Formula: Skill = Percentile × Confidence
func ComputeSolitaireSkillForUser(ctx context.Context, db *sql.DB, nk runtime.NakamaModule, userID string) (float64, SolitaireSkillResponse, error) {
	scores, err := readSolitaireScoreHistory(ctx, nk, userID)
	if err != nil {
		return 0, SolitaireSkillResponse{}, err
	}

	n := len(scores)
	resp := SolitaireSkillResponse{GamesPlayed: n}

	if n < solitaireMinGames {
		resp.IsEligible = false
		return 0, resp, nil
	}

	avg := utils.AverageFloat64(scores)
	confidence := math.Min(1, math.Sqrt(float64(n)/float64(solitaireTargetGames)))

	// Write avg score to leaderboard to establish ranking position
	leaderboardScore := int64(avg * 1000)
	record, lerr := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardSkillSolitaireID, userID, "", leaderboardScore, 0, nil, nil)
	if lerr != nil {
		return 0, resp, lerr
	}

	// Percentile = (L + 0.5 × E) / N × 100
	percentile := utils.ComputePercentile(ctx, db, leaderboard.LeaderboardSkillSolitaireID, leaderboardScore)

	skill := percentile * confidence

	resp.IsEligible = true
	resp.AverageScore = avg
	resp.Percentile = percentile
	resp.Confidence = confidence
	resp.Skill = skill
	resp.Rank = record.Rank
	return skill, resp, nil
}
