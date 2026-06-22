package account

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"game-server/systems/leaderboard"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	//rpcIdChangeDisplayName  = "change_display_name"
	rpcIdUpdateSelfMetadata  = "update_self_metadata"
	rpcIdAccountDelete       = "account_delete"
	rpcIdUserQueryByDeviceID = "user_query_by_device_id"
)

func InitAccountSystem(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	//if err := (*initializer).RegisterRpc(rpcIdChangeDisplayName, changeDisplayName); err != nil {
	//	return err
	//}

	if err := (*initializer).RegisterRpc(rpcIdUpdateSelfMetadata, UpdateSelfMetadata); err != nil {
		return err
	}

	// ------------------ Hooks
	if err := (*initializer).RegisterAfterAuthenticateDevice(func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, out *api.Session, in *api.AuthenticateDeviceRequest) error {

		if err := InitializeNewUser(ctx, logger, db, nk, out, in); err != nil {
			return err
		}

		return nil

	}); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdAccountDelete, DeleteAccount); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdUserQueryByDeviceID, queryUsersByDeviceIDs); err != nil {
		return err
	}

	return nil
}

/*
// changeDisplayName
// payload { "display_name": "newDisplayName" }
func changeDisplayName(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Context -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusNotFound, "Invalid context"), errors.New("Invalid context")
	}

	// ----------------------------- payload -----------------------------

	payloadMap := make(map[string]interface{})

	if err := utils.DeserializeObjectFromStringByRefsToMap(&payload, &payloadMap); err != nil {
		logger.Info("There is an error in DeserializeObjectFromStringByRefsToMap the paylaod to a map , err : ", err)
	}

	newDisplayName, ok := payloadMap["display_name"].(string)
	if ok {

		// search in json
		logger.Info("payload contains ", newDisplayName)
		// TODO convert json to a map string interface{} to able search for a item

	}

	// can change for first time free
	// after that will cost 10000 coins

	// ----------------------------- Read Cutome account Data -----------------------------

	var customAccountData string

	objectId := []*runtime.StorageRead{&runtime.StorageRead{
		Collection: shared_constants.UserDataCollectionName,
		Key:        shared_constants.CustomAccountDataKey,
		UserID:     userID,
	}}

	records, err := nk.StorageRead(ctx, objectId)

	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), nil
	} else {

		if records != nil {
			if len(records) > 0 {
				customAccountData = records[0].Value
			}
		}
	}

	// ----------------------------- Costume account Data -----------------------------

	customAccountDataMap := make(map[string]interface{})

	if err := utils.DeserializeObjectFromStringByRefsToMap(&customAccountData, &customAccountDataMap); err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, "DeserializeObjectFromStringByRefsToMap : "+err.Error()), nil
	}

	isChangeDisplayNameFreeUsed, ok := customAccountDataMap["is_change_display_name_free_used"].(bool)
	if !ok {
		return utils.CreateStatus(false, http.StatusNoContent, "is_change_display_name_free_used is in invalid "), nil
	}

	logger.Info("customAccountData %v ", customAccountData)
	logger.Info("customAccountDataMap %v ", customAccountDataMap)
	logger.Info("isChangeDisplayNameFreeUsed %v ", isChangeDisplayNameFreeUsed)
	// ----------------------------- wallet -----------------------------
	var isPurchaed = false
	if !isChangeDisplayNameFreeUsed {
		logger.Info("changeDisplayName 1")

		isPurchaed = true
	} else {
		changeset := map[string]int64{
			"coins": -10000,
		}
		metadata := map[string]interface{}{
			"sink": "change-profile-name",
		}

		updated, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
		if updated != nil && err == nil {
			isPurchaed = true
		}
	}

	if isPurchaed {
		logger.Info("changeDisplayName 2")

		// ----------------------------- update custom account data -----------------------------

		if !isChangeDisplayNameFreeUsed {
			logger.Info("changeDisplayName 3")

			customAccountDataMap["is_change_display_name_free_used"] = true

			customAccountDataChanged, err := utils.SerializeObjectToString(&customAccountDataMap)
			if err != nil {
				return utils.CreateStatus(false, http.StatusInternalServerError, "SerializeObjectToString : "+err.Error()), nil
			}

			version := ""
			if len(records) > 0 {
				// Use OCC to prevent concurrent writes.
				version = records[0].GetVersion()
			}

			// Update daily reward storage object for user.
			_, err = nk.StorageWrite(ctx, []*runtime.StorageWrite{{
				Collection:      shared_constants.UserDataCollectionName,
				Key:             shared_constants.CustomAccountDataKey,
				PermissionRead:  1,
				PermissionWrite: 0, // No client write.
				Value:           customAccountDataChanged,
				Version:         version,
				UserID:          userID,
			}})
		}

		logger.Info("changeDisplayName 4")

		if err := nk.AccountUpdateId(ctx, userID, "", nil, newDisplayName, "", "", "", ""); err == nil {
			return utils.CreateStatus(true, http.StatusOK), nil
		} else {
			return utils.CreateStatus(false, http.StatusNotModified, "account update failed"), err
		}

	} else {
		// TODO return a message to notify player has not enough money
		return utils.CreateStatus(false, http.StatusInsufficientStorage, "does not have enough money to purchase"), nil

	}
}
*/

