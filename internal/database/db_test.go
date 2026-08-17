package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"awesomeProject/internal/models"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type balanceQuerier struct {
	execErr   error
	row       pgx.Row
	execSQL   string
	querySQL  string
	execArgs  []any
	queryArgs []any
}

func (f *balanceQuerier) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, errors.New("transactions are not supported by this fake")
}

func (f *balanceQuerier) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execSQL = sql
	f.execArgs = args
	return pgconn.CommandTag{}, f.execErr
}

func (f *balanceQuerier) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.querySQL = sql
	f.queryArgs = args
	return f.row
}

type balanceRow struct {
	balance float64
	err     error
}

func (r balanceRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	value, ok := dest[0].(*float64)
	if !ok {
		return fmt.Errorf("unexpected scan destination %T", dest[0])
	}
	*value = r.balance
	return nil
}

func TestGetPlayerBalance_ReturnsBalance(t *testing.T) {
	querier := &balanceQuerier{row: balanceRow{balance: 125.50}}
	repo := &PostgresRepository{pool: querier}

	balance, err := repo.GetPlayerBalance(context.Background(), "client-1")
	if err != nil {
		t.Fatalf("GetPlayerBalance() error = %v", err)
	}
	if balance != 125.50 {
		t.Fatalf("GetPlayerBalance() = %v, want 125.50", balance)
	}
	if !strings.Contains(querier.execSQL, "ON CONFLICT (client_id) DO NOTHING") {
		t.Errorf("player creation query does not upsert safely: %s", querier.execSQL)
	}
	if !strings.Contains(querier.querySQL, "COALESCE(balance, 0)") {
		t.Errorf("balance query does not handle NULL: %s", querier.querySQL)
	}
	if len(querier.execArgs) != 1 || querier.execArgs[0] != "client-1" {
		t.Errorf("exec arguments = %#v, want client-1", querier.execArgs)
	}
	if len(querier.queryArgs) != 1 || querier.queryArgs[0] != "client-1" {
		t.Errorf("query arguments = %#v, want client-1", querier.queryArgs)
	}
}

func TestGetPlayerBalance_CreatePlayerError(t *testing.T) {
	wantErr := errors.New("insert failed")
	repo := &PostgresRepository{pool: &balanceQuerier{execErr: wantErr}}

	_, err := repo.GetPlayerBalance(context.Background(), "client-1")
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("GetPlayerBalance() error = %v, want wrapped insert error", err)
	}
}

func TestGetPlayerBalance_QueryError(t *testing.T) {
	wantErr := errors.New("select failed")
	repo := &PostgresRepository{pool: &balanceQuerier{row: balanceRow{err: wantErr}}}

	_, err := repo.GetPlayerBalance(context.Background(), "client-1")
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("GetPlayerBalance() error = %v, want wrapped select error", err)
	}
}

func TestNewPostgresRepository_InvalidDSN(t *testing.T) {
	ctx := context.Background()

	repo, err := NewPostgresRepository(ctx, "not-a-valid-dsn")
	if err == nil {
		t.Error("expected error, got nil")
	}

	if repo != nil {
		t.Fatal("expected nil repository error")
	}

	if !strings.Contains(err.Error(), "parse dsn") {
		t.Fatal("expected a dsn parse error, got: ", err)

	}
}
func TestNewPostgresRepository_PingFailsAfterRetries(t *testing.T) {
	// Port 1 is reserved and nothing listens on it, so the connection is
	// refused immediately. This keeps the test fast and independent of a
	// real Postgres instance, while still exercising the retry/backoff loop.
	dsn := "postgres://user:pass@127.0.0.1:1/dbname?sslmode=disable"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	start := time.Now()
	repo, err := NewPostgresRepository(ctx, dsn)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when postgres is unreachable, got nil")
	}
	if repo != nil {
		t.Fatal("expected nil repository on error")
	}
	if !strings.Contains(err.Error(), "ping failed after") {
		t.Errorf("expected a ping-failed error, got: %v", err)
	}
	// Sanity check: the retry/backoff loop should have actually run
	// (5 attempts with increasing delay), not failed on the first try.
	if elapsed < 500*time.Millisecond {
		t.Errorf("expected retries with backoff, finished too fast: %v", elapsed)
	}
}

