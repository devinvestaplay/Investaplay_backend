package notfication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdSendCustomNotification         = "send_custom_notification"
	rpcIdSendCustomNotificationBySystem = "send_custom_notification_by_system"
)

func InitNotification(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(rpcIdSendCustomNotification, sendCustomNotification); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdSendCustomNotificationBySystem, sendCustomNotificationBySystem); err != nil {
		return err
	}

	return nil
}

func sendCustomNotification(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("sendCustomNotification userID: %s", userID)

	// ----------------------------- Payload -----------------------------

	var customNotificationData CustomNotificationData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &customNotificationData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Notification -----------------------------

	// Parse the content string into a map for using in Notification
	var contentMap map[string]interface{}
	err = json.Unmarshal([]byte(customNotificationData.Content), &contentMap)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotAcceptable, err.Error()), err
	}

	err = nk.NotificationSend(ctx,
		customNotificationData.TargetUserID,
		customNotificationData.Subject,
		contentMap,
		customNotificationData.Code,
		userID,
		customNotificationData.Persistent)

	if err != nil {
		return utils.CreateStatus(false, http.StatusNotAcceptable, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK, "succeeded"), nil
}

type CustomNotificationData struct {
	TargetUserID string `json:"target_user_id"`
	Subject      string `json:"subject"`
	Content      string `json:"content"` // json string
	Code         int    `json:"code"`
	Persistent   bool   `json:"persistent"`
}

// payload sample for Developer Warning
//
//	{
//		"usernames": ["101"],
//		"subject": "warning",
//		"content": "{}",
//		"sender": "00000000-0000-0000-0000-000000000000",
//		"code": 1003,
//		"persistent": false
//	}
func sendCustomNotificationBySystem(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("sendCustomNotificationBySystem userID: %s", userID)

	if userID != shared_constants.ServerSystemUserId {

		return utils.CreateStatus(false, http.StatusBadRequest, "can only be called by System ID"), nil
	}

	// ----------------------------- Payload -----------------------------

	var systemNotificationData SystemNotificationData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &systemNotificationData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// Validate required fields
	if len(systemNotificationData.Usernames) == 0 {
		err := runtime.NewError("usernames cannot be empty", 13)
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	if systemNotificationData.Subject == "" {
		err := runtime.NewError("subject cannot be empty", 13)
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	if systemNotificationData.Content == "" {
		err := runtime.NewError("content cannot be empty", 13)
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	var ContentMap map[string]interface{}

	if err := utils.DeserializeObjectFromStringByRefsToMap(&systemNotificationData.Content, &ContentMap); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// Get users by their usernames
	users, err := nk.UsersGetUsername(ctx, systemNotificationData.Usernames)
	if err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	// Prepare notifications for each user
	notifications := make([]*runtime.NotificationSend, 0, len(users))
	for _, user := range users {
		notifications = append(notifications, &runtime.NotificationSend{
			UserID:     user.GetId(),
			Subject:    systemNotificationData.Subject,
			Content:    ContentMap,
			Code:       systemNotificationData.Code,
			Sender:     systemNotificationData.Sender,
			Persistent: systemNotificationData.Persistent,
		})
	}

	// Send all notifications
	if err := nk.NotificationsSend(ctx, notifications); err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK, "notifications sent!"), nil
}

type SystemNotificationData struct {
	Usernames  []string `json:"usernames"`
	Subject    string   `json:"subject"`
	Content    string   `json:"content"`
	Sender     string   `json:"sender"`
	Code       int      `json:"code"`
	Persistent bool     `json:"persistent"`
}

// admin notice
type AdminNoticeNotif struct {
	Title   string `json:"title"`
	Message string `json:"message"`
}
