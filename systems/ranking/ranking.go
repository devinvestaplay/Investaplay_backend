package ranking

import (
	"context"
	"database/sql"
	"errors"
	"game-server/systems/leaderboard"
	"game-server/systems/ludo"
	"game-server/systems/quiz"
	"game-server/systems/solitaire"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/api"
	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	rpcIdGetGlobalSkill = "get_global_skill"

	globalPrimaryWeight   = 0.85
	globalSecondaryWeight = 0.15
)

func InitRanking(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {
	if err := (*initializer).RegisterRpc(rpcIdGetGlobalSkill, getGlobalSkill); err != nil {
		return err
	}
	return nil
}

type GlobalSkillResponse struct {
	GlobalScore    float64                       `json:"global_score"`
	PrimarySkill   float64                       `json:"primary_skill"`
	SecondarySkill float64                       `json:"secondary_skill"`
	PrimaryGame    string                        `json:"primary_game"`
	SecondaryGame  string                        `json:"secondary_game"`
	LudoSkill      float64                       `json:"ludo_skill"`
	QuizSkill      float64                       `json:"quiz_skill"`
	SolitaireSkill float64                       `json:"solitaire_skill"`
	LudoEligible   bool                          `json:"ludo_eligible"`
	QuizEligible   bool                          `json:"quiz_eligible"`
	SolitaireElig  bool                          `json:"solitaire_eligible"`
	Rank           int64                         `json:"rank"`
	TopUsers       []GlobalSkillLeaderboardUser `json:"top_users"`
}

type GlobalSkillLeaderboardUser struct {
	UserID      string                 `json:"user_id"`
	Username    string                 `json:"username,omitempty"`
	Rank        int64                  `json:"rank"`
	Score       int64                  `json:"score"`
	GlobalScore float64                `json:"global_score"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

func getGlobalSkill(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	ludoScore, ludoResp, err := ludo.ComputeLudoSkillForUser(ctx, nk, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	quizScore, quizResp, err := quiz.ComputeQuizSkillForUser(ctx, db, nk, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	solScore, solResp, err := solitaire.ComputeSolitaireSkillForUser(ctx, db, nk, userID)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	resp := GlobalSkillResponse{
		LudoSkill:      ludoScore,
		QuizSkill:      quizScore,
		SolitaireSkill: solScore,
		LudoEligible:   ludoResp.IsEligible,
		QuizEligible:   quizResp.IsEligible,
		SolitaireElig:  solResp.IsEligible,
	}

	// Rank per-game skills to find primary (best) and secondary (second best).
	type gameSkill struct {
		name  string
		score float64
		elig  bool
	}
	games := []gameSkill{
		{"ludo", ludoScore, ludoResp.IsEligible},
		{"quiz", quizScore, quizResp.IsEligible},
		{"solitaire", solScore, solResp.IsEligible},
	}

	var eligible []gameSkill
	for _, g := range games {
		if g.elig {
			eligible = append(eligible, g)
		}
	}
	for i := 0; i < len(eligible); i++ {
		for j := i + 1; j < len(eligible); j++ {
			if eligible[j].score > eligible[i].score {
				eligible[i], eligible[j] = eligible[j], eligible[i]
			}
		}
	}

	var primary, secondary float64
	switch len(eligible) {
	case 0:
		// No eligible games yet, but still return the current global top 10.
		topUsers, topErr := listTopGlobalSkillUsers(ctx, nk)
		if topErr != nil {
			logger.Error("failed to list global skill leaderboard: %v", topErr)
		}
		resp.TopUsers = topUsers
		respJson, err := utils.SerializeObjectToString(&resp)
		if err != nil {
			return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
		}
		return respJson, nil
	case 1:
		primary = eligible[0].score
		secondary = 0
		resp.PrimaryGame = eligible[0].name
	default:
		primary = eligible[0].score
		secondary = eligible[1].score
		resp.PrimaryGame = eligible[0].name
		resp.SecondaryGame = eligible[1].name
	}

	resp.PrimarySkill = primary
	resp.SecondarySkill = secondary
	resp.GlobalScore = globalPrimaryWeight*primary + globalSecondaryWeight*secondary

	record, lerr := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardSkillGlobalID, userID, "", int64(resp.GlobalScore*1_000_000), 0, nil, nil)
	if lerr != nil {
		logger.Error("failed to write global skill leaderboard for user %s: %v", userID, lerr)
	} else {
		resp.Rank = record.Rank
	}

	topUsers, topErr := listTopGlobalSkillUsers(ctx, nk)
	if topErr != nil {
		logger.Error("failed to list global skill leaderboard: %v", topErr)
	} else {
		resp.TopUsers = topUsers
	}

	respJson, err := utils.SerializeObjectToString(&resp)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return respJson, nil
}

func listTopGlobalSkillUsers(ctx context.Context, nk runtime.NakamaModule) ([]GlobalSkillLeaderboardUser, error) {
	records, _, _, _, err := nk.LeaderboardRecordsList(ctx, leaderboard.LeaderboardSkillGlobalID, nil, 10, "", 0)
	if err != nil {
		return nil, err
	}

	if err := leaderboard.AddAccountMetadataToRecords(ctx, nk, records); err != nil {
		return nil, err
	}

	return globalSkillLeaderboardUsersFromRecords(records), nil
}

func globalSkillLeaderboardUsersFromRecords(records []*api.LeaderboardRecord) []GlobalSkillLeaderboardUser {
	users := make([]GlobalSkillLeaderboardUser, 0, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}

		user := GlobalSkillLeaderboardUser{
			UserID:      record.GetOwnerId(),
			Rank:        record.GetRank(),
			Score:       record.GetScore(),
			GlobalScore: float64(record.GetScore()) / 1_000_000,
		}

		if username := record.GetUsername(); username != nil {
			user.Username = username.Value
		}

		if metadata := record.GetMetadata(); metadata != "" {
			var metadataMap map[string]interface{}
			if err := utils.DeserializeObjectFromStringByRefsToMap(&metadata, &metadataMap); err == nil {
				user.Metadata = metadataMap
			}
		}

		users = append(users, user)
	}
	return users
}
