package ludo

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/heroiclabs/nakama-common/runtime"
)

const authoritativeLearningCollection = "ludo_bot_learning"

// authHumanBehavior stores aggregate tendencies only. No dice sequence or raw
// turn history is retained, and learned values can influence scoring but never
// legality, dice generation, economy, or authoritative state transitions.
type authHumanBehavior struct {
	Schema       int   `json:"schema"`
	Games        int64 `json:"games"`
	Moves        int64 `json:"moves"`
	Captures     int64 `json:"captures"`
	Finishes     int64 `json:"finishes"`
	Spawns       int64 `json:"spawns"`
	SafeMoves    int64 `json:"safe_moves"`
	ExposedMoves int64 `json:"exposed_moves"`
	UpdatedAt    int64 `json:"updated_at"`
}

func authoritativeLearningKey(userID string) string {
	digest := sha256.Sum256([]byte(userID))
	return fmt.Sprintf("human_%x", digest)
}

func loadAuthoritativeHumanModels(ctx context.Context, nk runtime.NakamaModule, game *authGame, logger runtime.Logger) {
	if nk == nil || game == nil {
		return
	}
	reads := make([]*runtime.StorageRead, 0, len(game.Players))
	playerIDs := make([]int, 0, len(game.Players))
	for _, player := range game.Players {
		if player.Bot {
			continue
		}
		reads = append(reads, &runtime.StorageRead{Collection: authoritativeLearningCollection, Key: authoritativeLearningKey(player.UserID)})
		playerIDs = append(playerIDs, player.ID)
	}
	if len(reads) == 0 {
		return
	}
	records, err := nk.StorageRead(ctx, reads)
	if err != nil {
		if logger != nil {
			logger.Warn("Ludo learning profiles unavailable: %v", err)
		}
		return
	}
	byKey := map[string]int{}
	for index, read := range reads {
		byKey[read.Key] = playerIDs[index]
	}
	for _, record := range records {
		playerID, found := byKey[record.Key]
		if !found {
			continue
		}
		var model authHumanBehavior
		if json.Unmarshal([]byte(record.Value), &model) == nil && model.Schema == 1 {
			game.HumanModels[playerID] = model
			game.HumanBaselines[playerID] = model
		}
	}
}

func (g *authGame) observeHumanMove(before *authGame, playerID int, move authMoveResult) {
	if g == nil || before == nil || move.PieceID < 0 || move.PieceID >= 4 {
		return
	}
	oldPlayer := before.player(playerID)
	newPlayer := g.player(playerID)
	if oldPlayer == nil || newPlayer == nil || newPlayer.Bot {
		return
	}
	model := g.HumanModels[playerID]
	model.Schema = 1
	model.Moves++
	oldPiece := oldPlayer.Pieces[move.PieceID]
	newPiece := newPlayer.Pieces[move.PieceID]
	if oldPiece.Passed == 0 && newPiece.Passed == 1 {
		model.Spawns++
	}
	if newPlayer.Kills > oldPlayer.Kills {
		model.Captures++
	}
	if newPlayer.Finished > oldPlayer.Finished {
		model.Finishes++
	}
	if newPiece.Passed >= 52 || (newPiece.Passed > 0 && authSafe(newPiece.Position)) {
		model.SafeMoves++
	}
	if calculateAuthoritativeCaptureRisk(g, playerID, newPiece.Passed, newPiece.Position) > 0 {
		model.ExposedMoves++
	}
	model.UpdatedAt = time.Now().Unix()
	g.HumanModels[playerID] = model
}

func persistAuthoritativeHumanModels(ctx context.Context, nk runtime.NakamaModule, game *authGame, logger runtime.Logger) error {
	if nk == nil || game == nil {
		return nil
	}
	persisted := 0
	for _, player := range game.Players {
		if player.Bot {
			continue
		}
		current := game.HumanModels[player.ID]
		baseline := game.HumanBaselines[player.ID]
		delta := subtractHumanBehavior(current, baseline)
		if delta.Moves == 0 {
			continue
		}
		key := authoritativeLearningKey(player.UserID)
		records, err := nk.StorageRead(ctx, []*runtime.StorageRead{{Collection: authoritativeLearningCollection, Key: key}})
		if err != nil {
			return err
		}
		latest := authHumanBehavior{Schema: 1}
		version := "*"
		if len(records) > 0 {
			if err := json.Unmarshal([]byte(records[0].Value), &latest); err != nil {
				return err
			}
			version = records[0].Version
		}
		merged := addHumanBehavior(latest, delta)
		merged.Schema = 1
		merged.Games++
		merged.UpdatedAt = time.Now().Unix()
		value, err := json.Marshal(merged)
		if err != nil {
			return err
		}
		if _, err := nk.StorageWrite(ctx, []*runtime.StorageWrite{{Collection: authoritativeLearningCollection, Key: key, Value: string(value), Version: version, PermissionRead: 0, PermissionWrite: 0}}); err != nil {
			return err
		}
		game.HumanModels[player.ID] = merged
		game.HumanBaselines[player.ID] = merged
		persisted++
	}
	if logger != nil && persisted > 0 {
		logger.Info("Ludo learning profiles persisted models=%d", persisted)
	}
	return nil
}

func subtractHumanBehavior(current, baseline authHumanBehavior) authHumanBehavior {
	return authHumanBehavior{
		Moves:        max(int64(0), current.Moves-baseline.Moves),
		Captures:     max(int64(0), current.Captures-baseline.Captures),
		Finishes:     max(int64(0), current.Finishes-baseline.Finishes),
		Spawns:       max(int64(0), current.Spawns-baseline.Spawns),
		SafeMoves:    max(int64(0), current.SafeMoves-baseline.SafeMoves),
		ExposedMoves: max(int64(0), current.ExposedMoves-baseline.ExposedMoves),
	}
}

func addHumanBehavior(base, delta authHumanBehavior) authHumanBehavior {
	base.Moves += delta.Moves
	base.Captures += delta.Captures
	base.Finishes += delta.Finishes
	base.Spawns += delta.Spawns
	base.SafeMoves += delta.SafeMoves
	base.ExposedMoves += delta.ExposedMoves
	return base
}

func learnedOpponentRiskMultiplier(model authHumanBehavior) float64 {
	if model.Moves < 5 {
		return 1
	}
	captureRate := float64(model.Captures) / float64(model.Moves)
	exposureRate := float64(model.ExposedMoves) / float64(model.Moves)
	safeRate := float64(model.SafeMoves) / float64(model.Moves)
	spawnRate := float64(model.Spawns) / float64(model.Moves)
	// Aggressive, capture-focused humans increase defensive risk weighting;
	// cautious safe play reduces it, while wider board presence adds pressure.
	multiplier := 1 + captureRate*1.5 + spawnRate*0.2 - safeRate*0.15 - exposureRate*0.25
	return mathMax(0.8, mathMin(1.6, multiplier))
}

func learnedOpponentFinishMultiplier(model authHumanBehavior) float64 {
	if model.Moves < 5 {
		return 1
	}
	finishRate := float64(model.Finishes) / float64(model.Moves)
	return mathMin(1.5, 1+finishRate*2)
}

func mathMin(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func mathMax(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
