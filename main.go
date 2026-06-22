package main

import (
	"context"
	"database/sql"
	"game-server/systems"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

// noinspection GoUnusedExportedFunction
func InitModule(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, initializer runtime.Initializer) error {
	initStart := time.Now()

	if err := systems.InitSystems(&ctx, &logger, db, &nk, &initializer); err != nil {
		logger.Error("[InitModule]=> unable to systems.InitSystems : %v", err)
		return err
	}

	logger.Info("[InitModule]=> Plugin loaded in '%d' msec.", time.Now().Sub(initStart).Milliseconds())
	return nil
}
