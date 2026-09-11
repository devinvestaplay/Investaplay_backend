# Ludo protocol 2 migration

Unity now sends intents directly to Nakama. The host phone no longer generates dice, approves moves, advances turns, or submits paid rankings for updated online matches.

## Implemented flow

1. Public and party matchmaking include `ludo_protocol=2` and the arena name. The matchmaker callback creates `ludo_authoritative_v2`. Full private parties use `ludo_authoritative_create`. Private room lobbies switch into the same authoritative engine when the room host starts the game. Online bot fallback joins the authoritative match returned by its RPC instead of creating a separate relay match.
2. Players are seated by ascending authenticated user ID: seats 0/2 for two players and 0/1/2/3 for four. Bot seats come from the server roster. Joining is restricted to the roster; a replacement session invalidates the previous session.
3. Each human sends Ready after Unity initializes its game view. Only then does Nakama collect all human entry fees in one atomic transaction and broadcast StartGame, SelectPlayerTurn, and WaitForRollDice. Bots do not pay fees. A lobby that never starts does not collect fees.
4. A manual tap sends a request ID and expected state version. No dice value is sent by Unity. Nakama generates a uniform 1–6 value using the existing cryptographic random source, updates the rules, and broadcasts the result, legal moves, and next phase in one packet.
5. Token selection sends only the piece and an already available dice value. Nakama validates the authenticated player, current phase, piece, available dice, exact home entry, safe cells, and opposing blockades. It resolves captures, bonus rolls, three sixes, rankings, and turns.
6. Unity executes confirmed commands through its existing animations. There is no provisional roll animation, predicted result, or delivery of future dice. Ordered commands can advance in the same frame; unfinished animations remain barriers.
7. Nakama controls turn deadlines (15 seconds; the opening phase has a 5-second setup allowance). Unity displays the remaining server turn time, subtracting time spent waiting in its animation queue. A client timeout does not submit an action. The server resolves expired turns itself. Server bots choose legal moves and act on a shorter timer.
8. Retries reuse the same request ID/version. Duplicate successful requests return the cached outcome. Reconnects restore a server snapshot; missing live batches can be replayed. Surrender is acknowledged before the manager forgets the saved match.
9. Final ranks, capture/death counts, rewards, and account statistics come from server state. The result receipt and wallet/stat changes commit atomically. A create-only receipt prevents duplicate wallet rewards; failures retry while the match remains alive. The old `ludo_match_finish` RPC rejects client rankings.

## Wire protocol

| Opcode | Direction | Meaning |
| --- | --- | --- |
| 310 | Client → server | Ready; optional cosmetic display data |
| 311 | Client → server | Roll: `requestId`, `version` |
| 312 | Client → server | Move: `requestId`, `version`, `pieceId`, `moveAmount` |
| 313 | Client → server | Snapshot, or replay with `afterVersion` |
| 314 | Client → server | Surrender: `requestId`, `version` |
| 320 | Server → clients | Ordered commands, version, next phase, remaining time, request ID, handler processing time |
| 321 | Server → client | Full reconnect state and legal moves |
| 322 | Server → client(s) | Rejection or start failure |

The current SDK represents a server-originated packet with no user presence. Unity accepts gameplay results only from that source. Legacy gameplay opcodes are ignored. Emoji/quick-chat remain separate, with the sender's player ID set by the server.

The board contract preserves Unity's 52 shared cells, `passed=0` at base, entry on six at `passed=1`, and private home cells `passed=52..57`. Three sixes discard the turn. Capturing or finishing a token grants the existing bonus roll. Normal finish ranks are assigned from the top; surrender uses rank 5. Shared-track blockades do not block a private home lane. The old Unity dice weighting is deliberately not reproduced.

## Build and coordinated rollout

The Go module uses Go 1.25.0 and the existing Dockerfile pairs Nakama/pluginbuilder 3.32.0. Build the plugin with that Dockerfile; a Windows Go build cannot produce the Linux Nakama plugin.

```sh
go test -mod=vendor ./...
docker build -t investaplay-nakama:ludo-v2 .
```

Run these from `NAKAMABACKEND`. Building an image does not deploy it. This change has not been deployed to a live server.

This is a coordinated protocol migration, not an old-client-compatible backend patch. Finish/drain existing matches before replacing the server, require the updated Unity client, then open matchmaking. Updated public clients reject a relay/old match rather than silently use host authority. Older clients cannot submit Ludo entry/result RPCs under the old contract after this deployment. Offline/local games retain their existing flow.

Keep arena names, enabled status, player counts, entry fees, and reward tables consistent with the client configuration. Entry/result receipts are private server storage objects in `ludo_authoritative`.

## Validation and actual latency measurements

Local checks cover engine rules, 20 complete deterministic simulated games, malicious/stale requests, retry deduplication, manual readiness, bot readiness, atomic fee failure, settlement retry, and rejection of client rankings. `testdata/authoritative_contract.json` is a checked fixture consumed by the standalone Unity DTO test at `../Tests/LudoProtocolContract.cs`.

Unity development/editor logs provide two measurements:

- `[LudoLatency] response ...`: intent submission to the main-thread response callback. Filter `accepted=True` for accepted actions. The server microsecond field measures handler work, not network RTT or tick waiting.
- `[LudoLatency] response queue to dice animation ...`: time a confirmed roll waits in Unity's command queue.

The handler runs at 30 Hz, an approximately 33 ms tick interval. Removing the host round trip reduces actual processing hops; network RTT, scheduling, and necessary animation time still remain. A zero-delay network guarantee is not possible.

Before production approval, exercise two/four real clients on the same deployed build: public matchmaking, full/partial parties, custom rooms, bot fallback, captures, bonus rolls, final ranks, duplicate taps, background/reconnect during dice and token animations, surrender, and entry failure. Compare p50/p95/p99 response and animation-queue times against the previous build using the same devices/network/server. Select region and capacity from those measurements and concurrent-match load tests. No real-device latency baseline or production improvement percentage has been measured locally.

## Operational limits

- Live board state, replay history, and pending retries remain in the Nakama match process. A server process crash is not covered by the client reconnect snapshot path. Entry/result receipts provide an audit trail, but automatic crash recovery/refund reconciliation is not implemented; drain active games for planned restarts and reconcile unmatched entry receipts after an unplanned failure.
- Leaderboard increments remain a best-effort projection after wallet settlement; leaderboard failure must never trigger duplicate wallet payouts. Account metadata follows the existing account update model; concurrent unrelated metadata writers can still require a broader account-statistics concurrency solution.
- Local source compilation and tests do not replace a Linux plugin build or multiplayer Unity device testing. Docker was not available in this workspace, so the deployment image has not been built here.
