package ban

import (
	"context"
	"database/sql"
	"errors"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"net/http"
	"time"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdBanUser   = "ban_user"
	rpcIdUnBanUser = "un_ban_user"
)

// const notificationCodeDeveloperWarning = 1003
const notificationCodeAccountBanned = 1004

func InitBan(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdBanUser, banUser); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdUnBanUser, unBanUser); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	//if err := (*initializer).RegisterBeforeAuthenticateDevice(onBeforeAuthenticateDevice); err != nil {
	//	return err
	//}

	return nil

}

// payload sample
// { "usernames": [ "" ], "ban_days": 1  }
// { "user_ids": [ "" ], "ban_days": 1  }
// { "usernames": [ "" ], "user_ids": [ "" ], "ban_days": 1  }
func banUser(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok && userID != shared_constants.ServerSystemUserId {
		err := errors.New("only system user is allowed, you are not authorized")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	// ----------------------------- payload -----------------------------
	var banUserData BanUserData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &banUserData); err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	var users []*api.User

	if banUserData.Usernames != nil && len(banUserData.Usernames) > 0 {
		_users, err := nk.UsersGetUsername(ctx, banUserData.Usernames)
		if err != nil {
			return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
		}

		users = append(users, _users...)
	}

	if banUserData.UserIDs != nil && len(banUserData.UserIDs) > 0 {
		_users, err := nk.UsersGetId(ctx, banUserData.UserIDs, nil)
		if err != nil {
			return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
		}

		users = append(users, _users...)
	}

	if len(users) < 1 {
		return utils.CreateStatus(false, http.StatusNotModified, "there are not any users to ban"), nil
	}

	// ----------------------------- save unban time -----------------------------

	now := time.Now()
	unbanTime := now.Add(time.Duration(banUserData.BanDays) * 24 * time.Hour).Unix()

	var newUpdateMetadata = map[string]any{
		"unban_time": unbanTime,
	}

	userIDs := make([]string, 0)

	for _, user := range users {

		err := utils.UpdateMetaData(&ctx, &nk, &logger, user.Id, newUpdateMetadata, true)
		if err != nil {
			continue
		}

		userIDs = append(userIDs, user.Id)
	}

	// ----------------------------- Ban -----------------------------

	// TODO send ban notif
	// Prepare notifications for each user
	notifications := make([]*runtime.NotificationSend, 0, len(users))
	for _, user := range users {
		notifications = append(notifications, &runtime.NotificationSend{
			UserID:     user.GetId(),
			Subject:    "account banned",
			Content:    newUpdateMetadata,
			Code:       notificationCodeAccountBanned,
			Sender:     shared_constants.ServerSystemUserId,
			Persistent: false,
		})
	}

	// Send all notifications
	if err := nk.NotificationsSend(ctx, notifications); err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

// payload sample
// { "username": "" }
// { "user_id": "" }
// { "username": "", "user_id": "" }
func unBanUser(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok && userID != shared_constants.ServerSystemUserId {
		err := errors.New("only system user is allowed, you are not authorized")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	// ----------------------------- payload -----------------------------
	var unBanUserData UnBanUserData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &unBanUserData); err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	var users []*api.User

	if unBanUserData.Username != "" {
		_users, err := nk.UsersGetUsername(ctx, []string{unBanUserData.Username})
		if err != nil {
			return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
		}

		users = append(users, _users...)
	}

	if unBanUserData.UserId != "" {
		_users, err := nk.UsersGetId(ctx, []string{unBanUserData.UserId}, nil)
		if err != nil {
			return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
		}

		users = append(users, _users...)
	}

	if len(users) < 1 {
		return utils.CreateStatus(false, http.StatusNotModified, "there are not any users to ban"), nil
	}

	var newUpdateMetadata = map[string]any{
		"unban_time": 0,
	}

	userIDs := make([]string, 0)
	for _, user := range users {

		err := utils.UpdateMetaData(&ctx, &nk, &logger, user.Id, newUpdateMetadata, true)
		if err != nil {
			continue
		}

		userIDs = append(userIDs, user.Id)
	}

	// ----------------------------- UnBan -----------------------------

	// will log the user out to refresh and re auth
	for _, userId := range userIDs {
		if err := nk.SessionLogout(userId, "", ""); err != nil {
			logger.Error("unable to logout user, err: %s", err.Error())
			continue
		}
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

// ----------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------
// ----------------------------------------------------------------------------------------------------

type BanUserData struct {
	Usernames []string `json:"usernames"`
	UserIDs   []string `json:"user_ids"`
	BanDays   int      `json:"ban_days"`
}

type UnBanUserData struct {
	Username string `json:"username"`
	UserId   string `json:"user_id"`
}
