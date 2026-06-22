package game_config

import (
	"context"
	"database/sql"
	"fmt"
	"game-server/systems/shared_constants"
	"game-server/utils"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdResetupGameConfig = "game_config_resetup"
	rpcIdGetGameConfig     = "game_config_get"

	BaseGameConfigJSONFilePath = "../configs/base-game-config.json"
	GameConfigKey              = "game_config"
)

var gameConfigJson string

func InitGameConfig(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// Load Game Config data
	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, shared_constants.ContainerCollectionName, GameConfigKey, BaseGameConfigJSONFilePath); err != nil {
		return err
	} else {
		gameConfigJson = data
		(*logger).Info("gameConfigJson : ", gameConfigJson)
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdResetupGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		// TODO add some user id as valid
		// TODO force to work only with system account
		gameConfigJson = payload

		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, shared_constants.ContainerCollectionName, GameConfigKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return gameConfigJson, nil
	}); err != nil {
		return err
	}

	return nil

}
