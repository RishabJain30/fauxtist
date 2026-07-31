# Invite Link + Voice Chat Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a shareable room invite link, then in-room WebRTC voice chat, to Fauxtist.

**Architecture:** Part A is frontend-only: a `/join/<CODE>` deep link and a lobby copy button. Part B adds a WebRTC full mesh — browsers exchange audio peer-to-peer while the existing Go WebSocket relays only signaling (never touching the game engine). A `useVoice` hook owns the peer connections; the reducer holds display-only voice state; a `VoiceBar` renders controls and per-player mic status.

**Tech Stack:** Go 1.26, React 18/19 + Vite, Vitest, browser WebRTC (`RTCPeerConnection`, `getUserMedia`, WebAudio), free Google STUN.

**Convention:** minimal comments — only non-obvious "why".

---

## File Structure

```
fauxtist/
  internal/wsproto/message.go     # + voice_* constants and payload types
  internal/room/room.go           # + voicePresent set, handle voice_* (relay, no engine)
  internal/room/broadcast.go      # + broadcastExcept + voice broadcasts
  internal/server/voice_test.go   # relay/presence integration test
  web/src/
    invite.js                     # parseInviteCode + inviteURL (pure)
    invite.test.js                # Vitest
    protocol.js                   # + Voice* constants
    reducer.js                    # + voice display state
    reducer.test.js               # + voice cases
    useRoomSocket.js              # + subscribe(fn) raw-message channel
    useVoice.js                   # WebRTC mesh hook
    App.jsx                       # parse invite code; wire useVoice + VoiceBar
    components/Landing.jsx        # accept initialCode (join-focused when invited)
    components/Lobby.jsx          # + Copy invite link button
    components/VoiceBar.jsx       # mic toggle + per-player status
```

---

# Part A — Invite Link

## Task A1: Invite helpers + deep-link into join screen

**Files:**
- Create: `web/src/invite.js`, `web/src/invite.test.js`
- Modify: `web/src/App.jsx`, `web/src/components/Landing.jsx`

- [ ] **Step 1: Write the failing test**

Create `web/src/invite.test.js`:
```js
import { describe, it, expect } from 'vitest'
import { parseInviteCode, inviteURL } from './invite.js'

describe('invite', () => {
  it('parses /join/<code> and upper-cases it', () => {
    expect(parseInviteCode('/join/ab3d')).toBe('AB3D')
    expect(parseInviteCode('/join/AB3D/')).toBe('AB3D')
  })
  it('returns null for non-invite paths', () => {
    expect(parseInviteCode('/')).toBeNull()
    expect(parseInviteCode('/join/')).toBeNull()
    expect(parseInviteCode('')).toBeNull()
  })
  it('builds an invite URL', () => {
    expect(inviteURL('https://x.example', 'AB3D')).toBe('https://x.example/join/AB3D')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/invite.test.js`
Expected: FAIL — cannot resolve `./invite.js`.

- [ ] **Step 3: Write the helper**

Create `web/src/invite.js`:
```js
export function parseInviteCode(pathname) {
  const m = /^\/join\/([A-Za-z0-9]+)\/?$/.exec(pathname || '')
  return m ? m[1].toUpperCase() : null
}

export function inviteURL(origin, code) {
  return `${origin}/join/${code}`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd web && npx vitest run src/invite.test.js`
Expected: PASS.

- [ ] **Step 5: Wire the deep link into App**

In `web/src/App.jsx`, import the helper and compute the initial code once:
```jsx
import { useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
```
Change the top-level `App` to pass it to `Landing`:
```jsx
export default function App() {
  const initialCode = useMemo(() => parseInviteCode(location.pathname), [])
  const [entry, setEntry] = useState(null)
  if (!entry) return <Landing onEnter={setEntry} initialCode={initialCode} />
  return <Room entry={entry} />
}
```
(Keep the rest of `App.jsx` unchanged for now; `Room` is modified in Part B.)

- [ ] **Step 6: Make Landing join-focused when invited**

