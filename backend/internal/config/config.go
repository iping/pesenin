package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	DatabaseURL    string
	MigrateURL     string
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
	origin := publicOrigin()
	databaseURL := getenv("DATABASE_URL", "postgres://pesenin:pesenin@127.0.0.1:54329/pesenin?sslmode=disable")
	return Config{
		DatabaseURL:    databaseURL,
		MigrateURL:     migrateURL(databaseURL),
		HTTPAddr:       listenAddr(),
		PublicOrigin:   origin,
		CookieSecure:   cookieSecure(origin),
		DevLogin:       getenv("DEV_LOGIN", "false") == "true",
		SeedDemo:       getenv("SEED_DEMO", "false") == "true",
		AdminEmail:     strings.ToLower(getenv("ADMIN_EMAIL", "admin@pesenin.test")),
		GoogleID:       os.Getenv("GOOGLE_CLIENT_ID"),
		GoogleSecret:   os.Getenv("GOOGLE_CLIENT_SECRET"),
		GoogleRedirect: getenv("GOOGLE_REDIRECT_URL", origin+"/api/v1/auth/google/callback"),
		StorageDir:     storageDir(),
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

// listenAddr prefers PORT, which the Vercel Go runtime injects.
// Docker Compose sets HTTP_ADDR and leaves PORT unset, so it still binds :8080.
func listenAddr() string {
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		if strings.HasPrefix(port, ":") {
			return port
		}
		return ":" + port
	}
	return getenv("HTTP_ADDR", ":18080")
}

// publicOrigin is the browser origin used for OAuth redirects and absolute links.
// PUBLIC_ORIGIN wins. On Vercel, the deployment host is the fallback so a missing
// value does not keep the local default.
func publicOrigin() string {
	if v := strings.TrimSpace(os.Getenv("PUBLIC_ORIGIN")); v != "" {
		return strings.TrimRight(v, "/")
	}
	if os.Getenv("VERCEL") == "1" {
		host := ""
		if os.Getenv("VERCEL_ENV") == "production" {
			host = strings.TrimSpace(os.Getenv("VERCEL_PROJECT_PRODUCTION_URL"))
		}
		if host == "" {
			host = strings.TrimSpace(os.Getenv("VERCEL_URL"))
		}
		if host != "" {
			host = strings.TrimPrefix(host, "https://")
			host = strings.TrimPrefix(host, "http://")
			return "https://" + strings.TrimRight(host, "/")
		}
	}
	return "http://127.0.0.1:43123"
}

func cookieSecure(origin string) bool {
	if v := strings.TrimSpace(os.Getenv("COOKIE_SECURE")); v != "" {
		return v == "true"
	}
	return strings.HasPrefix(origin, "https://")
}

// migrateURL prefers a direct Postgres URL. Neon's pooled DATABASE_URL goes
// through PgBouncer transaction mode, which cannot hold the advisory lock
// golang-migrate uses. The Marketplace integration sets the direct URL too.
func migrateURL(databaseURL string) string {
	for _, key := range []string{"DATABASE_URL_UNPOOLED", "POSTGRES_URL_NON_POOLING"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return databaseURL
}

func storageDir() string {
	if v := strings.TrimSpace(os.Getenv("STORAGE_DIR")); v != "" {
		return v
	}
	if os.Getenv("VERCEL") == "1" {
		return os.TempDir() + "/pesenin-uploads"
	}
	return "data/uploads"
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
