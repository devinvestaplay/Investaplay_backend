package ludo

import (
	"errors"
	"fmt"
	"sort"
)

// Protocol 2 uses Unity's 52-cell board and passed=0 (base), 1..51
// (shared track), 52..57 (private home lane). Dice are uniform 1..6.
const authoritativeModule = "ludo_authoritative_v2"
const authoritativeTickRate = 30
const authoritativeTurnTicks = 15 * authoritativeTickRate
const (
	authReady int64 = 310 + iota
	authRoll
	authMove
	authSync
	authSurrender
)
const (
	authBatch int64 = 320 + iota
	authSnapshot
	authError
)

type authCommand map[string]interface{}
type authPiece struct {
	PlayerID     int  `json:"playerId"`
	PieceID      int  `json:"pieceId"`
	Position     int  `json:"position"`
	Passed       int  `json:"passed"`
	OnHomeColumn bool `json:"onHomeColumn"`
}
type authPlayer struct {
	Bot      bool                   `json:"isBot"`
	Kills    int                    `json:"kills"`
	Deaths   int                    `json:"deaths"`
	ID       int                    `json:"id"`
	UserID   string                 `json:"userId"`
	Start    int                    `json:"startPosition"`
	Finished int                    `json:"piecesFinished"`
	Pieces   []authPiece            `json:"pieces"`
	Profile  map[string]interface{} `json:"profile"`
}
type authMoveData struct {
	Dice    int  `json:"diceValue"`
	Passed  int  `json:"passed"`
	PieceID int  `json:"pieceID"`
	Usable  bool `json:"usable"`
	Capture bool `json:"captureOpponent"`
}
type authGame struct {
	Players        []*authPlayer
	Current        int
	Rolls          []int
	Available      int
	Sixes          int
	Phase          string
	Number         int
	Version        int
	Deadline       int64
	BotActionTick  int64
	Ranks          map[int]int
	Left           map[int]bool
	Commands       []authCommand
	HumanModels    map[int]authHumanBehavior
	HumanBaselines map[int]authHumanBehavior
}

type authMoveResult struct {
	Dice        int
	PieceID     int
	OldPosition int
	NewPosition int
}

type authDestination struct {
	Passed   int
	Position int
}

var errOwnTokenOccupied = errors.New("OWN_TOKEN_OCCUPIED")

func newAuthGame(players []*authPlayer) *authGame {
	for _, p := range players {
		p.Start = p.ID * 13
		p.Pieces = make([]authPiece, 4)
		for i := range p.Pieces {
			p.Pieces[i] = authPiece{PlayerID: p.ID, PieceID: i, Position: -1}
		}
	}
	return &authGame{Players: players, Current: players[0].ID, Rolls: []int{}, Ranks: map[int]int{}, Left: map[int]bool{}, Phase: "ready", HumanModels: map[int]authHumanBehavior{}, HumanBaselines: map[int]authHumanBehavior{}}
}

func (g *authGame) clone() *authGame {
	cloned := *g
	cloned.Rolls = append([]int(nil), g.Rolls...)
	cloned.Players = make([]*authPlayer, len(g.Players))
	for i, player := range g.Players {
		copyPlayer := *player
		copyPlayer.Pieces = append([]authPiece(nil), player.Pieces...)
		cloned.Players[i] = &copyPlayer
	}
	cloned.Ranks = make(map[int]int, len(g.Ranks))
	for playerID, rank := range g.Ranks {
		cloned.Ranks[playerID] = rank
	}
	cloned.Left = make(map[int]bool, len(g.Left))
	for playerID, left := range g.Left {
		cloned.Left[playerID] = left
	}
	cloned.Commands = append([]authCommand(nil), g.Commands...)
	cloned.HumanModels = make(map[int]authHumanBehavior, len(g.HumanModels))
	for playerID, model := range g.HumanModels {
		cloned.HumanModels[playerID] = model
	}
	cloned.HumanBaselines = make(map[int]authHumanBehavior, len(g.HumanBaselines))
	for playerID, model := range g.HumanBaselines {
		cloned.HumanBaselines[playerID] = model
	}
	return &cloned
}

