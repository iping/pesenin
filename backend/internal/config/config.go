package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL    string
	HTTPAddr       string
	PublicOrigin   string
	CookieSecure   bool
	DevLogin       bool
	SeedDemo       bool
	AdminEmail     string
	GoogleID       string
	GoogleSecret   string
	GoogleRedirect string
	StorageDir     string
	StorageDriver  string
	R2Endpoint     string
	R2Bucket       string
	R2AccessKey    string
	R2SecretKey    string
	VAPIDPublic    string
	VAPIDPrivate   string
	VAPIDSubject   string
	SessionTTL     time.Duration
}

func Load() Config {
	origin := getenv("PUBLIC_ORIGIN", "http://127.0.0.1:43123")
	return Config{
		DatabaseURL:    getenv("DATABASE_URL", "postgres://pesenin:pesenin@127.0.0.1:54329/pesenin?sslmode=disable"),
		HTTPAddr:       getenv("HTTP_ADDR", ":18080"),
		PublicOrigin:   strings.TrimRight(origin, "/"),
		CookieSecure:   getenv("COOKIE_SECURE", "false") == "true",
		DevLogin:       getenv("DEV_LOGIN", "false") == "true",
		SeedDemo:       getenv("SEED_DEMO", "false") == "true",
		AdminEmail:     strings.ToLower(getenv("ADMIN_EMAIL", "admin@pesenin.test")),
		GoogleID:       os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirect: getenv("GOOGLE_REDIRECT_URL", origin+"/api/v1/auth/google/callback"),
		StorageDir:     getenv("STORAGE_DIR", "data/uploads"),
		StorageDriver:  getenv("STORAGE_DRIVER", "local"),
		R2Endpoint:     os.Getenv("R2_ENDPOINT"),
		R2Bucket:       os.Getenv("R2_BUCKET"),
		R2AccessKey:    os.Getenv("R2_ACCESS_KEY_ID"),
		R2SecretKey:    os.Getenv("R2_SECRET_ACCESS_KEY"),
		VAPIDPublic:    os.Getenv("VAPID_PUBLIC_KEY"),
		VAPIDPrivate:   os.Getenv("VAPID_PRIVATE_KEY"),
		VAPIDSubject:   getenv("VAPID_SUBJECT", "mailto:noreply@pesenin.product.biz.id"),
		SessionTTL:     time.Duration(getenvInt("SESSION_DAYS", 30)) * 24 * time.Hour,
	}
}

func (c Config) GoogleEnabled() bool {
	return c.GoogleID != "" && c.GoogleSecret != ""
}

func (c Config) R2Enabled() bool {
	return c.StorageDriver == "s3" && c.R2Endpoint != "" && c.R2Bucket != "" && c.R2AccessKey != "" && c.R2SecretKey != ""
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
