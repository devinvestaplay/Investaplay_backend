package ludo

import (
	"encoding/json"
	"sort"

	"github.com/heroiclabs/nakama-common/runtime"
)

// Unity rules use passed=0 at base, 1 on entry, 52..57 in the home column.
// These rules are selected explicitly; the original bot wire contract is unchanged.
type ludoOnlineState struct {
	Protocol int `json:"protocol"`
	Rolls []int `json:"rolls"`
	RollsAvailable int `json:"rolls_available"`
	Sixes int `json:"sixes"`
	Moves []ludoOnlineMove `json:"moves"`
	Ready map[string]bool `json:"-"`
	Unlucky map[string][2]int `json:"-"`
	DeadlineTick int64 `json:"-"`
	TurnMissed bool `json:"-"`
	LastBroadcastVersion int64 `json:"-"`
}

type ludoOnlineMove struct {
	ID int `json:"id"`
	TokenID int `json:"token_id"`
	Dice int `json:"dice"`
	To int `json:"to"`
	CapturedPlayer string `json:"captured_player,omitempty"`
	CapturedToken int `json:"captured_token"`
}

func initializeOnlineRules(state *LudoMatchState, params map[string]interface{}) {
	if ids, ok := params["users"].([]string); ok && len(ids) >= 2 {
		state.Players = map[string]*LudoPlayer{}
		state.PlayerOrder = nil
		state.IncludeBot = false
		for seat, id := range ids { addLudoPlayerToState(state, newLudoHumanPlayer(id, seat, ludoBotPlayerColor(seat))) }
		state.CurrentPlayerID = ids[0]
	}
	state.Online = &ludoOnlineState{Protocol:2, Rolls:[]int{}, Moves:[]ludoOnlineMove{}, Ready:map[string]bool{}, Unlucky:map[string][2]int{}}
	for id, player := range state.Players {
		for i := range player.Tokens { player.Tokens[i].Position = 0 }
		when, _ := secureRandomInt(5)
		state.Online.Unlucky[id] = [2]int{1, when+1}
	}
}

func onlineBoardPosition(player *LudoPlayer, passed, count int) int {
	seat := player.Seat
	if count == 2 { seat *= 2 }
	return (seat*13 + passed-1)%52
}

func onlineSafe(position int) bool { return position%13 == 0 || position%13 == 8 }

func onlineLegalMoves(state *LudoMatchState) []ludoOnlineMove {
	player := state.Players[state.CurrentPlayerID]
	moves := []ludoOnlineMove{}
	for _, token := range player.Tokens {
		seen := map[int]bool{}
		for _, dice := range state.Online.Rolls {
			if seen[dice] || token.Finished || token.Position < 0 { continue }
			seen[dice] = true
			to := token.Position+dice
			if token.Position == 0 {
				if dice != 6 { continue }; to = 1
			}
			if to > 57 { continue }
			move := ludoOnlineMove{ID:len(moves), TokenID:token.ID, Dice:dice, To:to, CapturedToken:-1}
			blocked := false
			// Opponent stacks block the shared track, never a private home column.
			if token.Position > 0 && token.Position < 52 {
				for passed := token.Position; passed <= to && passed < 52; passed++ {
					pos := onlineBoardPosition(player, passed, len(state.Players))
					if onlineSafe(pos) { continue }
					for _, id := range state.PlayerOrder {
						opponent := state.Players[id]
						if id == player.ID || opponent.Rank > 0 { continue }
						count := 0
						for _, other := range opponent.Tokens {
							if other.Position > 0 && other.Position < 52 && onlineBoardPosition(opponent, other.Position, len(state.Players)) == pos {
								count++
								if passed == to { move.CapturedPlayer = id; move.CapturedToken = other.ID }
							}
						}
						if count > 1 { blocked = true }
						if move.CapturedPlayer != "" { break }
					}
				}
			}
			if !blocked { moves = append(moves, move) }
		}
	}
	return moves
}

func onlineSetPhase(state *LudoMatchState, phase string) {
	state.Phase = phase
	state.Online.DeadlineTick = state.LastTick + 5*ludoBotMatchTickRate
	state.BotActionTick = state.LastTick + ludoBotDiceAnimationDelayTicks
}

func onlineNextTurn(state *LudoMatchState) {
	if state.Online.TurnMissed && state.Presences[state.CurrentPlayerID] == nil {
		state.MissedTurns[state.CurrentPlayerID]++
		if state.MissedTurns[state.CurrentPlayerID] >= 5 { onlineForfeit(state, state.CurrentPlayerID) }
	}
	if onlineFinish(state) { return }
	state.CurrentPlayerID = nextPlayerID(state, false)
	state.TurnNumber++
	state.Online.TurnMissed = false
	state.Online.Rolls = []int{}
	state.Online.RollsAvailable = 1
	state.Online.Sixes = 0
	state.CurrentDice = 0
	state.Online.Moves = []ludoOnlineMove{}
	onlineSetPhase(state, PhaseWaitingForRoll)
}

