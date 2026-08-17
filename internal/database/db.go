package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"awesomeProject/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool postgresQuerier
}

// postgresQuerier is the small part of pgxpool used by read operations
type postgresQuerier interface {
	Begin(context.Context) (pgx.Tx, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

var (
	ErrActivePlayExists  = errors.New("player already has an active play")
	ErrInsufficientFunds = errors.New("insufficient balance")
	ErrNoActiveSession   = errors.New("no active play to close")
	ErrPlayerNotFound    = errors.New("player not found")
)

func NewPostgresRepository(ctx context.Context, dsn string) (*PostgresRepository, error) {

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("database: parse dsn: %w", err)
	}

	cfg.MaxConns = 10
	cfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("database: create pool: %w", err)
	}

	const (
		maxAttempts = 5
		baseDelay   = 500 * time.Millisecond
	)

	var pingErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		pingErr = pool.Ping(ctx)
		if pingErr == nil {
			break
		}

		if attempt == maxAttempts {
			break
		}

		select {
		case <-ctx.Done():
			pool.Close()
			return nil, fmt.Errorf("database: ping cancelled: %w", ctx.Err())
		case <-time.After(baseDelay * time.Duration(attempt)):
		}
	}

	if pingErr != nil {
		pool.Close()
		return nil, fmt.Errorf("database: ping failed after %d attempts: %w", maxAttempts, pingErr)
	}

	return &PostgresRepository{pool: pool}, nil
}

// GetPlayerBalance() returns the player's current balance and if the player does not exist, it creates
func (r *PostgresRepository) GetPlayerBalance(ctx context.Context, clientID string) (float64, error) {
	const createPlayer = `
		INSERT INTO players (client_id, balance)
		VALUES ($1, 1000)
		ON CONFLICT (client_id) DO NOTHING
	`

	if _, err := r.pool.Exec(ctx, createPlayer, clientID); err != nil {
		return 0, fmt.Errorf("database: create player: %w", err)
	}

	const (
		getBalance = `
			SELECT COALESCE(balance, 0)
			FROM players
			WHERE client_id = $1`
	)

	var balance float64
	if err := r.pool.QueryRow(ctx, getBalance, clientID).Scan(&balance); err != nil {
		return 0, fmt.Errorf("database: get player balance: %w", err)
	}

	return balance, nil
}

// GetActiveSession() returns the active game session for the client, or nil if no active session exists
func (r *PostgresRepository) GetActiveSession(ctx context.Context, clientID string) (*models.GameSession, error) {
	const getActiveSession = `
SELECT session_id, client_id, bet_amount, bet_type, drawn_number, result, pending_win, state, created_at, closed_at
FROM game_session
WHERE client_id = $1 AND state = 'active'
LIMIT 1`

	var s models.GameSession
	err := r.pool.QueryRow(ctx, getActiveSession, clientID).Scan(
		&s.SessionID,
		&s.ClientID,
		&s.BetAmount,
		&s.BetType,
		&s.DrawnNumber,
		&s.Result,
		&s.PendingWin,
		&s.State,
		&s.CreatedAt,
		&s.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("database: get active session: %w", err)
	}

	return &s, nil
}

