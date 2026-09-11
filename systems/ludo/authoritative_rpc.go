package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"game-server/utils"
	"github.com/heroiclabs/nakama-common/runtime"
)

func ludoMatchStart(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	user, err := ludoAuthenticatedUserID(ctx)
	if err != nil {
		return "", err
	}
	var req LudoMatchStartData
	if json.Unmarshal([]byte(payload), &req) != nil || req.MatchID == "" {
		return "", runtime.NewError("match_id required; update the Ludo client", 3)
	}
	if req.RoomCode != "" {
		room, _, err := readLudoRoom(ctx, nk, req.RoomCode)
		if err != nil {
			return "", err
		}
		if room.MatchID != req.MatchID || room.ArenaName != req.ArenaName {
			return "", runtime.NewError("room mismatch", 3)
		}
		if _, ok := room.Players[user]; !ok {
			return "", runtime.NewError("not in room", 7)
		}
		if room.HostID == user {
			result, err := nk.MatchSignal(ctx, room.MatchID, ludoCustomRoomSignalStart)
			if err != nil {
				return "", err
			}
			var response map[string]interface{}
			_ = json.Unmarshal([]byte(result), &response)
			if message, ok := response["error"].(string); ok {
				return "", runtime.NewError(message, 9)
			}
		}
		return utils.CreateStatus(true, 200, "entry fees collected when all players are ready"), nil
	}
	signal, err := nk.MatchSignal(ctx, req.MatchID, "state")
	if err != nil {
		return "", err
	}
	var state struct {
		Protocol int           `json:"protocol"`
		Players  []*authPlayer `json:"players"`
	}
	if json.Unmarshal([]byte(signal), &state) != nil || state.Protocol != 2 {
		return "", runtime.NewError("server-authoritative match required", 9)
	}
	for _, p := range state.Players {
		if p.UserID == user {
			return utils.CreateStatus(true, 200, "server manages entry fee"), nil
		}
	}
	return "", runtime.NewError("not a match participant", 7)
}

func ludoMatchFinish(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	return "", runtime.NewError("Client rankings are not accepted. Nakama settles authoritative matches automatically.", 7)
}
