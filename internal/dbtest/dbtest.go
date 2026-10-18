// Package dbtest is the shared Postgres-backed harness for tests that exercise a
// data path, per ADR-0003. Each test package's TestMain calls Main, which
// provisions one Postgres for that package with the migrations applied once; the
// package's tests then take an emptied database with Setup or inspect what a
// request stored with Queries.
package dbtest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/sql/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// postgresImage is pinned to the major production runs: the vocabkr Cluster in
// the infra chart sets no imageName, so CloudNativePG 1.30 (chart 0.29.1) serves
// PostgreSQL 18.
const postgresImage = "postgres:18-alpine"

var (
	testPool   *pgxpool.Pool
	testDB     *db.Queries
	startupErr error
)

// Main provisions this package's Postgres with the migrations applied once, per
// ADR-0003, and runs the test binary. A missing container runtime is recorded
// rather than fatal: database-backed tests then skip, and fail instead when CI
// is set, so their coverage cannot disappear without anyone noticing.
func Main(m *testing.M) {
	container, err := provision(context.Background())
	startupErr = err

	code := m.Run()

	if testPool != nil {
		testPool.Close()
	}
	if container != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		_ = container.Terminate(ctx)
		cancel()
	}
	os.Exit(code)
}

func provision(parent context.Context) (testcontainers.Container, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(ctx, postgresImage,
		tcpostgres.WithDatabase("voabkr"),
		tcpostgres.WithUsername("voabkr"),
		tcpostgres.WithPassword("voabkr"),
		// The module ships no wait strategy; the entrypoint logs readiness once for
		// the server that runs the init scripts and once for the server that serves.
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		return nil, err
	}

	connString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return container, err
	}
	if err := migrations.Run(connString); err != nil {
		return container, err
	}

	pool, err := pgxpool.New(parent, connString)
	if err != nil {
		return container, err
	}

	testPool = pool
	testDB = db.New(pool)
	return container, nil
}

// Setup returns this package's queries over an emptied schema, so a test starts
// from no rows. It skips or fails the test when no container runtime was
// reachable.
func Setup(t *testing.T) *db.Queries {
	t.Helper()

	queries := Queries(t)
	_, err := testPool.Exec(context.Background(),
		`TRUNCATE users, decks, deck_shares, cards, user_cards, settings, review_logs, mail_verifications, password_resets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("failed to empty the test database: %v", err)
	}
	return queries
}

// Queries returns this package's queries without emptying the schema, for a test
// that checks what a request stored. It skips or fails the test when no
// container runtime was reachable.
func Queries(t *testing.T) *db.Queries {
	t.Helper()

	if testDB == nil {
		if startupErr == nil {
			t.Fatal("dbtest.Main must be called from the test package's TestMain")
		}
		if os.Getenv("CI") != "" {
			t.Fatalf("CI needs a container runtime to run the database-backed tests: %v", startupErr)
		}
		t.Skipf("no container runtime to run the database-backed tests: %v", startupErr)
	}
	return testDB
}

// pool returns this package's connection pool, skipping or failing exactly as
// Queries does when no container runtime was reachable.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	_ = Queries(t)
	return testPool
}
