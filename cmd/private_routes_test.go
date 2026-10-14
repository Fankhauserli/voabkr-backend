package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/gin-gonic/gin"
)

// Every private route refuses an anonymous caller in one shape, before any
// handler behind it sees the request — the handler needs no database here
// because none of them is reached.
func TestPrivateRoutesRefuseAnonymousCallers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupRouter(handlers.NewHandler(nil, nil), newTestSessionStore(t))

	tests := []struct {
		name         string
		method, path string
	}{
		{name: "profile", method: http.MethodGet, path: "/api/v1/user/profile"},
		{name: "update profile", method: http.MethodPut, path: "/api/v1/user/profile"},
		{name: "settings", method: http.MethodGet, path: "/api/v1/user/settings"},
		{name: "update settings", method: http.MethodPut, path: "/api/v1/user/settings"},
		{name: "erase account", method: http.MethodDelete, path: "/api/v1/user"},
		{name: "create deck", method: http.MethodPost, path: "/api/v1/decks/"},
		{name: "edit deck", method: http.MethodPut, path: "/api/v1/decks/1"},
		{name: "delete deck", method: http.MethodDelete, path: "/api/v1/decks/1"},
		{name: "share deck", method: http.MethodPost, path: "/api/v1/decks/1/share"},
		{name: "revoke share", method: http.MethodDelete, path: "/api/v1/decks/1/share/2"},
		{name: "join shared deck", method: http.MethodPost, path: "/api/v1/decks/shared/tok/join"},
		{name: "reviews", method: http.MethodGet, path: "/api/v1/reviews/"},
		{name: "create review", method: http.MethodPost, path: "/api/v1/reviews/"},
		{name: "edit review", method: http.MethodPut, path: "/api/v1/reviews/1"},
		{name: "reviews since", method: http.MethodGet, path: "/api/v1/reviews/since/2026-01-01T00:00:00Z"},
		{name: "grades", method: http.MethodPost, path: "/api/v1/reviews/batch"},
		{name: "logout", method: http.MethodPost, path: "/api/v1/logout"},
		{name: "create card", method: http.MethodPost, path: "/api/v1/cards/"},
		{name: "edit card", method: http.MethodPut, path: "/api/v1/cards/1"},
		{name: "delete card", method: http.MethodDelete, path: "/api/v1/cards/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401 for an anonymous caller, got %d: %s", w.Code, w.Body.String())
			}
			if w.Body.String() != `{"error":"Unauthorized"}` {
				t.Fatalf("expected the one refusal shape, got %s", w.Body.String())
			}
		})
	}
}

// A router built without a session store has no session to resolve a caller
// from: every private route answers 401 rather than reading a session that was
// never installed.
func TestPrivateRoutesWithoutASessionStore(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupRouter(handlers.NewHandler(nil, nil), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cards/", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401 without a session store, got %d: %s", w.Code, w.Body.String())
	}
}
