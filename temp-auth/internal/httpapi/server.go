package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Maxjalo/SED/temp-auth/internal/jwt"
	"github.com/Maxjalo/SED/temp-auth/internal/store"
)

type Server struct {
	store      *store.Store
	signingKey []byte
	tokenTTL   time.Duration
	nowFn      func() time.Time
	mux        *http.ServeMux
}

func NewServer(st *store.Store, signingKey []byte, ttl time.Duration) *Server {
	s := &Server{
		store:      st,
		signingKey: signingKey,
		tokenTTL:   ttl,
		nowFn:      time.Now,
		mux:        http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /dev/directory", s.handleDevDirectory)
	s.mux.HandleFunc("POST /dev/token", s.handleIssueToken)
	s.mux.HandleFunc("POST /dev/login", s.handlePasswordLogin)
	s.mux.HandleFunc("POST /dev/sessions/{jti}/revoke", s.handleRevokeSession)
	s.mux.HandleFunc("POST /dev/users/{id}/block", s.handleBlockUser(true))
	s.mux.HandleFunc("POST /dev/users/{id}/unblock", s.handleBlockUser(false))
	s.mux.HandleFunc("GET /api/v1/auth/me", s.handleAuthMe)
	s.mux.HandleFunc("GET /api/v1/auth/directory", s.handleDirectory)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "temp-auth",
	})
}

type issueTokenReq struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

func (s *Server) handleIssueToken(w http.ResponseWriter, r *http.Request) {
	var req issueTokenReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}

	var user store.User
	var err error
	if req.UserID != "" {
		user, err = s.store.FindUserByID(req.UserID)
	} else if req.Role != "" {
		user, err = s.store.FindUserByRole(req.Role)
	} else {
		writeError(w, http.StatusBadRequest, "user_id or role is required")
		return
	}
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.store.HasPassword(user.UserID) {
		writeError(w, http.StatusUnauthorized, "для этой учётной записи нужен пароль")
		return
	}
	s.writeToken(w, user)
}

func (s *Server) handlePasswordLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json body")
		return
	}
	user, err := s.store.Authenticate(strings.TrimSpace(req.Login), req.Password)
	if err == store.ErrUserBlocked {
		writeError(w, http.StatusForbidden, "user is blocked")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "неверный логин или пароль")
		return
	}
	s.writeToken(w, user)
}

func (s *Server) writeToken(w http.ResponseWriter, user store.User) {
	now := s.nowFn().UTC()
	sess, err := s.store.CreateSession(user.UserID, s.tokenTTL, now)
	if err != nil {
		if err == store.ErrUserBlocked {
			writeError(w, http.StatusForbidden, "user is blocked")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	claims := jwt.Claims{
		Sub:              user.UserID,
		JTI:              sess.JTI,
		OrganizationCode: user.OrganizationCode,
		DepartmentCode:   user.DepartmentCode,
		Roles:            user.Roles,
		IssuedAt:         now.Unix(),
		ExpiresAt:        sess.ExpiresAt.Unix(),
	}
	token, err := jwt.Sign(claims, s.signingKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": token,
		"token_type":   "Bearer",
		"jti":          sess.JTI,
		"expires_at":   sess.ExpiresAt.Format(time.RFC3339),
		"user":         user,
	})
}

func (s *Server) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	jti := r.PathValue("jti")
	if !s.store.RevokeSession(jti) {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"revoked": true,
		"jti":     jti,
	})
}

func (s *Server) handleBlockUser(blocked bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := r.PathValue("id")
		if err := s.store.SetBlocked(userID, blocked); err != nil {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user_id": userID,
			"blocked": blocked,
		})
	}
}

func (s *Server) handleDevDirectory(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"items": s.store.ListPublicDirectory(""),
	})
}

func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	user, ok := s.authenticateRequest(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (s *Server) handleDirectory(w http.ResponseWriter, r *http.Request) {
	caller, ok := s.authenticateRequest(w, r)
	if !ok {
		return
	}
	orgFilter := r.URL.Query().Get("organization_code")
	if orgFilter == "" {
		orgFilter = caller.OrganizationCode
	}
	users := s.store.ListDirectory(orgFilter)
	writeJSON(w, http.StatusOK, map[string]any{
		"organization_code": orgFilter,
		"items":             users,
	})
}

func (s *Server) authenticateRequest(w http.ResponseWriter, r *http.Request) (store.User, bool) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "missing bearer token")
		return store.User{}, false
	}
	rawToken := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	claims, err := jwt.Verify(rawToken, s.signingKey, s.nowFn().UTC())
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return store.User{}, false
	}
	user, err := s.store.ValidateSession(claims.JTI, claims.Sub, s.nowFn().UTC())
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return store.User{}, false
	}
	return user, true
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
