package cache

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
	"google.golang.org/protobuf/proto"
)

// Cache defines the interface for interacting with the multi-tier cache.
type Cache interface {
	GetProto(ctx context.Context, key string, msg proto.Message) (bool, error)
	SetProto(ctx context.Context, key string, msg proto.Message, ttl time.Duration) error
	Delete(ctx context.Context, keys ...string) error
	DeletePrefix(ctx context.Context, prefix string) error
	Close()
}

// Config configures the two-level cache (L1 local memory + L2 Valkey cluster).
type Config struct {
	Addresses        []string      // Valkey cluster addresses (e.g., ["valkey-cluster:6379"])
	Password         string        // Optional Valkey password
	Username         string        // Optional Valkey username
	DefaultTTL       time.Duration // Default expiration TTL for keys (default 5m)
	LocalCacheTTL    time.Duration // Expiration TTL for local memory cache (default 2m)
	EnableLocalCache bool          // Whether to enable local in-memory L1 cache (default true)
	CleanupInterval  time.Duration // Periodic cleanup interval for local cache (default 1m)
}

// TwoLevelCache implements Cache using a fast in-memory L1 cache and a distributed L2 Valkey cluster.
type TwoLevelCache struct {
	local      *MemoryCache
	valkey     valkey.Client
	defaultTTL time.Duration
	localTTL   time.Duration
}

// NewTwoLevelCache creates a new two-level cache with local memory and Valkey cluster tiers.
func NewTwoLevelCache(cfg Config) (*TwoLevelCache, error) {
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}
	if cfg.LocalCacheTTL <= 0 {
		cfg.LocalCacheTTL = 2 * time.Minute
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 1 * time.Minute
	}

	c := &TwoLevelCache{
		defaultTTL: cfg.DefaultTTL,
		localTTL:   cfg.LocalCacheTTL,
	}

	if cfg.EnableLocalCache {
		c.local = NewMemoryCache(cfg.CleanupInterval)
	}

	if len(cfg.Addresses) > 0 {
		clientOpt := valkey.ClientOption{
			InitAddress: cfg.Addresses,
			Password:    cfg.Password,
			Username:    cfg.Username,
			Dialer: net.Dialer{
				Timeout: 3 * time.Second,
			},
		}

		client, err := valkey.NewClient(clientOpt)
		if err != nil {
			log.Printf("[WARN] Failed to initialize Valkey cluster client (%v): %v. Operating with local memory cache only.", cfg.Addresses, err)
		} else {
			pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if pingErr := client.Do(pingCtx, client.B().Ping().Build()).Error(); pingErr != nil {
				log.Printf("[WARN] Valkey cluster ping failed: %v. Operating with local memory cache only.", pingErr)
				client.Close()
			} else {
				log.Printf("[INFO] Successfully connected to Valkey cluster at %v with client-side caching", cfg.Addresses)
				c.valkey = client
			}
		}
	} else {
		log.Printf("[INFO] No Valkey cluster addresses configured. Operating with local memory cache only.")
	}

	return c, nil
}

