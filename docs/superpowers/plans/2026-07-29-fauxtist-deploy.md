# Fauxtist Deploy Implementation Plan (Render)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve Fauxtist as a single container on Render's free tier — one public URL that serves the React UI and the WebSocket API from the same origin.

**Architecture:** The built React app is embedded into the Go binary with `go:embed`, so one process serves static files (SPA) plus the existing `/api` and `/ws` routes. A multi-stage Docker image builds the frontend (Node), compiles the Go binary with the embedded assets, and ships a distroless final image. Render builds this Dockerfile server-side and runs it; no local Docker required to deploy.

**Tech Stack:** Go 1.26 `embed`, Node 22 build, Docker multi-stage, Render free web service.

**Precondition:** Local validation (Plan 2, Task C1) has passed. Do not deploy otherwise.

**Convention:** keep code comments minimal.

---

## File Structure

```
fauxtist/
  internal/webui/
    webui.go               # //go:embed all:dist  + FS()/Handler()
    dist/.gitkeep          # placeholder so embed compiles before a build
  internal/server/server.go # mount static handler + /healthz + env-driven WS origin
  Dockerfile               # multi-stage node -> go -> distroless
  .dockerignore
  render.yaml              # Render Blueprint (free web service)
  .gitignore               # ignore built dist, keep .gitkeep
```

---

## Task D1: Embed the frontend and serve it (with health check)

**Files:**
- Create: `internal/webui/webui.go`, `internal/webui/dist/.gitkeep`
- Modify: `.gitignore`
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Create the embed placeholder and gitignore rule**

```bash
mkdir -p internal/webui/dist
touch internal/webui/dist/.gitkeep
```
Append to `.gitignore`:
```gitignore
# Embedded frontend build output (placeholder .gitkeep is kept)
/internal/webui/dist/*
!/internal/webui/dist/.gitkeep
```

- [ ] **Step 2: Write the embed package**

Create `internal/webui/webui.go`:
```go
package webui

import (
	"io/fs"
	"embed"
)

//go:embed all:dist
var dist embed.FS

// FS returns the embedded frontend rooted at the dist directory.
func FS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
```

- [ ] **Step 3: Write the failing test**

Append to `internal/server/server_test.go`:
```go
func TestHealthz(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServesIndexAtRoot(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (is internal/webui/dist populated?)", resp.StatusCode)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'TestHealthz|TestServesIndex' -v`
Expected: FAIL — `/healthz` 404 and `/` 404 (no static handler yet).
Note: `TestServesIndexAtRoot` needs a non-empty `dist`. For the test to pass with only `.gitkeep`, the static handler must serve `index.html` if present and fall back to a 200 placeholder otherwise. Step 5 handles both by writing a minimal `index.html` into the placeholder dir for local runs.

- [ ] **Step 5: Add a real placeholder index.html**

Replace `internal/webui/dist/.gitkeep` usage with a committed minimal page so local runs and tests serve something. Create `internal/webui/dist/index.html`:
```html
<!doctype html><html><head><meta charset="utf-8"><title>Fauxtist</title></head>
<body>Fauxtist server is running. Build the frontend for the full UI.</body></html>
```
Update the `.gitignore` rule to keep this file instead of `.gitkeep`:
```gitignore
# Embedded frontend build output (placeholder index.html is kept)
/internal/webui/dist/*
!/internal/webui/dist/index.html
```
Remove the now-unneeded `.gitkeep`:
```bash
rm -f internal/webui/dist/.gitkeep
```

- [ ] **Step 6: Mount the static handler and health check in the server**

In `internal/server/server.go`, add imports:
```go
	"io/fs"
	"net/http"

	"github.com/RishabJain30/fauxtist/internal/webui"
```
(Keep existing imports; `net/http` is already imported.)

In `New`, after the existing route registrations, add:
```go
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	if static, err := webui.FS(); err == nil {
		s.mux.Handle("/", spaHandler(static))
	}
```

Add the SPA handler at the bottom of the file:
```go
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
```

- [ ] **Step 7: Run test to verify it passes**

Run: `go test ./internal/server/ -run 'TestHealthz|TestServesIndex' -v`
Expected: PASS.

- [ ] **Step 8: Run the full suite and commit**

Run: `go test ./...`
Expected: all PASS.
```bash
git add internal/webui/ internal/server/server.go .gitignore
git commit -m "feat(server): embed frontend, serve SPA + /healthz"
```

---

## Task D2: Env-driven WebSocket origin

**Files:**
- Modify: `internal/server/server.go`

- [ ] **Step 1: Read the allowed origin from the environment**

In `internal/server/server.go`, add `"os"` to the imports. Change the `websocket.Accept` call in `joinRoom` from:
```go
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // dev; tightened for the prod deploy in Plan 2
	})
```
to:
```go
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: allowedOrigins(),
	})
```

