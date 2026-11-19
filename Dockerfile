# terminal-share: one Go binary serving the API and the share page, with the page's engine
# (rioterm, from npm) built in a stage of its own and embedded.

# Stage 1: the page's engine. npm installs the locked rioterm through Artifact Keeper, and
# build.mjs copies its module and WebAssembly into web/static/vendor/.
FROM artifacts.home.romaine.life/dockerhub/library/node:22-alpine@sha256:1314fb30ec37592d5845e5a160d522c0e680972aa534549940b8d73bde88481a AS web
WORKDIR /web
ENV NPM_CONFIG_REGISTRY=https://artifacts.home.romaine.life/npm/npm/
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/build.mjs ./
RUN node build.mjs

# Stage 2: the server, with web/static/ (the page, its font and the engine) embedded.
FROM artifacts.home.romaine.life/dockerhub/library/golang:1.26-alpine@sha256:30b1e546a9c7fa3b747c2bdba3a0c7bdf3a02ebb4c5b8405d2f948d7e8aac4ec AS build
WORKDIR /src
ENV GOPROXY=https://artifacts.home.romaine.life/go/go,direct
ENV GOMAXPROCS=4 GOFLAGS=-p=4

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY web/embed.go ./web/embed.go
COPY web/static ./web/static
COPY --from=web /web/static/vendor ./web/static/vendor
RUN --mount=type=cache,id=terminal-share-go-build,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/terminal-share ./cmd/terminal-share

FROM artifacts.home.romaine.life/gcr/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/terminal-share /terminal-share
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/terminal-share"]
