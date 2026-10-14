package handlers_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Fankhauserli/voabkr-backend/access"
	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/sql/db"
	"github.com/Fankhauserli/voabkr-backend/sql/migrations"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgtype"
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

// TestMain provisions one Postgres for this package with the migrations applied
// once, per ADR-0003. A missing container runtime is recorded rather than fatal:
// database-backed tests then skip, and fail instead when CI is set, so their
// coverage cannot disappear without anyone noticing.
func TestMain(m *testing.M) {
	container, err := provisionTestDB(context.Background())
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

func provisionTestDB(parent context.Context) (testcontainers.Container, error) {
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

// newTestHandler returns a handler over the test database with every table
// emptied, and no cache.
func newTestHandler(t *testing.T) *handlers.Handler {
	t.Helper()
	return handlers.NewHandler(emptyDatabase(t), nil)
}

// newTestHandlerWithCache returns a handler over the test database with every
// table emptied, and a local-only cache, so a test can watch what a cache entry
// does across two callers.
func newTestHandlerWithCache(t *testing.T) *handlers.Handler {
	t.Helper()
	queries := emptyDatabase(t)

	c, err := cache.NewTwoLevelCache(cache.Config{EnableLocalCache: true})
	if err != nil {
		t.Fatalf("failed to create cache: %v", err)
	}
	t.Cleanup(c.Close)

	return handlers.NewHandler(queries, c)
}

// emptyDatabase skips or fails the test when no container runtime was reachable,
// and otherwise hands back the package's queries over an emptied schema.
func emptyDatabase(t *testing.T) *db.Queries {
	t.Helper()
	if testDB == nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("CI needs a container runtime to run the database-backed tests: %v", startupErr)
		}
		t.Skipf("no container runtime to run the database-backed tests: %v", startupErr)
	}

	_, err := testPool.Exec(context.Background(),
		`TRUNCATE users, decks, deck_shares, cards, user_cards, settings, review_logs, mail_verifications, password_resets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("failed to empty the test database: %v", err)
	}
	return testDB
}

// seedUser inserts a learner and returns its id.
func seedUser(t *testing.T, email string) int64 {
	t.Helper()
	user, err := testDB.CreateUser(context.Background(), db.CreateUserParams{
		Name:         email,
		Email:        email,
		PasswordHash: "not-a-real-hash",
	})
	if err != nil {
		t.Fatalf("failed to seed learner %s: %v", email, err)
	}
	return user.ID
}

// seedDeck inserts a Deck. ownerID 0 inserts a System deck; every seeded Deck
// carries a share token, as CreateDeck leaves in production.
func seedDeck(t *testing.T, ownerID int64, isPublic bool) int64 {
	t.Helper()
	deck, err := testDB.CreateDeck(context.Background(), db.CreateDeckParams{
		Name:       "Seeded Deck",
		Type:       db.DeckTypeVocabulary,
		OwnerID:    pgtype.Int8{Int64: ownerID, Valid: ownerID > 0},
		IsPublic:   isPublic,
		ShareToken: pgtype.Text{String: fmt.Sprintf("sec_seed_%d", seedTokenSeq()), Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to seed deck: %v", err)
	}
	return deck.ID
}

var tokenSeq int

func seedTokenSeq() int {
	tokenSeq++
	return tokenSeq
}

// seedCard inserts a Card into deckID and returns its id.
func seedCard(t *testing.T, deckID int64, korean, english string) int64 {
	t.Helper()
	card, err := testDB.CreateCard(context.Background(), db.CreateCardParams{
		DeckID:      deckID,
		KoreanWord:  korean,
		EnglishWord: english,
	})
	if err != nil {
		t.Fatalf("failed to seed card: %v", err)
	}
	return card.ID
}

// seedDueReview gives learner a Review of cardID that fell due an hour ago, as
// one that was studied and left to come round again.
func seedDueReview(t *testing.T, learner, cardID int64) {
	t.Helper()
	seedScheduledReview(t, learner, cardID, time.Now().Add(-time.Hour))
}

// seedScheduledReview gives learner a Review of cardID due at nextReviewAt, as
// one the device scheduled and answered an hour before it.
func seedScheduledReview(t *testing.T, learner, cardID int64, nextReviewAt time.Time) {
	t.Helper()
	ctx := context.Background()

	if _, err := testDB.CreateUserCard(ctx, db.CreateUserCardParams{UserID: learner, CardID: cardID}); err != nil {
		t.Fatalf("failed to seed a review: %v", err)
	}

	err := testDB.UpdateUserCard(ctx, db.UpdateUserCardParams{
		UserID:         learner,
		CardID:         cardID,
		Efactor:        2.5,
		Interval:       6,
		Repetitions:    1,
		LastReviewedAt: pgtype.Timestamptz{Time: nextReviewAt.Add(-time.Hour), Valid: true},
		NextReviewAt:   pgtype.Timestamptz{Time: nextReviewAt, Valid: true},
	})
	if err != nil {
		t.Fatalf("failed to seed a scheduled review: %v", err)
	}
}

// seedShare grants userID permission on deckID.
func seedShare(t *testing.T, deckID, userID int64, permission string) {
	t.Helper()
	_, err := testDB.CreateDeckShare(context.Background(), db.CreateDeckShareParams{
		DeckID:     deckID,
		UserID:     pgtype.Int8{Int64: userID, Valid: true},
		Permission: permission,
	})
	if err != nil {
		t.Fatalf("failed to seed deck share: %v", err)
	}
}

// signedIn installs the session a signed-in learner's request carries.
func signedIn(userID int64, handler gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", strconv.FormatInt(userID, 10))
		_ = session.Save()
		handler(c)
	}
}

// serve sends method to path through handler as callerID — access.Anonymous
// sends no session at all — and returns what the caller saw. A non-empty body is
// sent as JSON.
func serve(t *testing.T, handler gin.HandlerFunc, callerID int64, method, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	router := setupTestRouter()
	if callerID == access.Anonymous {
		router.Handle(method, route, handler)
	} else {
		router.Handle(method, route, signedIn(callerID, handler))
	}

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// get serves route at path through handler as callerID, and returns what the
// caller saw.
func get(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodGet, route, path, "")
}

// post serves route at path through handler as callerID with a JSON body, and
// returns what the caller saw.
func post(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodPost, route, path, body)
}

// put serves route at path through handler as callerID with a JSON body, and
// returns what the caller saw.
func put(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodPut, route, path, body)
}

// del serves route at path through handler as callerID, and returns what the
// caller saw.
func del(t *testing.T, handler gin.HandlerFunc, callerID int64, route, path string) *httptest.ResponseRecorder {
	t.Helper()
	return serve(t, handler, callerID, http.MethodDelete, route, path, "")
}
