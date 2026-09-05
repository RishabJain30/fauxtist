package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/webui"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// maxWSMessageBytes bounds one inbound WebSocket frame, set before the
// join frame is even read. Generous enough for a stroke with many points
// (see internal/room/validate.go's maxStrokePoints) while still bounding
// how much any single frame can cost to read and parse.
const maxWSMessageBytes = 64 * 1024

// Sentinel join-frame failures, distinct from room.Join's business-rule
// rejections (name_taken, room_full, ...) which already get a structured
// error + reason via JoinErrorCode: these are transport/protocol-level
// problems with the frame itself, before the room ever sees a request.
var (
	errBadJoin            = errors.New("expected join frame")
	errUnsupportedVersion = errors.New("unsupported protocol version")
)

// Server wires HTTP routes to the hub.
type Server struct {
	hub            *hub.Hub
	mux            *http.ServeMux
	heartbeat      HeartbeatConfig
	allowedOrigins []string
	turn           TURNConfig
	logger         *slog.Logger

	// ready reflects liveness for /readyz: false until New has finished
	// wiring routes, and flipped back to false by SetNotReady during
	// graceful shutdown so a load balancer stops sending new traffic here
	// before the process actually exits.
	ready atomic.Bool
}

// Option configures optional Server behavior at construction time.
type Option func(*Server)

// WithHeartbeat overrides the default heartbeat timing — mainly for tests
// that want a fast, deterministic dead-connection detection window instead
// of waiting out the production defaults.
func WithHeartbeat(cfg HeartbeatConfig) Option {
	return func(s *Server) { s.heartbeat = cfg }
}

// WithAllowedOrigins overrides the WebSocket origin allowlist. Production
// call sites (cmd/fauxtist/main.go) resolve this once at startup via
// ResolveAllowedOrigins and fail fast on an invalid configuration;
// omitting this option (as most tests do) falls back to permissive
// local-development origins.
func WithAllowedOrigins(origins []string) Option {
	return func(s *Server) { s.allowedOrigins = origins }
}

// WithTURNConfig overrides the ICE-config endpoint's TURN credential
// settings.
func WithTURNConfig(cfg TURNConfig) Option {
	return func(s *Server) { s.turn = cfg }
}

// WithLogger overrides the structured logger used for request/connection
// lifecycle events. Defaults to slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) { s.logger = logger }
}

