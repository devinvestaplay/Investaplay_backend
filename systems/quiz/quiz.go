package quiz

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"game-server/systems/leaderboard"
	"game-server/systems/wallet"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	QuizGameConfigJSONFilePath = "../configs/quiz-game-config.json"
	rpcIdResetupQuizGameConfig = "quiz_game_config_resetup"
	rpcIdGetQuizGameConfig     = "quiz_game_config_get"

	QuizGameConfigKey = "quiz_game_config"

	// Category
	rpcIdGetCategories  = "quiz_categories_get"
	rpcIdAddCategory    = "quiz_category_add"
	rpcIdRemoveCategory = "quiz_category_remove"

	// Question
	rpcIdGetQuestionsByCategory     = "quiz_question_get_by_category"
	rpcIdAddQuestionToCategory      = "quiz_question_add_to_category"
	rpcIdRemoveQuestionFromCategory = "quiz_question_remove_from_category"

	// Game
	rpcIdQuizFiftyFifty   = "quiz_game_fiftyfifty"
	rpcIdQuizPassQuestion = "quiz_game_passquestion"
	rpcIdQuizAutoCorrect  = "quiz_game_autocorrect"
	rpcIdQuizHint         = "quiz_game_hint"

	rpcIdQuizFinish = "quiz_game_finish"

	QuizCollectionName = "Quiz" // parent of all categories
	QuizCategoriesKey  = "quiz_categories"
	// all categories have a storage object
)

// quiz config
var quizGameConfigJson string

// categories
var categories Categories

// questions
var questionsOfCategory map[string]Questions // key is categoryID

// quiz game config
var quizConfig QuizConfig

func InitQuiz(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	// Quiz Game Config data
	if data, err := utils.LoadBaseJsonData(ctx, logger, nk, QuizCollectionName, QuizGameConfigKey, QuizGameConfigJSONFilePath); err != nil {
		return err
	} else {
		quizGameConfigJson = data
		err := processQuizGameConfigJSON(quizGameConfigJson)
		if err != nil {
			return err
		}
		(*logger).Info("quizGameConfigJson : ", quizGameConfigJson)
	}

	if err := (*initializer).RegisterRpc(rpcIdResetupQuizGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

		quizGameConfigJson = payload
		err := processQuizGameConfigJSON(quizGameConfigJson)
		if err != nil {
			return "", err
		}

		if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, QuizGameConfigKey, &payload); err == nil {
			return fmt.Sprintf(`{"succeeded": %t}`, true), nil
		} else {
			return fmt.Sprintf(`{"succeeded": %t, "err": %s}`, false, err.Error()), err

		}

	}); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdGetQuizGameConfig, func(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
		return quizGameConfigJson, nil
	}); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetCategories, getCategories); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdAddCategory, addCategory); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdRemoveCategory, removeCategory); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdGetQuestionsByCategory, getQuestionsByCategory); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdAddQuestionToCategory, addQuestionToCategory); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdRemoveQuestionFromCategory, removeQuestionFromCategory); err != nil {
		return err
	}

	if err := loadCategories(ctx, logger, nk); err != nil {
		return err
	}

	if err := loadQuestions(ctx, logger, nk); err != nil {
		return err
	}

	// ----------------------------------------------------------------------------------------------------

	if err := (*initializer).RegisterRpc(rpcIdQuizFiftyFifty, fiftyFifty); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdQuizPassQuestion, passQuestion); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdQuizAutoCorrect, autoCorrect); err != nil {
		return err
	}
	if err := (*initializer).RegisterRpc(rpcIdQuizHint, hint); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(rpcIdQuizFinish, gameFinished); err != nil {
		return err
	}

	return nil
}

// -----------------------------------------------------------------------

func loadCategories(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule) error {

	categories = Categories{
		Categories: make(map[string]CategoryData),
	}

	jsonData, err := utils.ReadServerStorageObjectByKey(ctx, nk, QuizCollectionName, QuizCategoriesKey)
	if err != nil {
		(*logger).Error("Failed to load categories ", " err: ", err)
		// return err
	}

	if len(jsonData) == 0 {
		return nil
	}

	var data Categories
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		categories = data
	}
	return nil
}

