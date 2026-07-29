# Fauxtist Frontend + Lobby Completion Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Fauxtist playable end-to-end in the browser: finish the backend lobby-join wiring, build the React interface, and validate a full game locally across multiple browser windows.

**Architecture:** Part A closes the Plan 1 gap so joining players enter the engine roster and rounds scale to player count. Part B is a React + Vite SPA with one WebSocket hook feeding a pure reducer, so message-handling logic is unit-testable. Part C is a manual multi-window validation gate. Deployment (go:embed + Docker + Render) is deliberately a separate Plan 3, written only after local validation passes.

**Tech Stack:** Go 1.26 (existing backend), React 18 + Vite 5, Vitest for the reducer, plain CSS. Node 22 (already installed).

**Convention:** keep code comments minimal — only genuinely non-obvious "why". No narration comments.

---

## File Structure

```
fauxtist/
  internal/game/engine.go        # + UpsertPlayer, MaxPlayers, TotalRounds fix
  internal/game/errors.go        # + ErrRoomFull
  internal/room/room.go          # join adds to roster, returns error; leave broadcasts
  internal/room/broadcast.go     # + lobby_update / player_left broadcasts
  internal/wsproto/message.go    # + TypeLobbyUpdate constant
  internal/server/server.go      # handle Join error (reject full/started rooms)
  web/
    package.json
    vite.config.js               # dev proxy /api + /ws -> :8080
    index.html
    src/
      main.jsx
      App.jsx                     # screen router driven by game phase
      api.js                      # POST /api/rooms
      protocol.js                 # message-type constants (mirror wsproto)
      reducer.js                  # pure reduce(state, msg)
      reducer.test.js             # Vitest
      useRoomSocket.js            # WS connection + dispatch
      components/
        Landing.jsx               # create / join
        Lobby.jsx                 # player list + start (host)
        Canvas.jsx                # normalized-coordinate drawing surface
        GameBoard.jsx             # canvas + turn / word-or-category panel
        Chat.jsx                  # discussion chat
        Voting.jsx                # vote buttons + tally
        Reveal.jsx                # round result + impostor guess input
        GameOver.jsx              # final standings
      styles.css
```

---

# Part A — Backend Lobby Completion

## Task A1: Engine roster growth and round scaling

**Files:**
- Modify: `internal/game/engine.go`
- Modify: `internal/game/errors.go`
- Test: `internal/game/engine_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
func TestUpsertPlayerAddsDuringLobby(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "z", Name: "Zoe"}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if len(e.State().Players) != 5 {
		t.Fatalf("players = %d, want 5", len(e.State().Players))
	}
}

func TestUpsertPlayerRenamesExisting(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "a", Name: "Alice2"}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if len(e.State().Players) != 4 {
		t.Fatalf("players = %d, want 4 (rename, not add)", len(e.State().Players))
	}
	if e.State().Players[0].Name != "Alice2" {
		t.Fatalf("name = %q, want Alice2", e.State().Players[0].Name)
	}
}

func TestUpsertPlayerRejectsNewAfterStart(t *testing.T) {
	e := startedEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "z", Name: "Zoe"}); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase", err)
	}
}

func TestUpsertPlayerRejectsWhenFull(t *testing.T) {
	e := newTestEngine(t, MaxPlayers)
	if err := e.UpsertPlayer(Player{ID: "over", Name: "Over"}); err != ErrRoomFull {
		t.Fatalf("err = %v, want ErrRoomFull", err)
	}
}

func TestStartGameScalesRoundsToPlayers(t *testing.T) {
	e := newTestEngine(t, 4)
	_ = e.UpsertPlayer(Player{ID: "e", Name: "Eve"}) // now 5 players
	if _, err := e.StartGame(PlayerID("a")); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	if e.State().TotalRounds != 5 {
		t.Fatalf("totalRounds = %d, want 5", e.State().TotalRounds)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run 'TestUpsert|TestStartGameScales' -v`
Expected: FAIL — `undefined: MaxPlayers`, `e.UpsertPlayer undefined`, `undefined: ErrRoomFull`.

- [ ] **Step 3: Add the error and constant**

In `internal/game/errors.go`, add to the `var (...)` block:
```go
	ErrRoomFull = errors.New("room is full")
```
And below `MinPlayers`:
```go
// MaxPlayers is the maximum roster size for a room.
const MaxPlayers = 8
```

- [ ] **Step 4: Add UpsertPlayer and scale rounds at StartGame**

Append to `internal/game/engine.go`:
```go
// UpsertPlayer adds a new player during the lobby, or renames an existing one
// (any phase, for reconnects). New players are rejected once the game has
// started or the room is full.
func (e *Engine) UpsertPlayer(p Player) error {
	if i := e.playerIndex(p.ID); i >= 0 {
		if p.Name != "" {
			e.state.Players[i].Name = p.Name
		}
		return nil
	}
	if e.state.Phase != PhaseLobby {
		return ErrWrongPhase
	}
	if len(e.state.Players) >= MaxPlayers {
		return ErrRoomFull
	}
	e.state.Players = append(e.state.Players, p)
	return nil
}
```