// NewFromEnv parses environment variables to configure and instantiate the cache.
// Recognizes VALKEY_ADDRS (comma-separated), VALKEY_ADDR, or fallback REDIS_ADDR.
func NewFromEnv() (*TwoLevelCache, error) {
	addrStr := os.Getenv("VALKEY_ADDRS")
	if addrStr == "" {
		addrStr = os.Getenv("VALKEY_ADDR")
	}
	if addrStr == "" {
		addrStr = os.Getenv("REDIS_ADDR")
	}

	var addrs []string
	if addrStr != "" {
		parts := strings.Split(addrStr, ",")
		for _, p := range parts {
			trimmed := strings.TrimSpace(p)
			if trimmed != "" {
				addrs = append(addrs, trimmed)
			}
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

	defaultTTL := 5 * time.Minute
	if ttlStr := os.Getenv("VALKEY_DEFAULT_TTL_SEC"); ttlStr != "" {
		if sec, err := strconv.Atoi(ttlStr); err == nil && sec > 0 {
			defaultTTL = time.Duration(sec) * time.Second
		}
	}

	localTTL := 2 * time.Minute
	if ttlStr := os.Getenv("VALKEY_LOCAL_TTL_SEC"); ttlStr != "" {
		if sec, err := strconv.Atoi(ttlStr); err == nil && sec > 0 {
			localTTL = time.Duration(sec) * time.Second
		}
	}

	enableLocalCache := true
	if os.Getenv("VALKEY_DISABLE_LOCAL_CACHE") == "true" {
		enableLocalCache = false
	}

	return NewTwoLevelCache(Config{
		Addresses:        addrs,
		Password:         password,
		Username:         username,
		DefaultTTL:       defaultTTL,
		LocalCacheTTL:    localTTL,
		EnableLocalCache: enableLocalCache,
	})
}

// GetProto attempts to retrieve a Protobuf message from L1 local memory, falling back to L2 Valkey cluster.
func (c *TwoLevelCache) GetProto(ctx context.Context, key string, msg proto.Message) (bool, error) {
	// 1. Tier 1: Check in-memory local cache
	if c.local != nil {
		if data, found := c.local.Get(key); found {
			if err := proto.Unmarshal(data, msg); err == nil {
				return true, nil
			}
			c.local.Delete(key)
		}
	}

	// 2. Tier 2: Check Valkey cluster
	if c.valkey != nil {
		ttl := c.defaultTTL
		if c.localTTL > 0 {
			ttl = c.localTTL
		}

		cmd := c.valkey.B().Get().Key(key).Cache()
		res := c.valkey.DoCache(ctx, cmd, ttl)
		if err := res.Error(); err != nil {
			if valkey.IsValkeyNil(err) {
				return false, nil
			}
			log.Printf("[WARN] Valkey cluster Get failed for key %s: %v", key, err)
			return false, nil
		}

		data, err := res.AsBytes()
		if err != nil {
			return false, nil
		}

		// Populate Tier 1 local cache
		if c.local != nil {
			c.local.Set(key, data, ttl)
		}

		if err := proto.Unmarshal(data, msg); err != nil {
			log.Printf("[WARN] Protobuf unmarshal failed for key %s: %v", key, err)
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// SetProto serializes a Protobuf message to binary wire format and stores it in both local memory and Valkey cluster.
func (c *TwoLevelCache) SetProto(ctx context.Context, key string, msg proto.Message, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = c.defaultTTL
	}

	// Efficient Protobuf binary serialization
	data, err := proto.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal protobuf message: %w", err)
	}

	// Save to Tier 1: Local memory
	if c.local != nil {
		localTTL := ttl
		if c.localTTL > 0 && c.localTTL < ttl {
			localTTL = c.localTTL
		}
		c.local.Set(key, data, localTTL)
	}

	// Save to Tier 2: Valkey cluster
	if c.valkey != nil {
		cmd := c.valkey.B().Set().Key(key).Value(valkey.BinaryString(data)).Ex(ttl).Build()
		if err := c.valkey.Do(ctx, cmd).Error(); err != nil {
			log.Printf("[WARN] Valkey cluster Set failed for key %s: %v", key, err)
			return err
		}
	}

	return nil
}

// Delete removes keys from both Tier 1 local cache and Tier 2 Valkey cluster.
func (c *TwoLevelCache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}

	if c.local != nil {
		c.local.Delete(keys...)
	}

	if c.valkey != nil {
		cmd := c.valkey.B().Del().Key(keys...).Build()
		if err := c.valkey.Do(ctx, cmd).Error(); err != nil && !valkey.IsValkeyNil(err) {
			log.Printf("[WARN] Valkey cluster Del failed for keys %v: %v", keys, err)
			return err
		}
	}

	return nil
}

// DeletePrefix invalidates all keys starting with the given prefix across local memory and Valkey cluster nodes.
func (c *TwoLevelCache) DeletePrefix(ctx context.Context, prefix string) error {
	if c.local != nil {
		c.local.DeletePrefix(prefix)
	}

	if c.valkey != nil {
		nodes := c.valkey.Nodes()
		if len(nodes) == 0 {
			c.scanAndDelete(ctx, c.valkey, prefix)
		} else {
			for _, node := range nodes {
				c.scanAndDelete(ctx, node, prefix)
			}
		}
	}

	return nil
}

func (c *TwoLevelCache) scanAndDelete(ctx context.Context, client valkey.Client, prefix string) {
	var cursor uint64
	matchPattern := prefix + "*"
	for {
		cmd := client.B().Scan().Cursor(cursor).Match(matchPattern).Count(100).Build()
		res, err := client.Do(ctx, cmd).AsScanEntry()
		if err != nil {
			break
		}
		cursor = res.Cursor
		if len(res.Elements) > 0 {
			delCmd := client.B().Del().Key(res.Elements...).Build()
			_ = client.Do(ctx, delCmd)
		}
		if cursor == 0 {
			break
		}
	}
}

// Close gracefully closes the local memory cache and Valkey client connections.
func (c *TwoLevelCache) Close() {
	if c.local != nil {
		c.local.Close()
	}
	if c.valkey != nil {
		c.valkey.Close()
	}
}
