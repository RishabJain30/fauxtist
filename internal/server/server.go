package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/webui"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// errBadJoin is returned when the first WS frame is not a valid join.
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

type createRoomResp struct {
	Code string `json:"code"`
	// HostToken lets the host claim its pre-seated seat on the WS join frame.
	HostToken string `json:"hostToken"`
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	code := s.hub.CreateRoom(req.Name)
	host, _ := s.hub.HostID(code)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(createRoomResp{Code: code, HostToken: string(host)})
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	rm, ok := s.hub.Get(code)
	if !ok {
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // dev; tightened for the prod deploy in Plan 2
	})
	if err != nil {
		return
	}
	ctx := r.Context()

	// The first frame must be a join naming the player.
	name, playerID, err := readJoin(ctx, conn, code)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "expected join")
		return
	}

	c := room.NewClientForServer(playerID, name, conn)
	if err := rm.Join(c); err != nil {
		conn.Close(websocket.StatusPolicyViolation, "join rejected")
		return
	}
	defer rm.Leave(playerID)

	go c.WriteLoopForServer(ctx)
	readLoop(ctx, conn, rm, playerID)
}

// readJoin blocks for the initial join frame and resolves the player's ID. A
// reconnect token (issued to the host via POST, and to any client for reconnect)
// claims that exact seat; otherwise a fresh id is minted from the room + name.
func readJoin(ctx context.Context, conn *websocket.Conn, code string) (string, game.PlayerID, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return "", "", err
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != wsproto.TypeJoin {
		return "", "", errBadJoin
	}
	var p wsproto.JoinPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return "", "", errBadJoin
	}
	if p.ReconnectToken != "" {
		return p.Name, game.PlayerID(p.ReconnectToken), nil
	}
	return p.Name, game.PlayerID(code + "-" + p.Name), nil
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
func readLoop(ctx context.Context, conn *websocket.Conn, rm *room.Room, id game.PlayerID) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		rm.Submit(id, env)
	}
}
