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

// errBadJoin is returned when the first WS frame is not a valid join frame.
var errBadJoin = errors.New("expected join frame")

// Server wires HTTP routes to the hub.
type Server struct {
	hub *hub.Hub
	mux *http.ServeMux
}

// New builds a Server with routes registered.
func New(h *hub.Hub) *Server {
	s := &Server{hub: h, mux: http.NewServeMux()}
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
	ctx := r.Context()

	joinReq, err := readJoinFrame(ctx, conn)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "expected join")
		return
	}

	result, err := rm.Join(conn, joinReq)
	if err != nil {
		sendJoinError(ctx, conn, err)
		conn.Close(websocket.StatusPolicyViolation, "join rejected")
		return
	}

	go result.Client.WriteLoopForServer(ctx)
	defer rm.Leave(result.PlayerID, result.ConnID)
	readLoop(ctx, conn, rm, result.PlayerID, result.ConnID)
}

// readJoinFrame blocks for the initial WS frame and parses it into a join
// (name+emoji) or reconnect (playerId+reconnectToken) request. It only
// validates transport-level shape; business-rule validation (name format,
// name-taken, token verification, room capacity, phase) happens in
// room.Room.Join so failures get a structured protocol error instead of a
// bare connection close.
func readJoinFrame(ctx context.Context, conn *websocket.Conn) (room.JoinRequest, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return room.JoinRequest{}, err
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != wsproto.TypeJoin {
		return room.JoinRequest{}, errBadJoin
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

// sendJoinError writes a structured error frame (message + stable code)
// before the caller closes the connection.
func sendJoinError(ctx context.Context, conn *websocket.Conn, err error) {
	env, encErr := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{
		Message: err.Error(),
		Code:    room.JoinErrorCode(err),
	})
	if encErr != nil {
		return
	}
	b, err := json.Marshal(env)
	if err != nil {
		return
	}
	_ = conn.Write(ctx, websocket.MessageText, b)
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
		rm.Submit(id, connID, env)
	}
}