Replace `web/src/components/Landing.jsx`:
```jsx
import { useState } from 'react'
import { createRoom } from '../api.js'

export default function Landing({ onEnter, initialCode }) {
  const invited = !!initialCode
  const [name, setName] = useState('')
  const [code, setCode] = useState(initialCode || '')
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
        {invited
          ? <p className="muted">You're joining room <b>{initialCode}</b>. Enter a name to play.</p>
          : <p className="muted">One of you is faking it. Draw one stroke at a time — don't get caught.</p>}
        <input placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} />
        {!invited && (
          <div className="row">
            <button onClick={host} disabled={busy}>Create room</button>
          </div>
        )}
        <div className="row">
          <input
            placeholder="Room code"
            value={code}
            readOnly={invited}
            onChange={(e) => setCode(e.target.value)}
          />
          <button onClick={join} disabled={busy}>Join</button>
        </div>
        {err && <p style={{ color: '#ff6b6b' }}>{err}</p>}
      </div>
    </div>
  )
}
```

- [ ] **Step 7: Build + full frontend tests**

Run: `cd web && npx vite build && npx vitest run`
Expected: build OK; all tests pass.

- [ ] **Step 8: Commit**

```bash
cd .. && git add web/src/invite.js web/src/invite.test.js web/src/App.jsx web/src/components/Landing.jsx
git commit -m "feat(web): /join/<code> invite deep link"
```

---

## Task A2: Copy invite link button in the lobby

**Files:**
- Modify: `web/src/components/Lobby.jsx`

- [ ] **Step 1: Add the copy button**

Replace `web/src/components/Lobby.jsx`:
```jsx
import { useState } from 'react'
import { inviteURL } from '../invite.js'

export default function Lobby({ state, meId, code, onStart }) {
  const isHost = state.hostId === meId
  const enough = state.players.length >= 4
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    const url = inviteURL(location.origin, code)
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      window.prompt('Copy this invite link:', url)
    }
  }

  return (
    <div className="center">
      <div className="card col">
        <h2>Room {code}</h2>
        <p className="muted">Share this code or link. Need 4–8 players.</p>
        <div className="row">
          <button onClick={copy}>{copied ? 'Copied!' : 'Copy invite link'}</button>
        </div>
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

- [ ] **Step 2: Build to verify**

Run: `cd web && npx vite build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
cd .. && git add web/src/components/Lobby.jsx
git commit -m "feat(web): copy invite link button in lobby"
```

---

# Part B — Voice Chat

## Task B1: Voice signaling message types (backend)

**Files:**
- Modify: `internal/wsproto/message.go`

- [ ] **Step 1: Add constants and payload types**

In `internal/wsproto/message.go`, add to the client→server constants:
```go
	TypeVoiceJoin   = "voice_join"
	TypeVoiceLeave  = "voice_leave"
	TypeVoiceSignal = "voice_signal"
	TypeVoiceState  = "voice_state"
```
Add to the server→client constants:
```go
	TypeVoicePeers      = "voice_peers"
	TypeVoicePeerJoined = "voice_peer_joined"
	TypeVoicePeerLeft   = "voice_peer_left"
```
Add payload types at the bottom of the file:
```go
// VoiceSignalIn is a client's signaling message addressed to another peer.
type VoiceSignalIn struct {
	To      string          `json:"to"`
	Kind    string          `json:"kind"` // offer | answer | ice
	Payload json.RawMessage `json:"payload"`
}

