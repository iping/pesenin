package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"pesenin.product.biz.id/api/internal/db"
	"pesenin.product.biz.id/api/internal/service"
)

type Server struct {
	App *service.App
}

func New(app *service.App) *Server { return &Server{App: app} }

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer, middleware.Logger)
	r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"ok": "true"})
	})
	r.Get("/api/v1/internal/tick", s.tick)
	r.Post("/api/v1/internal/tick", s.tick)
	r.Get("/api/v1/media/*", s.media)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/auth/config", s.authConfig)
		r.Get("/auth/google/start", s.googleStart)
		r.Get("/auth/google/callback", s.googleCallback)
		r.With(httprate.LimitByIP(20, time.Minute)).Post("/auth/dev", s.devLogin)
		r.Post("/auth/password/signup", s.passwordSignup)
		r.Post("/auth/password/login", s.passwordLogin)
		r.Post("/auth/password/forgot", s.passwordForgot)
		r.Post("/auth/password/reset", s.passwordReset)
		r.Post("/auth/email/verify", s.emailVerify)
		r.Post("/auth/email/resend", s.emailResend)
		r.Post("/auth/logout", s.logout)
		r.Post("/auth/logout-all", s.logoutAll)

		r.Get("/public/brands/{slug}", s.publicBrand)
		r.Get("/public/brands/{slug}/stores/{store}", s.publicStore)
		r.Get("/public/batches/{code}", s.publicBatch)
		r.Post("/public/batches/{code}/quote", s.quoteOrder)
		r.With(httprate.LimitByIP(30, time.Minute)).Post("/public/batches/{code}/orders", s.placeOrder)
		r.Get("/public/orders/{token}", s.publicOrder)
		r.With(httprate.LimitByIP(20, time.Minute)).Post("/public/orders/{token}/proof", s.publicProof)
		r.Get("/public/qr.png", s.qrPNG)
		r.Get("/bank-codes", s.bankCodes)
		r.Get("/push/vapid-public-key", s.vapidKey)

		r.Group(func(r chi.Router) {
			r.Use(s.authn)
			r.Get("/me", s.me)
			r.Get("/me/verification", s.verification)
			r.Post("/me/email", s.emailChange)
			r.Post("/me/whatsapp/otp", s.whatsappOTP)
			r.Post("/me/whatsapp/otp/check", s.whatsappCheck)
			r.Post("/auth/password/link", s.passwordLink)
			r.Get("/dashboard", s.dashboard)
			r.Get("/slugs/check", s.slugCheck)
			r.Post("/brands", s.createBrand)
			r.Patch("/brands/{brandId}", s.patchBrand)
			r.Post("/brands/{brandId}/kyc/submit", s.kycSubmit)
			r.Get("/brands/{brandId}/stores", s.listStores)
			r.Patch("/stores/{storeId}", s.patchStore)
			r.Get("/brands/{brandId}/bank-accounts", s.listBanks)
			r.Post("/brands/{brandId}/bank-accounts", s.addBank)
			r.Get("/brands/{brandId}/products", s.listProducts)
			r.Post("/brands/{brandId}/products", s.saveProduct)
			r.Patch("/products/{productId}", s.patchProduct)
			r.Post("/products/{productId}/photo", s.productPhoto)
			r.Get("/brands/{brandId}/extra-charges", s.listCharges)
			r.Post("/brands/{brandId}/extra-charges", s.saveCharge)
			r.Patch("/extra-charges/{chargeId}", s.patchCharge)
			r.Get("/brands/{brandId}/templates", s.listTemplates)
			r.Put("/brands/{brandId}/templates", s.putTemplate)
			r.Get("/brands/{brandId}/customers", s.listCustomers)
			r.Get("/brands/{brandId}/batches", s.listBatches)
			r.Post("/brands/{brandId}/batches", s.createBatch)
			r.Get("/batches/{batchId}", s.getBatch)
			r.Patch("/batches/{batchId}", s.patchBatch)
			r.Post("/batches/{batchId}/open", s.openBatch)
			r.Post("/batches/{batchId}/close", s.closeBatch)
			r.Post("/batches/{batchId}/duplicate", s.duplicateBatch)
			r.Get("/batches/{batchId}/recap", s.recap)
			r.Get("/batches/{batchId}/recap.csv", s.recapCSV)
			r.Get("/orders", s.listOrders)
			r.Get("/orders/{orderId}", s.getOrder)
			r.Post("/orders/{orderId}/mark-paid", s.markPaid)
			r.Post("/orders/{orderId}/undo-paid", s.undoPaid)
			r.Post("/orders/{orderId}/reject-proof", s.rejectProof)
			r.Post("/orders/{orderId}/cancel", s.cancelOrder)
			r.Post("/orders/{orderId}/fulfilment", s.fulfil)
			r.Get("/orders/{orderId}/wa-link", s.waLink)
			r.Get("/wallet", s.wallet)
			r.Get("/wallet/transactions", s.ledger)
			r.Get("/topups/options", s.topupOptions)
			r.Post("/topups/quote", s.topupQuote)
			r.Get("/topups", s.listTopups)
			r.Post("/topups", s.createTopup)
			r.Get("/topups/{topupId}", s.getTopup)
			r.Post("/topups/{topupId}/proof", s.topupProof)
			r.Get("/notifications", s.notifications)
			r.Get("/notifications/unread-count", s.unread)
			r.Post("/notifications/read", s.markRead)
			r.Post("/push/subscriptions", s.savePush)
			r.Delete("/push/subscriptions/{id}", s.deletePush)

			r.Route("/admin", func(r chi.Router) {
				r.Get("/topups", s.adminTopups)
				r.Post("/topups/{topupId}/approve", s.adminApprove)
				r.Post("/topups/{topupId}/reject", s.adminReject)
				r.Get("/fee-configs", s.adminFees)
				r.Post("/fee-configs", s.adminPublishFee)
				r.Get("/settings", s.adminSettings)
				r.Put("/settings", s.adminSaveSettings)
				r.Post("/wallets/{accountId}/adjust", s.adminAdjust)
				r.Get("/reports/koin", s.adminReport)
				r.Get("/audit", s.adminAudit)
				r.Get("/kyc/brands", s.adminKYCBrands)
				r.Get("/kyc/brands/{brandId}", s.adminKYCBrand)
				r.Post("/kyc/brands/{brandId}/approve", s.adminKYCApprove)
				r.Post("/kyc/brands/{brandId}/reject", s.adminKYCReject)
				r.Post("/kyc/brands/{brandId}/suspend", s.adminKYCSuspend)
			})
		})
	})
	return r
}

