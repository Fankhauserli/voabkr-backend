package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/sessions"
	"github.com/valkey-io/valkey-go"
)

func initSessionStore() sessions.Store {
	addrStr := os.Getenv("VALKEY_ADDRS")
	if addrStr == "" {
		addrStr = os.Getenv("VALKEY_ADDR")
	}
	if addrStr == "" {
		addrStr = os.Getenv("REDIS_ADDR")
	}
	if addrStr == "" {
		log.Fatal("Neither VALKEY_ADDRS nor REDIS_ADDR environment variable is set")
	}

	var addrs []string
	for _, p := range strings.Split(addrStr, ",") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			addrs = append(addrs, trimmed)
		}
	}

	password := os.Getenv("VALKEY_PASSWORD")
	if password == "" {
		password = os.Getenv("REDIS_PASSWORD")
	}
	username := os.Getenv("VALKEY_USERNAME")
	if username == "" {
		username = os.Getenv("REDIS_USERNAME")
	}

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		log.Fatal("SESSION_SECRET environment variable is not set")
	}

	clientOpt := valkey.ClientOption{
		InitAddress: addrs,
		Password:    password,
		Username:    username,
		Dialer: net.Dialer{
			Timeout: 3 * time.Second,
		},
	}

	client, err := valkey.NewClient(clientOpt)
	if err != nil {
		log.Fatalf("failed to create Valkey client for sessions: %v", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if pingErr := client.Do(pingCtx, client.B().Ping().Build()).Error(); pingErr != nil {
		log.Printf("[WARNING] Could not connect to Valkey at %v: %v. (Check VALKEY_ADDRS/REDIS_ADDR, password, and network)", addrs, pingErr)
	} else {
		log.Printf("[INFO] Successfully connected to Valkey (cluster-aware) for sessions at %v", addrs)
	}

	store := NewValkeyStore(client, []byte(secret))
	store.Options(sessions.Options{
		Path:     "/",
		MaxAge:   3600 * 24, // 24 hours
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
	return store
}
