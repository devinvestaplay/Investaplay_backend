package ludo

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/heroiclabs/nakama-common/runtime"
	"sort"
)

func (s *LudoCustomRoomMatchState) startAuthority(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, d runtime.MatchDispatcher, tick int64) error {
	roster := map[string]runtime.Presence{}
	for _, p := range s.Presences {
		roster[p.GetUserId()] = p
	}
	if len(roster) != s.MaxPlayers {
		return errors.New("all room players must join before starting")
	}
	ids := []string{}
	for id := range roster {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	players := []*authPlayer{}
	for i, id := range ids {
		account, err := nk.AccountGetId(ctx, id)
		if err != nil {
			return err
		}
		seat := i
		if len(ids) == 2 {
			seat = i * 2
		}
		profile := map[string]interface{}{"userID": id, "username": account.User.Username, "displayName": account.User.DisplayName, "currentAvatar": ludoNormalizeAvatarAddressableKey(account.User.AvatarUrl), "currentDice": 1, "currentBoard": 1, "currentPiece": 1}
		players = append(players, &authPlayer{ID: seat, UserID: id, Profile: profile})
	}
	encoded, _ := json.Marshal(players)
	state, _, _ := (&AuthoritativeLudoMatch{}).MatchInit(ctx, logger, db, nk, map[string]interface{}{"players": string(encoded), "arena_name": s.ArenaName})
	if state == nil {
		return errors.New("invalid room arena")
	}
	s.Authority = state.(*authMatchState)
	s.Authority.CreatedTick = tick
	s.Authority.Presences = roster
	ludoCustomRoomMarkPlaying(ctx, logger, nk, d, s)
	authSend(d, logger, 200, []byte(`{"protocol":2}`), nil)
	return nil
}