func TestNewPostgresRepository_ContextCancelledDuringRetry(t *testing.T) {
	dsn := "postgres://user:pass@127.0.0.1:1/dbname?sslmode=disable"

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	repo, err := NewPostgresRepository(ctx, dsn)

	if err == nil {
		t.Fatal("expected error when context is cancelled, got nil")
	}
	if repo != nil {
		t.Fatal("expected nil repository on error")
	}
	if !strings.Contains(err.Error(), "ping cancelled") {
		t.Errorf("expected a cancellation error, got: %v", err)
	}

}

type sessionQueryer struct {
	row      pgx.Row
	querySQL string
	args     []any
}

func (f *sessionQueryer) Begin(_ context.Context) (pgx.Tx, error) {
	return nil, errors.New("transactions are not supported by this fake")
}

func (f *sessionQueryer) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (f *sessionQueryer) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	f.querySQL = sql
	f.args = args
	return f.row
}

type sessionRow struct {
	session *models.GameSession
	err     error
}

func (r sessionRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 10 {
		return fmt.Errorf("unexpected destination count %d", len(dest))
	}
	*(dest[0].(*uuid.UUID)) = r.session.SessionID
	*(dest[1].(*uuid.UUID)) = r.session.ClientID
	*(dest[2].(*float64)) = r.session.BetAmount
	*(dest[3].(*string)) = r.session.BetType
	*(dest[4].(**int)) = r.session.DrawnNumber
	*(dest[5].(**string)) = r.session.Result
	*(dest[6].(**float64)) = r.session.PendingWin
	*(dest[7].(*string)) = r.session.State
	*(dest[8].(*time.Time)) = r.session.CreatedAt
	*(dest[9].(**time.Time)) = r.session.ClosedAt
	return nil
}

func TestGetActiveSession_Found(t *testing.T) {
	sessionID := uuid.New()
	clientID := uuid.New()
	now := time.Now().UTC()

	expectedSession := &models.GameSession{
		SessionID:   sessionID,
		ClientID:    clientID,
		BetAmount:   50.0,
		BetType:     "even",
		DrawnNumber: new(4),
		Result:      new("win"),
		PendingWin:  new(100.0),
		State:       "active",
		CreatedAt:   now,
		ClosedAt:    nil,
	}

	querier := &sessionQueryer{row: sessionRow{session: expectedSession}}
	repo := &PostgresRepository{pool: querier}

	session, err := repo.GetActiveSession(t.Context(), clientID.String())
	if err != nil {
		t.Fatalf("GetActiveSession() error = %v", err)
	}
	if session == nil {
		t.Fatal("GetActiveSession() returned nil session, want active session")
	}

	if session.SessionID != expectedSession.SessionID {
		t.Errorf("SessionID = %v, want %v", session.SessionID, expectedSession.SessionID)
	}
	if session.ClientID != expectedSession.ClientID {
		t.Errorf("ClientID = %v, want %v", session.ClientID, expectedSession.ClientID)
	}
	if session.BetAmount != expectedSession.BetAmount {
		t.Errorf("BetAmount = %v, want %v", session.BetAmount, expectedSession.BetAmount)
	}
	if session.BetType != expectedSession.BetType {
		t.Errorf("BetType = %v, want %v", session.BetType, expectedSession.BetType)
	}
	if session.State != expectedSession.State {
		t.Errorf("State = %v, want %v", session.State, expectedSession.State)
	}
	if !strings.Contains(querier.querySQL, "state = 'active'") {
		t.Errorf("query does not filter for active state: %s", querier.querySQL)
	}
	if len(querier.args) != 1 || querier.args[0] != clientID.String() {
		t.Errorf("query arguments = %#v, want %s", querier.args, clientID.String())
	}
}

func TestGetActiveSession_NotFound(t *testing.T) {
	querier := &sessionQueryer{row: sessionRow{err: pgx.ErrNoRows}}
	repo := &PostgresRepository{pool: querier}

	session, err := repo.GetActiveSession(t.Context(), "client-1")
	if err != nil {
		t.Fatalf("GetActiveSession() error = %v, want nil for ErrNoRows", err)
	}
	if session != nil {
		t.Fatalf("GetActiveSession() = %v, want nil", session)
	}
}

func TestGetActiveSession_QueryError(t *testing.T) {
	wantErr := errors.New("connection failed")
	repo := &PostgresRepository{pool: &sessionQueryer{row: sessionRow{err: wantErr}}}

	session, err := repo.GetActiveSession(t.Context(), "client-1")
	if err == nil || !errors.Is(err, wantErr) {
		t.Fatalf("GetActiveSession() error = %v, want wrapped query error", err)
	}
	if session != nil {
		t.Fatalf("GetActiveSession() = %v, want nil on error", session)
	}
}
