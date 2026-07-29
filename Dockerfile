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
