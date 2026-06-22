package leaderboard

import (
	"context"
	"database/sql"
	"fmt"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	LeaderboardTotalEarnedCoinsGlobalID    = "total_earned_coins_global"
	LeaderboardTotalEarnedCoinsLudoID      = "total_earned_coins_ludo"
	LeaderboardTotalEarnedCoinsQuizID      = "total_earned_coins_quiz"
	LeaderboardTotalEarnedCoinsSolitaireID = "total_earned_coins_solitaire"
)

const (
	rpcIdLeaderboardWriteRecord = "leaderboard_write_record"
	rpcIdLeaderboardReset       = "leaderboard_reset" // will delete and create
)

func InitLeaderboardSystem(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// global
	//globalRestTime := "0 0 1 1,7 *"
	globalRestTime := ""
	if err := (*nk).LeaderboardCreate(*ctx, LeaderboardTotalEarnedCoinsGlobalID, true, "desc", "incr", globalRestTime, nil, true); err != nil {
		(*logger).Info("Leaderboard Create LeaderboardTotalEarnedCoinsGlobalID Failed : " + err.Error())
		//return err
	}

	// ludo
	//ludoRestTime := "0 0 1 1,7 *"
	ludoRestTime := ""
	if err := (*nk).LeaderboardCreate(*ctx, LeaderboardTotalEarnedCoinsLudoID, true, "desc", "incr", ludoRestTime, nil, true); err != nil {
		(*logger).Info("Leaderboard Create LeaderboardTotalEarnedCoinsLudoID Failed : " + err.Error())
		//return err
	}

	// quiz
	//quizRestTime := "0 0 1 1,7 *"
	quizRestTime := ""
	if err := (*nk).LeaderboardCreate(*ctx, LeaderboardTotalEarnedCoinsQuizID, true, "desc", "incr", quizRestTime, nil, true); err != nil {
		(*logger).Info("Leaderboard Create LeaderboardTotalEarnedCoinsQuizID Failed : " + err.Error())
		//return err
	}

	// solitaire
	//solitaireRestTime := "0 0 1 1,7 *"
	solitaireRestTime := ""
	if err := (*nk).LeaderboardCreate(*ctx, LeaderboardTotalEarnedCoinsSolitaireID, true, "desc", "incr", solitaireRestTime, nil, true); err != nil {
		(*logger).Info("Leaderboard Create LeaderboardTotalEarnedCoinsSolitaireID Failed : " + err.Error())
		//return err
	}

	// -------------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterAfterListLeaderboardRecords(AfterListLeaderboardRecords); err != nil {
		return err
	}

	// -------------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdLeaderboardWriteRecord, leaderBoardWriteRecord); err != nil {
		return err
	}

	// -------------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdLeaderboardReset, resetLeaderboard); err != nil {
		return err
	}
	return nil
}

// -------------------------------------------------------------------------------------------------------

func AfterListLeaderboardRecords(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, out *api.LeaderboardRecordList, in *api.ListLeaderboardRecordsRequest) error {

	if err := AddAccountMetadataToRecords(ctx, nk, out.Records); err != nil {
		return err
	}
	if err := AddAccountMetadataToRecords(ctx, nk, out.OwnerRecords); err != nil {
		return err
	}

	return nil
}

func AddAccountMetadataToRecords(ctx context.Context, nk runtime.NakamaModule, records []*api.LeaderboardRecord) error {

	for _, record := range records {

		recordOwnerId := record.OwnerId

		var userMetaData map[string]interface{}

		account, err := nk.AccountGetId(ctx, recordOwnerId)
		if err != nil {
			return err
		}

		err = utils.DeserializeObjectFromStringByRefsToMap(&account.User.Metadata, &userMetaData)
		if err != nil {
			return err
		}

		recordMetadata := make(map[string]interface{})

		recordMetadata["display_name"] = account.User.DisplayName

		recordMetadata["avatar"] = account.User.AvatarUrl

		if data, isOK := userMetaData["frame"]; isOK {
			recordMetadata["frame"] = data
		}

		recordMetadataJson, err := utils.SerializeObjectToString(&recordMetadata)
		if err != nil {
			return err
		}

		record.Metadata = recordMetadataJson
	}
	return nil
}

// -------------------------------------------------------------------------------------------------------
// payload must be ManualWrite json
func leaderBoardWriteRecord(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	type ManualWrite struct {
		LeaderboardId string `json:"leaderboard_id"`
		UserID        string `json:"userID"`
		Score         int    `json:"score"`
		Metadata      string `json:"metadata"`
	}

	var manualWrite ManualWrite

	if err := utils.DeserializeObjectFromStringByRefs(&payload, &manualWrite); err != nil {
		return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err
	}

	metaDataMap := map[string]interface{}{}
	if err := utils.DeserializeObjectFromStringByRefsToMap(&manualWrite.Metadata, &metaDataMap); err != nil {
		return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err
	}

	_, err := nk.LeaderboardRecordWrite(ctx, manualWrite.LeaderboardId, manualWrite.UserID, "", int64(manualWrite.Score), 0, metaDataMap, nil)
	if err != nil {
		return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

	} else {
		return fmt.Sprintf(`{"succeeded": %t}`, true), nil
	}
}

// -------------------------------------------------------------------------------------------------------
func resetLeaderboard(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	type ResetLeaderboard struct {
		LeaderboardId string `json:"leaderboard_id"`
	}

	var resetLeaderboard ResetLeaderboard

	if err := utils.DeserializeObjectFromStringByRefs(&payload, &resetLeaderboard); err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	// Step 1: Delete the existing leaderboard
	err := nk.LeaderboardDelete(ctx, resetLeaderboard.LeaderboardId)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotAcceptable, err.Error()), err
	}

	// Step 2: Recreate it with the same parameters
	err = nk.LeaderboardCreate(ctx, resetLeaderboard.LeaderboardId, true, "desc", "incr", "", nil, true)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotModified, err.Error()), err
	}

	logger.Info("Resetting leaderboard: %s", resetLeaderboard.LeaderboardId)

	return utils.CreateStatus(true, http.StatusOK), nil
}