type ctxKey int

const userKey ctxKey = 1
const viaKey ctxKey = 2

func withVia(ctx context.Context, via string) context.Context {
	return context.WithValue(ctx, viaKey, via)
}

func via(r *http.Request) string {
	v, _ := r.Context().Value(viaKey).(string)
	return v
}

func (s *Server) authn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("pn_session")
		if err != nil {
			writeErr(w, service.E(401, "unauthenticated", "Masuk dulu ya."))
			return
		}
		u, via, err := s.App.SessionUser(r.Context(), c.Value)
		if err != nil {
			writeErr(w, err)
			return
		}
		ctx := withUser(r.Context(), u)
		next.ServeHTTP(w, r.WithContext(withVia(ctx, via)))
	})
}

// tick runs the same maintenance pass as the in-process worker.
// Vercel can call it with Authorization: Bearer $CRON_SECRET when the
// Go instance has scaled to zero. The route stays closed until that secret is set.
func (s *Server) tick(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("CRON_SECRET")
	got := r.Header.Get("Authorization")
	expect := "Bearer " + secret
	if secret == "" || len(got) != len(expect) || subtle.ConstantTimeCompare([]byte(got), []byte(expect)) != 1 {
		writeErr(w, service.E(401, "unauthorized", "Unauthorized."))
		return
	}
	s.App.Tick(r.Context())
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var e *service.Error
	if errors.As(err, &e) {
		st := e.Status
		if st == 0 {
			st = 400
		}
		writeJSON(w, st, map[string]string{"error": e.Code, "message": e.Message})
		return
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, 404, map[string]string{"error": "not_found", "message": "Data tidak ketemu."})
		return
	}
	writeJSON(w, 500, map[string]string{"error": "internal", "message": "Ada gangguan. Coba lagi ya."})
}

func readJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	if err := dec.Decode(dst); err != nil {
		return service.E(400, "bad_json", "Data yang dikirim belum lengkap.")
	}
	return nil
}

func withUser(ctx context.Context, u db.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func user(r *http.Request) db.User {
	u, _ := r.Context().Value(userKey).(db.User)
	return u
}

func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, service.E(400, "bad_id", "ID tidak valid.")
	}
	return id, nil
}

func queryInt(r *http.Request, name string) (int64, bool) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}