Add the helper at the bottom of the file:
```go
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
```

- [ ] **Step 2: Verify build, vet, and tests**

Run: `go build ./... && go vet ./... && go test ./internal/server/`
Expected: exit 0, tests PASS (test env sets neither var → `"*"`, so existing WS tests still connect).

- [ ] **Step 3: Commit**

```bash
git add internal/server/server.go
git commit -m "feat(server): restrict WS origin to deployed host via env"
```

---

## Task D3: Dockerfile and local image smoke test

**Files:**
- Create: `Dockerfile`, `.dockerignore`

- [ ] **Step 1: Write `.dockerignore`**

```
web/node_modules
internal/webui/dist
.git
docs
tasks
*.md
```
(The real `dist` is produced inside the image from the Node stage, so the host copy is excluded.)

- [ ] **Step 2: Write the `Dockerfile`**

```dockerfile
# 1) Build the React frontend
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 2) Compile the Go binary with the embedded frontend
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./internal/webui/dist
RUN CGO_ENABLED=0 go build -o /bin/fauxtist ./cmd/fauxtist

# 3) Minimal runtime image
FROM gcr.io/distroless/static-debian12
COPY --from=build /bin/fauxtist /fauxtist
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/fauxtist"]
```

- [ ] **Step 3: Local image smoke test (only if Docker is installed)**

Run:
```bash
docker build -t fauxtist:local . && \
docker run --rm -d -p 8090:8080 --name fauxtist-smoke fauxtist:local && \
sleep 3 && \
curl -s localhost:8090/healthz && echo && \
curl -s -XPOST localhost:8090/api/rooms -d '{"name":"smoke"}' && echo && \
docker stop fauxtist-smoke
```
Expected: `ok`, then a `{"code":...}` JSON. If Docker is not installed, skip this step — Render builds the image server-side regardless.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile .dockerignore
git commit -m "build: multi-stage Dockerfile (node -> go -> distroless)"
```

---

## Task D4: Render Blueprint and live deploy

**Files:**
- Create: `render.yaml`

- [ ] **Step 1: Write `render.yaml`**

```yaml
services:
  - type: web
    name: fauxtist
    runtime: docker
    plan: free
    healthCheckPath: /healthz
    autoDeploy: true
```
`RENDER_EXTERNAL_HOSTNAME` is injected automatically by Render, so no env vars need to be set for the WS origin restriction to take effect.

- [ ] **Step 2: Commit and push**

```bash
git add render.yaml
git commit -m "build: render blueprint (free docker web service)"
git push
```

- [ ] **Step 3: Connect Render to the repo (interactive — user action)**

The user performs these once in the browser (Render account required; free):
1. Sign in at https://dashboard.render.com (GitHub login is easiest).
2. **New → Blueprint**, authorize Render to access the private `RishabJain30/fauxtist` repo.
3. Render detects `render.yaml`; confirm and create the service.
4. First build runs the Dockerfile (Node build → Go build → image). Watch the build logs.

- [ ] **Step 4: Live smoke test**

Once Render reports "Live", note the URL (e.g. `https://fauxtist.onrender.com`). Then:
```bash
curl -s https://<your-app>.onrender.com/healthz && echo
curl -s -XPOST https://<your-app>.onrender.com/api/rooms -d '{"name":"live"}' && echo
```
Expected: `ok`, then a room-code JSON.

- [ ] **Step 5: Browser playthrough on the live URL**

Open the live URL in multiple windows and run one full round (as in Plan 2 C1) to confirm WSS works through Render's TLS/proxy and the same-origin embed serves the real UI (not the placeholder page). The first request after idle may take ~30s (free-tier cold start).

- [ ] **Step 6: Record the deploy**

```bash
git commit --allow-empty -m "chore: deployed to Render (<url>)"
git push
```

---

## Self-Review Notes

- **Single-origin design:** frontend uses relative `/api` and `/ws` paths, which work identically whether proxied in dev (Vite) or served by the embedded static handler in prod — no code change between environments.
- **Embed compile safety:** `internal/webui/dist/index.html` is committed as a placeholder so `go build`/tests compile before any frontend build; the Docker Node stage overwrites `dist` with the real build. Built assets are gitignored.
- **Route precedence:** Go 1.22 `ServeMux` matches the specific `/api/rooms`, `/ws/room/{code}`, and `/healthz` patterns over the catch-all `/`, so the static handler never shadows the API.
- **Security:** WS origin is `*` only when no deploy env var is present (local dev); on Render it is pinned to `RENDER_EXTERNAL_HOSTNAME`.
- **Cost:** Render free web service ($0); `*.onrender.com` subdomain; ~30s cold start after idle is the only trade-off. No org resources — public base images and Render's free plan.
- **Deferred (optional, not blocking):** custom domain; nickname-collision handling; between-rounds reveal hold.
```
