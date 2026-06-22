package ludo

import (
	"context"
	"database/sql"
	"errors"
	"game-server/systems/arena"
	"game-server/systems/leaderboard"
	"game-server/systems/wallet"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

// TODO add start/finish match rpc for wallet (fee and reward)

const (
	rpcIdLudoMatchCheckBalance = "ludo_match_check_balance"
	rpcIdLudoMatchStart        = "ludo_match_start"
	rpcIdLudoMatchFinish       = "ludo_match_finish"
)

func InitLudo(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(rpcIdLudoMatchCheckBalance, ludoMatchCheckBalance); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoMatchStart, ludoMatchStart); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdLudoMatchFinish, ludoMatchFinish); err != nil {
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

func ludoMatchStart(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	// ----------------------------- Payload -----------------------------

	var ludoStart LudoMatchStartData

	err := utils.DeserializeObjectFromStringByRefs(&payload, &ludoStart)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Arena -----------------------------

	matchArena := arena.LudoArena.Arenas[ludoStart.ArenaName]

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

	// ----------------------------- Fee Deduction -----------------------------

	if walletData.Coins < feeAmount {
		err := errors.New("insufficient balance")
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), err
	}

	// ----------------------------- Changeset -----------------------------

	changeset := map[string]int64{"coins": int64(-feeAmount)}

	metadata := map[string]interface{}{
		"arena":       ludoStart.ArenaName,
		"fee":         feeAmount,
		"description": "Ludo match entry fee deduction",
	}

	// ----------------------------- Wallet Update -----------------------------

	updated, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		return utils.CreateStatus(false, http.StatusConflict, err.Error()), err
	}

	logger.Info("Wallet updated for user %s: %+v", userID, updated)

	// ----------------------------- Response -----------------------------

	return utils.CreateStatus(true, http.StatusOK, "fee deducted successfully"), nil
}

func ludoMatchFinish(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	hostID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("ludoMatchFinish HostID: %s", hostID)

	// ----------------------------- Payload -----------------------------

	var finishData LudoMatchFinishData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &finishData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Arena -----------------------------

	matchArena, ok := arena.LudoArena.Arenas[finishData.ArenaName]
	if !ok {
		err := errors.New("arena not found")
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	// ----------------------------- Rewards Per Rank -----------------------------

	for userID, rank := range finishData.Ranking {
		rewardCurrencies, ok := matchArena.Rewards[rank]

		if !ok || len(rewardCurrencies) == 0 {
			metaDataMap := make(map[string]any)
			metaDataMap["ludo_played_match"] = 1
			if rank == arena.RankWinner {
				metaDataMap["ludo_win_match"] = 1
			}
			if err := utils.UpdateMetaData(&ctx, &nk, &logger, userID, metaDataMap, false); err != nil {
				return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
			}

			continue // no rewards for this rank
		}

		// Build wallet changeset + calculate score
		changeset := make(map[string]int64)
		var totalEarned int64 = 0

		for _, vc := range rewardCurrencies {
			changeset["coins"] += int64(vc.Amount)
			totalEarned += int64(vc.Amount)
		}

		metadata := map[string]interface{}{
			"arena":   finishData.ArenaName,
			"rank":    rank,
			"rewards": rewardCurrencies,
		}

		_, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
		if err != nil {
			logger.Error("failed to update wallet for user %s: %v", userID, err)
			continue
		}

		// ----------------------------- Leaderboard Updates -----------------------------

		grecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsGlobalID, userID, "", totalEarned, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update global leaderboard for user %s: %v", userID, err)
		}
		logger.Info("global leaderboard for user %s: %+v", userID, grecord)

		lrecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsLudoID, userID, "", totalEarned, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update ludo leaderboard for user %s: %v", userID, err)
		}
		logger.Info("ludo leaderboard for user %s: %+v", userID, lrecord)

		// ----------------------------- Stats Update -----------------------------

		metaDataMap := make(map[string]any)
		metaDataMap["ludo_total_earned_coins"] = totalEarned
		metaDataMap["ludo_played_match"] = 1
		if rank == 1 {
			metaDataMap["ludo_win_match"] = 1
		}

		if err := utils.UpdateMetaData(&ctx, &nk, &logger, userID, metaDataMap, false); err != nil {
			return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
		}

	}

	// ----------------------------- Response -----------------------------

	return utils.CreateStatus(true, http.StatusOK, "match finished and rewards distributed"), nil
}

type LudoMatchStartData struct {
	ArenaName string `json:"arena_name"`
}

type LudoMatchFinishData struct {
	ArenaName string                `json:"arena_name"`
	Ranking   map[string]arena.Rank `json:"ranking"` // key : userID
}
