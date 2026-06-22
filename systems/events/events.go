package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"game-server/systems/currency"
	"game-server/utils"
	"net/http"

	"github.com/heroiclabs/nakama-common/runtime"
)

const (
	EventsCollectionName = "Events" // parent
	EventsStorageKey     = "events" // key for EventsData map

	RpcIdGetAllEvents    = "event_get_all"
	RpcIdAddEvent        = "event_add"
	RpcIdRemoveEvent     = "event_remove"
	RpcIdGetEventRewards = "event_get_rewards"
)

var eventsData EventsData

func InitEvents(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule, initializer *runtime.Initializer) error {

	if err := (*initializer).RegisterRpc(RpcIdGetAllEvents, getAllEvents); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(RpcIdAddEvent, addEvent); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(RpcIdRemoveEvent, removeEvent); err != nil {
		return err
	}

	if err := (*initializer).RegisterRpc(RpcIdGetEventRewards, getEventRewards); err != nil {
		return err
	}

	if err := loadEvents(ctx, logger, nk); err != nil {
		(*logger).Error("Failed to load events: ", err)
	}

	return nil
}

func loadEvents(ctx *context.Context, logger *runtime.Logger, nk *runtime.NakamaModule) error {

	eventsData = EventsData{
		Events: make(map[string]EventData),
	}

	jsonData, err := utils.ReadServerStorageObjectByKey(ctx, nk, EventsCollectionName, EventsStorageKey)
	if err != nil {
		(*logger).Warn("Events storage not found. Initializing empty map.")
		return nil
	}

	if len(jsonData) == 0 {
		return nil
	}

	if err := json.Unmarshal([]byte(jsonData), &eventsData); err != nil {
		return err
	}

	return nil
}

func getAllEvents(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	resp, err := utils.SerializeObjectToString(&eventsData)
	if err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	return resp, nil
}

func addEvent(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	var ev EventData
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &ev); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}

	if ev.EventID == "" {
		return utils.CreateStatus(false, http.StatusBadRequest, "missing eventId"), nil
	}

	// Validate time window
	if ev.EndSecondsUnix != 0 && ev.StartSecondsUnix != 0 && ev.EndSecondsUnix <= ev.StartSecondsUnix {
		return utils.CreateStatus(false, http.StatusBadRequest, "endUnix must be greater than startUnix"), nil
	}

	// Ensure map exists
	if eventsData.Events == nil {
		eventsData.Events = make(map[string]EventData)
	}

	if _, exists := eventsData.Events[ev.EventID]; exists {
		return utils.CreateStatus(false, http.StatusConflict, "event already exists"), nil
	}

	eventsData.Events[ev.EventID] = ev

	jsonStr, err := utils.SerializeObjectToString(&eventsData)
	if err != nil {
		logger.Error("Events: failed to serialize events data: ", err)
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, EventsCollectionName, EventsStorageKey, &jsonStr); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

func removeEvent(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {
	var req EventIDRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}
	if req.EventID == "" {
		return utils.CreateStatus(false, http.StatusBadRequest, "missing eventId"), nil
	}

	if eventsData.Events == nil {
		return utils.CreateStatus(false, http.StatusNotFound, "no events"), nil
	}
	if _, ok := eventsData.Events[req.EventID]; !ok {
		return utils.CreateStatus(false, http.StatusNotFound, "event_not_found"), nil
	}

	delete(eventsData.Events, req.EventID)

	jsonStr, err := utils.SerializeObjectToString(&eventsData)
	if err != nil {
		logger.Error("Events: failed to serialize events data: ", err)
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}
	if err := utils.WriteServerStorageObjectByKey(&ctx, &nk, EventsCollectionName, EventsStorageKey, &jsonStr); err != nil {
		return utils.CreateStatus(false, http.StatusInternalServerError, err.Error()), err
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

func getEventRewards(ctx context.Context, logger runtime.Logger, db *sql.DB, nk runtime.NakamaModule, payload string) (string, error) {

	userID, ok := ctx.Value(runtime.RUNTIME_CTX_USER_ID).(string)
	if !ok {
		return utils.CreateStatus(false, http.StatusUnauthorized, "invalid user"), nil
	}

	var req EventIDRequest
	if err := utils.DeserializeObjectFromStringByRefs(&payload, &req); err != nil {
		return utils.CreateStatus(false, http.StatusBadRequest, err.Error()), err
	}
	if req.EventID == "" {
		return utils.CreateStatus(false, http.StatusBadRequest, "missing eventId"), nil
	}

	if _, exists := eventsData.Events[req.EventID]; !exists {
		return utils.CreateStatus(false, http.StatusNotFound, "event not found"), nil
	}

	event := eventsData.Events[req.EventID]

	if !event.IsActive {
		return utils.CreateStatus(false, http.StatusNotFound, "event_not_active"), nil
	}

	changeset := map[string]int64{"coins": int64(event.Reward.Amount)}

	metadata := map[string]interface{}{
		"event_id": event.EventID,
	}

	_, _, err := nk.WalletUpdate(ctx, userID, changeset, metadata, true)
	if err != nil {
		logger.Error("failed to update wallet for user %s: %v", userID, err)
	}

	return utils.CreateStatus(true, http.StatusOK), nil
}

// -----------------------------------------------------------------------
// -----------------------------------------------------------------------

type GameType int

const (
	GameLudo GameType = iota
	GameSolitaire
	GameQuiz
)

type GameMode int

const (
	LudoOnline2 GameMode = iota
	LudoOnline4
	SolitaireClassic
	SolitaireChallenge
	QuizEasy
	QuizMedium
	QuizHard
	// TODO add other types
)

type EventData struct {
	EventID     string `json:"event_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	IsActive    bool   `json:"is_active"`

	// Time window
	StartSecondsUnix int64 `json:"start_seconds_unix"` // in utc
	EndSecondsUnix   int64 `json:"end_seconds_unix"`   // in utc

	// Filter
	Game     GameType `json:"game"`
	GameMode GameMode `json:"game_mode"`

	// Rules
	WinUnderSeconds int `json:"win_under_seconds"` // 300 = 5 min (if 0 = ignored)

	// Reward
	Reward currency.VirtualCurrency `json:"reward"`

	// Optional free-form metadata (kept minimal)
	Meta json.RawMessage `json:"meta,omitempty"`
}

type EventsData struct {
	Events map[string]EventData `json:"events"` // key = EventID
}

type EventIDRequest struct {
	EventID string `json:"event_id"`
}
