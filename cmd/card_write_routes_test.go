package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/gin-gonic/gin"
)

// Every Card write sits in the private route group, so an anonymous caller is
// refused by the session gate before any handler sees the request.
func TestCardWriteRoutesRefuseAnonymousCallers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := setupRouter(handlers.NewHandler(nil, nil), newTestSessionStore(t))

	tests := []struct {
		name         string
		method, path string
	}{
		{name: "create", method: http.MethodPost, path: "/api/v1/cards/"},
		{name: "edit", method: http.MethodPut, path: "/api/v1/cards/1"},
		{name: "delete", method: http.MethodDelete, path: "/api/v1/cards/1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(`{"deckId":1}`))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected status 401 for an anonymous caller, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
