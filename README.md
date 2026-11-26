# terminal-share

Share what a home PC's terminal window shows as a public web page.

Picking "Share this window" in the at menu captures the window the menu is open over
(every pane, in its layout) with `term-share` (romaine-life/at-home-tools) and posts it
here. The service stores the snapshot under an unguessable id and answers with a link,
`https://share.romaine.life/s/<id>`, that anyone can open for 30 days. The page draws the
snapshot with Rio's terminal engine compiled for the browser, in the terminal's own font
and colors.

Founding work package: [TSH-1](https://openproject.home.romaine.life/wp/TSH-1).

## What's here

| Path | What it is |
|---|---|
| `cmd/terminal-share/` | The server; `--self-test` checks a built image, `--memory` runs without Azure |
| `internal/server/` | The routes, the snapshot's validation, the request log |
| `internal/auth/` | The machine-token check against auth.romaine.life's keys |
| `internal/store/` | `Store`: Azure Blob in production, in memory for tests |
| `internal/testissuer/` | A stand-in auth.romaine.life (signing keys and a JWKS) for the tests and self-test |
| `web/` | The page (`static/`), embedded in the binary; its engine, the npm package `rioterm`, pinned by `package-lock.json` and copied into `static/vendor/` by `build.mjs` |
| `web/static/fonts/` | CaskaydiaMono Nerd Font (Regular, Bold) from the Nerd Fonts v3.5.1 release, as WOFF2; SIL Open Font License, `LICENSE.txt` |
| `k8s/` | The Helm chart Argo CD runs |
| `tofu/` | The storage account, its container and lifecycle rule, and the pod's Azure identity |

## Contract

`POST /api/snapshots` with `Authorization: Bearer <token>` and a JSON body of at most
1 MiB (413 beyond):

```json
{"cols": 120, "rows": 40, "vt": "<UTF-8 VT stream that redraws the screen on a cols x rows terminal>",
 "title": "<window name>",
 "theme": {"foreground": "#RRGGBB", "background": "#RRGGBB", "cursor": "#RRGGBB",
           "palette": ["16 x #RRGGBB, ANSI 0-15 in order"]},
 "font": {"family": "CaskaydiaMono NF"}}
```

- `cols` 1-1000, `rows` 1-500, `vt` non-empty, `title` optional (at most 256
  characters), colors `#RRGGBB`, `palette` exactly 16, `font.family` one family name
  (letters, digits, spaces and `. _ + ( ) -`). Anything else is a 400.
- The token is an auth.romaine.life machine token: RS256 against
  `https://auth.romaine.life/api/auth/jwks`, issuer and audience
  `https://auth.romaine.life`, `purpose` `machine`, and a `device` claim naming a home
  PC: `nelsonpc`, `upstairspc`, `treadmill` or `nelsonlaptop`. No token, or one that
  doesn't verify, is a 401; a valid token that isn't a home PC's machine token is a 403.
  The keys are fetched when a token names a key id the service doesn't hold, never on a
  timer.
- 201: `{"id": "<26 characters>", "url": "https://share.romaine.life/s/<id>", "expires_at": "<RFC 3339>"}`.
  The id is 128 bits from `crypto/rand`, lowercase base32 without padding; a share expires
  30 days after it is created.

`GET /api/snapshots/<id>` answers with the stored body plus `id`, `created_at`,
`expires_at` and `device`, or 404 `{"error": "This share has expired or never existed."}`
once `expires_at` has passed or for an id it doesn't hold.

Every error is `{"error": "<one sentence>"}`.

`GET /s/<id>` is the page, `GET /` says what the site is, `GET /healthz` answers `ok`.

## How it fits together

- `term-share` (at-home-tools) captures the window, mints a machine token with the PC's
  auth.romaine.life enrollment, and posts the snapshot.
- The service keeps each snapshot as the blob `<id>.json` in the `snapshots` container,
  signing in with its workload identity (`terminal-share-identity`, federated to the
  `terminal-share` ServiceAccount). The container's lifecycle rule deletes blobs 30 days
  after creation; the service refuses an expired share itself, whether or not the blob is
  gone yet. There is no other state.
- The page fetches the snapshot, creates a rioterm `Terminal` of its `cols` x `rows`,
  writes the `vt` stream into it with the cursor hidden, and draws it with rioterm's
  `CanvasRenderer` in the snapshot's colors and font, sized to the page's width. It shows
  the title and "shared <time ago>, expires <date>" above it, and says so in words while
  loading, for an expired or unknown share, and when the network fails.
- Each request is one line on stdout in the estate's log line
  ([logline](https://forgejo.romaine.life/romaine-life/logline), AHT-204): `request` with
  `method`, `path` (a share's id cut to its first six characters), `status`, `ms`, the
  `device` for a share and, when it failed, the `error` (masked); a server failure is an
  `error` line. Tokens and snapshot content are never logged; a passing health check isn't.

## Running and testing locally

```sh
npm --prefix web ci
npm --prefix web run build      # the engine into web/static/vendor/
go test -p 4 ./...
npm --prefix web test           # the page's logic, and rioterm drawing a snapshot headless
go run ./cmd/terminal-share --memory
```

With `--memory` it listens on `:8080` (`PORT`) and keeps snapshots in memory. A real
machine token from `romaine-auth token --out <file>` (its `token` field) posts to it.
`go run ./cmd/terminal-share --self-test` runs the image's check against the local build.
In the cluster it reads `STORAGE_ACCOUNT_URL`, `STORAGE_CONTAINER` and `PUBLIC_URL`.

## How it ships

CI comes from the pipeline-templates suites `go`, `web-app` (with `web-image`),
`cluster-service` and `tofu`, selected by this repository's entry in pipeline-templates'
`fleet.json`. A pull request runs the Go and npm tests, renders and checks `k8s/`, plans
`tofu/`, and builds the image and runs its `--self-test`. A merge applies `tofu/`,
publishes `romainecr.azurecr.io/terminal-share`, and opens a pin PR that rewrites the
image in `k8s/values.yaml` to the new tag and digest. Argo CD runs `k8s/` from its
Application in infra-bootstrap's `k8s/apps`, in the `terminal-share` namespace;
external-dns publishes `share.romaine.life` from the HTTPRoute, and cert-manager issues
its certificate.

`serviceAccount.clientId` in `k8s/values.yaml` is the tofu output
`terminal_share_identity_client_id`, the pod's workload identity.
