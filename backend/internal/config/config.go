// Package config loads runtime configuration from the environment.
package config

import (
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

const GiB = int64(1) << 30

type Config struct {
	ListenAddr string
	UploadDir  string
	DBPath     string

	// MaxUploadBytes: files must be strictly smaller than this.
	MaxUploadBytes int64
	ReserveBytes   int64
	ChunkSizeMiB   int

	RootUsername string
	RootPassword string

	SessionIdle time.Duration
	SessionMax  time.Duration

	PartialExpiry time.Duration
	VerifyWorkers int

	TrustedProxies []netip.Prefix
	InsecureCookie bool // dev only
	FileGID        int  // -1 = leave alone
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr:     env("LISTEN_ADDR", ":8080"),
		UploadDir:      env("UPLOAD_DIR", "/data/uploads"),
		DBPath:         env("DB_PATH", "/data/db/lfu.sqlite"),
		MaxUploadBytes: 100 * GiB,
		ReserveBytes:   10 * GiB,
		ChunkSizeMiB:   32,
		RootUsername:   os.Getenv("ROOT_USERNAME"),
		RootPassword:   os.Getenv("ROOT_PASSWORD"),
		SessionIdle:    24 * time.Hour,
		SessionMax:     14 * 24 * time.Hour,
		PartialExpiry:  7 * 24 * time.Hour,
		VerifyWorkers:  1,
		InsecureCookie: os.Getenv("DEV_INSECURE_COOKIES") == "1",
		FileGID:        -1,
	}
	var err error
	if c.MaxUploadBytes, err = envInt64("MAX_UPLOAD_BYTES", c.MaxUploadBytes); err != nil {
		return nil, err
	}
	if c.ReserveBytes, err = envInt64("RESERVE_BYTES", c.ReserveBytes); err != nil {
		return nil, err
	}
	if v, err := envInt64("CHUNK_SIZE_MIB", int64(c.ChunkSizeMiB)); err != nil {
		return nil, err
	} else {
		c.ChunkSizeMiB = int(v)
	}
	if v, err := envInt64("SESSION_IDLE_HOURS", 24); err != nil {
		return nil, err
	} else {
		c.SessionIdle = time.Duration(v) * time.Hour
	}
	if v, err := envInt64("PARTIAL_EXPIRY_DAYS", 7); err != nil {
		return nil, err
	} else {
		c.PartialExpiry = time.Duration(v) * 24 * time.Hour
	}
	if v, err := envInt64("VERIFY_WORKERS", 1); err != nil {
		return nil, err
	} else {
		c.VerifyWorkers = int(v)
	}
	if v, err := envInt64("FILE_GID", -1); err != nil {
		return nil, err
	} else {
		c.FileGID = int(v)
	}
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDR"), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pre, err := netip.ParsePrefix(p)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDR %q: %w", p, err)
		}
		c.TrustedProxies = append(c.TrustedProxies, pre)
	}
	if c.MaxUploadBytes < 2 || c.ChunkSizeMiB < 1 || c.ChunkSizeMiB > 60 || c.VerifyWorkers < 1 {
		return nil, fmt.Errorf("invalid numeric configuration")
	}
	return c, nil
}

// MaxChunkBytes is the largest PATCH body the server accepts (chunk size plus slack).
func (c *Config) MaxChunkBytes() int64 { return int64(c.ChunkSizeMiB+4) << 20 }

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt64(k string, def int64) (int64, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", k, err)
	}
	return n, nil
}
