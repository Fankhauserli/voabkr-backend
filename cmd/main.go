package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/Fankhauserli/voabkr-backend/cache"
	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func setupRouter(handler *handlers.Handler, sessionStore sessions.Store) *gin.Engine {
	// Create a Gin router with logger and custom recovery to ensure panics log details and return JSON
	router := gin.New()
	router.Use(gin.Logger())
	router.Use(gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Printf("[PANIC RECOVERED] %v", recovered)
		resp := gin.H{"error": "Internal server error"}
		if gin.Mode() == gin.DebugMode {
			resp["details"] = fmt.Sprint(recovered)
		}
		c.AbortWithStatusJSON(http.StatusInternalServerError, resp)
	}))
	router.Use(middleware.CORSMiddleware())
	router.Use(middleware.PrometheusMiddleware())
	if sessionStore != nil {
		router.Use(sessions.Sessions(middleware.SessionName, sessionStore))
	}

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))

	api := router.Group("/api")
	// The caller is resolved once, for every group: each group then declares
	// what it does with an unresolved caller. Without a session store there is
	// no session to resolve a caller from, so nothing resolves and every private
	// route answers 401 rather than panicking on a session it does not have.
	if sessionStore != nil {
		api.Use(middleware.ResolveCaller())
	}
	{
		// /api/v1 auth routes carry no gate. Signing in, registering, verifying,
		// resending, resetting and signing out must work for a learner whose
		// session this instance cannot place — the session is what they are
		// there to change.
		v1auth := api.Group("/v1")
		{
			v1auth.POST("/login", handler.Login)
			v1auth.POST("/register", handler.Register)
			v1auth.POST("/resend-verification", handler.ResendVerificationEmail)
			v1auth.POST("/verification/:token", handler.VerifyEmail)
			v1auth.POST("/forgot-password", handler.ForgotPassword)
			v1auth.POST("/reset-password/:token", handler.ResetPassword)
			v1auth.POST("/logout", handler.Logout)
		}

		// Public content serves guests, but refuses a session this instance cannot
		// place rather than handing it a guest's view.
		v1guest := api.Group("/v1")
		v1guest.Use(middleware.AllowGuest())
		{
			v1guest.GET("/decks/", handler.GetDecks)
			v1guest.GET("/decks/:id", handler.GetDeckByID)
			v1guest.GET("/decks/shared/:token", handler.GetSharedDeckByToken)
			v1guest.GET("/cards/", handler.GetCards)
			v1guest.GET("/cards/:id", handler.GetCardByID)
		}

		v1private := api.Group("/v1")
		v1private.Use(middleware.RequireCaller())
		{
			userGroup := v1private.Group("/user")
			{
				userGroup.GET("/profile", handler.GetUserProfile)
				userGroup.PUT("/profile", handler.UpdateUserProfile)
				userGroup.DELETE("", handler.DeleteAccount)
				userGroup.GET("/settings", handler.GetSettings)
				userGroup.PUT("/settings", handler.UpdateSettings)
			}

			deckGroup := v1private.Group("/decks")
			{
				deckGroup.POST("/", handler.CreateDeck)
				deckGroup.PUT("/:id", handler.UpdateDeck)
				deckGroup.DELETE("/:id", handler.DeleteDeck)
				deckGroup.POST("/:id/share", handler.ShareDeck)
				deckGroup.DELETE("/:id/share/:user_id", handler.RevokeDeckShare)
				deckGroup.POST("/shared/:token/join", handler.JoinSharedDeck)
			}

			cardGroup := v1private.Group("/cards")
			{
				cardGroup.POST("/", handler.CreateCards)
				cardGroup.PUT("/:id", handler.UpdateCard)
				cardGroup.DELETE("/:id", handler.DeleteCard)
			}

			reviewGroup := v1private.Group("/reviews")
			{
				reviewGroup.GET("/", handler.GetReviews)
				reviewGroup.POST("/", handler.CreateReview)
				reviewGroup.POST("/batch", handler.SubmitGrades)
				reviewGroup.GET("/since/:time", handler.GetReviewsSince)
				reviewGroup.PUT("/:id", handler.UpdateReview)
			}
		}
	}

	return router
}

func main() {
	db, err := initDB()
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// Initialize two-level cache (L1 local memory + L2 Valkey cluster)
	appCache, err := cache.NewFromEnv()
	if err != nil {
		log.Printf("[WARN] Failed to configure cache: %v", err)
	} else {
		defer appCache.Close()
	}

	// Create a new handler with database and cache
	handler := handlers.NewHandler(db, appCache)

	sessionStore := initSessionStore()
	router := setupRouter(handler, sessionStore)

	// Start server on port 8080
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