func onlineAdvance(state *LudoMatchState) {
	if onlineFinish(state) { return }
	state.Online.Moves = onlineLegalMoves(state)
	if state.Players[state.CurrentPlayerID].Rank > 0 || state.Online.Sixes >= 3 {
		onlineNextTurn(state)
	} else if len(state.Online.Moves) > 0 {
		onlineSetPhase(state, PhaseWaitingForMove)
	} else if state.Online.RollsAvailable > 0 {
		onlineSetPhase(state, PhaseWaitingForRoll)
	} else { onlineNextTurn(state) }
}

func onlineDice(state *LudoMatchState) (int, error) {
	p := state.Players[state.CurrentPlayerID]
	allowed := []int{}
	for dice:=1; dice<=6; dice++ {
		forbidden := false
		for _, token := range p.Tokens {
			if token.Position < 0 || token.Position >= 52 || (token.Position == 0 && dice != 6) { continue }
			to := token.Position+dice
			if token.Position == 0 { to = 1 }
			if to >= 52 { continue }
			for _, other := range p.Tokens {
				if other.ID != token.ID && other.Position == to { forbidden = true }
			}
		}
		if !forbidden { allowed = append(allowed,dice) }
	}
	if len(allowed)==0 { allowed=[]int{1,2,3,4,5,6} }
	i, err := secureRandomInt(len(allowed)); if err!=nil { return 0,err }
	dice := allowed[i]
	if unlucky, ok:=state.Online.Unlucky[p.ID]; ok {
		if unlucky[0]==unlucky[1] { dice=6 }
		unlucky[0]++; state.Online.Unlucky[p.ID]=unlucky
	}
	return dice,nil
}

func onlineRoll(state *LudoMatchState) bool {
	dice,err:=onlineDice(state); if err!=nil { return false }
	state.CurrentDice=dice
	state.Online.Rolls=append(state.Online.Rolls,dice)
	state.Online.RollsAvailable--
	if dice==6 { state.Online.Sixes++; state.Online.RollsAvailable++ }
	onlineAdvance(state)
	return true
}

func onlineMove(state *LudoMatchState, move ludoOnlineMove) {
	p:=state.Players[state.CurrentPlayerID]
	p.Tokens[move.TokenID].Position=move.To
	p.Tokens[move.TokenID].Finished=move.To==57
	for i,dice:=range state.Online.Rolls { if dice==move.Dice { state.Online.Rolls=append(state.Online.Rolls[:i],state.Online.Rolls[i+1:]...); break } }
	if move.CapturedPlayer!="" {
		state.Players[move.CapturedPlayer].Tokens[move.CapturedToken].Position=0
		state.Online.RollsAvailable++; state.Online.Sixes=0
	}
	if allTokensFinished(p) { p.Rank=len(state.Ranks)+1; state.Ranks=append(state.Ranks,p.ID) } else if move.To==57 { state.Online.RollsAvailable++; state.Online.Sixes=0 }
	for id,player:=range state.Players {
		unlucky:=true
		for _,token:=range player.Tokens { if token.Position>0 && token.Position<57 { unlucky=false } }
		if !unlucky { delete(state.Online.Unlucky,id) } else if _,ok:=state.Online.Unlucky[id]; !ok { when,_:=secureRandomInt(6); state.Online.Unlucky[id]=[2]int{1,when+1} }
	}
	onlineAdvance(state)
}

func onlineForfeit(state *LudoMatchState, id string) {
	p:=state.Players[id]; if p.Rank>0 { return }
	p.Rank=len(state.Players)
	for i:=range p.Tokens { p.Tokens[i].Position=-100 }
}

func onlineFinish(state *LudoMatchState) bool {
	if state.MatchFinished { return true }
	remaining:=0
	for _,p:=range state.Players { if p.Rank==0 { remaining++ } }
	if remaining>1 { return false }
	for _,id:=range state.PlayerOrder { if state.Players[id].Rank==0 { state.Players[id].Rank=len(state.Ranks)+1; state.Ranks=append(state.Ranks,id) } }
	state.MatchFinished=true; state.Phase=PhaseGameFinished; state.FinishedTick=state.LastTick
	state.Online.Moves=[]ludoOnlineMove{}
	for _,id:=range state.Ranks { if state.Players[id].Rank==1 { state.WinnerID=id; break } }
	return true
}

