package main

import (
	"log"
	"net/http"
	"os"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
)

func ensureSessionMiddleware(router *gin.Engine) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		log.Fatal("REDIS_ADDR environment variable is not set")
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	if redisPassword == "" {
		log.Fatal("REDIS_PASSWORD environment variable is not set")
	}

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		log.Fatal("SESSION_SECRET environment variable is not set")
	}

	// Connect to Redis for session storage
	store, err := redis.NewStore(10, "tcp", redisAddr, redisPassword, secret)
	if err != nil {
		panic(err)
	}

	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 24, // 24 hours (also syncs the Redis TTL)
		HttpOnly: true,      // Blocks client-side JS access
		Secure:   true,      // Set to true in production with HTTPS
		SameSite: http.SameSiteLaxMode,
	})
	router.Use(sessions.Sessions("userSession", store))
}
