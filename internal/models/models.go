package models

import (
	"time"

	"github.com/google/uuid"
)

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
	BetType     string     `db:"bet_type"` // 'even' or 'odd'
	DrawnNumber *int       `db:"drawn_number"`
	Result      *string    `db:"result"` // 'win' or 'lose'
	PendingWin  *float64   `db:"pending_win"`
	State       string     `db:"state"` // 'active' or 'closed'
	CreatedAt   time.Time  `db:"created_at"`
	ClosedAt    *time.Time `db:"closed_at"`
}

type Transaction struct {
	ID        uuid.UUID `db:"transaction_id"`
	SessionID uuid.UUID `db:"related_session_id"`
	ClientID  uuid.UUID `db:"client_id"`
	Amount    float64   `db:"amount"`
	Type      string    `db:"type"`
	CreatedAt time.Time `db:"created_at"`
}

// Bet type constants
const (
	BetTypeEven = "even"
	BetTypeOdd  = "odd"
)

// Result constants
const (
	ResultWin  = "win"
	ResultLose = "lose"
)

// Session state constants
const (
	StateActive = "active"
	StateClosed = "closed"
)

// Action constants for WebSocket routing
const (
	ActionWallet  = "wallet"
	ActionPlay    = "play"
	ActionEndPlay = "endplay"
)

// WSMessage define the format for input/output messages
type WSMessage struct {
	Action      string      `json:"action,omitempty"`
	Event       string      `json:"event,omitempty"`
	ClientID    string      `json:"clientId,omitempty"`
	BetAmount   float64     `json:"betAmount,omitempty"`
	BetType     string      `json:"type,omitempty"`
	Balance     *float64    `json:"balance,omitempty"`
	DrawnNumber int         `json:"drawnNumber,omitempty"`
	Result      string      `json:"result,omitempty"`
	Message     string      `json:"message,omitempty"`
	Payload     interface{} `json:"payload,omitempty"`
}