func onlineSnapshot(dispatcher runtime.MatchDispatcher,state *LudoMatchState,recipients []runtime.Presence,requestID string) {
	state.TurnDeadlineMs=0
	if !state.MatchFinished && state.Phase!=PhaseWaitingForHuman {
		remaining:=state.Online.DeadlineTick-state.LastTick; if remaining<0 { remaining=0 }
		state.TurnDeadlineMs=state.ServerTimeMs+remaining*1000/ludoBotMatchTickRate
	}
	broadcastPayloadTo(dispatcher,OpRecoverySnapshot,map[string]interface{}{"request_id":requestID,"state":state},recipients)
}

func onlineMatchLoop(dispatcher runtime.MatchDispatcher,logger runtime.Logger,state *LudoMatchState,messages []runtime.MatchData) interface{} {
	if state.MatchFinished && state.LastTick-state.FinishedTick>300*ludoBotMatchTickRate { return nil }
	for _,message:=range messages {
		if !isActiveLudoPresence(state,message) { continue }
		if message.GetOpCode()==OpRecoverySync {
			var req struct { RequestID string `json:"request_id"` }
			if len(message.GetData())<=256 { _=json.Unmarshal(message.GetData(),&req) }
			onlineSnapshot(dispatcher,state,[]runtime.Presence{message},req.RequestID)
			logger.Debug("ludo_online_sync version=%d phase=%s",state.StateVersion,state.Phase)
			continue
		}
		if message.GetOpCode()!=OpRecoveryAction { continue }
		var action struct { ludoRecoveryAction; MoveID int `json:"move_id"` }
		if len(message.GetData())>1024 || json.Unmarshal(message.GetData(),&action)!=nil || len(action.ActionID)==0 || len(action.ActionID)>64 { continue }
		key:=message.GetUserId()+":"+action.ActionID
		_,duplicate:=state.AcceptedActions[key]
		accepted:=false
		if !duplicate && !state.MatchFinished {
			if action.Kind=="ready" && state.Phase==PhaseWaitingForHuman {
				state.Online.Ready[message.GetUserId()]=true
				ready:=true
				for id,p:=range state.Players { if !p.IsBot && !state.Online.Ready[id] { ready=false } }
				if ready { state.Online.RollsAvailable=1; onlineSetPhase(state,PhaseWaitingForRoll) }
				accepted=true
			} else if action.Kind=="surrender" {
				onlineForfeit(state,message.GetUserId())
				if !onlineFinish(state) && state.CurrentPlayerID==message.GetUserId() { onlineNextTurn(state) }
				accepted=true
			} else if action.Version==state.StateVersion && state.CurrentPlayerID==message.GetUserId() {
				if action.Kind=="roll" && state.Phase==PhaseWaitingForRoll { accepted=onlineRoll(state) }
				if action.Kind=="move" && state.Phase==PhaseWaitingForMove && action.MoveID>=0 && action.MoveID<len(state.Online.Moves) { onlineMove(state,state.Online.Moves[action.MoveID]); accepted=true }
			}
		}
		if accepted {
			state.StateVersion++; state.AcceptedActions[key]=state.StateVersion
			for id,v:=range state.AcceptedActions { if v<state.StateVersion-256 { delete(state.AcceptedActions,id) } }
			logger.Debug("ludo_online_action version=%d kind=%s",state.StateVersion,action.Kind)
		}
		onlineSnapshot(dispatcher,state,[]runtime.Presence{message},action.ActionID)
	}
	if !state.MatchFinished && state.Phase!=PhaseWaitingForHuman {
		p:=state.Players[state.CurrentPlayerID]
		if (p.IsBot && state.LastTick>=state.BotActionTick) || state.LastTick>=state.Online.DeadlineTick {
			if !p.IsBot && state.Presences[p.ID]==nil { state.Online.TurnMissed=true }
			if state.Phase==PhaseWaitingForRoll { if onlineRoll(state) { state.StateVersion++ } } else if len(state.Online.Moves)>0 {
				moves:=append([]ludoOnlineMove{},state.Online.Moves...)
				sort.SliceStable(moves,func(i,j int)bool { return onlineMoveScore(moves[i])>onlineMoveScore(moves[j]) })
				onlineMove(state,moves[0]); state.StateVersion++
			}
		}
	}
	if state.StateVersion!=state.Online.LastBroadcastVersion {
		onlineSnapshot(dispatcher,state,nil,""); state.Online.LastBroadcastVersion=state.StateVersion
	}
	return state
}

func onlineMoveScore(move ludoOnlineMove) int {
	score:=move.To
	if move.CapturedPlayer!="" { score+=700 }; if move.To==57 { score+=1000 }
	return score
}
