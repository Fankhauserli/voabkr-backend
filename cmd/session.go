package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/redis"
	"github.com/gin-gonic/gin"
	redigo "github.com/gomodule/redigo/redis"
)

func ensureSessionMiddleware(router *gin.Engine) {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		log.Fatal("REDIS_ADDR environment variable is not set")
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisUsername := os.Getenv("REDIS_USERNAME")

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		log.Fatal("SESSION_SECRET environment variable is not set")
	}

	// Test Redis connectivity at startup with a short timeout
	dialOpts := []redigo.DialOption{
		redigo.DialConnectTimeout(3 * time.Second),
	}
	if redisPassword != "" {
		dialOpts = append(dialOpts, redigo.DialPassword(redisPassword))
	}
	if redisUsername != "" {
		dialOpts = append(dialOpts, redigo.DialUsername(redisUsername))
	}

	testConn, err := redigo.Dial("tcp", redisAddr, dialOpts...)
	if err != nil {
		log.Printf("[WARNING] Could not connect to Redis at %s: %v. (Check REDIS_ADDR, REDIS_PASSWORD, and network)", redisAddr, err)
	} else {
		if _, pingErr := testConn.Do("PING"); pingErr != nil {
			log.Printf("[WARNING] Redis ping at %s failed: %v", redisAddr, pingErr)
		} else {
			log.Printf("[INFO] Successfully connected to Redis at %s", redisAddr)
		}
		testConn.Close()
	}

	// Connect to Redis for session storage
	store, err := redis.NewStore(10, "tcp", redisAddr, redisUsername, redisPassword, []byte(secret))
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
