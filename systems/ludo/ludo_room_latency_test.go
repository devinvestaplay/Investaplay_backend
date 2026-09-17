package ludo

import (
	"context"
	"testing"

	"github.com/heroiclabs/nakama-common/runtime"
)

type relayTestPresence struct {
	runtime.Presence
	user, session string
}

func (p relayTestPresence) GetUserId() string    { return p.user }
func (p relayTestPresence) GetSessionId() string { return p.session }

type relayTestMessage struct {
	relayTestPresence
	op   int64
	data []byte
}

func (m relayTestMessage) GetOpCode() int64      { return m.op }
func (m relayTestMessage) GetData() []byte       { return m.data }
func (m relayTestMessage) GetReliable() bool     { return true }
func (m relayTestMessage) GetReceiveTime() int64 { return 0 }

type relayTestDispatcher struct {
	runtime.MatchDispatcher
	ops     []int64
	senders []runtime.Presence
}

func (d *relayTestDispatcher) BroadcastMessage(op int64, data []byte, recipients []runtime.Presence, sender runtime.Presence, reliable bool) error {
	d.ops = append(d.ops, op)
	d.senders = append(d.senders, sender)
	return nil
}

func TestCustomRoomSchedulingInterval(t *testing.T) {
	_, rate, _ := (&LudoCustomRoomMatch{}).MatchInit(context.Background(), nil, nil, nil, map[string]interface{}{})
	if rate < 30 || rate > 60 {
		t.Fatalf("expected scheduling interval <= 34ms within Nakama limits, got %d Hz", rate)
	}
}

func TestCustomRoomLobbyRejectsClientDiceAndMoveCommands(t *testing.T) {
	player := relayTestPresence{user: "player", session: "session"}
	state := &LudoCustomRoomMatchState{
		Protocol:  2,
		Status:    ludoCustomRoomStatusPlaying,
		Presences: map[string]runtime.Presence{"session": player},
	}
	dispatcher := &relayTestDispatcher{}
	messages := []runtime.MatchData{
		relayTestMessage{relayTestPresence: player, op: 4, data: []byte(`{"commandName":"RollDice"}`)},
		relayTestMessage{relayTestPresence: player, op: 5, data: []byte(`{"commandName":"PlayerMove"}`)},
	}
	result := (&LudoCustomRoomMatch{}).MatchLoop(context.Background(), nil, nil, nil, dispatcher, 1, state, messages)
	if result != state || len(dispatcher.ops) != 0 {
		t.Fatal("lobby accepted host gameplay commands")
	}
}