func loadQuestions(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule) error {

	questionsOfCategory = make(map[string]Questions)

	// Loop through all category IDs
	for categoryID := range categories.Categories {

		jsonData, err := utils.ReadServerStorageObjectByKey(ctx, nk, QuizCollectionName, categoryID)
		if err != nil {
			(*logger).Error("Failed to load questions for category ", categoryID, " err: ", err)
			continue // skip but dont return error
		}

		var qs Questions
		if err := json.Unmarshal([]byte(jsonData), &qs); err != nil {
			(*logger).Error("Failed to unmarshal questions for category ", categoryID, " err: ", err)
			continue
		}

		questionsOfCategory[categoryID] = qs
	}

	return nil
}

// -----------------------------------------------------------------------

func getCategories(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	categoriesJson, err := utils.SerializeObjectToString(&categories)

	if err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}
	return categoriesJson, nil
}

func addCategory(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Payload -----------------------------

	var requestData CategoryData

	err := utils.DeserializeObjectFromStringByRefs(&payload, &requestData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Check Exists Before -----------------------------

	if _, exists := categories.Categories[requestData.CategoryId]; exists {
		return utils.CreateStatus(false, http.StatusNotAcceptable, "Category ID already exists"), nil
	}

	// ----------------------------- Sample Question -----------------------------

	sampleQuestion := Questions{
		Questions: make(map[string]QuestionData),
	}
	sampleQuestion.Questions["q_0"] = QuestionData{
		QuestionId:         "q_0",
		CategoryId:         requestData.CategoryId,
		QuestionText:       "What is your question text?",
		QuestionTimerSec:   45,
		Option1:            "the is option 1",
		Option2:            "the is option 2",
		Option3:            "the is option 3",
		Option4:            "the is option 4",
		CorrectOptionIndex: 1,
		Hint:               &QuestionHintData{HintText: "Hint text"},
		Difficulty:         Easy,
		IsActive:           false,
	}

	sampleQuestionJson, err := utils.SerializeObjectToString(&sampleQuestion)

	if err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	// ----------------------------- Write Category Storage Object -----------------------------

	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, requestData.CategoryId, &sampleQuestionJson); err != nil {
		return utils.CreateStatus(false, http.StatusNotAcceptable, err.Error()), err
	}

	// ----------------------------- Update Categories Storage Object -----------------------------
	categories.Categories[requestData.CategoryId] = requestData

	// ----------------------------- Cache New Category in Questions Map -----------------------------
	questionsOfCategory[requestData.CategoryId] = Questions{
		Questions: make(map[string]QuestionData),
	}

	// Add sample question into the in-memory cache
	questionsOfCategory[requestData.CategoryId].Questions["q_0"] = sampleQuestion.Questions["q_0"]

	categoriesJson, err := utils.SerializeObjectToString(&categories)

	if err != nil {
		return utils.CreateStatus(false, http.StatusNoContent, err.Error()), err
	}

	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, QuizCategoriesKey, &categoriesJson); err != nil {
		return utils.CreateStatus(false, http.StatusNotAcceptable, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil

}

func removeCategory(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Payload -----------------------------
	var req CategoryIDRequestData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Check Exists -----------------------------
	if _, exists := categories.Categories[req.CategoryID]; !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "Category not found"), nil
	}

	// ----------------------------- Delete Questions Storage Object -----------------------------
	// All questions of this category are stored under storage object with name = CategoryID
	deleteReq := []*runtime.StorageDelete{
		{
			Collection: QuizCollectionName,
			Key:        req.CategoryID,
		},
	}

	if err := nk.StorageDelete(ctx, deleteReq); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	// ----------------------------- Remove From Cache: Categories -----------------------------
	delete(categories.Categories, req.CategoryID)

	// Update categories storage object
	catsJson, err := utils.SerializeObjectToString(&categories)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, QuizCategoriesKey, &catsJson); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	// ----------------------------- Remove From Cache: Questions -----------------------------
	delete(questionsOfCategory, req.CategoryID)

	return utils.CreateStatus(true, http.StatusOK), nil
}

// -----------------------------------------------------------------------

func getQuestionsByCategory(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Payload -----------------------------
	var req CategoryIDRequestData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Check Exists -----------------------------
	qs, exists := questionsOfCategory[req.CategoryID]
	if !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "Category not found or no questions"), nil
	}

	// ----------------------------- Serialize -----------------------------
	jsonStr, err := utils.SerializeObjectToString(&qs)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	return jsonStr, nil
}

