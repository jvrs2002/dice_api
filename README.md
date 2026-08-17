# 🎲 Dice API

A real-time **even/odd** dice game backend API built in Go using **WebSockets** and **PostgreSQL 15**. Players can check their wallet balance, place bets on `even` or `odd`, have the server roll a 6-sided die (1–6), and settle their winnings. All financial operations and game state transitions are handled **atomically and concurrency-safely** within PostgreSQL.

- **Transport:** WebSocket (`ws://localhost:8080/ws`) + HTTP Health Check (`http://localhost:8080/health`).
- **Persistence:** PostgreSQL 15 with row-level locks (`SELECT ... FOR UPDATE`), transaction rollback safety, and a partial unique index ensuring *at most one active session per player*.
- **Game Engine:** Pure domain logic without I/O dependencies, isolated in `internal/game`.
- **Web Clients:** Ready-to-use frontend interfaces included in `web/` (2D Canvas UI and 3D Three.js UI).

---

## 📑 Table of Contents

- [Stack](#-stack)
- [Architecture](#-architecture)
- [Project Structure](#-project-structure)
- [Data Structures & Schema](#-data-structures--schema)
  - [Database Schema (PostgreSQL)](#database-schema-postgresql)
  - [Go Models (`internal/models/models.go`)](#go-models-internalmodelsmodelsgo)
  - [WebSocket Contract (`WSMessage`)](#websocket-contract-wsmessage)
- [API Contract](#-api-contract)
  - [Endpoints](#endpoints)
  - [Actions & Events](#actions--events)
  - [Payload Examples](#payload-examples)
- [Getting Started](#-getting-started)
  - [Prerequisites](#prerequisites)
  - [Running with Docker & Go](#running-with-docker--go)
  - [Testing via Web Clients & CLI](#testing-via-web-clients--cli)
- [Testing](#-testing)
  - [Unit Tests](#unit-tests)
  - [Integration Tests](#integration-tests)
- [Environment Variables](#-environment-variables)
- [Error Handling](#-error-handling)
- [Related Files](#-related-files)

---

## 🧰 Stack

| Layer | Technology | Description |
|---|---|---|
| **Language** | Go **1.26** | Standard library HTTP server & concurrency primitives |
| **Transport** | [`gorilla/websocket`](https://github.com/gorilla/websocket) `v1.5.3` | Full-duplex WebSocket connection handling |
| **Database** | PostgreSQL **15** | ACID storage, partial unique indexes, check constraints |
| **Driver & Pool** | [`jackc/pgx/v5`](https://github.com/jackc/pgx) `v5.10.0` | High-performance driver using `pgxpool` |
| **UUIDs** | [`google/uuid`](https://github.com/google/uuid) `v1.6.0` | UUID v4 generation & serialization |
| **Infrastructure** | Docker & Docker Compose | Local database provisioning with automated schema initialization |
| **Frontend** | HTML5 / JavaScript / Three.js | Lightweight web clients in `web/` |

---

## 🏗️ Architecture

The application follows a clean, layered technical architecture:
- The **transport layer** (`internal/websocket`) handles HTTP upgrades and WebSocket frame serialization, but knows nothing about SQL queries.
- The **game engine** (`internal/game`) contains pure domain logic and mathematical rules, completely independent from databases and network layers.
- The **database layer** (`internal/database`) encapsulates all SQL queries, transaction management, row-level locking, and connection pool resiliency.

```mermaid
flowchart LR
    C[Client / Browser / Postman] -->|JSON via WebSocket| WS[internal/websocket<br/>Handler]
    WS --> G[internal/game<br/>Pure Engine]
    WS --> R[internal/database<br/>PostgresRepository]
    R --> DB[(PostgreSQL 15)]
    WS -->|Uses structs| M[internal/models]
    G -->|Uses constants| M
    R -->|Uses structs| M
    MAIN[cmd/main.go] -->|Wires dependencies| WS
    MAIN -->|Initializes pool| R
```

| Package / File | Responsibility |
|---|---|
| [`cmd/main.go`](./cmd/main.go) | **Composition Root**: Initializes database pool with retry/backoff, mounts HTTP router, and coordinates graceful shutdown. |
| [`internal/websocket`](./internal/websocket) | Manages WebSocket connections, JSON message loop, edge validation, action dispatching, and error formatting. |
| [`internal/game`](./internal/game) | **Pure Game Engine**: Bet validation (`ValidateBet`), random roll generation (`RollDice`), and win/loss resolution (`Play`). |
| [`internal/database`](./internal/database) | **Persistence Repository**: Executes queries, acquires row locks (`FOR UPDATE`), and ensures atomic multi-step balance debit/credit. |
| [`internal/models`](./internal/models) | Data structures, database entities, domain constants, and WebSocket message schemas. |
| [`web`](./web) | Static HTML/JS frontend test clients (2D interface and 3D Three.js dice roller). |

---

## 📁 Project Structure

```text
.
├── cmd/
│   └── main.go                     # Application entrypoint, wiring, and graceful shutdown
├── internal/
│   ├── database/
│   │   ├── db.go                   # PostgresRepository (pgxpool, transactions, queries)
│   │   ├── db_test.go              # Unit tests for repository initialization and retries
│   │   ├── db_integration_test.go  # Live integration tests with PostgreSQL
│   │   └── seed_test.go            # Test fixture helpers and seed data
│   ├── game/
│   │   └── engine.go               # Pure domain game engine (ValidateBet, RollDice, Play)
│   ├── models/
│   │   └── models.go               # Database models, domain constants, and WSMessage schema
│   └── websocket/
│       └── handler.go              # WebSocket upgrader, reader loop, routing, and error mapping
├── web/
│   ├── index.html                  # Interactive 2D web client interface
│   └── threejs.html                # Interactive 3D Three.js dice rolling interface
├── docker-compose.yaml             # PostgreSQL 15 container definition
├── go.mod                          # Go module definition
├── go.sum                          # Go module checksums
├── init.sql                        # PostgreSQL schema: tables, constraints, and indexes
├── postman_collection.json         # Postman collection for WebSocket and health endpoints
└── README.md                       # Main project documentation
```

---

## 🗃️ Data Structures & Schema

### Database Schema (PostgreSQL)

Defined in [`init.sql`](./init.sql). The system manages three core tables: `players`, `game_session`, and `transactions`.

```mermaid
erDiagram
    players ||--o{ game_session : "has"
    players ||--o{ transactions : "records"
    game_session ||--o{ transactions : "generates"

    players {
        UUID        client_id PK "default uuid_generate_v4()"
        NUMERIC     balance      "NOT NULL, default 0.00, CHECK >= 0"
        TIMESTAMPTZ created_at   "default NOW()"
        TIMESTAMPTZ updated_at   "default NOW()"
    }
    game_session {
        UUID        session_id   PK "default uuid_generate_v4()"
        UUID        client_id    FK "references players(client_id)"
        NUMERIC     bet_amount      "NOT NULL, CHECK > 0"
        TEXT        bet_type        "NOT NULL, CHECK in ('even','odd')"
        INT         drawn_number    "nullable, 1..6"
        TEXT        result          "nullable, CHECK in ('win','lose')"
        NUMERIC     pending_win     "nullable, CHECK >= 0"
        TEXT        state           "NOT NULL, default 'active', CHECK in ('active','closed')"
        TIMESTAMPTZ created_at      "default NOW()"
        TIMESTAMPTZ closed_at       "nullable"
    }
    transactions {
        UUID        transaction_id     PK "default uuid_generate_v4()"
        UUID        related_session_id FK "nullable, references game_session(session_id)"
        UUID        client_id          FK "references players(client_id)"
        NUMERIC     amount             "NOT NULL"
        TEXT        type               "NOT NULL, 'debit' | 'credit'"
        TIMESTAMPTZ created_at         "default NOW()"
    }
```

#### Table Details & Constraints

- **`players`** — Stores the player's wallet balance:
  - `client_id` (`UUID`, PK): Unique player identifier.
  - `balance` (`NUMERIC(15,2)`): Current balance. Guarded by `CHECK (balance >= 0)`.
  - `created_at` / `updated_at` (`TIMESTAMPTZ`): Timestamps.

- **`game_session`** — Represents each individual game round:
  - `session_id` (`UUID`, PK): Unique session identifier.
  - `client_id` (`UUID`, FK): References `players.client_id`.
  - `bet_amount` (`NUMERIC(15,2)`): Wager amount (`CHECK (bet_amount > 0)`).
  - `bet_type` (`TEXT`): `'even'` or `'odd'`.
  - `drawn_number` (`INT`): Drawn die number (1–6).
  - `result` (`TEXT`): `'win'` or `'lose'`.
  - `pending_win` (`NUMERIC(15,2)`): Winnings pending settlement (`betAmount * 2` on win, `0` on loss).
  - `state` (`TEXT`): `'active'` or `'closed'`.
  - `created_at` / `closed_at` (`TIMESTAMPTZ`): Round start and close timestamps.
  - **Constraint:** `game_session_closed_at_consistent` enforces that `active` sessions must have `closed_at IS NULL`, while `closed` sessions must have `closed_at IS NOT NULL`.
  - **Partial Unique Index:** `idx_game_session_client_state` on `(client_id, state) WHERE (state = 'active')` ensures that a player can never have more than one active round concurrently, even under race conditions.

- **`transactions`** — Complete audit trail of balance movements:
  - `transaction_id` (`UUID`, PK): Unique record identifier.
  - `related_session_id` (`UUID`, FK, nullable): Associated game session.
  - `client_id` (`UUID`, FK): Associated player.
  - `amount` (`NUMERIC(15,2)`): Movement value.
  - `type` (`TEXT`): `'debit'` (wager placed) or `'credit'` (payout credited).
  - `created_at` (`TIMESTAMPTZ`): Transaction timestamp.
  - **Index:** `idx_transactions_client_created_at` on `(client_id, created_at)` for historical lookups.

---

### Go Models ([`internal/models/models.go`](./internal/models/models.go))

```go
type Player struct {
    ClientID  uuid.UUID `db:"client_id"`
    Balance   float64   `db:"balance"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}

type GameSession struct {
    SessionID   uuid.UUID  `db:"session_id"`
    ClientID    uuid.UUID  `db:"client_id"`
    BetAmount   float64    `db:"bet_amount"`
    BetType     string     `db:"bet_type"`     // 'even' | 'odd'
    DrawnNumber *int       `db:"drawn_number"` // 1..6
    Result      *string    `db:"result"`       // 'win' | 'lose'
    PendingWin  *float64   `db:"pending_win"`  // nullable
    State       string     `db:"state"`        // 'active' | 'closed'
    CreatedAt   time.Time  `db:"created_at"`
    ClosedAt    *time.Time `db:"closed_at"`    // nullable
}

type Transaction struct {
    ID        uuid.UUID `db:"transaction_id"`
    SessionID uuid.UUID `db:"related_session_id"`
    ClientID  uuid.UUID `db:"client_id"`
    Amount    float64   `db:"amount"`
    Type      string    `db:"type"`           // 'debit' | 'credit'
    CreatedAt time.Time `db:"created_at"`
}
```

---

### WebSocket Contract (`WSMessage`)

A unified message struct is used across incoming actions and outgoing event broadcasts:

```go
type WSMessage struct {
    Action      string      `json:"action,omitempty"`      // Incoming action: wallet | play | endplay
    Event       string      `json:"event,omitempty"`       // Outgoing event: wallet | play_result | play_closed | error
    ClientID    string      `json:"clientId,omitempty"`    // Player UUID
    BetAmount   float64     `json:"betAmount,omitempty"`   // Incoming (play): Bet value
    BetType     string      `json:"type,omitempty"`        // Incoming (play): "even" | "odd"
    Balance     *float64    `json:"balance,omitempty"`     // Outgoing (wallet / play_result / play_closed)
    DrawnNumber int         `json:"drawnNumber,omitempty"` // Outgoing (play_result): 1..6
    Result      string      `json:"result,omitempty"`      // Outgoing (play_result): "win" | "lose"
    Message     string      `json:"message,omitempty"`     // Outgoing (error): Descriptive error string
    Payload     interface{} `json:"payload,omitempty"`
}
```

---

## 📡 API Contract

### Endpoints

| Protocol | Path | Description |
|---|---|---|
| **WebSocket** | `/ws` | Main full-duplex WebSocket connection for game actions |
| **HTTP** | `/health` | Liveness and health check endpoint (returns HTTP 200 `ok`) |

---

### Actions & Events

Every WebSocket action requires a valid `clientId` UUID string.

| Action (`action`) | Input Fields | Emitted Event (`event`) | Response Payload | Description |
|---|---|---|---|---|
| `wallet` | `clientId` | `wallet` | `balance` | Returns the player's balance. Automatically provisions new players with **1000.00** starting balance. |
| `play` | `clientId`, `betAmount`, `type` (`even` \| `odd`) | `play_result` | `drawnNumber`, `result` | Validates bet, rolls die (1–6), debits wager, creates active session, and returns roll outcome. |
| `endplay` | `clientId` | `play_closed` | `balance` | Closes the active session, credits pending winnings, and returns the updated balance. |

---

### Payload Examples

#### 1. Check Wallet Balance (`wallet`)
**Request:**
```json
{
  "action": "wallet",
  "clientId": "11111111-1111-1111-1111-111111111111"
}
```
**Response:**
```json
{
  "event": "wallet",
  "clientId": "11111111-1111-1111-1111-111111111111",
  "balance": 1000
}
```

#### 2. Place a Bet (`play`)
**Request:**
```json
{
  "action": "play",
  "clientId": "11111111-1111-1111-1111-111111111111",
  "betAmount": 100,
  "type": "even"
}
```
**Response:**
```json
{
  "event": "play_result",
  "clientId": "11111111-1111-1111-1111-111111111111",
  "drawnNumber": 4,
  "result": "win"
}
```

#### 3. Close Session (`endplay`)
**Request:**
```json
{
  "action": "endplay",
  "clientId": "11111111-1111-1111-1111-111111111111"
}
```
**Response:**
```json
{
  "event": "play_closed",
  "clientId": "11111111-1111-1111-1111-111111111111",
  "balance": 1100
}
```

#### 4. Error Responses
```json
{
  "event": "error",
  "clientId": "11111111-1111-1111-1111-111111111111",
  "message": "player already has an active play"
}
```

---

## ▶️ Getting Started

### Prerequisites

- **Go:** 1.26 or higher
- **Docker & Docker Compose** (for PostgreSQL)

---

### Running with Docker & Go

#### 1. Start PostgreSQL
```bash
docker-compose up -d
```
> The PostgreSQL container starts on port `5432` and applies [`init.sql`](./init.sql) on initial boot.

#### 2. Run the Application
```bash
export DATABASE_URL="postgres://root:root@localhost:5432/dice_api?sslmode=disable"
export HTTP_ADDR=":8080"
go run ./cmd
```

#### 3. Health Check
```bash
curl http://localhost:8080/health
# Output: ok
```

---

### Testing via Web Clients & CLI

#### Option A: Web Browser UIs
- **Standard 2D UI:** Open [`web/index.html`](./web/index.html) directly in your browser.
- **3D Three.js UI:** Open [`web/threejs.html`](./web/threejs.html) in your browser for real-time 3D dice physics and animations.

#### Option B: Postman
Import [`postman_collection.json`](./postman_collection.json) to execute the complete test suite.

#### Option C: WebSocket CLI (`wscat`)
```bash
wscat -c ws://localhost:8080/ws

# Check wallet
> {"action":"wallet","clientId":"11111111-1111-1111-1111-111111111111"}

# Place a bet
> {"action":"play","clientId":"11111111-1111-1111-1111-111111111111","betAmount":100,"type":"even"}

# Settle winnings
> {"action":"endplay","clientId":"11111111-1111-1111-1111-111111111111"}
```

---

## 🧪 Testing

### Unit Tests
```bash
go test -v ./internal/database
```

### Integration Tests
```bash
RUN_INTEGRATION_TESTS=1 TEST_DATABASE_URL="postgres://root:root@localhost:5432/dice_api?sslmode=disable" go test -v ./internal/database
```

---

## 🔐 Environment Variables

| Variable | Default Value | Description |
|---|---|---|
| `DATABASE_URL` | `postgres://root:root@localhost:5432/dice_api?sslmode=disable` | PostgreSQL DSN connection string |
| `HTTP_ADDR` | `:8080` | Bind address for HTTP and WebSocket server |

---

## 🚨 Error Handling

All WebSocket error events follow this payload structure:
```json
{
  "event": "error",
  "clientId": "<client-id>",
  "message": "<descriptive-error-message>"
}
```

| Error Scenario | Emitted `message` | Connection Status |
|---|---|---|
| Invalid or malformed JSON | `invalid JSON message` | Maintained |
| Missing or whitespace-only `clientId` | `clientId is required` | Maintained |
| Bet amount `betAmount <= 0` | `betAmount must be greater than 0` | Maintained |
| Bet type not `'even'` or `'odd'` | `type must be 'even' or 'odd'` | Maintained |
| Unknown action received | `unknown action` | Maintained |
| Concurrent active session exists | `player already has an active play` | Maintained |
| Insufficient player balance | `insufficient balance` | Maintained |
| `endplay` called without an active session | `no active play to close` | Maintained |
| Player record not found | `player not found` | Maintained |
| Unexpected internal / database error | `internal error` | Maintained (logged on server) |

---

## 📚 Related Files

- [`init.sql`](./init.sql) — PostgreSQL schema definition and constraints.
- [`docker-compose.yaml`](./docker-compose.yaml) — Docker Compose configuration for PostgreSQL.
- [`postman_collection.json`](./postman_collection.json) — Postman collection with test requests.
- [`web/index.html`](./web/index.html) — 2D web client.
- [`web/threejs.html`](./web/threejs.html) — 3D Three.js dice animation web client.
