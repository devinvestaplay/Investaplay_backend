package systems

import (
	"context"
	"database/sql"
	"game-server/systems/account"
	ads "game-server/systems/ad"
	"game-server/systems/arena"
	"game-server/systems/ban"
	"game-server/systems/events"
	"game-server/systems/game_config"
	"game-server/systems/leaderboard"
	"game-server/systems/ludo"
	"game-server/systems/metrics"
	"game-server/systems/notfication"
	"game-server/systems/party"
	"game-server/systems/quiz"
	"game-server/systems/ranking"
	"game-server/systems/server_time"
	"game-server/systems/shop"
	"game-server/systems/solitaire"
	"game-server/utils"

	"github.com/heroiclabs/nakama-common/runtime"
)

func InitSystems(ctx *context.Context, logger *runtime.Logger, db *sql.DB, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// ------------------ Session
	if err := account.RegisterSessionEvents(db, nk, initializer); err != nil {
		return err
	}

	// ------------------ Metrics
	if err := metrics.InitMetricsSystem(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ account
	if err := account.InitAccountSystem(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Arena
	if err := arena.InitArenaSystem(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Arena
	if err := ludo.InitLudo(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Game Config
	if err := game_config.InitGameConfig(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Shop Config
	if err := shop.InitShopConfig(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Time
	if err := server_time.InitServerTime(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Leaderboards
	if err := leaderboard.InitLeaderboardSystem(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Ban
	if err := ban.InitBan(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Notification
	if err := notfication.InitNotification(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Ads
	if err := ads.InitAdsSystem(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Test RPC
	(*initializer).RegisterRpc("test", func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		logger.Info("Test Called")

		return utils.CreateStatus(true), nil
	})

	// ------------------ Quiz
	if err := quiz.InitQuiz(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Solitaire
	if err := solitaire.InitSolitaire(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Events
	if err := events.InitEvents(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Party
	if err := party.InitParty(ctx, logger, nk, initializer); err != nil {
		return err
	}

	// ------------------ Ranking / Global Skill
	if err := ranking.InitRanking(ctx, logger, nk, initializer); err != nil {
		return err
	}

	return nil
}
