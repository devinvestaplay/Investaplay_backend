package notfication

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"game-server/systems/shared_constants"
	"game-server/utils"
	"net/http"
	"strings"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdSendCustomNotification         = "send_custom_notification"
	rpcIdSendCustomNotificationBySystem = "send_custom_notification_by_system"
	rpcIdNotificationSendAll            = "notification_send_all"

	notificationCodeBroadcast    = 202
	notificationSendAllBatchSize = 100
	maxNotificationSubjectLength = 128
	maxNotificationTitleLength   = 256
	maxNotificationMessageLength = 2048
)

func InitNotification(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(rpcIdSendCustomNotification, sendCustomNotification); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdSendCustomNotificationBySystem, sendCustomNotificationBySystem); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdNotificationSendAll, NotificationSendAll); err != nil {
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

type NotificationSendAllRequest struct {
	Subject    string `json:"subject"`
	Title      string `json:"title"`
	Message    string `json:"message"`
	Persistent *bool  `json:"persistent"`
}

type NotificationSendAllResponse struct {
	Success      bool   `json:"success"`
	Message      string `json:"message"`
	TotalUsers   int64  `json:"totalUsers"`
	SuccessCount int64  `json:"successCount"`
	FailureCount int64  `json:"failureCount"`
}

func NotificationSendAll(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	startTime := time.Now()

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok || userID != shared_constants.ServerSystemUserId {
		err := runtime.NewError("only system user is allowed, you are not authorized", 13)
		return "", err
	}

	logger.Info("NotificationSendAll RPC start userID: %s", userID)

	var request NotificationSendAllRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &request); err != nil {
		return "", runtime.NewError(fmt.Sprintf("invalid payload: %v", err), 13)
	}

	request.Subject = strings.TrimSpace(request.Subject)
	request.Title = strings.TrimSpace(request.Title)
	request.Message = strings.TrimSpace(request.Message)

	if request.Subject == "" {
		return "", runtime.NewError("subject cannot be empty", 13)
	}
	if request.Title == "" {
		return "", runtime.NewError("title cannot be empty", 13)
	}
	if request.Message == "" {
		return "", runtime.NewError("message cannot be empty", 13)
	}
	if request.Persistent == nil {
		return "", runtime.NewError("persistent must be provided", 13)
	}
	if len(request.Subject) > maxNotificationSubjectLength {
		return "", runtime.NewError("subject exceeds maximum length", 13)
	}
	if len(request.Title) > maxNotificationTitleLength {
		return "", runtime.NewError("title exceeds maximum length", 13)
	}
	if len(request.Message) > maxNotificationMessageLength {
		return "", runtime.NewError("message exceeds maximum length", 13)
	}

	contentMap := map[string]interface{}{
		"title":   request.Title,
		"message": request.Message,
	}

	logger.Info("NotificationSendAll subject: %s persistent: %t", request.Subject, *request.Persistent)

	totalUsers, err := countUsersForNotificationAll(ctx, db)
	if err != nil {
		logger.Error("NotificationSendAll count users failed: %v", err)
		return "", runtime.NewError("failed to enumerate players", 13)
	}

	var successCount int64
	var failureCount int64
	lastUserID := shared_constants.ServerSystemUserId
	batchSize := notificationSendAllBatchSize

	for {
		if err := ctx.Err(); err != nil {
			logger.Error("NotificationSendAll context canceled: %v", err)
			return "", err
		}

		batchUserIDs, err := queryUsersForNotificationAll(ctx, db, lastUserID, batchSize)
		if err != nil {
			logger.Error("NotificationSendAll query batch failed: %v", err)
			return "", runtime.NewError("failed to read players batch", 13)
		}
		if len(batchUserIDs) == 0 {
			break
		}

		notifications := make([]*runtime.NotificationSend, 0, len(batchUserIDs))
		for _, recipientUserID := range batchUserIDs {
			notifications = append(notifications, &runtime.NotificationSend{
				UserID:     recipientUserID,
				Subject:    request.Subject,
				Content:    contentMap,
				Code:       notificationCodeBroadcast,
				Sender:     shared_constants.ServerSystemUserId,
				Persistent: *request.Persistent,
			})
		}

		logger.Info("NotificationSendAll batch size: %d", len(notifications))

		if err := nk.NotificationsSend(ctx, notifications); err != nil {
			logger.Error("NotificationSendAll batch failed: %v", err)
			failureCount += int64(len(notifications))
			lastUserID = batchUserIDs[len(batchUserIDs)-1]
			continue
		}

		successCount += int64(len(notifications))
		lastUserID = batchUserIDs[len(batchUserIDs)-1]
	}

	logger.Info("NotificationSendAll completed totalUsers: %d successCount: %d failureCount: %d duration: %s", totalUsers, successCount, failureCount, time.Since(startTime))

	response := NotificationSendAllResponse{
		Success:      true,
		Message:      "Notification sent to all players",
		TotalUsers:   totalUsers,
		SuccessCount: successCount,
		FailureCount: failureCount,
	}

	responseJSON, err := utils.SerializeObjectToString(&response)
	if err != nil {
		return "", runtime.NewError(fmt.Sprintf("failed to serialize response: %v", err), 13)
	}

	return responseJSON, nil
}

func countUsersForNotificationAll(ctx context.Context, db *sql.DB) (int64, error) {
	var totalUsers int64
	query := `SELECT COUNT(*) FROM users WHERE id != $1`
	if err := db.QueryRowContext(ctx, query, shared_constants.ServerSystemUserId).Scan(&totalUsers); err != nil {
		return 0, err
	}
	return totalUsers, nil
}

func queryUsersForNotificationAll(ctx context.Context, db *sql.DB, lastUserID string, batchSize int) ([]string, error) {
	query := `SELECT id FROM users WHERE id != $1 AND id > $2`
	args := []any{shared_constants.ServerSystemUserId, lastUserID}

	query += ` ORDER BY id LIMIT $3`
	args = append(args, batchSize)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	userIDs := make([]string, 0, batchSize)
	for rows.Next() {
		var userID string
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return userIDs, nil
}