In `StartGame`, set the round count from the current roster. Change:
```go
	e.impostorOrder = e.rng.Perm(len(e.state.Players))
	return e.beginRound(1)
```
to:
```go
	e.state.TotalRounds = len(e.state.Players)
	e.impostorOrder = e.rng.Perm(len(e.state.Players))
	return e.beginRound(1)
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/game/ -run 'TestUpsert|TestStartGameScales' -v`
Expected: PASS.

- [ ] **Step 6: Run the full game suite (no regressions) and commit**

Run: `go test ./internal/game/ -cover`
Expected: PASS, coverage ≥ 85%.
```bash
git add internal/game/
git commit -m "feat(game): roster growth via UpsertPlayer, scale rounds to player count"
```

---

## Task A2: Room adds joiners to the roster and broadcasts lobby changes

**Files:**
- Modify: `internal/wsproto/message.go`
- Modify: `internal/room/room.go`
- Modify: `internal/room/broadcast.go`

- [ ] **Step 1: Add the lobby_update message type**

In `internal/wsproto/message.go`, add to the server-to-client constants:
```go
	TypeLobbyUpdate = "lobby_update"
```

- [ ] **Step 2: Make Join roster-aware and error-returning**

In `internal/room/room.go`, replace the `joinReq` type and the `Join`/`Run` join handling.

Change `joinReq`:
```go
type joinReq struct {
	client *Client
	resp   chan error
}
```

Change the `Run` loop's join case from:
```go
		case j := <-r.joins:
			r.clients[j.client.PlayerID] = j.client
			r.sendSnapshot(j.client)
			close(j.resp)
```
to:
```go
		case j := <-r.joins:
			err := r.engine.UpsertPlayer(game.Player{ID: j.client.PlayerID, Name: j.client.Name})
			if err != nil {
				j.resp <- err
				continue
			}
			r.clients[j.client.PlayerID] = j.client
			r.sendSnapshot(j.client)
			r.broadcastLobby()
			j.resp <- nil
```

Change `Join` from:
```go
func (r *Room) Join(c *Client) {
	resp := make(chan struct{})
	r.joins <- joinReq{client: c, resp: resp}
	<-resp
}
```
to:
```go
func (r *Room) Join(c *Client) error {
	resp := make(chan error, 1)
	r.joins <- joinReq{client: c, resp: resp}
	return <-resp
}
```

Change the `Run` loop's leave case from:
```go
		case id := <-r.leaves:
			delete(r.clients, id)
```
to:
```go
		case id := <-r.leaves:
			delete(r.clients, id)
			r.broadcastPlayerLeft(id)
```

- [ ] **Step 3: Add the lobby broadcast helpers**

Append to `internal/room/broadcast.go`:
```go
func (r *Room) broadcastLobby() {
	s := r.engine.State()
	env, err := wsproto.Encode(wsproto.TypeLobbyUpdate, map[string]any{
		"players": s.Players,
		"hostId":  string(s.HostID),
	})
	if err == nil {
		r.broadcast(env)
	}
}

func (r *Room) broadcastPlayerLeft(id game.PlayerID) {
	env, err := wsproto.Encode(wsproto.TypePlayerLeft, map[string]any{"id": string(id)})
	if err == nil {
		r.broadcast(env)
	}
}

// broadcastReveal tells clients who was caught when entering the reveal phase.
// The word is withheld from the impostor, who still has to guess it.
func (r *Room) broadcastReveal() {
	res := r.engine.State().LastResult
	if res == nil {
		return
	}
	for id, c := range r.clients {
		payload := map[string]any{
			"impostorId": string(res.ImpostorID),
			"caught":     res.Caught,
			"tally":      res.Tally,
		}
		if id != res.ImpostorID {
			payload["word"] = res.Word
		}
		if env, err := wsproto.Encode(wsproto.TypeRoundResult, payload); err == nil {
			c.trySend(env)
		}
	}
}
```

- [ ] **Step 3b: Emit the reveal data when the phase becomes reveal**

In `internal/room/broadcast.go`, the `broadcastEvent` function's `PhaseChanged` case currently reads:
```go
	case game.PhaseChanged:
		env, _ := wsproto.Encode(wsproto.TypePhaseChanged, wsproto.PhaseChangedPayload{Phase: string(e.Phase)})
		r.broadcast(env)
		r.onPhaseChange(e.Phase)
```
Change it to also push the reveal snapshot when entering reveal:
```go
	case game.PhaseChanged:
		env, _ := wsproto.Encode(wsproto.TypePhaseChanged, wsproto.PhaseChangedPayload{Phase: string(e.Phase)})
		r.broadcast(env)
		if e.Phase == game.PhaseReveal {
			r.broadcastReveal()
		}
		r.onPhaseChange(e.Phase)
```

