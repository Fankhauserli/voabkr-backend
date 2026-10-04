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
	if sessionStore != nil {
		router.Use(sessions.Sessions("userSession", sessionStore))
	}

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api")
	{
		// /api/v1
		v1public := api.Group("/v1")
		{
			v1public.POST("/login", handler.Login)
			v1public.POST("/register", handler.Register)
			v1public.POST("/resend-verification", handler.ResendVerificationEmail)
			v1public.POST("/verification/:token", handler.VerifyEmail)

			// Public read-only access to decks and cards
			v1public.GET("/decks/", handler.GetDecks)
			v1public.GET("/decks/:id", handler.GetDeckByID)
			v1public.GET("/cards/", handler.GetCards)
			v1public.GET("/cards/:id", handler.GetCardByID)
		}

		v1private := api.Group("/v1")
		v1private.Use(middleware.AuthMiddleware())
		{
			v1private.POST("/logout", handler.Logout)

			userGroup := v1private.Group("/user")
			{
				userGroup.GET("/profile", handler.GetUserProfile)
				userGroup.PUT("/profile", handler.UpdateUserProfile)
				userGroup.GET("/settings", handler.GetSettings)
				userGroup.PUT("/settings", handler.UpdateSettings)
			}

			deckGroup := v1private.Group("/decks")
			{
				deckGroup.POST("/", handler.CreateDeck)
				deckGroup.PUT("/:id", handler.UpdateDeck)
				deckGroup.DELETE("/:id", handler.DeleteDeck)
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