func (g *authGame) validateCanonicalState() error {
	if len(g.Players) != 2 && len(g.Players) != 4 {
		return fmt.Errorf("invalid player count %d", len(g.Players))
	}
	if g.player(g.Current) == nil {
		return fmt.Errorf("current player %d does not exist", g.Current)
	}
	seenPlayers := map[int]bool{}
	for _, player := range g.Players {
		if player == nil || seenPlayers[player.ID] {
			return errors.New("nil or duplicate player")
		}
		seenPlayers[player.ID] = true
		if len(player.Pieces) != 4 {
			return fmt.Errorf("player %d has %d pieces", player.ID, len(player.Pieces))
		}
		occupied := map[string]int{}
		for index, piece := range player.Pieces {
			if piece.PlayerID != player.ID || piece.PieceID != index {
				return fmt.Errorf("player %d piece %d identity mismatch", player.ID, index)
			}
			if g.Left[player.ID] && piece.Passed == -100 && piece.Position == -1 {
				continue
			}
			if piece.Passed < 0 || piece.Passed > 57 {
				return fmt.Errorf("player %d piece %d has invalid progress %d", player.ID, index, piece.Passed)
			}
			expectedPosition := -1
			if piece.Passed > 0 && piece.Passed < 52 {
				expectedPosition = (player.Start + piece.Passed - 1) % 52
			} else if piece.Passed >= 52 {
				expectedPosition = piece.Passed - 52
			}
			if piece.Position != expectedPosition || piece.OnHomeColumn != (piece.Passed >= 52) {
				return fmt.Errorf("player %d piece %d position is not canonical", player.ID, index)
			}
			if piece.Passed > 0 && piece.Passed < 57 {
				zone := "track"
				if piece.Passed >= 52 {
					zone = "home"
				}
				key := fmt.Sprintf("%s:%d", zone, piece.Position)
				if other, exists := occupied[key]; exists {
					return fmt.Errorf("player %d pieces %d and %d occupy the same playable square", player.ID, other, index)
				}
				occupied[key] = index
			}
		}
	}
	switch g.Phase {
	case "ready", "roll", "move", "finished":
	default:
		return fmt.Errorf("invalid phase %q", g.Phase)
	}
	if g.Phase == "move" && (len(g.Rolls) == 0 || len(g.moves()) == 0) {
		return errors.New("move phase has no authoritative legal move")
	}
	return nil
}
func (g *authGame) player(id int) *authPlayer {
	for _, p := range g.Players {
		if p.ID == id {
			return p
		}
	}
	return nil
}
func (g *authGame) emit(name string, data authCommand) {
	if data == nil {
		data = authCommand{}
	}
	g.Number++
	data["commandName"] = name
	data["commandNumber"] = g.Number
	g.Commands = append(g.Commands, data)
}
func (g *authGame) start(tick int64) {
	g.emit("StartGame", nil)
	g.selectTurn(g.Current, tick)
	// Initial board reveal lasts several seconds in Unity.
	g.Deadline += 5 * authoritativeTickRate
	if g.BotActionTick > 0 {
		g.BotActionTick += 5 * authoritativeTickRate
	}
}
func (g *authGame) selectTurn(id int, tick int64) {
	g.Current = id
	g.Rolls = []int{}
	g.Available = 1
	g.Sixes = 0
	g.emit("SelectPlayerTurn", authCommand{"playerId": id})
	g.advance(tick)
}
func (g *authGame) next(tick int64) {
	for i, p := range g.Players {
		if p.ID == g.Current {
			for offset := 1; offset <= len(g.Players); offset++ {
				next := g.Players[(i+offset)%len(g.Players)].ID
				if g.Ranks[next] == 0 && !g.Left[next] {
					g.selectTurn(next, tick)
					return
				}
			}
		}
	}
}
func authSafe(pos int) bool { return pos%13 == 0 || pos%13 == 8 }
func (g *authGame) destination(p *authPlayer, piece authPiece, dice int) (int, int) {
	if piece.Passed == 0 {
		return 1, p.Start
	}
	passed := piece.Passed + dice
	if passed >= 52 {
		return passed, passed - 52
	}
	return passed, (p.Start + passed - 1) % 52
}