func addQuestionToCategory(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Payload -----------------------------
	var q QuestionData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &q); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Validate Category -----------------------------
	qs, exists := questionsOfCategory[q.CategoryId]
	if !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "Category ID not found"), nil
	}

	// ----------------------------- Check Exists -----------------------------
	if _, exists := qs.Questions[q.QuestionId]; exists {
		return utils.CreateStatus(false, http.StatusNotAcceptable, "Question ID already exists"), nil
	}

	// ----------------------------- Add to Cache -----------------------------
	qs.Questions[q.QuestionId] = q
	questionsOfCategory[q.CategoryId] = qs

	// ----------------------------- Save to Storage -----------------------------
	qsJson, err := utils.SerializeObjectToString(&qs)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, q.CategoryId, &qsJson); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

func removeQuestionFromCategory(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- Payload -----------------------------
	var req QuestionRemoveRequestData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Validate Category -----------------------------
	qs, exists := questionsOfCategory[req.CategoryID]
	if !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "Category not found"), nil
	}

	// ----------------------------- Validate Question -----------------------------
	if _, exists := qs.Questions[req.QuestionID]; !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "Question not found"), nil
	}

	// ----------------------------- Remove From Cache -----------------------------
	delete(qs.Questions, req.QuestionID)
	questionsOfCategory[req.CategoryID] = qs

	// ----------------------------- Save Updated Storage -----------------------------
	jsonStr, err := utils.SerializeObjectToString(&qs)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, QuizCollectionName, req.CategoryID, &jsonStr); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

// -----------------------------------------------------------------------

func fiftyFifty(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, quizConfig.RemoveTwoWrongAnswerCoinCost, "fifty_fifty")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func passQuestion(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, quizConfig.PassQuestionCoinCost, "pass_question")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func autoCorrect(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, quizConfig.AutoCorrectCoinCost, "auto_correct")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func hint(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	updatedWalletJson, err := chargeLifelineCost(ctx, nk, logger, userID, quizConfig.HintCoinCost, "hint")
	if err != nil {
		return utils.CreateStatus(false, http.StatusPaymentRequired, err.Error()), nil
	}

	return utils.CreateStatus(true, http.StatusOK, updatedWalletJson), nil
}

func chargeLifelineCost(ctx context.Context, nk runtime.NakamaModule, logger runtime.Logger, userID string, cost int, lifelineName string) (string, error) {

	acc, err := nk.AccountGetId(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("account get error: %w", err)
	}

	walletData, err := wallet.DeserializeWalletData(&acc.Wallet)
	if err != nil {
		return "", fmt.Errorf("wallet parse error: %w", err)
	}

	if walletData.Coins < cost {
		return "", errors.New("insufficient balance")
	}

	changeset := map[string]int64{"coins": int64(-cost)}

	metadata := map[string]interface{}{
		"cost":     cost,
		"lifeline": lifelineName,
	}

	updatedWallet, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)

	updatedWalletJson, err := utils.SerializeObjectToString(&updatedWallet)

	return updatedWalletJson, err
}

func gameFinished(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	// ----------------------------- User Id -----------------------------

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		err := errors.New("invalid context")
		return utils.CreateStatus(false, http.StatusUnauthorized, err.Error()), err
	}

	logger.Info("quiz gameFinished userID: %s", userID)

	// ----------------------------- Payload -----------------------------

	var finishData QuizFinishGameData
	err := utils.DeserializeObjectFromStringByRefs(&payload, &finishData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	// ----------------------------- Wallet -----------------------------

	changeset := map[string]int64{"coins": int64(finishData.Coins)}

	metadata := map[string]interface{}{
		"coins":  finishData.Coins,
		"points": finishData.Points,
	}

	_, _, err = nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		logger.Error("failed to update wallet for user %s: %v", userID, err)
	}

	// ----------------------------- Leaderboard Updates -----------------------------

	grecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsGlobalID, userID, "", int64(finishData.Coins), int64(finishData.Points), metadata, nil)
	if err != nil {
		logger.Error("failed to update global leaderboard for user %s: %v", userID, err)
	}
	logger.Info("global leaderboard for user %s: %+v", userID, grecord)

	lrecord, err := nk.LeaderboardRecordWrite(ctx, leaderboard.LeaderboardTotalEarnedCoinsQuizID, userID, "", int64(finishData.Coins), int64(finishData.Points), metadata, nil)
	if err != nil {
		logger.Error("failed to update quiz leaderboard for user %s: %v", userID, err)
	}
	logger.Info("quiz leaderboard for user %s: %+v", userID, lrecord)

	// ----------------------------- Stats Update -----------------------------

	metaDataMap := make(map[string]any)
	metaDataMap["quiz_total_earned_coins"] = finishData.Coins
	metaDataMap["quiz_played_match"] = 1
	if finishData.IsWinner {
		metaDataMap["quiz_win_match"] = 1
	}

	if err := utils.UpdateMetaData(&ctx, &nk, &logger, userID, metaDataMap, false); err != nil {
		return utils.CreateStatus(false, http.StatusNotFound, err.Error()), err
	}

	// ----------------------------- Response -----------------------------

	return utils.CreateStatus(true, http.StatusOK, "match finished and rewards distributed"), nil
}

