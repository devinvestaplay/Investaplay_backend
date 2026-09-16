package ludo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"game-server/systems/arena"
	"game-server/systems/leaderboard"
	"github.com/heroiclabs/nakama-common/runtime"
)

const authRecords = "ludo_authoritative"

// Fees and results are committed atomically with a create-only receipt. Retrying
// a settlement cannot credit a wallet twice. Neither operation accepts client ranks.
func (s *authMatchState) charge(ctx context.Context, nk runtime.NakamaModule) error {
	if s.EntryPaid {
		return nil
	}
	receipts, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: authRecords, Key: s.MatchID + "_entry"}})
	if err != nil {
		return err
	}
	if len(receipts) > 0 {
		s.EntryPaid = true
		return nil
	}
	changes := []*runtime.WalletUpdate{}
	for _, p := range s.Game.Players {
		if !p.Bot {
			changes = append(changes, &runtime.WalletUpdate{UserID: p.UserID, Changeset: map[string]int64{"coins": -int64(s.Arena.FeeCurrencyData.Amount)}, Metadata: map[string]interface{}{"match_id": s.MatchID, "arena": s.Arena.Name, "reason": "ludo entry"}})
		}
	}
	value, _ := json.Marshal(map[string]interface{}{"players": s.Game.Players, "arena": s.Arena.Name})
	_, _, err = nk.MultiUpdate(ctx, nil, []*runtime.StorageWrite{{Collection: authRecords, Key: s.MatchID + "_entry", Value: string(value), Version: "*", PermissionRead: 0, PermissionWrite: 0}}, nil, changes, true)
	if err != nil {
		if receipts, readErr := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: authRecords, Key: s.MatchID + "_entry"}}); readErr == nil && len(receipts) > 0 {
			s.EntryPaid = true
			return nil
		}
	}
	if err == nil {
		s.EntryPaid = true
	}
	return err
}

func (s *authMatchState) settle(ctx context.Context, nk runtime.NakamaModule) error {
	if !s.EntryPaid {
		return errors.New("cannot settle without collected entry fees")
	}
	// A previous successful commit may have lost its acknowledgement.
	records, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: authRecords, Key: s.MatchID + "_result"}})
	if err != nil {
		return err
	}
	if len(records) > 0 {
		s.Persisted = true
		return nil
	}
	changes := []*runtime.WalletUpdate{}
	accounts := []*runtime.AccountUpdate{}
	for _, p := range s.Game.Players {
		if p.Bot {
			continue
		}
		rank := s.Game.Ranks[p.ID]
		if rank == 0 {
			return errors.New("cannot settle unfinished match")
		}
		earned := int64(0)
		for _, r := range s.Arena.Rewards[arena.Rank(rank)] {
			earned += int64(r.Amount)
		}
		account, err := nk.AccountGetId(ctx, p.UserID)
		if err != nil {
			return err
		}
		meta := map[string]interface{}{}
		if account.User.Metadata != "" {
			if err = json.Unmarshal([]byte(account.User.Metadata), &meta); err != nil {
				return err
			}
		}
		if meta == nil {
			meta = map[string]interface{}{}
		}
		increments := map[string]int64{"ludo_played_match": 1, "ludo_kills": int64(p.Kills), "ludo_deaths": int64(p.Deaths), "ludo_total_earned_coins": earned}
		if rank == 1 {
			increments["ludo_win_match"] = 1
		}
		for k, v := range increments {
			old, _ := meta[k].(float64)
			meta[k] = old + float64(v)
		}
		accounts = append(accounts, &runtime.AccountUpdate{UserID: p.UserID, Metadata: meta})
		changes = append(changes, &runtime.WalletUpdate{UserID: p.UserID, Changeset: map[string]int64{"coins": earned}, Metadata: map[string]interface{}{"match_id": s.MatchID, "arena": s.Arena.Name, "rank": rank}})
	}
	value, _ := json.Marshal(map[string]interface{}{"players": s.Game.Players, "ranks": s.Game.Ranks, "arena": s.Arena.Name})
	_, _, err = nk.MultiUpdate(ctx, accounts, []*runtime.StorageWrite{{Collection: authRecords, Key: s.MatchID + "_result", Value: string(value), Version: "*", PermissionRead: 0, PermissionWrite: 0}}, nil, changes, true)
	if err == nil {
		s.Persisted = true
		// Leaderboards are a best-effort projection; wallet settlement above is
		// atomic and must never be repeated to retry a leaderboard write.
		for _, change := range changes {
			earned := change.Changeset["coins"]
			for _, board := range []string{leaderboard.LeaderboardTotalEarnedCoinsGlobalID, leaderboard.LeaderboardTotalEarnedCoinsLudoID} {
				_, _ = nk.LeaderboardRecordWrite(ctx, board, change.UserID, "", earned, 0, change.Metadata, nil)
			}
		}
	}
	return err
}

func authArena(name string, count int) (arena.LudoArenaItemData, error) {
	a, ok := arena.LudoArena.Arenas[name]
	if !ok || !a.Enabled || a.FeeCurrencyData.Amount < 0 {
		return a, errors.New("arena unavailable")
	}
	two := a.Mode == arena.Mode2POnline || a.Mode == arena.Mode2PWithFriends
	four := a.Mode == arena.Mode4POnline || a.Mode == arena.Mode4PWithFriends
	if !(two && count == 2 || four && count == 4) {
		return a, errors.New("arena player count mismatch")
	}
	for _, rewards := range a.Rewards {
		for _, r := range rewards {
			if r.Amount < 0 {
				return a, fmt.Errorf("invalid arena reward")
			}
		}
	}
	return a, nil
}