// canLandOnSquare is the single same-color occupancy rule used by every
// authoritative human and bot move. Base, completed, surrendered and virtual
// positions are not playable landing squares and are intentionally excluded.
func (g *authGame) canLandOnSquare(playerID, movingTokenID int, destination authDestination) bool {
	if destination.Passed <= 0 || destination.Passed >= 57 {
		return true
	}
	player := g.player(playerID)
	if player == nil {
		return false
	}
	destinationInHome := destination.Passed >= 52
	for _, piece := range player.Pieces {
		if piece.PieceID == movingTokenID || piece.Passed <= 0 || piece.Passed >= 57 {
			continue
		}
		pieceInHome := piece.Passed >= 52
		if pieceInHome == destinationInHome && piece.Position == destination.Position {
			return false
		}
	}
	return true
}
func (g *authGame) blocked(id, pos int) bool {
	if authSafe(pos) {
		return false
	}
	for _, p := range g.Players {
		if p.ID == id || g.Left[p.ID] {
			continue
		}
		count := 0
		for _, pc := range p.Pieces {
			if pc.Passed > 0 && pc.Passed < 52 && pc.Position == pos {
				count++
			}
		}
		if count > 1 {
			return true
		}
	}
	return false
}
func (g *authGame) victim(id, pos int) (*authPlayer, int) {
	if authSafe(pos) {
		return nil, -1
	}
	for _, p := range g.Players {
		if p.ID == id || g.Left[p.ID] {
			continue
		}
		for i, pc := range p.Pieces {
			if pc.Passed > 0 && pc.Passed < 52 && pc.Position == pos {
				return p, i
			}
		}
	}
	return nil, -1
}
func (g *authGame) legalWithoutOwnOccupancy(pieceID, dice int) bool {
	p := g.player(g.Current)
	if p == nil || pieceID < 0 || pieceID >= 4 || dice < 1 || dice > 6 {
		return false
	}
	pc := p.Pieces[pieceID]
	if pc.Passed < 0 {
		return false
	}
	if pc.Passed == 0 {
		return dice == 6
	}
	if pc.Passed+dice > 57 {
		return false
	}
	// Home lanes are private; do not check another player's shared-track cells.
	for step := 0; step <= dice && pc.Passed+step < 52; step++ {
		if g.blocked(p.ID, (p.Start+pc.Passed+step-1)%52) {
			return false
		}
	}
	return true
}