// -----------------------------------------------------------------------

type Difficulty int

const (
	Easy Difficulty = iota
	Medium
	Hard
)

type LifelineType int

const (
	FiftyFifty   LifelineType = iota // Removes 2 wrong answers (-5 coins default)
	PassQuestion                     // Skip without penalty (-8 coins default)
	AutoCorrect                      // Auto-selects correct (-15 coins default)
	Hint                             // Shows clue (text/image, -10 coins default)
)

type CategoryData struct {
	CategoryId   string     `json:"categoryId"`
	CategoryName string     `json:"categoryName"`
	Difficulty   Difficulty `json:"difficulty"`
	IsActive     bool       `json:"isActive"`
}

type Categories struct {
	Categories map[string]CategoryData `json:"categories"` // key is categoryId
}

// -----------------------------------------------------------------------

type QuestionData struct {
	QuestionId         string            `json:"questionId"`
	CategoryId         string            `json:"categoryId"`
	QuestionText       string            `json:"questionText"`
	QuestionTimerSec   float32           `json:"questionTimerSec"`
	Option1            string            `json:"option1"`
	Option2            string            `json:"option2"`
	Option3            string            `json:"option3"`
	Option4            string            `json:"option4"`
	CorrectOptionIndex int               `json:"correctOptionIndex"` // 0-3
	Hint               *QuestionHintData `json:"hint,omitempty"`     // nullable
	Difficulty         Difficulty        `json:"difficulty"`
	IsActive           bool              `json:"isActive"`
}

type QuestionHintData struct {
	HintText     string `json:"hintText,omitempty"`
	HintImageUrl string `json:"hintImageUrl,omitempty"`
}

type Questions struct {
	Questions map[string]QuestionData `json:"Questions"` // key is questionId
}

// -----------------------------------------------------------------------
// -----------------------------------------------------------------------

type CategoryIDRequestData struct {
	CategoryID string `json:"category_id"`
}

type QuestionRemoveRequestData struct {
	CategoryID string `json:"category_id"`
	QuestionID string `json:"question_id"`
}

// -----------------------------------------------------------------------
// -----------------------------------------------------------------------

func processQuizGameConfigJSON(jsonData string) error {
	var data QuizConfig
	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return err
	} else {
		quizConfig = data
	}
	return nil
}

type QuizConfig struct {
	RemoveTwoWrongAnswerCoinCost int `json:"removeTwoWrongAnswerCoinCost"`
	PassQuestionCoinCost         int `json:"passQuestionCoinCost"`
	AutoCorrectCoinCost          int `json:"autoCorrectCoinCost"`
	HintCoinCost                 int `json:"hintCoinCost"`

	MaxLifelinesPerLevel int  `json:"maxLifelinesPerLevel"`
	AdRefillEnabled      bool `json:"adRefillEnabled"`

	CorrectAnswerCoinReward int `json:"correctAnswerCoinReward"`
	WrongAnswerCoinReward   int `json:"wrongAnswerCoinReward"`
	StreakBonusCoinReward   int `json:"streakBonusCoinReward"`
	FastAnswerCoinReward    int `json:"fastAnswerCoinReward"`
	FastAnswerThresholdSec  int `json:"fastAnswerThresholdSec"`
	NumberOfQuestions       int `json:"numberOfQuestions"`
}

type QuizFinishGameData struct {
	IsWinner bool `json:"is_winner"`
	Coins    int  `json:"coins"`
	Points   int  `json:"points"`
}