- [ ] **Step 4: Verify the package builds and vets**

Run: `go build ./internal/room/ && go vet ./internal/room/`
Expected: exit 0. (The server calls `Join` without checking the error yet — that is fixed in Task A3, which compiles because the return value may be ignored... note: Go does NOT error on ignored return values, so this builds.)

- [ ] **Step 5: Commit**

```bash
git add internal/wsproto/message.go internal/room/
git commit -m "feat(room): add joiners to engine roster, broadcast lobby/player-left"
```

---

## Task A3: Server rejects full / started rooms on join

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/server/server_test.go`:
```go
func TestJoinRejectedWhenRoomFull(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	// The host pre-seat occupies one slot; fill the remaining 7 with players.
	conns := []*websocket.Conn{}
	for i := 0; i < 7; i++ {
		c := dial(t, wsURL, "P"+string(rune('a'+i)))
		conns = append(conns, c)
		_ = readEnv(t, c) // drain room_state
	}
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()

	// The 9th participant (roster already 8) must be rejected: the connection
	// is closed by the server, so a Read returns an error.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	over, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer over.Close(websocket.StatusNormalClosure, "")
	join, _ := wsproto.Encode(wsproto.TypeJoin, wsproto.JoinPayload{Name: "TooMany"})
	jb, _ := json.Marshal(join)
	_ = over.Write(ctx, websocket.MessageText, jb)
	if _, _, err := over.Read(ctx); err == nil {
		t.Fatal("expected rejected join to close the connection")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestJoinRejectedWhenRoomFull -v`
Expected: FAIL — the over-capacity client is currently registered and receives a `room_state` instead of being closed.

- [ ] **Step 3: Handle the Join error in the server**

In `internal/server/server.go`, change:
```go
	c := room.NewClientForServer(playerID, name, conn)
	rm.Join(c)
	defer rm.Leave(playerID)
```
to:
```go
	c := room.NewClientForServer(playerID, name, conn)
	if err := rm.Join(c); err != nil {
		conn.Close(websocket.StatusPolicyViolation, "join rejected")
		return
	}
	defer rm.Leave(playerID)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestJoinRejectedWhenRoomFull -v`
Expected: PASS.

- [ ] **Step 5: Run full suite and commit**

Run: `go test ./... `
Expected: all PASS.
```bash
git add internal/server/
git commit -m "feat(server): reject joins to full or in-progress rooms"
```

---

# Part B — React Frontend

## Task B1: Vite + React scaffold with dev proxy

**Files:**
- Create: `web/package.json`, `web/vite.config.js`, `web/index.html`, `web/src/main.jsx`, `web/src/styles.css`

- [ ] **Step 1: Scaffold the app**

Run:
```bash
cd web 2>/dev/null || mkdir web && cd web
npm create vite@latest . -- --template react
```
If the CLI prompts about a non-empty directory, choose "Ignore files and continue". Then:
```bash
npm install
npm install --save-dev vitest
```

- [ ] **Step 2: Replace `web/vite.config.js`**

```js
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/ws': { target: 'ws://localhost:8080', ws: true },
    },
  },
  test: { environment: 'node' },
})
```

- [ ] **Step 3: Replace `web/index.html`**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Fauxtist</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.jsx"></script>
  </body>
</html>
```

- [ ] **Step 4: Replace `web/src/main.jsx`**

```jsx
import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.jsx'
import './styles.css'

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>,
)
```

- [ ] **Step 5: Create `web/src/styles.css`**

```css
:root { --bg:#12121a; --panel:#1e1e2b; --accent:#7c5cff; --text:#e8e8f0; --muted:#9a9ab0; }
* { box-sizing: border-box; }
body { margin:0; background:var(--bg); color:var(--text); font-family: system-ui, sans-serif; }
button { background:var(--accent); color:#fff; border:0; border-radius:8px; padding:10px 16px; font-size:14px; cursor:pointer; }
button:disabled { opacity:.4; cursor:not-allowed; }
input { background:var(--panel); border:1px solid #33334a; color:var(--text); border-radius:8px; padding:10px; font-size:14px; }
.center { min-height:100vh; display:flex; align-items:center; justify-content:center; }
.card { background:var(--panel); padding:24px; border-radius:14px; width:min(720px,92vw); }
.row { display:flex; gap:12px; align-items:center; }
.col { display:flex; flex-direction:column; gap:12px; }
.muted { color:var(--muted); }
.players li { padding:6px 0; }
.me { color:var(--accent); font-weight:600; }
canvas { background:#fff; border-radius:12px; touch-action:none; width:100%; aspect-ratio:4/3; }
.badge { background:#33334a; border-radius:6px; padding:2px 8px; font-size:12px; }
```

- [ ] **Step 6: Delete unused scaffold files**