func (g *authGame) legal(pieceID, dice int) bool {
	if !g.legalWithoutOwnOccupancy(pieceID, dice) {
		return false
	}
	player := g.player(g.Current)
	passed, position := g.destination(player, player.Pieces[pieceID], dice)
	return g.canLandOnSquare(player.ID, pieceID, authDestination{Passed: passed, Position: position})
}
func (g *authGame) moves() map[int][]authMoveData {
	result := map[int][]authMoveData{}
	p := g.player(g.Current)
	for i, pc := range p.Pieces {
		for _, dice := range g.Rolls {
			if !g.legal(i, dice) {
				continue
			}
			passed, pos := g.destination(p, pc, dice)
			capture := false
			if passed < 52 {
				v, _ := g.victim(p.ID, pos)
				capture = v != nil
			}
			result[i] = append(result[i], authMoveData{dice, pc.Passed, i, true, capture})
		}
	}
	return result
}
func (g *authGame) advance(tick int64) {
	if g.Phase == "finished" {
		return
	}
	if g.Sixes >= 3 {
		g.next(tick)
		return
	}
	moves := g.moves()
	g.Deadline = tick + authoritativeTurnTicks
	if len(moves) > 0 {
		g.Phase = "move"
		g.emit("WaitForPlayerMove", authCommand{"playerId": g.Current, "moveablePieces": moves})
		scheduleAuthoritativeBotAction(g, tick)
		return
	}
	allFinal := true
	for _, pc := range g.player(g.Current).Pieces {
		if pc.Passed < 52 {
			allFinal = false
		}
	}
	if g.Available > 0 && !(allFinal && len(g.Rolls) > 0) {
		g.Phase = "roll"
		g.emit("WaitForRollDice", authCommand{"playerId": g.Current})
		scheduleAuthoritativeBotAction(g, tick)
		return
	}
	g.BotActionTick = 0
	g.next(tick)
}
func (g *authGame) roll(dice int, tick int64) error {
	if g.Phase != "roll" || dice < 1 || dice > 6 {
		return errors.New("not waiting for a roll")
	}
	g.Rolls = append(g.Rolls, dice)
	g.Available--
	if dice == 6 {
		g.Available++
		g.Sixes++
	}
	g.emit("RollDice", authCommand{"playerId": g.Current, "diceValue": dice, "rollState": 0, "isReroll": false})
	g.advance(tick)
	return nil
}
func (g *authGame) move(pieceID, dice int, tick int64) error {
	if g.Phase != "move" {
		return errors.New("illegal move")
	}
	if g.legalWithoutOwnOccupancy(pieceID, dice) {
		player := g.player(g.Current)
		passed, position := g.destination(player, player.Pieces[pieceID], dice)
		if !g.canLandOnSquare(player.ID, pieceID, authDestination{Passed: passed, Position: position}) {
			return errOwnTokenOccupied
		}
	}
	if !g.legal(pieceID, dice) {
		return errors.New("illegal move")
	}
	index := -1
	for i, v := range g.Rolls {
		if v == dice {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("dice value not available")
	}
	p := g.player(g.Current)
	pc := &p.Pieces[pieceID]
	passed, pos := g.destination(p, *pc, dice)
	pc.Passed = passed
	pc.Position = pos
	pc.OnHomeColumn = passed >= 52
	g.Rolls = append(g.Rolls[:index], g.Rolls[index+1:]...)
	g.emit("PlayerMove", authCommand{"playerId": p.ID, "pieceId": pieceID, "moveAmount": dice})
	if passed == 57 {
		p.Finished++
		if p.Finished < 4 {
			g.Available++
			g.Sixes = 0
		}
	}
	if p.Finished == 4 {
		g.Ranks[p.ID] = g.nextRank()
		if g.finishIfNeeded(p.ID) {
			return nil
		}
		g.emitFinish(p.ID, false)
		g.next(tick)
		return nil
	}
	if passed < 52 {
		v, i := g.victim(p.ID, pos)
		if v != nil {
			p.Kills++
			v.Deaths++
			v.Pieces[i].Passed = 0
			v.Pieces[i].Position = -1
			v.Pieces[i].OnHomeColumn = false
			g.Available++
			g.Sixes = 0
			g.emit("CapturePiece", authCommand{"playerId": p.ID, "pieceId": pieceID})
		}
	}
	g.advance(tick)
	return nil
}

// moveRequested treats moveAmount as an optional compatibility assertion.
// The dice and destination always come from current authoritative state.
func (g *authGame) moveRequested(pieceID, claimedDice int, tick int64) (authMoveResult, error) {
	if g.Phase != "move" {
		return authMoveResult{}, errors.New("not waiting for a move")
	}
	if pieceID < 0 || pieceID >= 4 {
		return authMoveResult{}, errors.New("illegal token selection")
	}
	if claimedDice != 0 {
		available := false
		for _, dice := range g.Rolls {
			if dice == claimedDice {
				available = true
				break
			}
		}
		if !available {
			return authMoveResult{Dice: claimedDice, PieceID: pieceID, OldPosition: g.player(g.Current).Pieces[pieceID].Position, NewPosition: -1}, errors.New("move amount does not match authoritative dice")
		}
		player := g.player(g.Current)
		if g.legalWithoutOwnOccupancy(pieceID, claimedDice) {
			passed, position := g.destination(player, player.Pieces[pieceID], claimedDice)
			if !g.canLandOnSquare(player.ID, pieceID, authDestination{Passed: passed, Position: position}) {
				return authMoveResult{Dice: claimedDice, PieceID: pieceID, OldPosition: player.Pieces[pieceID].Position, NewPosition: position}, errOwnTokenOccupied
			}
		}
	}
	options := g.moves()[pieceID]
	if len(options) == 0 {
		player := g.player(g.Current)
		for _, dice := range g.Rolls {
			if claimedDice != 0 && dice != claimedDice {
				continue
			}
			if !g.legalWithoutOwnOccupancy(pieceID, dice) {
				continue
			}
			passed, position := g.destination(player, player.Pieces[pieceID], dice)
			if !g.canLandOnSquare(player.ID, pieceID, authDestination{Passed: passed, Position: position}) {
				return authMoveResult{Dice: dice, PieceID: pieceID, OldPosition: player.Pieces[pieceID].Position, NewPosition: position}, errOwnTokenOccupied
			}
		}
		return authMoveResult{}, errors.New("selected token cannot move")
	}
	selectedDice := options[0].Dice
	if claimedDice != 0 {
		matched := false
		for _, option := range options {
			if option.Dice == claimedDice {
				selectedDice = option.Dice
				matched = true
				break
			}
		}
		if !matched {
			return authMoveResult{}, errors.New("move amount does not match authoritative dice")
		}
	}
	player := g.player(g.Current)
	oldPosition := player.Pieces[pieceID].Position
	_, newPosition := g.destination(player, player.Pieces[pieceID], selectedDice)
	if err := g.move(pieceID, selectedDice, tick); err != nil {
		return authMoveResult{}, err
	}
	return authMoveResult{Dice: selectedDice, PieceID: pieceID, OldPosition: oldPosition, NewPosition: newPosition}, nil
}
func (g *authGame) emitFinish(id int, complete bool) {
	ranks := map[int]int{}
	for k, v := range g.Ranks {
		ranks[k] = v
	}
	g.emit("FinishGame", authCommand{"playerReachedFinal": id, "playerReachedFinalRank": g.Ranks[id], "playerRankings": ranks, "matchCompleted": complete})
}
func (g *authGame) finishIfNeeded(id int) bool {
	active := []int{}
	for _, p := range g.Players {
		if g.Ranks[p.ID] == 0 && !g.Left[p.ID] {
			active = append(active, p.ID)
		}
	}
	if len(active) > 1 {
		return false
	}
	if len(active) == 1 {
		g.Ranks[active[0]] = g.nextRank()
	}
	g.Phase = "finished"
	g.emitFinish(id, true)
	return true
}
func (g *authGame) surrender(id int, tick int64) {
	if g.Phase == "finished" || g.Left[id] || g.Ranks[id] > 0 {
		return
	}
	g.Left[id] = true
	g.Ranks[id] = 5
	for i := range g.player(id).Pieces {
		g.player(id).Pieces[i].Passed = -100
		g.player(id).Pieces[i].Position = -1
		g.player(id).Pieces[i].OnHomeColumn = false
	}
	g.player(id).Finished = 0
	g.emit("ServerPlayerLeft", authCommand{"playerId": id})
	if !g.finishIfNeeded(id) && id == g.Current {
		g.next(tick)
	}
}
func (g *authGame) nextRank() int {
	for rank := 1; rank <= 4; rank++ {
		used := false
		for _, r := range g.Ranks {
			if r == rank {
				used = true
			}
		}
		if !used {
			return rank
		}
	}
	return 5
}
func (g *authGame) timeout(tick int64) error {
	if g.Phase == "roll" {
		dice, err := rollDice()
		if err != nil {
			return err
		}
		return g.roll(dice, tick)
	}
	if g.Phase == "move" {
		moves := g.moves()
		ids := []int{}
		for id := range moves {
			ids = append(ids, id)
		}
		sort.Ints(ids)
		if len(ids) > 0 {
			return g.move(ids[0], moves[ids[0]][0].Dice, tick)
		}
	}
	return nil
}