// VoiceStateIn is a client's current mic state.
type VoiceStateIn struct {
	Muted    bool `json:"muted"`
	Speaking bool `json:"speaking"`
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./internal/wsproto/`
Expected: exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/wsproto/message.go
git commit -m "feat(wsproto): voice signaling message types"
```

---

## Task B2: Room relays voice signaling and tracks presence

**Files:**
- Modify: `internal/room/room.go`, `internal/room/broadcast.go`
- Test: `internal/server/voice_test.go`

- [ ] **Step 1: Write the failing integration test**

Create `internal/server/voice_test.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func readUntil(t *testing.T, c *websocket.Conn, typ string) wsproto.Envelope {
	t.Helper()
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read waiting for %s: %v", typ, err)
		}
		var env wsproto.Envelope
		_ = json.Unmarshal(data, &env)
		if env.Type == typ {
			return env
		}
	}
	t.Fatalf("never saw %s", typ)
	return wsproto.Envelope{}
}

func TestVoiceSignalRelayedToTarget(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	a := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "A", ReconnectToken: cr.HostToken})
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "B"})
	defer b.Close(websocket.StatusNormalClosure, "")
	aID := cr.HostToken
	bID := cr.Code + "-B"

	time.Sleep(100 * time.Millisecond)

	// A enables voice -> B must observe A joining voice.
	writeMsg(t, a, wsproto.TypeVoiceJoin, map[string]any{})
	pj := readUntil(t, b, wsproto.TypeVoicePeerJoined)
	var pjp map[string]any
	_ = json.Unmarshal(pj.Payload, &pjp)
	if pjp["id"] != aID {
		t.Fatalf("peer_joined id = %v, want %s", pjp["id"], aID)
	}

	// B enables voice -> B's voice_peers must list A.
	writeMsg(t, b, wsproto.TypeVoiceJoin, map[string]any{})
	peers := readUntil(t, b, wsproto.TypeVoicePeers)
	var pp map[string]any
	_ = json.Unmarshal(peers.Payload, &pp)
	ids, _ := pp["ids"].([]any)
	if len(ids) != 1 || ids[0] != aID {
		t.Fatalf("voice_peers = %v, want [%s]", ids, aID)
	}

	// A sends an offer addressed to B -> only B receives it, with from=A.
	writeMsg(t, a, wsproto.TypeVoiceSignal, map[string]any{
		"to": bID, "kind": "offer", "payload": map[string]any{"sdp": "x"},
	})
	sig := readUntil(t, b, wsproto.TypeVoiceSignal)
	var sp map[string]any
	_ = json.Unmarshal(sig.Payload, &sp)
	if sp["from"] != aID || sp["kind"] != "offer" {
		t.Fatalf("signal = %v, want from=%s kind=offer", sp, aID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestVoiceSignalRelayedToTarget -v`
Expected: FAIL — the room ignores `voice_join`/`voice_signal` (times out waiting for `voice_peer_joined`).

- [ ] **Step 3: Add voicePresent tracking to the room**

In `internal/room/room.go`, add a field to the `Room` struct:
```go
	voicePresent map[game.PlayerID]bool
```
Initialize it in `NewRoom` (alongside `clients`):
```go
		voicePresent:  map[game.PlayerID]bool{},
```
In the `Run` loop's leave case, also drop voice presence and notify peers. Change:
```go
		case id := <-r.leaves:
			delete(r.clients, id)
			r.broadcastPlayerLeft(id)
```
to:
```go
		case id := <-r.leaves:
			delete(r.clients, id)
			if r.voicePresent[id] {
				delete(r.voicePresent, id)
				r.broadcastVoicePeerLeft(id)
			}
			r.broadcastPlayerLeft(id)
```

- [ ] **Step 4: Handle voice messages in `handle`**

In `internal/room/room.go`, add these cases to the `switch msg.envelope.Type` in `handle` (before `default`):
```go
	case wsproto.TypeVoiceJoin:
		r.voiceJoin(msg.from)
	case wsproto.TypeVoiceLeave:
		r.voiceLeave(msg.from)
	case wsproto.TypeVoiceSignal:
		var p wsproto.VoiceSignalIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.relayVoiceSignal(msg.from, p)
	case wsproto.TypeVoiceState:
		var p wsproto.VoiceStateIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.broadcastVoiceState(msg.from, p)
```

- [ ] **Step 5: Add the voice broadcast/relay helpers**

Append to `internal/room/broadcast.go`:
```go
// broadcastExcept sends to every connected client except one.
func (r *Room) broadcastExcept(except game.PlayerID, env wsproto.Envelope) {
	for id, c := range r.clients {
		if id != except {
			c.trySend(env)
		}
	}
}

func (r *Room) voiceJoin(from game.PlayerID) {
	others := []string{}
	for id := range r.voicePresent {
		if id != from {
			others = append(others, string(id))
		}
	}
	r.voicePresent[from] = true
	if env, err := wsproto.Encode(wsproto.TypeVoicePeers, map[string]any{"ids": others}); err == nil {
		r.sendTo(from, env)
	}
	if env, err := wsproto.Encode(wsproto.TypeVoicePeerJoined, map[string]any{"id": string(from)}); err == nil {
		r.broadcastExcept(from, env)
	}
}

func (r *Room) voiceLeave(from game.PlayerID) {
	if !r.voicePresent[from] {
		return
	}
	delete(r.voicePresent, from)
	r.broadcastVoicePeerLeft(from)
}

func (r *Room) broadcastVoicePeerLeft(id game.PlayerID) {
	if env, err := wsproto.Encode(wsproto.TypeVoicePeerLeft, map[string]any{"id": string(id)}); err == nil {
		r.broadcastExcept(id, env)
	}
}

func (r *Room) relayVoiceSignal(from game.PlayerID, p wsproto.VoiceSignalIn) {
	env, err := wsproto.Encode(wsproto.TypeVoiceSignal, map[string]any{
		"from": string(from), "kind": p.Kind, "payload": p.Payload,
	})
	if err == nil {
		r.sendTo(game.PlayerID(p.To), env)
	}
}

func (r *Room) broadcastVoiceState(from game.PlayerID, p wsproto.VoiceStateIn) {
	env, err := wsproto.Encode(wsproto.TypeVoiceState, map[string]any{
		"id": string(from), "muted": p.Muted, "speaking": p.Speaking,
	})
	if err == nil {
		r.broadcastExcept(from, env)
	}
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestVoiceSignalRelayedToTarget -v`
Expected: PASS.

- [ ] **Step 7: Full suite + commit**

Run: `go test ./...`
Expected: all PASS (game engine untouched by voice).
```bash
git add internal/room/ internal/server/voice_test.go
git commit -m "feat(room): relay voice signaling and track voice presence"
```

---

## Task B3: Protocol constants + reducer voice display state

**Files:**
- Modify: `web/src/protocol.js`, `web/src/reducer.js`, `web/src/reducer.test.js`

- [ ] **Step 1: Write the failing test**

Append to `web/src/reducer.test.js` (inside the existing `describe`):
```js
  it('tracks voice peers and state', () => {
    let s = reduce(initialState(), { type: T.VoicePeers, payload: { ids: ['a', 'b'] } })
    expect(s.voicePeers).toEqual(['a', 'b'])
    s = reduce(s, { type: T.VoicePeerJoined, payload: { id: 'c' } })
    expect(s.voicePeers).toContain('c')
    s = reduce(s, { type: T.VoiceState, payload: { id: 'c', muted: false, speaking: true } })
    expect(s.voiceStates.c).toEqual({ muted: false, speaking: true })
    s = reduce(s, { type: T.VoicePeerLeft, payload: { id: 'c' } })
    expect(s.voicePeers).not.toContain('c')
    expect(s.voiceStates.c).toBeUndefined()
  })
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && npx vitest run src/reducer.test.js`
Expected: FAIL — `T.VoicePeers` undefined / `voicePeers` undefined.

- [ ] **Step 3: Add protocol constants**

In `web/src/protocol.js`, add to the object:
```js
  // voice (client -> server)
  VoiceJoin: 'voice_join',
  VoiceLeave: 'voice_leave',
  VoiceSignal: 'voice_signal',
  VoiceState: 'voice_state',
  // voice (server -> client)
  VoicePeers: 'voice_peers',
  VoicePeerJoined: 'voice_peer_joined',
  VoicePeerLeft: 'voice_peer_left',
```

- [ ] **Step 4: Add reducer state + cases**

In `web/src/reducer.js`, add to `initialState()`:
```js
    voicePeers: [],
    voiceStates: {},
```
Add these cases before `default`:
```js
    case T.VoicePeers:
      return { ...state, voicePeers: p.ids || [] }
    case T.VoicePeerJoined:
      return {
        ...state,
        voicePeers: state.voicePeers.includes(p.id) ? state.voicePeers : [...state.voicePeers, p.id],
      }
    case T.VoicePeerLeft: {
      const voiceStates = { ...state.voiceStates }
      delete voiceStates[p.id]
      return { ...state, voicePeers: state.voicePeers.filter((id) => id !== p.id), voiceStates }
    }
    case T.VoiceState:
      return { ...state, voiceStates: { ...state.voiceStates, [p.id]: { muted: p.muted, speaking: p.speaking } } }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd web && npx vitest run src/reducer.test.js`
Expected: PASS (all reducer tests).

- [ ] **Step 6: Commit**

```bash
cd .. && git add web/src/protocol.js web/src/reducer.js web/src/reducer.test.js
git commit -m "feat(web): voice display state in reducer + protocol constants"
```

---

## Task B4: Raw-message subscription in the socket hook

**Files:**
- Modify: `web/src/useRoomSocket.js`

- [ ] **Step 1: Add a subscription channel**

Replace `web/src/useRoomSocket.js`:
```jsx
import { useEffect, useRef, useReducer, useCallback } from 'react'
import { reduce, initialState } from './reducer.js'

export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const wsRef = useRef(null)
  const subsRef = useRef(new Set())

  useEffect(() => {
    if (!code || !join) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws/room/${code}`)
    wsRef.current = ws
    ws.onopen = () => ws.send(JSON.stringify({ type: 'join', payload: join }))
    ws.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch { return }
      dispatch(msg)
      subsRef.current.forEach((fn) => fn(msg))
    }
    return () => ws.close()
  }, [code, join])

  const send = useCallback((type, payload = {}) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, payload }))
  }, [])

  const subscribe = useCallback((fn) => {
    subsRef.current.add(fn)
    return () => subsRef.current.delete(fn)
  }, [])

  return { state, send, subscribe }
}
```

- [ ] **Step 2: Build + tests**

Run: `cd web && npx vite build && npx vitest run`
Expected: build OK; tests pass.

- [ ] **Step 3: Commit**

```bash
cd .. && git add web/src/useRoomSocket.js
git commit -m "feat(web): raw-message subscribe channel on socket hook"
```

---

## Task B5: useVoice hook (WebRTC mesh)

**Files:**
- Create: `web/src/useVoice.js`

This hook has no Vitest coverage (WebRTC/`getUserMedia` need a real browser); it is verified by build and by the manual validation in Task B7.

- [ ] **Step 1: Write the hook**

Create `web/src/useVoice.js`:
```jsx
import { useEffect, useRef, useState, useCallback } from 'react'
import { T } from './protocol.js'

