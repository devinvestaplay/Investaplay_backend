package party

import (
	"context"
	"database/sql"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdSendPartyInvite = "party_send_invite"
)

func InitParty(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	err := (*initializer).RegisterRpc(rpcIdSendPartyInvite, sendPartyInvite)
	if err != nil {
		return err
	}

	return nil
}

func sendPartyInvite(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	var partyInviteData PartyInviteRequestData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &partyInviteData); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	var contentMap map[string]interface{}

	if err := utils.DeserializeObjectFromStringByRefsToMap(&partyInviteData.Content, &contentMap); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	if err := nk.NotificationSend(ctx, partyInviteData.ReceiverUserId, partyInviteData.Subject, contentMap, partyInviteData.Code, partyInviteData.SenderUserId, partyInviteData.Persistent); err != nil {
		return utils.CreateStatus(false, http.StatusBadGateway, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

type PartyInviteRequestData struct {
	SenderUserId   string `json:"senderUserId"`
	ReceiverUserId string `json:"receiverUserId"`
	Subject        string `json:"subject"`
	Content        string `json:"content"`
	Code           int    `json:"code"`
	Persistent     bool   `json:"persistent"`
}
