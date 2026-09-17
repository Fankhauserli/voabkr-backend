package main

import (
	"log"

	"github.com/Fankhauserli/voabkr-backend/handlers"
	"github.com/Fankhauserli/voabkr-backend/middleware"
	"github.com/gin-gonic/gin"
)

func main() {
	// Create a Gin router with default middleware (logger and recovery)
	router := gin.Default()

	router.GET("/healthz")
	router.GET("/readyz")

	ensureSessionMiddleware(router)

	db, err := initDB()
	if err != nil {
		log.Fatalf("failed to initialize database: %v", err)
	}

	// Create a new handler with the database connection
	handler := handlers.NewHandler(db)

	api := router.Group("/api")
	{
		// /api/v1
		v1public := api.Group("/v1")
		{
			v1public.POST("/login", handler.Login)
			v1public.POST("/register", handler.Register)
			v1public.POST("/verification/:token", handler.VerifyEmail)
		}

		v1private := api.Group("/v1")
		v1private.Use(middleware.AuthMiddleware())
		{
			v1private.POST("/logout", handler.Logout)

			userGroup := v1private.Group("/user")
			{
				userGroup.GET("/profile", handler.GetUserProfile)
				userGroup.PUT("/profile", handler.UpdateUserProfile)
			}

			deckGroup := v1private.Group("/decks")
			{
				deckGroup.POST("/", handler.CreateDeck)
				deckGroup.GET("/", handler.GetDecks)
				deckGroup.GET("/:id", handler.GetDeckByID)
				deckGroup.PUT("/:id", handler.UpdateDeck)
				deckGroup.DELETE("/:id", handler.DeleteDeck)
			}

			cardGroup := v1private.Group("/cards")
			{
				cardGroup.POST("/", handler.CreateCard)
				cardGroup.GET("/", handler.GetCards)
				cardGroup.GET("/:id", handler.GetCardByID)
				cardGroup.PUT("/:id", handler.UpdateCard)
				cardGroup.DELETE("/:id", handler.DeleteCard)
			}

			reviewGroup := v1private.Group("/reviews")
			{
				reviewGroup.GET("/", handler.GetReviews)
				reviewGroup.POST("/", handler.CreateReview)
				reviewGroup.PUT("/:id", handler.UpdateReview)
			}
		}
	}

	// Start server on port 8080
	if err := router.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
