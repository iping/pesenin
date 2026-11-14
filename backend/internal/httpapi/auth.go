package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"pesenin.product.biz.id/api/internal/service"
)

func (s *Server) setSession(w http.ResponseWriter, raw string) {
	http.SetCookie(w, &http.Cookie{
		Name: "pn_session", Value: raw, Path: "/", HttpOnly: true,
		Secure: s.App.Cfg.CookieSecure, SameSite: http.SameSiteLaxMode,
		MaxAge: int(s.App.Cfg.SessionTTL.Seconds()),
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"google":         s.App.Cfg.GoogleEnabled(),
		"password":       true,
		"devLogin":       s.App.Cfg.DevLogin,
		"kycGate":        s.App.Cfg.KYCEnforced(),
		"origin":         s.App.Cfg.PublicOrigin,
		"storage":        s.App.Blob.Mode(),
		"vapidPublicKey": s.App.Push.PublicKey(),
	})
}

func (s *Server) devLogin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.App.DevLogin(r.Context(), body.Email, body.Name)
	if err != nil {
		writeErr(w, err)
		return
	}
	raw, err := s.App.StartSessionVia(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, "dev")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.setSession(w, raw)
	me, err := s.App.LoadMe(r.Context(), u)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, meJSON(me))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("pn_session"); err == nil {
		_ = s.App.Logout(r.Context(), c.Value)
	}
	clearCookie(w, "pn_session")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) logoutAll(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("pn_session")
	if err != nil {
		writeErr(w, service.E(401, "unauthenticated", "Masuk dulu ya."))
		return
	}
	u, err := s.App.UserFromSession(r.Context(), c.Value)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := s.App.LogoutAll(r.Context(), u.ID); err != nil {
		writeErr(w, err)
		return
	}
	clearCookie(w, "pn_session")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) googleStart(w http.ResponseWriter, r *http.Request) {
	if !s.App.Cfg.GoogleEnabled() {
		writeErr(w, service.E(503, "google_off", "Google belum diset. Pakai login demo di lingkungan lokal."))
		return
	}
	conf, provider, err := s.google()
	if err != nil {
		writeErr(w, err)
		return
	}
	state := randToken()
	nonce := randToken()
	verifier := oauth2.GenerateVerifier()
	payload, _ := json.Marshal(map[string]string{"state": state, "nonce": nonce, "verifier": verifier})
	http.SetCookie(w, &http.Cookie{
		Name: "pn_oauth", Value: base64.RawURLEncoding.EncodeToString(payload),
		Path: "/api/v1/auth/google", HttpOnly: true, Secure: s.App.Cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode, MaxAge: 600,
	})
	_ = provider
	u := conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce))
	http.Redirect(w, r, u, http.StatusFound)
}

func (s *Server) googleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.App.Cfg.GoogleEnabled() {
		writeErr(w, service.E(503, "google_off", "Google belum diset."))
		return
	}
	conf, provider, err := s.google()
	if err != nil {
		writeErr(w, err)
		return
	}
	c, err := r.Cookie("pn_oauth")
	if err != nil {
		writeErr(w, service.E(400, "oauth_state", "Sesi login Google hilang. Coba lagi ya."))
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(c.Value)
	if err != nil {
		writeErr(w, service.E(400, "oauth_state", "Sesi login Google rusak."))
		return
	}
	var st struct {
		State, Nonce, Verifier string
	}
	if err := json.Unmarshal(raw, &st); err != nil || st.State == "" || st.State != r.URL.Query().Get("state") {
		writeErr(w, service.E(400, "oauth_state", "State Google tidak cocok."))
		return
	}
	clearCookie(w, "pn_oauth")
	tok, err := conf.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		writeErr(w, service.E(400, "oauth_code", "Google menolak kode login."))
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := provider.Verifier(&oidc.Config{ClientID: s.App.Cfg.GoogleID}).Verify(r.Context(), rawID)
	if err != nil {
		writeErr(w, service.E(400, "oauth_token", "Token Google tidak valid."))
		return
	}
	if idt.Nonce != st.Nonce {
		writeErr(w, service.E(400, "oauth_nonce", "Nonce Google tidak cocok."))
		return
	}
	var claims struct {
		Sub     string `json:"sub"`
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
		EmailOK bool   `json:"email_verified"`
	}
	if err := idt.Claims(&claims); err != nil || claims.Sub == "" || !claims.EmailOK {
		writeErr(w, service.E(400, "oauth_email", "Email Google belum terverifikasi."))
		return
	}
	u, err := s.App.UpsertGoogle(r.Context(), claims.Sub, claims.Email, claims.Name, claims.Picture)
	if err != nil {
		writeErr(w, err)
		return
	}
	rawSess, err := s.App.StartSessionVia(r.Context(), u.ID, r.UserAgent(), r.RemoteAddr, "google")
	if err != nil {
		writeErr(w, err)
		return
	}
	s.setSession(w, rawSess)
	http.Redirect(w, r, s.App.Cfg.PublicOrigin+"/app", http.StatusFound)
}

func (s *Server) google() (*oauth2.Config, *oidc.Provider, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, "https://accounts.google.com")
	if err != nil {
		return nil, nil, err
	}
	conf := &oauth2.Config{
		ClientID: s.App.Cfg.GoogleID, ClientSecret: s.App.Cfg.GoogleSecret,
		RedirectURL: s.App.Cfg.GoogleRedirect, Endpoint: provider.Endpoint(),
		Scopes: []string{oidc.ScopeOpenID, "email", "profile"},
	}
	return conf, provider, nil
}

func randToken() string {
	b := make([]byte, 18)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