Run: `rm -f web/src/App.css web/src/index.css web/src/assets/react.svg`
(These come from the Vite template; `App.jsx` is fully replaced in Task B6.)

- [ ] **Step 7: Commit**

```bash
cd .. && git add web/ && git commit -m "chore(web): vite + react scaffold with dev proxy"
```

---

## Task B2: Protocol constants and pure reducer (with tests)

**Files:**
- Create: `web/src/protocol.js`, `web/src/reducer.js`, `web/src/reducer.test.js`

- [ ] **Step 1: Write the failing test**

Create `web/src/reducer.test.js`:
```js
import { describe, it, expect } from 'vitest'
import { reduce, initialState } from './reducer.js'
import { T } from './protocol.js'

describe('reduce', () => {
  it('initializes from room_state', () => {
    const s = reduce(initialState(), {
      type: T.RoomState,
      payload: { phase: 'lobby', players: [{ id: 'a', name: 'A', score: 0 }], hostId: 'a' },
    })
    expect(s.phase).toBe('lobby')
    expect(s.players).toHaveLength(1)
    expect(s.hostId).toBe('a')
  })

  it('replaces players on lobby_update', () => {
    let s = reduce(initialState(), { type: T.LobbyUpdate, payload: { players: [{ id: 'a' }, { id: 'b' }], hostId: 'a' } })
    expect(s.players).toHaveLength(2)
  })

  it('appends strokes on stroke_broadcast', () => {
    let s = initialState()
    s = reduce(s, { type: T.StrokeBroadcast, payload: { by: 'a', points: [{ x: 0.1, y: 0.1 }] } })
    expect(s.strokes).toHaveLength(1)
  })

  it('sets phase and clears strokes on round_started', () => {
    let s = initialState()
    s = reduce(s, { type: T.StrokeBroadcast, payload: { by: 'a', points: [] } })
    s = reduce(s, { type: T.RoundStarted, payload: { round: 1, category: 'Animal', word: 'Giraffe', youAreImpostor: false } })
    expect(s.phase).toBe('drawing')
    expect(s.strokes).toHaveLength(0)
    expect(s.word).toBe('Giraffe')
    expect(s.round).toBe(1)
  })

  it('tracks current drawer and phase changes', () => {
    let s = reduce(initialState(), { type: T.TurnChanged, payload: { currentPlayer: 'b', lap: 0, totalLaps: 2 } })
    expect(s.currentPlayer).toBe('b')
    s = reduce(s, { type: T.PhaseChanged, payload: { phase: 'voting' } })
    expect(s.phase).toBe('voting')
  })

  it('records round result and game over', () => {
    let s = reduce(initialState(), { type: T.RoundResult, payload: { impostorId: 'a', word: 'Giraffe', caught: true } })
    expect(s.lastResult.caught).toBe(true)
    s = reduce(s, { type: T.GameOver, payload: { finalScores: [{ id: 'a', score: 2 }] } })
    expect(s.phase).toBe('game_over')
    expect(s.finalScores).toHaveLength(1)
  })

  it('accumulates chat', () => {
    let s = reduce(initialState(), { type: T.ChatBroadcast, payload: { from: 'a', text: 'hi' } })
    expect(s.chat).toHaveLength(1)
    expect(s.chat[0].text).toBe('hi')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/reducer.test.js`
Expected: FAIL — cannot resolve `./reducer.js` / `./protocol.js`.

- [ ] **Step 3: Write the protocol constants**

Create `web/src/protocol.js`:
```js
export const T = {
  // client -> server
  Join: 'join',
  StartGame: 'start_game',
  Stroke: 'stroke',
  ChatMessage: 'chat_message',
  CastVote: 'cast_vote',
  ImpostorGuess: 'impostor_guess',
  EndDiscussion: 'end_discussion',
  // server -> client
  RoomState: 'room_state',
  LobbyUpdate: 'lobby_update',
  PlayerLeft: 'player_left',
  RoundStarted: 'round_started',
  StrokeBroadcast: 'stroke_broadcast',
  TurnChanged: 'turn_changed',
  PhaseChanged: 'phase_changed',
  VoteUpdate: 'vote_update',
  RoundResult: 'round_result',
  GameOver: 'game_over',
  ChatBroadcast: 'chat_broadcast',
  Error: 'error',
}
```

- [ ] **Step 4: Write the reducer**

