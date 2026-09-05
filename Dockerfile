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

# Build/version metadata, stamped from outside (CI passes real values; a
# local `docker build` with no --build-arg still produces a working image
# with "dev"/"unknown" placeholders — see cmd/fauxtist/main.go).
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

# CGO_ENABLED=0 (statically linked, no libc dependency — required for the
# distroless static base below) and -trimpath/-s/-w keep the binary
# reproducible and free of local filesystem paths or debug symbols.
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildDate=${BUILD_DATE}" \
    -o /bin/fauxtist ./cmd/fauxtist

# 3) Minimal runtime image. The "nonroot" tag runs as uid/gid 65532
# instead of root, with no shell or package manager to misuse even if
# something did find a way to execute arbitrary code.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/fauxtist /fauxtist
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/fauxtist"]
