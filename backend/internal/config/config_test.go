package config

import "testing"

func TestListenAddrKeepsComposeDefault(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("HTTP_ADDR", ":8080")
	if got := listenAddr(); got != ":8080" {
		t.Fatalf("HTTP_ADDR = %q", got)
	}
}

func TestListenAddrDefaultsWithoutPort(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("HTTP_ADDR", "")
	if got := listenAddr(); got != ":18080" {
		t.Fatalf("default = %q", got)
	}
}

func TestListenAddrPrefersPort(t *testing.T) {
	t.Setenv("PORT", "3000")
	t.Setenv("HTTP_ADDR", ":8080")
	if got := listenAddr(); got != ":3000" {
		t.Fatalf("PORT = %q", got)
	}
}

func TestPublicOriginLocalDefault(t *testing.T) {
	t.Setenv("PUBLIC_ORIGIN", "")
	t.Setenv("VERCEL", "")
	if got := publicOrigin(); got != "http://127.0.0.1:43123" {
		t.Fatalf("origin = %q", got)
	}
}

func TestPublicOriginExplicit(t *testing.T) {
	t.Setenv("PUBLIC_ORIGIN", "https://pesenin-dev.example/")
	t.Setenv("VERCEL", "1")
	t.Setenv("VERCEL_URL", "pesenin-abc.vercel.app")
	if got := publicOrigin(); got != "https://pesenin-dev.example" {
		t.Fatalf("origin = %q", got)
	}
}

func TestPublicOriginVercelFallback(t *testing.T) {
	t.Setenv("PUBLIC_ORIGIN", "")
	t.Setenv("VERCEL", "1")
	t.Setenv("VERCEL_ENV", "preview")
	t.Setenv("VERCEL_URL", "pesenin-abc.vercel.app")
	if got := publicOrigin(); got != "https://pesenin-abc.vercel.app" {
		t.Fatalf("origin = %q", got)
	}
}

func TestCookieSecureFollowsHTTPSWhenUnset(t *testing.T) {
	t.Setenv("COOKIE_SECURE", "")
	if cookieSecure("http://localhost:43123") {
		t.Fatal("http origin should not be secure by default")
	}
	if !cookieSecure("https://pesenin-dev.example") {
		t.Fatal("https origin should be secure when COOKIE_SECURE is unset")
	}
	t.Setenv("COOKIE_SECURE", "false")
	if cookieSecure("https://pesenin-dev.example") {
		t.Fatal("explicit COOKIE_SECURE=false must win")
	}
}

func TestMigrateURLPrefersDirect(t *testing.T) {
	t.Setenv("DATABASE_URL_UNPOOLED", "")
	t.Setenv("POSTGRES_URL_NON_POOLING", "")
	pooled := "postgres://u:p@ep-x-pooler.neon.tech/neondb"
	if got := migrateURL(pooled); got != pooled {
		t.Fatalf("fallback = %q", got)
	}
	t.Setenv("POSTGRES_URL_NON_POOLING", "postgres://direct")
	if got := migrateURL(pooled); got != "postgres://direct" {
		t.Fatalf("non pooling = %q", got)
	}
	t.Setenv("DATABASE_URL_UNPOOLED", "postgres://unpooled")
	if got := migrateURL(pooled); got != "postgres://unpooled" {
		t.Fatalf("unpooled = %q", got)
	}
}

func TestStorageDir(t *testing.T) {
	t.Setenv("STORAGE_DIR", "")
	t.Setenv("VERCEL", "")
	if got := storageDir(); got != "data/uploads" {
		t.Fatalf("local = %q", got)
	}
	t.Setenv("VERCEL", "1")
	if got := storageDir(); got == "data/uploads" || got == "" {
		t.Fatalf("vercel dir = %q", got)
	}
	t.Setenv("STORAGE_DIR", "/data/uploads")
	if got := storageDir(); got != "/data/uploads" {
		t.Fatalf("explicit = %q", got)
	}
}

func TestPasswordAndKYCDefaults(t *testing.T) {
	t.Setenv("SMTP_HOST", "")
	t.Setenv("MAIL_DRIVER", "")
	t.Setenv("KYC_GATE", "")
	t.Setenv("WA_PROVIDER", "")
	t.Setenv("SMTP_PORT", "")
	cfg := Load()
	if cfg.KYCGate != "contact" || !cfg.KYCEnforced() {
		t.Fatalf("kyc gate %q", cfg.KYCGate)
	}
	if !cfg.MailLog() || cfg.WAProvider != "log" || cfg.SMTPPort != 587 {
		t.Fatalf("mail=%v wa=%s port=%d", cfg.MailLog(), cfg.WAProvider, cfg.SMTPPort)
	}
	t.Setenv("MAIL_DRIVER", "smtp")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("KYC_GATE", "off")
	cfg = Load()
	if cfg.MailLog() || cfg.KYCEnforced() {
		t.Fatal("explicit smtp and gate off")
	}
}