// CreatePlay() atomically validates and opens a play, debits the player's
// balance, and records the debit for auditing
func (r *PostgresRepository) CreatePlay(
	ctx context.Context,
	clientID string,
	betAmount float64,
	betType string,
	drawnNumber int,
	result string,
	pendingWin float64,
) (*models.GameSession, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("database: begin create play transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var balance float64
	const lockPlayer = `
SELECT balance
FROM players
WHERE client_id = $1
FOR UPDATE`
	if err := tx.QueryRow(ctx, lockPlayer, clientID).Scan(&balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlayerNotFound
		}
		return nil, fmt.Errorf("database: lock player for create play: %w", err)
	}

	const activePlay = `
SELECT 1
FROM game_session
WHERE client_id = $1 AND state = 'active'
LIMIT 1`
	var active int
	if err := tx.QueryRow(ctx, activePlay, clientID).Scan(&active); err == nil {
		return nil, ErrActivePlayExists
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("database: check active play: %w", err)
	}

	if betAmount > balance {
		return nil, ErrInsufficientFunds
	}

	const debitPlayer = `
UPDATE players
SET balance = balance - $1, updated_at = NOW()
WHERE client_id = $2`
	if _, err := tx.Exec(ctx, debitPlayer, betAmount, clientID); err != nil {
		return nil, fmt.Errorf("database: debit player: %w", err)
	}

	const createSession = `
INSERT INTO game_session(client_id, bet_amount, bet_type, drawn_number, result, pending_win, state)
VALUES ($1, $2, $3, $4, $5, $6, 'active')
RETURNING session_id, client_id, bet_amount, bet_type, drawn_number, result,
          pending_win, state, created_at, closed_at`

	var session models.GameSession
	if err := tx.QueryRow(
		ctx,
		createSession,
		clientID,
		betAmount,
		betType,
		drawnNumber,
		result,
		pendingWin,
	).Scan(
		&session.SessionID,
		&session.ClientID,
		&session.BetAmount,
		&session.BetType,
		&session.DrawnNumber,
		&session.Result,
		&session.PendingWin,
		&session.State,
		&session.CreatedAt,
		&session.ClosedAt,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrActivePlayExists
		}
		return nil, fmt.Errorf("database: create game session: %w", err)
	}

	const recordDebit = `
INSERT INTO transactions (related_session_id, client_id, amount, type)
VALUES ($1, $2, $3, 'debit')`
	if _, err := tx.Exec(ctx, recordDebit, session.SessionID, clientID, betAmount); err != nil {
		return nil, fmt.Errorf("database: record debit transaction: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("database: commit create play transaction: %w", err)
	}

	return &session, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// ClosePlay() atomically closes the client's active play, credits any pending winnings, and records the credit for auditing
func (r *PostgresRepository) ClosePlay(
	ctx context.Context,
	clientID string,
) (*models.GameSession, float64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("database: begin close play transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const lockPlayer = `
SELECT balance
FROM players
WHERE client_id = $1
FOR UPDATE`
	var balance float64
	if err := tx.QueryRow(ctx, lockPlayer, clientID).Scan(&balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrPlayerNotFound
		}
		return nil, 0, fmt.Errorf("database: lock player for close play: %w", err)
	}

	const lockActiveSession = `
SELECT session_id, client_id, bet_amount, bet_type, drawn_number, result,
       pending_win, state, created_at, closed_at
FROM game_session
WHERE client_id = $1 AND state = 'active'
LIMIT 1
FOR UPDATE`

	var session models.GameSession
	if err := tx.QueryRow(ctx, lockActiveSession, clientID).Scan(
		&session.SessionID,
		&session.ClientID,
		&session.BetAmount,
		&session.BetType,
		&session.DrawnNumber,
		&session.Result,
		&session.PendingWin,
		&session.State,
		&session.CreatedAt,
		&session.ClosedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrNoActiveSession
		}
		return nil, 0, fmt.Errorf("database: lock active play: %w", err)
	}

	pendingWin := float64(0)
	if session.PendingWin != nil {
		pendingWin = *session.PendingWin
	}
	if pendingWin > 0 {
		const creditPlayer = `
UPDATE players
SET balance = balance + $1, updated_at = NOW()
WHERE client_id = $2`
		if _, err := tx.Exec(ctx, creditPlayer, pendingWin, clientID); err != nil {
			return nil, 0, fmt.Errorf("database: credit player: %w", err)
		}

		const recordCredit = `
INSERT INTO transactions (related_session_id, client_id, amount, type)
VALUES ($1, $2, $3, 'credit')`
		if _, err := tx.Exec(ctx, recordCredit, session.SessionID, clientID, pendingWin); err != nil {
			return nil, 0, fmt.Errorf("database: record credit transaction: %w", err)
		}
	}

	const closeSession = `
UPDATE game_session
SET state = 'closed', closed_at = NOW()
WHERE session_id = $1
RETURNING session_id, client_id, bet_amount, bet_type, drawn_number, result,
          pending_win, state, created_at, closed_at`
	if err := tx.QueryRow(ctx, closeSession, session.SessionID).Scan(
		&session.SessionID,
		&session.ClientID,
		&session.BetAmount,
		&session.BetType,
		&session.DrawnNumber,
		&session.Result,
		&session.PendingWin,
		&session.State,
		&session.CreatedAt,
		&session.ClosedAt,
	); err != nil {
		return nil, 0, fmt.Errorf("database: close game session: %w", err)
	}

	const getBalance = `
SELECT balance
FROM players
WHERE client_id = $1`
	if err := tx.QueryRow(ctx, getBalance, clientID).Scan(&balance); err != nil {
		return nil, 0, fmt.Errorf("database: get balance after close play: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("database: commit close play transaction: %w", err)
	}

	return &session, balance, nil
}