const ICE = { iceServers: [{ urls: 'stun:stun.l.google.com:19302' }] }

export function useVoice({ meId, send, subscribe }) {
  const [active, setActive] = useState(false)
  const [muted, setMuted] = useState(true)
  const [speaking, setSpeaking] = useState(false)
  const [error, setError] = useState(null)

  const localStream = useRef(null)
  const pcs = useRef(new Map())     // peerId -> RTCPeerConnection
  const audios = useRef(new Map())  // peerId -> HTMLAudioElement
  const activeRef = useRef(false)
  const mutedRef = useRef(true)
  const speakingRef = useRef(false)
  const audioCtx = useRef(null)
  const speakTimer = useRef(null)

  const sendState = useCallback((m, sp) => send(T.VoiceState, { muted: m, speaking: sp }), [send])

  const closePeer = useCallback((peerId) => {
    const pc = pcs.current.get(peerId)
    if (pc) { pc.close(); pcs.current.delete(peerId) }
    const el = audios.current.get(peerId)
    if (el) { el.srcObject = null; audios.current.delete(peerId) }
  }, [])

  const makePeer = useCallback((peerId) => {
    const existing = pcs.current.get(peerId)
    if (existing) return existing
    const pc = new RTCPeerConnection(ICE)
    pcs.current.set(peerId, pc)
    if (localStream.current) {
      localStream.current.getTracks().forEach((t) => pc.addTrack(t, localStream.current))
    }
    pc.onicecandidate = (e) => {
      if (e.candidate) send(T.VoiceSignal, { to: peerId, kind: 'ice', payload: e.candidate })
    }
    pc.ontrack = (e) => {
      let el = audios.current.get(peerId)
      if (!el) { el = new Audio(); el.autoplay = true; audios.current.set(peerId, el) }
      el.srcObject = e.streams[0]
      el.play().catch(() => {})
    }
    return pc
  }, [send])

  const callPeer = useCallback(async (peerId) => {
    const pc = makePeer(peerId)
    try {
      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      send(T.VoiceSignal, { to: peerId, kind: 'offer', payload: pc.localDescription })
    } catch { /* ignore */ }
  }, [makePeer, send])

  const onSignal = useCallback(async (from, kind, payload) => {
    const pc = makePeer(from)
    try {
      if (kind === 'offer') {
        await pc.setRemoteDescription(payload)
        const answer = await pc.createAnswer()
        await pc.setLocalDescription(answer)
        send(T.VoiceSignal, { to: from, kind: 'answer', payload: pc.localDescription })
      } else if (kind === 'answer') {
        await pc.setRemoteDescription(payload)
      } else if (kind === 'ice') {
        await pc.addIceCandidate(payload)
      }
    } catch { /* ignore malformed/late signals */ }
  }, [makePeer, send])

  useEffect(() => {
    const off = subscribe((msg) => {
      const p = msg.payload || {}
      switch (msg.type) {
        case T.VoicePeers:
          (p.ids || []).forEach((id) => { if (meId < id) callPeer(id) })
          break
        case T.VoicePeerJoined:
          if (p.id !== meId && meId < p.id) callPeer(p.id)
          break
        case T.VoicePeerLeft:
          closePeer(p.id)
          break
        case T.VoiceSignal:
          if (p.from) onSignal(p.from, p.kind, p.payload)
          break
        default:
          break
      }
    })
    return off
  }, [subscribe, meId, callPeer, closePeer, onSignal])

  const startSpeakingDetection = useCallback((stream) => {
    const Ctx = window.AudioContext || window.webkitAudioContext
    const ctx = new Ctx()
    audioCtx.current = ctx
    const analyser = ctx.createAnalyser()
    analyser.fftSize = 512
    ctx.createMediaStreamSource(stream).connect(analyser)
    const data = new Uint8Array(analyser.frequencyBinCount)
    speakTimer.current = setInterval(() => {
      analyser.getByteTimeDomainData(data)
      let sum = 0
      for (let i = 0; i < data.length; i++) { const v = (data[i] - 128) / 128; sum += v * v }
      const rms = Math.sqrt(sum / data.length)
      const sp = !mutedRef.current && rms > 0.05
      if (sp !== speakingRef.current) {
        speakingRef.current = sp
        setSpeaking(sp)
        sendState(mutedRef.current, sp)
      }
    }, 250)
  }, [sendState])

  const enable = useCallback(async () => {
    if (activeRef.current) return
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      stream.getAudioTracks().forEach((t) => (t.enabled = false))
      localStream.current = stream
      activeRef.current = true
      setActive(true)
      startSpeakingDetection(stream)
      send(T.VoiceJoin, {})
      sendState(true, false)
    } catch {
      setError('Mic unavailable — you can still play')
    }
  }, [send, sendState, startSpeakingDetection])

  const toggleMute = useCallback(() => {
    if (!activeRef.current) return
    const m = !mutedRef.current
    mutedRef.current = m
    setMuted(m)
    localStream.current?.getAudioTracks().forEach((t) => (t.enabled = !m))
    sendState(m, m ? false : speakingRef.current)
  }, [sendState])

  useEffect(() => {
    return () => {
      if (speakTimer.current) clearInterval(speakTimer.current)
      if (audioCtx.current) audioCtx.current.close().catch(() => {})
      pcs.current.forEach((pc) => pc.close())
      pcs.current.clear()
      audios.current.forEach((el) => { el.srcObject = null })
      audios.current.clear()
      localStream.current?.getTracks().forEach((t) => t.stop())
      if (activeRef.current) send(T.VoiceLeave, {})
    }
  }, [send])

  return { active, muted, speaking, error, enable, toggleMute }
}
```

- [ ] **Step 2: Build to verify**

Run: `cd web && npx vite build`
Expected: succeeds.

- [ ] **Step 3: Commit**

```bash
cd .. && git add web/src/useVoice.js
git commit -m "feat(web): useVoice WebRTC mesh hook"
```

---

## Task B6: VoiceBar UI and App wiring

**Files:**
- Create: `web/src/components/VoiceBar.jsx`
- Modify: `web/src/App.jsx`

- [ ] **Step 1: Write the VoiceBar**

Create `web/src/components/VoiceBar.jsx`:
```jsx
export default function VoiceBar({ voice, state, meId }) {
  const { active, muted, speaking, error, enable, toggleMute } = voice

  const icon = (v) => (v.speaking ? '🔊' : v.muted ? '🔇' : '🎙️')

  return (
    <div className="card row" style={{ justifyContent: 'space-between' }}>
      <div className="row">
        {!active
          ? <button onClick={enable}>🎤 Enable voice</button>
          : <button onClick={toggleMute}>{muted ? '🔇 Unmute' : '🎙️ Mute'}</button>}
        {error && <span className="muted">{error}</span>}
      </div>
      <div className="row" style={{ flexWrap: 'wrap' }}>
        {state.players.map((p) => {
          const isMe = p.id === meId
          const present = isMe ? active : state.voicePeers.includes(p.id)
          if (!present) return null
          const v = isMe ? { muted, speaking } : (state.voiceStates[p.id] || { muted: true, speaking: false })
          return <span key={p.id} className="badge">{icon(v)} {p.name}</span>
        })}
      </div>
    </div>
  )
}
```

- [ ] **Step 2: Wire useVoice + VoiceBar into App**

Replace `web/src/App.jsx`:
```jsx
import { useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
import { useRoomSocket } from './useRoomSocket.js'
import { useVoice } from './useVoice.js'
import Landing from './components/Landing.jsx'
import Lobby from './components/Lobby.jsx'
import GameBoard from './components/GameBoard.jsx'
import Chat from './components/Chat.jsx'
import Voting from './components/Voting.jsx'
import Reveal from './components/Reveal.jsx'
import GameOver from './components/GameOver.jsx'
import VoiceBar from './components/VoiceBar.jsx'

export default function App() {
  const initialCode = useMemo(() => parseInviteCode(location.pathname), [])
  const [entry, setEntry] = useState(null)
  if (!entry) return <Landing onEnter={setEntry} initialCode={initialCode} />
  return <Room entry={entry} />
}

function Room({ entry }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send, subscribe } = useRoomSocket(entry.code, join)
  const meId = useMemo(() => {
    if (join.reconnectToken) return join.reconnectToken
    return `${entry.code}-${join.name}`
  }, [entry, join])
  const voice = useVoice({ meId, send, subscribe })

  if (state.phase === 'connecting') {
    return <div className="center"><div className="card">Connecting…</div></div>
  }

  const isHost = state.hostId === meId
  let content
  if (state.phase === 'lobby') {
    content = <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} />
  } else if (state.phase === 'game_over') {
    content = <GameOver state={state} />
  } else {
    content = (
      <>
        {state.error && <div className="card" style={{ color: '#ff6b6b' }}>{state.error}</div>}
        {state.phase === 'drawing' && <GameBoard state={state} meId={meId} send={send} />}
        {state.phase === 'discussion' && (
          <Chat state={state} meId={meId} send={send} canEndDiscussion={isHost} onEnd={() => send('end_discussion')} />
        )}
        {state.phase === 'voting' && <Voting state={state} meId={meId} send={send} />}
        {state.phase === 'reveal' && <Reveal state={state} meId={meId} send={send} />}
      </>
    )
  }

  return (
    <div className="center">
      <div className="col" style={{ width: 'min(880px,94vw)' }}>
        <VoiceBar voice={voice} state={state} meId={meId} />
        {content}
      </div>
    </div>
  )
}
```

- [ ] **Step 3: Build + tests**

Run: `cd web && npx vite build && npx vitest run`
Expected: build OK; all tests pass.

- [ ] **Step 4: Commit**

```bash
cd .. && git add web/src/components/VoiceBar.jsx web/src/App.jsx
git commit -m "feat(web): voice bar UI and app wiring"
```

---

## Task B7: Local voice validation (manual, multi-window)

**Files:** none (validation only).

- [ ] **Step 1: Build the frontend into the embed dir and run the binary**

```bash
cd "/Users/rishab.j@postman.com/Practice Project/fauxtist"
cd web && npm run build && cd ..
rm -rf internal/webui/dist && cp -r web/dist internal/webui/dist
go build -o /tmp/fauxtist-live ./cmd/fauxtist
PORT=8080 /tmp/fauxtist-live
```
(Or run the split dev setup: `go run ./cmd/fauxtist` + `cd web && npm run dev` on :5173.)

Note: browsers only allow mic capture on `https://` or `http://localhost`. Localhost is fine for this test.

