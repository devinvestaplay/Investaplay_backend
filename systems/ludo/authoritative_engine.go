package ludo

import (
	"errors"
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
	Players       []*authPlayer
	Current       int
	Rolls         []int
	Available     int
	Sixes         int
	Phase         string
	Number        int
	Version       int
	Deadline      int64
	BotActionTick int64
	Ranks         map[int]int
	Left          map[int]bool
	Commands      []authCommand
}

func newAuthGame(players []*authPlayer) *authGame {
	for _, p := range players {
		p.Start = p.ID * 13
		p.Pieces = make([]authPiece, 4)
		for i := range p.Pieces {
			p.Pieces[i] = authPiece{PlayerID: p.ID, PieceID: i, Position: -1}
		}
	}
	return &authGame{Players: players, Current: players[0].ID, Rolls: []int{}, Ranks: map[int]int{}, Left: map[int]bool{}, Phase: "ready"}
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
func (g *authGame) legal(pieceID, dice int) bool {
	p := g.player(g.Current)
	if p == nil || pieceID < 0 || pieceID >= 4 || dice < 1 || dice > 6 {
		return false
	}
	pc := p.Pieces[pieceID]
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
	if g.Phase != "move" || !g.legal(pieceID, dice) {
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
