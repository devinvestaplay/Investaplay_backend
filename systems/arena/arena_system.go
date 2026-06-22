package arena

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"game-server/systems/currency"
	"game-server/systems/shared_constants"
	"game-server/utils"

	"github.com/heroiclabs/nakama-common/runtime"
)

// -------------- LUDO --------------
const (
	rpcIdResetupLudoArena = "ludo_arena_resetup"
	rpcIdGetLudoArena     = "ludo_arena_get"

	LudoArenaJSONFilePath = "../configs/ludo-arena.json"
	LudoArenaKey          = "arena"
)

var LudoArena LudoArenaDatas
var ludoArenaDataConfigJson string

// TODO add others

// TODO use in all match state and handlers

func InitArenaSystem(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, shared_constants.LudoCollectionName, LudoArenaKey, LudoArenaJSONFilePath); err != nil {
		return err
	} else {
		ludoArenaDataConfigJson = data
		err := processLudoArenaJSON(ludoArenaDataConfigJson)
		if err != nil {
			return err
		}

		(*logger).Info("ludoArenaDataConfigJson : ", ludoArenaDataConfigJson)
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdResetupLudoArena, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		ludoArenaDataConfigJson = payload
		err := processLudoArenaJSON(ludoArenaDataConfigJson)
		if err != nil {
			return "", err
		}

		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, shared_constants.LudoCollectionName, LudoArenaKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetLudoArena, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return ludoArenaDataConfigJson, nil
	}); err != nil {
		return err
	}

	return nil

}

func processLudoArenaJSON(jsonData string) error {
	var data LudoArenaDatas
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		LudoArena = data
	}
	return nil
}

// Arena game modes for Ludo
type ArenaMode string

const (
	Mode2POnline      ArenaMode = "2P_ONLINE"       // random 2 players
	Mode2PWithFriends ArenaMode = "2P_WITH_FRIENDS" // private 2 players
	Mode4POnline      ArenaMode = "4P_ONLINE"       // random 4 players
	Mode4PWithFriends ArenaMode = "4P_WITH_FRIENDS" // private 4 players
)

// Player rank in arena (max 4 players supported)
type Rank int

const (
	RankWinner Rank = 1 // 1st place
	RankSecond Rank = 2 // 2nd place
	RankThird  Rank = 3 // 3rd place
	RankFourth Rank = 4 // 4th place
	RankLooser Rank = 5 // looser place
)

type LudoArenaDatas struct {
	Arenas map[string]LudoArenaItemData `json:"arenas"` // key is name
}

type LudoArenaItemData struct {
	Name            string                              `json:"name"`
	Mode            ArenaMode                           `json:"mode"`        // arena type (2p/4p online/friends)
	FeeCurrencyData currency.VirtualCurrency            `json:"fee_cost"`    // entry fee
	Rewards         map[Rank][]currency.VirtualCurrency `json:"rewards"`     // rewards by rank
	JsonConfig      json.RawMessage                     `json:"json_config"` // Ludo-specific configs
	Enabled         bool                                `json:"enabled"`
}
