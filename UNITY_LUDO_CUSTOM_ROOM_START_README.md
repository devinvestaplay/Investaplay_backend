# Unity Ludo Custom Room Start Flow

This guide explains how Unity should start a Ludo custom room match after the backend start-signal fix.

## Summary

Custom room gameplay starts through the backend `ludo_match_start` RPC. The backend deducts the entry fee, signals the authoritative Nakama match, changes the room state to `playing`, and broadcasts opcode `200` to all joined realtime clients.

Unity should:

1. Create or join the custom room.
2. Save `room_code`, `match_id`, and `arena_name`.
3. Join the realtime Nakama match using `match_id`.
4. Call `ludo_match_start` once per player.
5. Wait for opcode `200`.
6. Start gameplay only after opcode `200` is received.

## 1. Create Room

The host creates a room with `ludo_room_create`.

```json
{
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "room_name": "Test Room",
  "max_players": 2
}
```

Example response:

```json
{
  "status": true,
  "room_code": "123456",
  "match_id": "nakama_match_id",
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "mode": "2P_WITH_FRIENDS",
  "host_id": "host_user_id",
  "max_players": 2,
  "room_status": "open",
  "players": []
}
```

Unity must save:

```text
room_code
match_id
arena_name
```

## 2. Join Room

The guest joins with `ludo_room_join`.

```json
{
  "room_code": "123456"
}
```

The response also contains `match_id`. The guest must save the same room and match values.

## 3. Join Realtime Match

After `ludo_room_create` or `ludo_room_join` succeeds, each Unity client must join the Nakama realtime match.

```csharp
IMatch match = await socket.JoinMatchAsync(matchId);
```

Do this before expecting opcode `200` or any gameplay relay messages.

## 4. Start Match And Deduct Fee

Each player should call `ludo_match_start` once.

```json
{
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "room_code": "123456",
  "match_id": "nakama_match_id"
}
```

`match_id` is optional if `room_code` is present, but Unity should send both when available.

Expected success response:

```json
{
  "status": true,
  "code": 200,
  "message": "fee deducted successfully"
}
```

Important behavior:

- Host call deducts the host fee and signals the authoritative match to start.
- Guest call deducts the guest fee.
- Only call this once per player.
- Unity should not start gameplay immediately from this RPC response. Wait for opcode `200`.

## 5. Listen For Match Start

The backend broadcasts opcode `200` when the custom room is officially playing.

```csharp
socket.ReceivedMatchState += state =>
{
    if (state.OpCode == 200)
    {
        // CustomRoomStart
        // Hide lobby/waiting UI.
        // Load or enable the Ludo gameplay screen.
    }
};
```

## 6. Send Gameplay Messages

After opcode `200` is received, Unity can send normal match data.

```csharp
await socket.SendMatchStateAsync(matchId, opCode, payload);
```

The backend relays match messages to the other joined players only after the authoritative match state is `playing`.

## Recommended Host Flow

```text
ludo_room_create
save room_code and match_id
socket.JoinMatchAsync(match_id)
wait for guest to join room and realtime match
ludo_match_start with arena_name, room_code, match_id
wait for opcode 200
start gameplay
```

## Recommended Guest Flow

```text
ludo_room_join with room_code
save match_id
socket.JoinMatchAsync(match_id)
ludo_match_start with arena_name, room_code, match_id
wait for opcode 200
start gameplay
```

## Compatibility Note

If Unity already sends `CustomRoomStart` as opcode `200`, the backend can also accept host-sent opcode `200` while the match is waiting and transition the match to `playing`.

The recommended flow is still to start through `ludo_match_start`, because that keeps wallet deduction and authoritative match state in sync.

## Common Mistakes

- Do not start gameplay from the `ludo_match_start` RPC response alone.
- Do not send gameplay messages before receiving opcode `200`.
- Do not call `ludo_match_start` multiple times for the same player.
- Do not skip `socket.JoinMatchAsync(matchId)`.
- Do not let the guest trigger the room start. The host should trigger it.