// UpdateSelfMetadata
func UpdateSelfMetadata(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Context -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusNotFound, "Invalid context"), errors.New("Invalid context")
	}

	// ----------------------------- payload -----------------------------

	payloadMap := make(map[string]interface{})

	if err := utils.DeserializeObjectFromStringByRefsToMap(&payload, &payloadMap); err != nil {
		logger.Info("There is an error in DeserializeObjectFromStringByRefsToMap the paylaod to a map , err : ", err)
	}

	// TODO update this condition if wanted to set more data from client
	logger.Info("payload len(payloadMap) ", len(payloadMap))

	if len(payloadMap) == 1 {

		if newData, ok := payloadMap["is_voice_chat_enabled"].(bool); ok {

			logger.Info("payload contains ", newData)

			err := utils.UpdateMetaData(&ctx, &nk, &logger, userID, payloadMap, true)
			if err != nil {
				return utils.CreateStatus(false, http.StatusNotModified), err
			}

			return utils.CreateStatus(true, http.StatusOK), nil

		} else {
			return utils.CreateStatus(false, http.StatusForbidden), nil
		}

	} else {
		return utils.CreateStatus(false, http.StatusNoContent), nil
	}

}

func InitializeNewUser(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, out *api.Session, in *api.AuthenticateDeviceRequest) error {
	if out.Created {
		logger.Info("[InitModule]=> InitializeNewUser : runtime.RUNTIME_CTX_USER_ID : ", ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string),
			" runtime.RUNTIME_CTX_USERNAME : ", ctx.Value(runtime.RUNTIME_CTX_USERNAME).(string),
			" RUNTIME_CTX_CLIENT_IP : ", ctx.Value(runtime.RUNTIME_CTX_CLIENT_IP).(string))

		// Only run this logic if the account that has authenticated is new.
		userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
		if !ok {
			return errors.New("invalid context")
		}

		// ----------------------------- wallet -----------------------------
		changeset := map[string]int64{
			"coins": 1000,
		}

		metadata := map[string]interface{}{
			"in-game": "initialize-user",
		}

		updatedMap, previousMap, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
		if err != nil {
			// Handle error.
			logger.Error("[InitModule]=> InitializeNewUser : Wallet Not Updated! , changeset : ", fmt.Sprintf("%#v", changeset), ", err : ", err)
		}

		logger.Info("InitializeNewUser : Wallet Updated! changeset", fmt.Sprintf("%#v", changeset))
		logger.Info("InitializeNewUser : Wallet Updated! updatedMap", fmt.Sprintf("%#v", updatedMap))
		logger.Info("InitializeNewUser : Wallet Updated! previousMap", fmt.Sprintf("%#v", previousMap))

		// ----------------------------- account -----------------------------

		avatarName := "avatar_1"

		metaDataMap := make(map[string]interface{})

		// ludo
		metaDataMap["ludo_total_earned_coins"] = 0
		metaDataMap["ludo_played_match"] = 0
		metaDataMap["ludo_win_match"] = 0

		// TODO quiz ..

		// ----------------------------- username and display name-----------------------------

		//logger.Info("InitializeNewUser : in username", in.Username)
		//logger.Info("InitializeNewUser : ctx username", ctx.Value(runtime.RUNTIME_CTX_USERNAME).(string))
		displayName := ctx.Value(runtime.RUNTIME_CTX_USERNAME).(string)
		displayName = fmt.Sprintf("Guest-%s", displayName[:4])
		account, err := nk.AccountGetId(ctx, userID)
		if err == nil {
			if err := nk.AccountUpdateId(ctx, account.User.Id, in.Username, metaDataMap, displayName, "", "", "", avatarName); err != nil {
				return runtime.NewError("could not update account", 13)
			}
		}

		// ----------------------------- Leaderboard Updates -----------------------------

		grecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsGlobalID, userID, "", 0, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update global leaderboard for user %s: %v", userID, err)
		}
		logger.Info("global leaderboard for user %s: %+v", userID, grecord)

		lrecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsLudoID, userID, "", 0, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update ludo leaderboard for user %s: %v", userID, err)
		}
		logger.Info("ludo leaderboard for user %s: %+v", userID, lrecord)

		qrecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsQuizID, userID, "", 0, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update quiz leaderboard for user %s: %v", userID, err)
		}
		logger.Info("quiz leaderboard for user %s: %+v", userID, qrecord)

		srecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsSolitaireID, userID, "", 0, 0, metadata, nil)
		if err != nil {
			logger.Error("failed to update solitaire leaderboard for user %s: %v", userID, err)
		}
		logger.Info("solitaire leaderboard for user %s: %+v", userID, srecord)

		// ----------------------------- custom account data -----------------------------

		// "daily_reward_claimed_time_unix": %d,
		customAccountData := fmt.Sprintf(`{
			  "is_change_display_name_free_used": false
			}`)

		logger.Info("customAccountData  ", customAccountData)

		newCustomAccountDataObject := []*runtime.StorageWrite{&runtime.StorageWrite{
			Collection:      shared_constants.UserDataCollectionName,
			Key:             shared_constants.CustomAccountDataKey,
			Value:           customAccountData,
			UserID:          userID,
			PermissionRead:  1,
			PermissionWrite: 0},
		}

		write, err := nk.StorageWrite(ctx, newCustomAccountDataObject)
		if err == nil {
			logger.Info("customAccountData StorageWrite ", write)
		}

		// -----------------------------  -----------------------------

	}

	return nil
}

func DeleteAccount(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Context -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusNotFound, "Invalid context"), errors.New("Invalid context")
	}

	if err := nk.AccountDeleteId(ctx, userID, false); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}
