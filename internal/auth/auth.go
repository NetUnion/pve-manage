package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/config"
	"github.com/NetUnion/pve-manage/internal/model"

	"database/sql"

	oidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Manager struct {
	cfg      *config.App
	db       *sql.DB
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth2   *oauth2.Config
	secret   []byte
}

type Session struct {
	Username string `json:"username"`
	Expires  int64  `json:"expires"`
}

type Claims struct {
	Username string
	Email    string
	Name     string
	Groups   []string
}

func NewManager(ctx context.Context, cfg *config.App, db *sql.DB) (*Manager, error) {
	provider, err := oidc.NewProvider(ctx, cfg.OIDC.Issuer)
	if err != nil {
		return nil, err
	}

	scopes := append([]string(nil), cfg.OIDC.Scopes...)
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "profile", "email", "groups"}
	}

	return &Manager{
		cfg:      cfg,
		db:       db,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.OIDC.ClientID}),
		oauth2: &oauth2.Config{
			ClientID:     cfg.OIDC.ClientID,
			ClientSecret: cfg.OIDC.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  cfg.OIDC.RedirectURL,
			Scopes:       scopes,
		},
		secret: []byte(cfg.OIDC.ClientSecret),
	}, nil
}

func (m *Manager) LoginURL() string {
	state := randomString(32)
	return m.oauth2.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

func (m *Manager) HandleLogin(w http.ResponseWriter, r *http.Request) {
	state := randomString(32)
	setSignedCookie(w, m.cfg.OIDC.Session.CookieName+"_state", state, m.secret, 10*time.Minute, true, m.cookieSameSite())
	http.Redirect(w, r, m.oauth2.AuthCodeURL(state, oauth2.AccessTypeOffline), http.StatusFound)
}

func (m *Manager) HandleCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stateCookie, err := r.Cookie(m.cfg.OIDC.Session.CookieName + "_state")
	if err != nil {
		http.Error(w, "missing oauth state", http.StatusBadRequest)
		return
	}
	state, ok := verifySignedCookie(stateCookie.Value, m.secret)
	if !ok || state == "" {
		http.Error(w, "invalid oauth state", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("state") != state {
		http.Error(w, "state mismatch", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	token, err := m.oauth2.Exchange(ctx, code)
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "missing id_token", http.StatusBadRequest)
		return
	}

	idToken, err := m.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "id token verify failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	claims, err := m.claimsFromToken(idToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !config.ValidateUsername(claims.Username) {
		http.Error(w, "invalid username format", http.StatusBadRequest)
		return
	}

	isAdmin := m.userIsAdmin(claims.Groups)
	if _, err := m.upsertUser(ctx, claims, isAdmin); err != nil {
		http.Error(w, "upsert user failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	setSignedCookie(w, m.cfg.OIDC.Session.CookieName, claims.Username, m.secret, m.cfg.SessionDuration(), m.cfg.OIDC.Session.CookieSecure, m.cookieSameSite())
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.OIDC.Session.CookieName + "_state",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cfg.OIDC.Session.CookieSecure,
		SameSite: m.cookieSameSite(),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     m.cfg.OIDC.Session.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.cfg.OIDC.Session.CookieSecure,
		SameSite: m.cookieSameSite(),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (m *Manager) CurrentUser(r *http.Request) (*model.User, error) {
	cookie, err := r.Cookie(m.cfg.OIDC.Session.CookieName)
	if err != nil {
		return nil, errors.New("missing session")
	}
	username, ok := verifySignedCookie(cookie.Value, m.secret)
	if !ok || username == "" {
		return nil, errors.New("invalid session")
	}
	var user model.User
	var createdAt string
	var updatedAt string
	var lastLogin sql.NullString
	err = m.db.QueryRowContext(r.Context(), `
		SELECT id, username, email, name, groups_json, is_active, is_admin, created_at, updated_at, last_login_at
		FROM users
		WHERE username = $1 AND is_active = 1
	`, username).Scan(
		&user.ID,
		&user.Username,
		&user.Email,
		&user.Name,
		&user.GroupsJSON,
		&user.IsActive,
		&user.IsAdmin,
		&createdAt,
		&updatedAt,
		&lastLogin,
	)
	if err != nil {
		return nil, err
	}
	if t, parseErr := time.Parse(time.RFC3339Nano, createdAt); parseErr == nil {
		user.CreatedAt = t
	}
	if t, parseErr := time.Parse(time.RFC3339Nano, updatedAt); parseErr == nil {
		user.UpdatedAt = t
	}
	if lastLogin.Valid {
		t, parseErr := time.Parse(time.RFC3339Nano, lastLogin.String)
		if parseErr == nil {
			user.LastLoginAt = &t
		}
	}
	return &user, nil
}

func (m *Manager) claimsFromToken(token *oidc.IDToken) (Claims, error) {
	var raw map[string]any
	if err := token.Claims(&raw); err != nil {
		return Claims{}, err
	}

	username, _ := claimStringAny(raw, m.cfg.OIDC.Claims.Username, "preferred_username", "username")
	email := claimString(raw, m.cfg.OIDC.Claims.Email)
	name := claimString(raw, m.cfg.OIDC.Claims.Name)
	groups := claimStringSlice(raw, m.cfg.OIDC.Claims.Groups)

	if username == "" {
		return Claims{}, fmt.Errorf("username claim missing; tried %q and common fallbacks, token keys=%v", m.cfg.OIDC.Claims.Username, mapKeys(raw))
	}

	return Claims{
		Username: username,
		Email:    email,
		Name:     name,
		Groups:   groups,
	}, nil
}

func (m *Manager) upsertUser(ctx context.Context, claims Claims, isAdmin bool) (int64, error) {
	groupsJSON, err := json.Marshal(claims.Groups)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	var id int64
	err = m.db.QueryRowContext(ctx, `
		INSERT INTO users(username, email, name, groups_json, is_active, is_admin, created_at, updated_at, last_login_at)
		VALUES($1, $2, $3, $4, 1, $5, $6, $7, $8)
		ON CONFLICT(username) DO UPDATE SET
			email = excluded.email,
			name = excluded.name,
			groups_json = excluded.groups_json,
			is_admin = excluded.is_admin,
			updated_at = excluded.updated_at,
			last_login_at = excluded.last_login_at
		RETURNING id
	`, claims.Username, claims.Email, claims.Name, string(groupsJSON), boolToInt(isAdmin), now, now, now).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (m *Manager) userIsAdmin(groups []string) bool {
	adminGroups := make(map[string]struct{}, len(m.cfg.Root.User.AdminGroup))
	for _, g := range m.cfg.Root.User.AdminGroup {
		adminGroups[g] = struct{}{}
	}
	for _, g := range groups {
		if _, ok := adminGroups[g]; ok {
			return true
		}
	}
	return false
}

func (m *Manager) cookieSameSite() http.SameSite {
	switch strings.ToLower(m.cfg.OIDC.Session.CookieSameSite) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

func claimString(raw map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := raw[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func claimStringAny(raw map[string]any, keys ...string) (string, string) {
	for _, key := range keys {
		if value := claimString(raw, key); value != "" {
			return value, key
		}
	}
	return "", ""
}

func claimStringSlice(raw map[string]any, key string) []string {
	if key == "" {
		return nil
	}
	v, ok := raw[key]
	if !ok {
		return nil
	}
	switch vv := v.(type) {
	case []any:
		out := make([]string, 0, len(vv))
		for _, item := range vv {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return append([]string(nil), vv...)
	default:
		return nil
	}
}

func mapKeys(raw map[string]any) []string {
	out := make([]string, 0, len(raw))
	for key := range raw {
		out = append(out, key)
	}
	return out
}

func randomString(n int) string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			b[i] = alphabet[0]
			continue
		}
		b[i] = alphabet[idx.Int64()]
	}
	return string(b)
}

func setSignedCookie(w http.ResponseWriter, name, value string, secret []byte, ttl time.Duration, secure bool, sameSite http.SameSite) {
	payload := Session{
		Username: value,
		Expires:  time.Now().Add(ttl).Unix(),
	}
	raw, _ := json.Marshal(payload)
	token := signPayload(raw, secret)
	cookieValue := base64.RawURLEncoding.EncodeToString(raw) + "." + base64.RawURLEncoding.EncodeToString(token)
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func verifySignedCookie(value string, secret []byte) (string, bool) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	if !hmac.Equal(sig, signPayload(raw, secret)) {
		return "", false
	}

	var session Session
	if err := json.Unmarshal(raw, &session); err != nil {
		return "", false
	}
	if time.Now().Unix() > session.Expires {
		return "", false
	}
	return session.Username, true
}

func signPayload(payload, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