// New builds a Server with routes registered.
func New(h *hub.Hub, opts ...Option) *Server {
	s := &Server{
		hub:       h,
		mux:       http.NewServeMux(),
		heartbeat: DefaultHeartbeatConfig(),
		turn:      DefaultTURNConfig(),
		logger:    slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	if s.allowedOrigins == nil {
		// Tests overwhelmingly construct a Server with New(h) alone; default
		// to the same permissive local-dev behavior ResolveAllowedOrigins
		// would give with no environment configured, rather than making
		// every call site pass origins explicitly.
		origins, err := ResolveAllowedOrigins()
		if err != nil {
			origins = localDevOrigins
		}
		s.allowedOrigins = origins
	}

	s.mux.HandleFunc("POST /api/rooms", s.createRoom)
	s.mux.HandleFunc("/ws/room/{code}", s.joinRoom)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	s.mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if static, err := webui.FS(); err == nil {
		s.mux.Handle("/", spaHandler(static))
	}
	s.ready.Store(true)
	return s
}

// SetNotReady flips /readyz to fail, for graceful shutdown: a load
// balancer or health check should stop routing new traffic here before
// the process actually stops accepting connections.
func (s *Server) SetNotReady() { s.ready.Store(false) }

// Handler exposes the mux, wrapped with baseline security headers (for
// httptest and main).
func (s *Server) Handler() http.Handler { return securityHeaders(s.mux) }

type createRoomReq struct {
	Name string `json:"name"`
}

// createRoomResp hands the host their seat credentials. ReconnectToken is
// the raw bearer secret; the server only ever keeps its hash afterward.
type createRoomResp struct {
	Code           string `json:"code"`
	PlayerID       string `json:"playerId"`
	ReconnectToken string `json:"reconnectToken"`
}

// maxCreateRoomBodyBytes bounds the request body for POST /api/rooms —
// generous for a JSON object holding nothing but a display name, far
// short of anything that could meaningfully burden the decoder.
const maxCreateRoomBodyBytes = 4 << 10 // 4 KiB

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	if ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || ct != "application/json" {
		writeJSONError(w, http.StatusUnsupportedMediaType, "invalid_content_type", "Content-Type must be application/json")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxCreateRoomBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields() // a client sending fields this endpoint doesn't understand is more likely confused or probing than intentional; reject rather than silently ignore
	var req createRoomReq
	if err := dec.Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "request body must be a JSON object with a name field")
		return
	}
	if dec.More() {
		writeJSONError(w, http.StatusBadRequest, "invalid_body", "unexpected trailing data after the JSON body")
		return
	}

	name, err := room.ValidatePlayerName(req.Name)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid_name", "invalid player name")
		return
	}
	code, playerID, token, err := s.hub.CreateRoom(name)
	if err != nil {
		if errors.Is(err, hub.ErrHubAtCapacity) {
			s.logger.Warn("room creation rejected: hub at capacity")
			writeJSONError(w, http.StatusServiceUnavailable, "capacity_reached", "the server is at capacity, please try again shortly")
			return
		}
		s.logger.Error("room creation failed", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "could not create room")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(createRoomResp{Code: code, PlayerID: string(playerID), ReconnectToken: token})
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	rm, ok := s.hub.Get(code)
	if !ok {
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.allowedOrigins,
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(maxWSMessageBytes)
	// A dedicated context for this one connection's background work
	// (heartbeat, write pump): canceled the moment the read loop returns,
	// for any reason, so nothing outlives the connection it serves.
	connCtx, connCancel := context.WithCancel(r.Context())
	defer connCancel()
	ctx := r.Context()

	joinReq, err := readJoinFrame(ctx, conn)
	if err != nil {
		rejectJoinFrame(ctx, conn, err)
		return
	}

	result, err := rm.Join(conn, joinReq)
	if err != nil {
		sendJoinError(ctx, conn, err)
		status := websocket.StatusPolicyViolation
		if errors.Is(err, room.ErrRoomClosed) {
			status = websocket.StatusCode(wsproto.CloseRoomClosed)
		}
		conn.Close(status, "join rejected")
		return
	}

	go result.Client.WriteLoopForServer(connCtx)
	go runHeartbeat(connCtx, conn, s.heartbeat)
	defer rm.Leave(result.PlayerID, result.ConnID)
	readLoop(ctx, conn, rm, result.PlayerID, result.ConnID, s.turn)
}

// readJoinFrame blocks for the initial WS frame and parses it into a join
// (name+emoji) or reconnect (playerId+reconnectToken) request. It only
// validates transport/protocol-level shape (a valid envelope, the join
// type, a supported version); business-rule validation (name format,
// name-taken, token verification, room capacity, phase) happens in
// room.Room.Join so those failures get a structured protocol error instead
// of a bare connection close.
func readJoinFrame(ctx context.Context, conn *websocket.Conn) (room.JoinRequest, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return room.JoinRequest{}, err
	}
	env, ok := decodeEnvelope(data)
	if !ok || env.Type != wsproto.TypeJoin {
		return room.JoinRequest{}, errBadJoin
	}
	if env.Version != wsproto.ProtocolVersion {
		return room.JoinRequest{}, errUnsupportedVersion
	}
	var p wsproto.JoinPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return room.JoinRequest{}, errBadJoin
	}
	if p.PlayerID != "" || p.ReconnectToken != "" {
		return room.JoinRequest{Reconnect: true, PlayerID: game.PlayerID(p.PlayerID), Token: p.ReconnectToken}, nil
	}
	return room.JoinRequest{Name: p.Name, Emoji: p.Emoji}, nil
}