Create `web/src/reducer.js`:
```js
import { T } from './protocol.js'

export function initialState() {
  return {
    phase: 'connecting',
    players: [],
    hostId: null,
    round: 0,
    totalRounds: 0,
    category: '',
    word: null,
    youAreImpostor: false,
    currentPlayer: null,
    lap: 0,
    totalLaps: 2,
    strokes: [],
    votesCast: 0,
    votesTotal: 0,
    lastResult: null,
    finalScores: null,
    chat: [],
    error: null,
  }
}

export function reduce(state, msg) {
  const p = msg.payload || {}
  switch (msg.type) {
    case T.RoomState:
      return {
        ...state,
        phase: p.phase,
        players: p.players || [],
        hostId: p.hostId ?? state.hostId,
        round: p.round ?? 0,
        totalRounds: p.totalRounds ?? 0,
        category: p.category || '',
        word: p.word ?? null,
        youAreImpostor: !!p.youAreImpostor,
        strokes: p.strokes || [],
        lap: p.lap ?? 0,
        totalLaps: p.totalLaps ?? 2,
        lastResult: p.lastResult ?? null,
      }
    case T.LobbyUpdate:
      return { ...state, players: p.players || [], hostId: p.hostId ?? state.hostId }
    case T.PlayerLeft:
      return { ...state, players: state.players.map((pl) => (pl.id === p.id ? { ...pl, gone: true } : pl)) }
    case T.RoundStarted:
      return {
        ...state,
        phase: 'drawing',
        round: p.round,
        category: p.category,
        word: p.word ?? null,
        youAreImpostor: !!p.youAreImpostor,
        strokes: [],
        lastResult: null,
        votesCast: 0,
      }
    case T.StrokeBroadcast:
      return { ...state, strokes: [...state.strokes, p] }
    case T.TurnChanged:
      return { ...state, currentPlayer: p.currentPlayer, lap: p.lap, totalLaps: p.totalLaps }
    case T.PhaseChanged:
      return { ...state, phase: p.phase }
    case T.VoteUpdate:
      return { ...state, votesCast: p.votesCast, votesTotal: p.votesTotal }
    case T.RoundResult:
      return { ...state, lastResult: p, phase: 'reveal' }
    case T.GameOver:
      return { ...state, phase: 'game_over', finalScores: p.finalScores || [] }
    case T.ChatBroadcast:
      return { ...state, chat: [...state.chat, p] }
    case T.Error:
      return { ...state, error: p.message }
    default:
      return state
  }
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/reducer.test.js`
Expected: PASS (7 tests).

- [ ] **Step 6: Commit**

```bash
cd .. && git add web/src/protocol.js web/src/reducer.js web/src/reducer.test.js
git commit -m "feat(web): protocol constants and unit-tested message reducer"
```

---

## Task B3: WebSocket hook

**Files:**
- Create: `web/src/api.js`, `web/src/useRoomSocket.js`

- [ ] **Step 1: Write the room API helper**

Create `web/src/api.js`:
```js
export async function createRoom(name) {
  const res = await fetch('/api/rooms', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  })
  if (!res.ok) throw new Error('could not create room')
  return res.json() // { code, hostToken }
}
```

- [ ] **Step 2: Write the socket hook**

Create `web/src/useRoomSocket.js`:
```js
import { useEffect, useRef, useReducer, useCallback } from 'react'
import { reduce, initialState } from './reducer.js'

export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const wsRef = useRef(null)

  useEffect(() => {
    if (!code || !join) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws/room/${code}`)
    wsRef.current = ws
    ws.onopen = () => ws.send(JSON.stringify({ type: 'join', payload: join }))
    ws.onmessage = (e) => {
      try { dispatch(JSON.parse(e.data)) } catch { /* ignore malformed */ }
    }
    return () => ws.close()
  }, [code, join])

  const send = useCallback((type, payload = {}) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, payload }))
  }, [])

  return { state, send }
}
```

- [ ] **Step 3: Verify it compiles (build the app)**

Run: `cd web && npx vite build`
Expected: build succeeds (hooks are imported by `App.jsx` in Task B6; for now confirm no syntax errors by building — it will succeed even if unused).

- [ ] **Step 4: Commit**

```bash
cd .. && git add web/src/api.js web/src/useRoomSocket.js
git commit -m "feat(web): room API helper and websocket hook"
```

---

## Task B4: Canvas drawing surface

**Files:**
- Create: `web/src/components/Canvas.jsx`

Coordinates are normalized to [0,1] so every client renders identically regardless of pixel size. The active drawer emits one stroke per `onStrokeComplete`.

- [ ] **Step 1: Write the canvas component**

Create `web/src/components/Canvas.jsx`:
```jsx
import { useRef, useEffect, useCallback } from 'react'

