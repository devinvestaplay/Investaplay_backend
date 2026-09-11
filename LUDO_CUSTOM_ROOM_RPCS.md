# Ludo Custom Room RPCs

This guide is for Unity/client developers implementing Ludo custom room creation and joining through Nakama RPCs.

## Required Arena Config

Before using room RPCs, make sure the Ludo arena config exists in this shape. The wrapper key `arenas` is required.

RPC:

```text
ludo_arena_resetup
```

Request body:

```json
{
  "arenas": {
    "2P_WITH_FRIENDS_ARENA": {
      "mode": "2P_WITH_FRIENDS",
      "name": "2P_WITH_FRIENDS_ARENA",
      "enabled": true,
      "rewards": {
        "1": [
          {
            "type": 2,
            "amount": 160
          }
        ],
        "2": [
          {
            "type": 2,
            "amount": 40
          }
        ],
        "5": [
          {
            "type": 2,
            "amount": 10
          }
        ]
      },
      "fee_cost": {
        "type": 2,
        "amount": 100
      },
      "json_config": {},
      "consolation_reward": []
    },
    "4P_WITH_FRIENDS_ARENA": {
      "mode": "4P_WITH_FRIENDS",
      "name": "4P_WITH_FRIENDS_ARENA",
      "enabled": true,
      "rewards": {
        "1": [
          {
            "type": 2,
            "amount": 300
          }
        ],
        "2": [
          {
            "type": 2,
            "amount": 60
          }
        ],
        "3": [
          {
            "type": 2,
            "amount": 40
          }
        ],
        "5": [
          {
            "type": 2,
            "amount": 20
          }
        ]
      },
      "fee_cost": {
        "type": 2,
        "amount": 100
      },
      "json_config": {},
      "consolation_reward": []
    }
  }
}
```

Check current arena config:

RPC:

```text
ludo_arena_get
```

Request body:

```json
{}
```

The response must contain:

```json
{
  "arenas": {
    "2P_WITH_FRIENDS_ARENA": {
      "enabled": true
    }
  }
}
```

## Client Flow

Host flow:

```text
1. Call ludo_room_create.
2. Save room_code and match_id from response.
3. Call ludo_match_start to deduct entry fee.
4. Join realtime match using match_id.
5. Share room_code with friend.
```

Friend flow:

```text
1. Call ludo_room_join with room_code.
2. Save match_id from response.
3. Call ludo_match_start to deduct entry fee.
4. Join realtime match using match_id.
```

Finish flow:

```text
1. Submit final ranking with ludo_match_finish.
2. Server distributes rewards based on arena config.
```

## Create Room

RPC:

```text
ludo_room_create
```

2-player request:

```json
{
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "room_name": "Test Room",
  "max_players": 2
}
```

4-player request:

```json
{
  "arena_name": "4P_WITH_FRIENDS_ARENA",
  "room_name": "Test Room",
  "max_players": 4
}
```

