package ludo

import "github.com/heroiclabs/nakama-common/runtime"

const (
	ludoBotMatchModule             = "ludo_bot_match"
	ludoBotMatchRequestStorageKey       = "bot_match_request_"
	ludoOnlineBotMatchRequestStorageKey = "online_bot_match_request_"
	ludoBotMatchTickRate           = 10
	ludoBotTokensPerPlayer         = 4
	ludoBotHomePosition            = 57
	ludoBotBasePosition            = -1
	ludoBotDefaultHumanLevel       = 1
	ludoBotDefaultCountry          = "IN"
	ludoBotDefaultAvatarID         = 1
	ludoBotMaxConsecutiveSixes     = 3
	ludoBotDiceAnimationDelayTicks = 12
)

const (
	OpMatchStart    int64 = 1
	OpTurnStart     int64 = 2
	OpRollDice      int64 = 3
	OpDiceResult    int64 = 4
	OpSelectToken   int64 = 5
	OpTokenMoved    int64 = 6
	OpTokenCaptured int64 = 7
	OpExtraTurn     int64 = 8
	OpStateSync     int64 = 9
	OpMatchFinished int64 = 10
	OpActionError   int64 = 11
)

const (
	PhaseWaitingForHuman = "waiting_for_human"
	PhaseWaitingForRoll  = "waiting_for_roll"
	PhaseWaitingForMove  = "waiting_for_move"
	PhaseAnimatingMove   = "animating_move"
	PhaseTurnEnd         = "turn_end"
	PhaseGameFinished    = "game_finished"
)

type ludoBotMatchCreateOptions struct {
	HumanUserID   string
	Mode          string
	Difficulty    BotDifficulty
	RequestID     string
	IncludeBot    bool
	StorageKey    string
	StorageRecord ludoBotMatchRequestRecord
}

type ludoBotMatchCreateResult struct {
	MatchID    string
	Mode       string
	Difficulty string
}

type ludoBotMatchConfig struct {
	MatchID       string
	Mode          string
	HumanUserID   string
	IncludeBot    bool
	BotDifficulty BotDifficulty
	RequestID     string
}

type LudoBotMatchCreateRequest struct {
	Mode       string `json:"mode"`
	Difficulty string `json:"difficulty"`
	RequestID  string `json:"request_id"`
}

type LudoBotMatchCreateResponse struct {
	Success bool   `json:"success"`
	MatchID string `json:"match_id"`
	HasBot  bool   `json:"has_bot"`
	Mode    string `json:"mode"`
}

type LudoOnlineBotMatchCreateRequest struct {
	ArenaName   string `json:"arena_name"`
	PlayerCount int    `json:"player_count"`
	Difficulty  string `json:"difficulty"`
	RequestID   string `json:"request_id"`
}

type LudoOnlineBotMatchCreateResponse struct {
	Success          bool                        `json:"success"`
	MatchID          string                      `json:"match_id"`
	ArenaName        string                      `json:"arena_name,omitempty"`
	EntryFeeRequired bool                        `json:"entry_fee_required"`
	EntryFeeAmount   int                         `json:"entry_fee_amount"`
	Players          []LudoOnlineBotMatchPlayer `json:"players"`
}

type LudoOnlineBotMatchPlayer struct {
	UserID      string `json:"user_id"`
	DisplayName string `json:"display_name"`
	Avatar      string `json:"avatar"`
	PlayerID    int    `json:"player_id"`
	Seat        int    `json:"seat"`
	Color       string `json:"color"`
	IsBot       bool   `json:"is_bot"`
}

type ludoBotMatchRequestRecord struct {
	MatchID     string `json:"match_id"`
	Mode        string `json:"mode"`
	Difficulty  string `json:"difficulty"`
	ArenaName   string `json:"arena_name,omitempty"`
	PlayerCount int    `json:"player_count,omitempty"`
}

type LudoToken struct {
	ID       int  `json:"id"`
	Position int  `json:"position"`
	Finished bool `json:"finished"`
}

type LudoPlayer struct {
	ID            string        `json:"id"`
	UserID        string        `json:"user_id,omitempty"`
	Name          string        `json:"name"`
	AvatarID      int           `json:"avatar_id"`
	Level         int           `json:"level"`
	Country       string        `json:"country"`
	Seat          int           `json:"seat"`
	Color         string        `json:"color"`
	IsBot         bool          `json:"is_bot"`
	BotDifficulty BotDifficulty `json:"bot_difficulty,omitempty"`
	Tokens        []LudoToken   `json:"tokens"`
	Rank          int           `json:"rank,omitempty"`
}

type LegalMove struct {
	TokenID          int   `json:"token_id"`
	FromPosition     int   `json:"from_position"`
	ToPosition       int   `json:"to_position"`
	Path             []int `json:"path"`
	LeavesBase       bool  `json:"leaves_base"`
	Captures         bool  `json:"captures"`
	CapturedPlayerID string `json:"captured_player_id"`
	CapturedTokenID  int   `json:"captured_token_id"`
	ReachesSafe      bool  `json:"reaches_safe"`
	ReachesHome      bool  `json:"reaches_home"`
	FinishesToken    bool  `json:"finishes_token"`
	ExposedAfter     bool  `json:"exposed_after"`
}

type ScoredMove struct {
	LegalMove
	Score int `json:"score"`
}

type LudoMatchState struct {
	MatchID          string                     `json:"match_id"`
	Mode             string                     `json:"mode"`
	HumanUserID      string                     `json:"human_user_id"`
	IncludeBot       bool                       `json:"include_bot"`
	BotDifficulty    BotDifficulty              `json:"bot_difficulty"`
	RequestID        string                     `json:"request_id"`
	Phase            string                     `json:"phase"`
	Players          map[string]*LudoPlayer     `json:"players"`
	PlayerOrder      []string                   `json:"player_order"`
	CurrentPlayerID  string                     `json:"current_player_id"`
	CurrentDice      int                        `json:"current_dice"`
	MovableTokens    []int                      `json:"movable_tokens"`
	LegalMoves       []LegalMove                `json:"legal_moves"`
	TurnNumber       int                        `json:"turn_number"`
	BotActionTick    int64                      `json:"bot_action_tick"`
	LastTick         int64                      `json:"last_tick"`
	BotPendingMove   bool                       `json:"bot_pending_move"`
	Ranks            []string                   `json:"ranks"`
	MatchFinished    bool                       `json:"match_finished"`
	WinnerID         string                     `json:"winner_id,omitempty"`
	ConsecutiveSixes map[string]int             `json:"consecutive_sixes"`
	Presences        map[string]runtime.Presence `json:"-"`
}
