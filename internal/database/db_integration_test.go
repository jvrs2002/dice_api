package database

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultIntegrationDSN = "postgres://root:root@localhost:5432/dice_api?sslmode=disable"

func TestNewPostgresRepository_Integration(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run integration tests")
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	repo, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	if repo == nil {
		t.Fatal("NewPostgresRepository() returned a nil repository")
	}

	pool, ok := repo.pool.(*pgxpool.Pool)
	if !ok {
		t.Fatalf("repository pool has unexpected type %T", repo.pool)
	}
	defer pool.Close()
}

func TestCreatePlay_Integration(t *testing.T) {
	seed := seedIntegration(t, []testSeedPlayer{
		{ClientID: uuid.New(), Balance: 100},
		{ClientID: uuid.New(), Balance: 100},
	})
	clientID := seed.Players[0].ClientID
	poorClientID := seed.Players[1].ClientID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	repo, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	p := seed.Pool

	session, err := repo.CreatePlay(ctx, clientID.String(), 25, "even", 4, "win", 50)
	if err != nil {
		t.Fatalf("CreatePlay() error = %v", err)
	}
	if session.State != "active" || session.BetAmount != 25 || session.DrawnNumber == nil || *session.DrawnNumber != 4 {
		t.Fatalf("CreatePlay() returned unexpected session: %+v", session)
	}

	balance, err := repo.GetPlayerBalance(ctx, clientID.String())
	if err != nil {
		t.Fatalf("GetPlayerBalance() error = %v", err)
	}
	if balance != 75 {
		t.Fatalf("balance after play = %v, want 75", balance)
	}

	if _, err := repo.CreatePlay(ctx, clientID.String(), 10, "odd", 3, "win", 20); !errors.Is(err, ErrActivePlayExists) {
		t.Fatalf("second CreatePlay() error = %v, want ErrActivePlayExists", err)
	}

	if _, err := repo.CreatePlay(ctx, poorClientID.String(), 101, "even", 2, "win", 202); !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("insufficient-balance CreatePlay() error = %v, want ErrInsufficientFunds", err)
	}

	var amount float64
	var transactionType string
	if err := p.QueryRow(ctx, `
		SELECT amount, type
		FROM transactions
		WHERE related_session_id = $1`, session.SessionID).Scan(&amount, &transactionType); err != nil {
		t.Fatalf("query debit transaction: %v", err)
	}
	if amount != 25 || transactionType != "debit" {
		t.Fatalf("debit transaction = amount %v, type %q; want 25, debit", amount, transactionType)
	}
}

func TestClosePlay_Integration(t *testing.T) {
	seed := seedIntegration(t, []testSeedPlayer{
		{ClientID: uuid.New(), Balance: 100},
		{ClientID: uuid.New(), Balance: 100},
	})
	winningClientID := seed.Players[0].ClientID
	losingClientID := seed.Players[1].ClientID

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	repo, err := NewPostgresRepository(ctx, dsn)
	if err != nil {
		t.Fatalf("NewPostgresRepository() error = %v", err)
	}
	p := seed.Pool

	winningSession, err := repo.CreatePlay(ctx, winningClientID.String(), 25, "even", 4, "win", 75)
	if err != nil {
		t.Fatalf("CreatePlay() winning game error = %v", err)
	}
	closedSession, balance, err := repo.ClosePlay(ctx, winningClientID.String())
	if err != nil {
		t.Fatalf("ClosePlay() winning game error = %v", err)
	}
	if closedSession.State != "closed" || closedSession.ClosedAt == nil {
		t.Fatalf("ClosePlay() returned unexpected closed session: %+v", closedSession)
	}
	if balance != 150 {
		t.Fatalf("winning balance = %v, want 150", balance)
	}

	var creditAmount float64
	var transactionType string
	if err := p.QueryRow(ctx, `
SELECT amount, type
FROM transactions
WHERE related_session_id = $1 AND type = 'credit'`, winningSession.SessionID).Scan(&creditAmount, &transactionType); err != nil {
		t.Fatalf("query credit transaction: %v", err)
	}
	if creditAmount != 75 || transactionType != "credit" {
		t.Fatalf("credit transaction = amount %v, type %q; want 75, credit", creditAmount, transactionType)
	}

	if _, _, err := repo.ClosePlay(ctx, winningClientID.String()); !errors.Is(err, ErrNoActiveSession) {
		t.Fatalf("second ClosePlay() error = %v, want ErrNoActiveSession", err)
	}

	losingSession, err := repo.CreatePlay(ctx, losingClientID.String(), 25, "odd", 4, "lose", 0)
	if err != nil {
		t.Fatalf("CreatePlay() losing game error = %v", err)
	}
	losingClosed, losingBalance, err := repo.ClosePlay(ctx, losingClientID.String())
	if err != nil {
		t.Fatalf("ClosePlay() losing game error = %v", err)
	}
	if losingClosed.State != "closed" || losingBalance != 75 {
		t.Fatalf("losing close result = state %q, balance %v; want closed, 75", losingClosed.State, losingBalance)
	}

	var creditCount int
	if err := p.QueryRow(ctx, `
SELECT COUNT(*)
FROM transactions
WHERE related_session_id = $1 AND type = 'credit'`, losingSession.SessionID).Scan(&creditCount); err != nil {
		t.Fatalf("query losing credit transactions: %v", err)
	}
	if creditCount != 0 {
		t.Fatalf("losing game credit transaction count = %d, want 0", creditCount)
	}
}