export default function Canvas({ strokes, canDraw, onStrokeComplete }) {
  const ref = useRef(null)
  const drawing = useRef(false)
  const current = useRef([])

  const redraw = useCallback(() => {
    const cv = ref.current
    if (!cv) return
    const ctx = cv.getContext('2d')
    const { width: w, height: h } = cv
    ctx.clearRect(0, 0, w, h)
    const paint = (pts, color, width) => {
      if (!pts || pts.length === 0) return
      ctx.strokeStyle = color || '#111'
      ctx.lineWidth = (width || 3) * (w / 800)
      ctx.lineJoin = ctx.lineCap = 'round'
      ctx.beginPath()
      pts.forEach((pt, i) => {
        const x = pt.x * w, y = pt.y * h
        i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y)
      })
      ctx.stroke()
    }
    strokes.forEach((s) => paint(s.points, s.color, s.width))
    paint(current.current, '#111', 3)
  }, [strokes])

  useEffect(() => { redraw() }, [redraw])

  useEffect(() => {
    const cv = ref.current
    if (!cv) return
    const resize = () => {
      cv.width = cv.clientWidth
      cv.height = cv.clientHeight
      redraw()
    }
    resize()
    window.addEventListener('resize', resize)
    return () => window.removeEventListener('resize', resize)
  }, [redraw])

  const pos = (e) => {
    const r = ref.current.getBoundingClientRect()
    return { x: (e.clientX - r.left) / r.width, y: (e.clientY - r.top) / r.height }
  }
  const start = (e) => { if (!canDraw) return; drawing.current = true; current.current = [pos(e)]; redraw() }
  const move = (e) => { if (!drawing.current) return; current.current.push(pos(e)); redraw() }
  const end = () => {
    if (!drawing.current) return
    drawing.current = false
    const pts = current.current
    current.current = []
    if (pts.length > 0) onStrokeComplete({ points: pts, color: '#111', width: 3 })
  }

  return (
    <canvas
      ref={ref}
      onPointerDown={start}
      onPointerMove={move}
      onPointerUp={end}
      onPointerLeave={end}
      style={{ cursor: canDraw ? 'crosshair' : 'default' }}
    />
  )
}
```

- [ ] **Step 2: Build to verify**

Run: `cd web && npx vite build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
cd .. && git add web/src/components/Canvas.jsx
git commit -m "feat(web): normalized-coordinate drawing canvas"
```

---

## Task B5: Presentational components (Landing, Lobby, GameBoard, Chat, Voting, Reveal, GameOver)

**Files:**
- Create the seven component files under `web/src/components/`.

- [ ] **Step 1: Landing**

Create `web/src/components/Landing.jsx`:
```jsx
import { useState } from 'react'
import { createRoom } from '../api.js'