Success response:

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
  "players": [
    {
      "user_id": "host_user_id",
      "username": "player1",
      "joined_at": 1783700000,
      "is_host": true
    }
  ]
}
```

Common error:

```json
{
  "status": false,
  "code": 404,
  "message": "arena not found"
}
```

Fix: call `ludo_arena_get` and confirm the response has `arenas.2P_WITH_FRIENDS_ARENA`.

Common error:

```json
{
  "status": false,
  "code": 403,
  "message": "arena disabled"
}
```

Fix: add `"enabled": true` to the arena config and call `ludo_arena_resetup`.

## Join Room

RPC:

```text
ludo_room_join
```

Request:

```json
{
  "room_code": "123456"
}
```

Success response:

```json
{
  "status": true,
  "room_code": "123456",
  "match_id": "nakama_match_id",
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "mode": "2P_WITH_FRIENDS",
  "host_id": "host_user_id",
  "max_players": 2,
  "room_status": "full",
  "players": [
    {
      "user_id": "host_user_id",
      "username": "player1",
      "joined_at": 1783700000,
      "is_host": true
    },
    {
      "user_id": "friend_user_id",
      "username": "player2",
      "joined_at": 1783700020,
      "is_host": false
    }
  ]
}
```

Common full-room error:

```json
{
  "status": false,
  "code": 409,
  "message": "room is full"
}
```

## Get Room

RPC:

```text
ludo_room_get
```

Request:

```json
{
  "room_code": "123456"
}
```

Success response:

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

## Leave Room

RPC:

```text
ludo_room_leave
```

Request:

```json
{
  "room_code": "123456"
}
```

Success response:

```json
{
  "status": true,
  "room_code": "123456",
  "match_id": "nakama_match_id",
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "mode": "2P_WITH_FRIENDS",
  "host_id": "host_user_id",
  "max_players": 2,
  "room_status": "closed",
  "players": []
}
```

If the host leaves, the room is closed.

## Deduct Entry Fee

Room creation and joining do not deduct coins. Each player must call this before gameplay starts.

RPC:

```text
ludo_match_start
```

2-player request:

```json
{
  "arena_name": "2P_WITH_FRIENDS_ARENA"
}
```

4-player request:

```json
{
  "arena_name": "4P_WITH_FRIENDS_ARENA"
}
```

Success response:

```json
{
  "status": true,
  "code": 200,
  "message": "fee deducted successfully"
}
```

Insufficient balance:

```json
{
  "status": false,
  "code": 402,
  "message": "insufficient balance"
}
```

Important: `ludo_match_start` can currently deduct more than once if the client calls it multiple times. The Unity client should only call it once per player per room.

## Finish Match And Reward

RPC:

```text
ludo_match_finish
```

2-player request:

```json
{
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "ranking": {
    "winner_user_id": {
      "rank": 1,
      "kills": 3,
      "deaths": 1
    },
    "loser_user_id": {
      "rank": 2,
      "kills": 1,
      "deaths": 3
    }
  }
}
```

4-player request:

```json
{
  "arena_name": "4P_WITH_FRIENDS_ARENA",
  "ranking": {
    "user_id_1": {
      "rank": 1,
      "kills": 4,
      "deaths": 1
    },
    "user_id_2": {
      "rank": 2,
      "kills": 2,
      "deaths": 2
    },
    "user_id_3": {
      "rank": 3,
      "kills": 1,
      "deaths": 3
    },
    "user_id_4": {
      "rank": 5,
      "kills": 0,
      "deaths": 4
    }
  }
}
```

Success response:

```json
{
  "status": true,
  "code": 200,
  "message": "match finished and rewards distributed"
}
```

## Realtime Match Join

After `ludo_room_create` or `ludo_room_join`, Unity must join the Nakama realtime match using the returned `match_id`.

Pseudo flow:

```text
response = RPC("ludo_room_create", createPayload)
matchId = response.match_id
socket.JoinMatchAsync(matchId)
```

For friend:

```text
response = RPC("ludo_room_join", joinPayload)
matchId = response.match_id
socket.JoinMatchAsync(matchId)
```

The authoritative match module name is:

```text
ludo_custom_room
```

Unity does not call this directly. The server uses it internally when creating the room match.

## Where Room Data Is Stored

Custom room data is stored in Nakama storage, which is backed by PostgreSQL.

Storage location:

```text
collection: Ludo
key: custom_room_<room_code>
user_id: 00000000-0000-0000-0000-000000000000
```

Example:

```text
collection: Ludo
key: custom_room_123456
user_id: 00000000-0000-0000-0000-000000000000
```

The stored value is JSON:

```json
{
  "room_code": "123456",
  "room_name": "Test Room",
  "match_id": "nakama_match_id",
  "arena_name": "2P_WITH_FRIENDS_ARENA",
  "mode": "2P_WITH_FRIENDS",
  "host_id": "host_user_id",
  "max_players": 2,
  "status": "open",
  "players": {
    "host_user_id": {
      "user_id": "host_user_id",
      "username": "player1",
      "joined_at": 1783700000,
      "is_host": true
    }
  },
  "created_at": 1783700000,
  "updated_at": 1783700000
}
```

SQL check:

```sql
SELECT collection, key, user_id, value
FROM storage
WHERE collection = 'Ludo'
  AND key LIKE 'custom_room_%';
```

## RPC Summary

```text
ludo_arena_resetup
ludo_arena_get
ludo_room_create
ludo_room_join
ludo_room_get
ludo_room_leave
ludo_match_start
ludo_match_finish
```