- [ ] **Step 2: Open 2–3 windows on http://localhost:8080 (or :5173) and validate**

- [ ] Create a room in window 1; join from windows 2–3 (min 4 players not required to test voice — voice works in the lobby).
- [ ] Each window: click **Enable voice** → browser asks for mic permission → grant it.
- [ ] Click **Unmute** in window 1 and talk; windows 2–3 hear the audio.
- [ ] The VoiceBar shows 🔊 next to the speaking player and 🔇 for muted players, updating live.
- [ ] Mute in window 1 → others stop hearing it and see 🔇.
- [ ] Close window 3 → windows 1–2 drop that peer (its badge disappears; no console errors).
- [ ] Deny mic permission in a fresh window → "Mic unavailable — you can still play" shows and the game is still fully playable.

- [ ] **Step 3: Restore the committed placeholder and record validation**

```bash
git checkout -- internal/webui/dist/index.html
git commit --allow-empty -m "chore: voice chat validated locally (multi-window)"
```

---

## Self-Review Notes

- **Spec coverage:** invite deep link + copy button (A1–A2); WebRTC mesh with server-relayed signaling that never calls the engine (B2); join-muted + toggle + speaking indicator (B5–B6); free STUN, no TURN (B5); reducer display state + player mic icons (B3, B6); Go relay/presence test + reducer tests + manual browser validation (B2, B3, B7).
- **Type/name consistency:** `web/src/protocol.js` voice constants mirror the Go `wsproto` strings exactly (`voice_join`, `voice_signal`, `voice_peers`, …). The `VoiceSignal` payload is `{to, kind, payload}` client→server and relayed as `{from, kind, payload}` — matched in `relayVoiceSignal` and `onSignal`. `voice_state` is `{muted, speaking}` up and `{id, muted, speaking}` down — matched in `broadcastVoiceState` and the reducer.
- **Glare:** deterministic offerer (`meId < peerId` offers) applied consistently in `VoicePeers` and `VoicePeerJoined` handling, so exactly one side offers per pair.
- **Known limitations (carried):** no TURN → strict-NAT peers may be inaudible; mesh is for 4–8 players; voice is available during drawing (players self-regulate). Nickname collisions still share an id (pre-existing).
```
