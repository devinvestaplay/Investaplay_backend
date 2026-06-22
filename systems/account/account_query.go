package account

import (
	"context"
	"database/sql"
	"errors"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

type QueryAccountsRequest struct {
	DeviceIDs []string `json:"device_ids"`
}

type QueryAccountsResponse struct {
	Users map[string]string `json:"users"` // device id : user id
}

func queryUsersByDeviceIDs(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	_, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	// ----------------------------- Payload -----------------------------

	var queryData QueryAccountsRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &queryData); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Query -----------------------------

	usersResponse := QueryAccountsResponse{Users: make(map[string]string)}

	for _, deviceId := range queryData.DeviceIDs {

		// Look for an existing account.
		query := "SELECT user_id FROM user_device WHERE id = $1"
		var dbUserID string
		if err := db.QueryRowContext(ctx, query, deviceId).Scan(&dbUserID); err != nil {
			//return utils.CreateStatus(false, http.StatusNoContent, "did not found any account with the device id"), err
			continue
		}

		usersResponse.Users[deviceId] = dbUserID
	}

	usersResponseJSON, err := utils.SerializeObjectToString(&usersResponse)

	return usersResponseJSON, err

}
