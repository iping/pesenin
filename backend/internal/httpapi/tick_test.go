package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"pesenin.product.biz.id/api/internal/service"
)

func TestTickRequiresCronSecret(t *testing.T) {
	s := New(&service.App{})

	t.Setenv("CRON_SECRET", "")
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/internal/tick", nil)
	s.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("empty secret status = %d", rr.Code)
	}

	t.Setenv("CRON_SECRET", "s3cret")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/internal/tick", nil)
	req.Header.Set("Authorization", "Bearer other")
	s.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("bad bearer status = %d", rr.Code)
	}
}