export default function Landing({ onEnter }) {
  const [name, setName] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const host = async () => {
    if (!name.trim()) return setErr('Enter a name')
    setBusy(true); setErr('')
    try {
      const { code, hostToken } = await createRoom(name.trim())
      onEnter({ code, join: { name: name.trim(), reconnectToken: hostToken } })
    } catch { setErr('Could not create room'); setBusy(false) }
  }
  const join = () => {
    if (!name.trim()) return setErr('Enter a name')
    if (!code.trim()) return setErr('Enter a room code')
    onEnter({ code: code.trim().toUpperCase(), join: { name: name.trim() } })
  }

  return (
    <div className="center">
      <div className="card col">
        <h1>Fauxtist</h1>
        <p className="muted">One of you is faking it. Draw one stroke at a time — don't get caught.</p>
        <input placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} />
        <div className="row">
          <button onClick={host} disabled={busy}>Create room</button>
        </div>
        <div className="row">
          <input placeholder="Room code" value={code} onChange={(e) => setCode(e.target.value)} />
          <button onClick={join} disabled={busy}>Join</button>
        </div>
        {err && <p style={{ color: '#ff6b6b' }}>{err}</p>}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Lobby**

Create `web/src/components/Lobby.jsx`:
```jsx
export default function Lobby({ state, meId, code, onStart }) {
  const isHost = state.hostId === meId
  const enough = state.players.length >= 4
  return (
    <div className="center">
      <div className="card col">
        <h2>Room {code}</h2>
        <p className="muted">Share this code. Need 4–8 players.</p>
        <ul className="players">
          {state.players.map((p) => (
            <li key={p.id} className={p.id === meId ? 'me' : ''}>
              {p.name} {p.id === state.hostId && <span className="badge">host</span>}
            </li>
          ))}
        </ul>
        {isHost
          ? <button onClick={onStart} disabled={!enough}>{enough ? 'Start game' : 'Waiting for players…'}</button>
          : <p className="muted">Waiting for the host to start…</p>}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: GameBoard**

Create `web/src/components/GameBoard.jsx`:
```jsx
import Canvas from './Canvas.jsx'

export default function GameBoard({ state, meId, send }) {
  const myTurn = state.currentPlayer === meId && state.phase === 'drawing'
  const drawerName = state.players.find((p) => p.id === state.currentPlayer)?.name || '…'
  return (
    <div className="card col">
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <span>Round {state.round}/{state.totalRounds} · Lap {state.lap + 1}/{state.totalLaps}</span>
        <span className="badge">
          {state.youAreImpostor ? `You are the IMPOSTOR — category: ${state.category}` : `Word: ${state.word}`}
        </span>
      </div>
      <p className="muted">{myTurn ? 'Your turn — draw ONE stroke' : `${drawerName} is drawing…`}</p>
      <Canvas
        strokes={state.strokes}
        canDraw={myTurn}
        onStrokeComplete={(s) => send('stroke', s)}
      />
    </div>
  )
}
```

- [ ] **Step 4: Chat**

Create `web/src/components/Chat.jsx`:
```jsx
import { useState } from 'react'

export default function Chat({ state, meId, send, canEndDiscussion, onEnd }) {
  const [text, setText] = useState('')
  const submit = (e) => {
    e.preventDefault()
    if (text.trim()) { send('chat_message', { text: text.trim() }); setText('') }
  }
  const nameOf = (id) => state.players.find((p) => p.id === id)?.name || id
  return (
    <div className="card col">
      <h3>Discussion</h3>
      <div style={{ maxHeight: 180, overflowY: 'auto' }}>
        {state.chat.map((m, i) => <div key={i}><b>{nameOf(m.from)}:</b> {m.text}</div>)}
      </div>
      <form className="row" onSubmit={submit}>
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Who's faking it?" />
        <button type="submit">Send</button>
      </form>
      {canEndDiscussion && <button onClick={onEnd}>Start voting</button>}
    </div>
  )
}
```

- [ ] **Step 5: Voting**

Create `web/src/components/Voting.jsx`:
```jsx
import { useState } from 'react'

export default function Voting({ state, meId, send }) {
  const [voted, setVoted] = useState(false)
  const cast = (target) => { send('cast_vote', { target }); setVoted(true) }
  return (
    <div className="card col">
      <h3>Vote for the impostor</h3>
      <p className="muted">{state.votesCast}/{state.votesTotal} voted</p>
      <div className="col">
        {state.players.map((p) => (
          <button key={p.id} disabled={voted || p.id === meId} onClick={() => cast(p.id)}>{p.name}</button>
        ))}
      </div>
    </div>
  )
}
```

- [ ] **Step 6: Reveal**

Create `web/src/components/Reveal.jsx`:
```jsx
import { useState } from 'react'

export default function Reveal({ state, meId, send }) {
  const [guess, setGuess] = useState('')
  const r = state.lastResult || {}
  const impostorName = state.players.find((p) => p.id === r.impostorId)?.name || '?'
  const iAmImpostor = r.impostorId === meId
  const submit = (e) => { e.preventDefault(); if (guess.trim()) send('impostor_guess', { guess: guess.trim() }) }
  return (
    <div className="card col">
      <h3>{r.caught ? `Caught! ${impostorName} was the impostor.` : `${impostorName} got away with it!`}</h3>
      {r.caught && iAmImpostor && !r.impostorGuess && (
        <form className="col" onSubmit={submit}>
          <p>You're caught — guess the word to steal the win:</p>
          <div className="row">
            <input value={guess} onChange={(e) => setGuess(e.target.value)} placeholder="The secret word" />
            <button type="submit">Guess</button>
          </div>
        </form>
      )}
      {r.caught && !iAmImpostor && <p className="muted">Waiting for the impostor to guess the word…</p>}
      {r.word && <p className="muted">The word was: <b>{r.word}</b></p>}
    </div>
  )
}
```

- [ ] **Step 7: GameOver**

Create `web/src/components/GameOver.jsx`:
```jsx
export default function GameOver({ state }) {
  const scores = [...(state.finalScores || [])].sort((a, b) => b.score - a.score)
  return (
    <div className="center">
      <div className="card col">
        <h2>Game over</h2>
        <ol className="players">
          {scores.map((p) => <li key={p.id}>{p.name} — {p.score}</li>)}
        </ol>
        <button onClick={() => location.reload()}>Play again</button>
      </div>
    </div>
  )
}
```

- [ ] **Step 8: Build to verify all components compile**

Run: `cd web && npx vite build`
Expected: succeeds (App.jsx still the scaffold default; wired in B6).

- [ ] **Step 9: Commit**

```bash
cd .. && git add web/src/components/
git commit -m "feat(web): landing, lobby, board, chat, voting, reveal, game-over"
```

---

## Task B6: App wiring — screen router by phase

**Files:**
- Modify: `web/src/App.jsx`

- [ ] **Step 1: Replace `web/src/App.jsx`**

```jsx
import { useMemo, useState } from 'react'
import { useRoomSocket } from './useRoomSocket.js'
import Landing from './components/Landing.jsx'
import Lobby from './components/Lobby.jsx'
import GameBoard from './components/GameBoard.jsx'
import Chat from './components/Chat.jsx'
import Voting from './components/Voting.jsx'
import Reveal from './components/Reveal.jsx'
import GameOver from './components/GameOver.jsx'

export default function App() {
  const [entry, setEntry] = useState(null) // { code, join }
  if (!entry) return <Landing onEnter={setEntry} />
  return <Room entry={entry} />
}

function Room({ entry }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send } = useRoomSocket(entry.code, join)

  const meId = useMemo(() => {
    if (join.reconnectToken) return join.reconnectToken
    return `${entry.code}-${join.name}`
  }, [entry, join])

  if (state.phase === 'connecting') return <div className="center"><div className="card">Connecting…</div></div>
  if (state.phase === 'lobby') return <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} />
  if (state.phase === 'game_over') return <GameOver state={state} />

  const isHost = state.hostId === meId
  return (
    <div className="center">
      <div className="col" style={{ width: 'min(880px,94vw)' }}>
        {state.error && <div className="card" style={{ color: '#ff6b6b' }}>{state.error}</div>}
        {(state.phase === 'drawing') && <GameBoard state={state} meId={meId} send={send} />}
        {state.phase === 'discussion' && (
          <Chat state={state} meId={meId} send={send} canEndDiscussion={isHost} onEnd={() => send('end_discussion')} />
        )}
        {state.phase === 'voting' && <Voting state={state} meId={meId} send={send} />}
        {state.phase === 'reveal' && <Reveal state={state} meId={meId} send={send} />}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Build to verify**

Run: `cd web && npx vite build`
Expected: succeeds with no unresolved imports.

- [ ] **Step 3: Run the reducer tests once more**

Run: `cd web && npx vitest run`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
cd .. && git add web/src/App.jsx
git commit -m "feat(web): wire screens to game phase"
```

---

# Part C — Local Validation

## Task C1: Full multi-window playthrough

**Files:** none (validation only). Do NOT deploy until this passes.

- [ ] **Step 1: Start the backend**

In terminal 1:
```bash
cd "/Users/rishab.j@postman.com/Practice Project/fauxtist"
go run ./cmd/fauxtist
```
Expected: `fauxtist listening on :8080`.

- [ ] **Step 2: Start the frontend dev server**

In terminal 2:
```bash
cd "/Users/rishab.j@postman.com/Practice Project/fauxtist/web"
npm run dev
```
Expected: Vite serves on `http://localhost:5173`.

- [ ] **Step 3: Open 4 browser windows/profiles to `http://localhost:5173`**

Use one normal window + 3 incognito/other-profile windows so each is a distinct player (4 is the minimum).

- [ ] **Step 4: Validate the flow against this checklist**

- [ ] Window 1 creates a room; the room code appears.
- [ ] Windows 2–4 join with that code; all four names appear in every window's lobby list.
- [ ] Only window 1 (host) sees an enabled "Start game" button.
- [ ] Start the game: every window enters the drawing phase; exactly one window shows "You are the IMPOSTOR — category: X" and the other three show the full word.
- [ ] Turn indicator names the current drawer; only that window can draw; a completed stroke appears in ALL four windows.
- [ ] After 2 laps (8 strokes), all windows move to discussion; chat messages broadcast to everyone.
- [ ] Host clicks "Start voting"; all windows show voting buttons; the tally counter increments as votes come in.
- [ ] Reveal shows whether the impostor was caught; if caught, only the impostor window gets a guess box; the word is shown.
- [ ] The game advances through all rounds and ends on a sorted final scoreboard.

- [ ] **Step 5: If anything fails, stop and debug before deploying**

Use browser devtools (Network → WS frames) and the Go server logs. Fix, re-run the affected unit/integration tests, and repeat Step 4. Only when the whole checklist passes is the app ready for Plan 3 (deployment).

- [ ] **Step 6: Record the validated state**

```bash
cd "/Users/rishab.j@postman.com/Practice Project/fauxtist"
git commit --allow-empty -m "chore: local multi-window validation passed"
```

---

## Self-Review Notes

- **Spec coverage:** lobby join + roster growth (A1–A2), max-8 enforcement (A1 engine + A3 server), round scaling to player count (A1), per-player secret reveal already in Plan 1 and surfaced by GameBoard/reducer (`youAreImpostor`, `word`), all phases have a screen (B5–B6), local validation gate (C1). Deployment intentionally deferred to Plan 3.
- **Type/name consistency:** message-type strings in `web/src/protocol.js` mirror the Go `wsproto` constants exactly (`lobby_update`, `player_left`, etc.). `meId` derivation matches the server's `readJoin` id rule (`reconnectToken` if present, else `code-name`). `createRoom` returns `{ code, hostToken }` matching the Go `createRoomResp`.
- **Reveal correctness:** entering the reveal phase now pushes a filtered `round_result` (impostor id + caught, word withheld from the impostor) via `broadcastReveal`, so the Reveal screen renders correctly for the caught path; the full result (with word + guess outcome) follows on `RoundEnded`.
- **Known limitations carried into Plan 3:** (1) two players choosing the same name collide on id (`code-name`); nickname uniqueness is a Plan 3 item — not blocking with distinct names. (2) On the not-caught path the engine advances to the next round immediately, so that reveal is brief; adding a between-rounds reveal hold (a room timer) is a Plan 3 polish item. Final scores are always shown on game over.
```
