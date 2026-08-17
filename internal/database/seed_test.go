package database

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testSeedPlayer represents a seeded player fixture.
type testSeedPlayer struct {
	ClientID uuid.UUID
	Balance  float64
}

// testSeed holds seeded test fixtures and provides access to the connection pool.
type testSeed struct {
	Pool    *pgxpool.Pool
	Players []testSeedPlayer
}

// seedIntegration provisions test players in the database and registers a cleanup
// callback to truncate data in foreign-key order after the test finishes.
func seedIntegration(t *testing.T, players []testSeedPlayer) *testSeed {
	t.Helper()

	if os.Getenv("RUN_INTEGRATION_TESTS") == "" {
		t.Skip("set RUN_INTEGRATION_TESTS=1 to run integration tests")
	}

	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = defaultIntegrationDSN
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("seedIntegration: open pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("seedIntegration: ping: %v", err)
	}

	ids := make([]uuid.UUID, 0, len(players))
	for i, p := range players {
		id := p.ClientID
		if id == uuid.Nil {
			id = uuid.New()
			players[i].ClientID = id
		}
		ids = append(ids, id)

		if _, err := pool.Exec(ctx,
			`INSERT INTO players (client_id, balance) VALUES ($1, $2)`,
			id, p.Balance,
		); err != nil {
			pool.Close()
			t.Fatalf("seedIntegration: insert player %s: %v", id, err)
		}
	}

	t.Cleanup(func() {
		bg := context.Background()
		for _, id := range ids {
			_, _ = pool.Exec(bg, `DELETE FROM transactions WHERE client_id = $1`, id)
			_, _ = pool.Exec(bg, `DELETE FROM game_session WHERE client_id = $1`, id)
			_, _ = pool.Exec(bg, `DELETE FROM players      WHERE client_id = $1`, id)
		}
		pool.Close()
	})

	return &testSeed{Pool: pool, Players: players}
}
