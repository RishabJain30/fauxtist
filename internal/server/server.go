package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/webui"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

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
	hub       *hub.Hub
	mux       *http.ServeMux
	heartbeat HeartbeatConfig
}

// Option configures optional Server behavior at construction time.
type Option func(*Server)

// WithHeartbeat overrides the default heartbeat timing — mainly for tests
// that want a fast, deterministic dead-connection detection window instead
// of waiting out the production defaults.
func WithHeartbeat(cfg HeartbeatConfig) Option {
	return func(s *Server) { s.heartbeat = cfg }
}

// New builds a Server with routes registered.
func New(h *hub.Hub, opts ...Option) *Server {
	s := &Server{hub: h, mux: http.NewServeMux(), heartbeat: DefaultHeartbeatConfig()}
	for _, opt := range opts {
		opt(s)
	}
	s.mux.HandleFunc("POST /api/rooms", s.createRoom)
	s.mux.HandleFunc("/ws/room/{code}", s.joinRoom)
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if static, err := webui.FS(); err == nil {
		s.mux.Handle("/", spaHandler(static))
	}
	return s
}

// Handler exposes the mux (for httptest and main).
func (s *Server) Handler() http.Handler { return s.mux }

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

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	name, err := room.ValidatePlayerName(req.Name)
	if err != nil {
		http.Error(w, "invalid name", http.StatusBadRequest)
		return
	}
	code, playerID, token, err := s.hub.CreateRoom(name)
	if err != nil {
		http.Error(w, "could not create room", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
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
		OriginPatterns: allowedOrigins(),
	})
	if err != nil {
		return
	}
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
		conn.Close(websocket.StatusPolicyViolation, "join rejected")
		return
	}

	go result.Client.WriteLoopForServer(connCtx)
	go runHeartbeat(connCtx, conn, s.heartbeat)
	defer rm.Leave(result.PlayerID, result.ConnID)
	readLoop(ctx, conn, rm, result.PlayerID, result.ConnID)
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
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != wsproto.TypeJoin {
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

// allowedOrigins restricts WebSocket upgrades to the deployed host when
// RENDER_EXTERNAL_HOSTNAME (set automatically by Render) or ALLOWED_ORIGIN is
// present; otherwise it allows all origins for local development.
func allowedOrigins() []string {
	if h := os.Getenv("RENDER_EXTERNAL_HOSTNAME"); h != "" {
		return []string{h}
	}
	if o := os.Getenv("ALLOWED_ORIGIN"); o != "" {
		return []string{o}
	}
	return []string{"*"}
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
// A malformed frame, or one declaring an unsupported protocol version, is
// dropped rather than closing an otherwise-healthy mid-game connection over
// one bad message — safety just means never panicking on it, not treating
// it as fatal.
func readLoop(ctx context.Context, conn *websocket.Conn, rm *room.Room, id game.PlayerID, connID uint64) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		if env.Version != wsproto.ProtocolVersion {
			continue
		}
		rm.Submit(id, connID, env)
	}
}