// rejectJoinFrame closes a connection whose very first frame failed
// transport/protocol-level validation, writing a structured error first
// when there was anything coherent enough to respond to (a raw read
// failure, e.g. the peer vanished before sending anything, has nothing
// worth writing to). Each failure kind gets its own documented close code
// so a client can tell "you're speaking a version I don't support" apart
// from "that wasn't a valid envelope at all".
func rejectJoinFrame(ctx context.Context, conn *websocket.Conn, err error) {
	switch {
	case errors.Is(err, errUnsupportedVersion):
		writeControlError(ctx, conn, "unsupported protocol version", "unsupported_version")
		conn.Close(websocket.StatusCode(wsproto.CloseUnsupportedVersion), "unsupported protocol version")
	case errors.Is(err, errBadJoin):
		writeControlError(ctx, conn, "expected a valid join frame", "invalid_envelope")
		conn.Close(websocket.StatusCode(wsproto.CloseInvalidEnvelope), "expected join")
	default:
		conn.Close(websocket.StatusPolicyViolation, "expected join")
	}
}

// writeControlError writes a structured, typed error frame directly to the
// connection, for rejections that happen before any room.Room session
// exists (so there's no Client/send-channel to route it through).
func writeControlError(ctx context.Context, conn *websocket.Conn, message, code string) {
	env, err := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{Message: message, Code: code})
	if err != nil {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, b)
}

// sendJoinError writes a structured error frame (message + stable code)
// before the caller closes the connection.
func sendJoinError(ctx context.Context, conn *websocket.Conn, err error) {
	writeControlError(ctx, conn, err.Error(), room.JoinErrorCode(err))
}

// spaHandler serves embedded static files, falling back to index.html for
// paths that do not map to a file (single-page app client routes).
func spaHandler(static fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(static, trimLeadingSlash(r.URL.Path)); err != nil && r.URL.Path != "/" {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return "."
	}
	return p
}

// readLoop pumps inbound frames into the room until the connection closes.
// A malformed frame, one declaring an unsupported protocol version, or one
// that otherwise fails wsproto.ValidateEnvelope's shape checks (unknown
// type, missing/oversized requestId, oversized payload) is dropped rather
// than closing an otherwise-healthy mid-game connection over one bad
// message — safety just means never panicking on it, not treating it as
// fatal. Per-message-type semantic validation (stroke bounds, chat length,
// etc.) happens once the room actor itself unmarshals the payload.
func readLoop(ctx context.Context, conn *websocket.Conn, rm *room.Room, id game.PlayerID, connID uint64, turnCfg TURNConfig) {
	// A dedicated, generous-but-bounded limiter for ice_config_request:
	// it depends on no room state, so it's answered directly here rather
	// than via the room actor, and a client repeatedly asking for it
	// (e.g. retrying voice) must not be able to churn HMAC computations
	// unbounded.
	iceLimiter := rate.NewLimiter(rate.Every(5*time.Second), 3)
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		env, ok := decodeEnvelope(data)
		if !ok {
			continue
		}
		if env.Version != wsproto.ProtocolVersion {
			continue
		}
		if wsproto.ValidateEnvelope(env) != nil {
			continue
		}
		if env.Type == wsproto.TypeIceConfigRequest {
			if iceLimiter.Allow() {
				writeIceConfig(ctx, conn, turnCfg)
			}
			continue
		}
		rm.Submit(id, connID, env)
	}
}

// writeIceConfig answers one ice_config_request directly on the
// connection. Safe to call concurrently with the connection's own write
// pump and the heartbeat's Ping calls — nhooyr's Conn permits concurrent
// calls to every method except Reader/Read.
func writeIceConfig(ctx context.Context, conn *websocket.Conn, cfg TURNConfig) {
	env, err := wsproto.Encode(wsproto.TypeIceConfig, wsproto.IceConfigPayload{IceServers: buildIceServers(cfg, time.Now())})
	if err != nil {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, b)
}

// decodeEnvelope parses exactly one JSON object as a wsproto.Envelope,
// rejecting any trailing data after it — a client frame must contain one
// JSON value, not a value followed by garbage or a second value.
func decodeEnvelope(data []byte) (wsproto.Envelope, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var env wsproto.Envelope
	if err := dec.Decode(&env); err != nil {
		return wsproto.Envelope{}, false
	}
	if dec.More() {
		return wsproto.Envelope{}, false
	}
	return env, true
}
