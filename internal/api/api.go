package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/NetUnion/pve-manage/internal/auth"
	"github.com/NetUnion/pve-manage/internal/config"
	"github.com/NetUnion/pve-manage/internal/service"
	"github.com/NetUnion/pve-manage/internal/webui"

	"github.com/go-chi/chi/v5"
)

type Server struct {
	logger  *slog.Logger
	config  *config.App
	db      *sql.DB
	auth    *auth.Manager
	options service.OptionsResponse
	webui   http.Handler
}

func NewServer(ctx context.Context, logger *slog.Logger, cfg *config.App, db *sql.DB) (*Server, error) {
	authMgr, err := auth.NewManager(ctx, cfg, db)
	if err != nil {
		return nil, err
	}
	ui, err := webui.Handler()
	if err != nil {
		return nil, err
	}

	return &Server{
		logger:  logger,
		config:  cfg,
		db:      db,
		auth:    authMgr,
		options: service.BuildOptions(cfg),
		webui:   ui,
	}, nil
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(requestLogger(s.logger))

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if s.webui != nil {
			s.webui.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})
	r.Handle("/assets/*", s.webui)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			http.NotFound(w, r)
			return
		}
		if filepath.Ext(r.URL.Path) != "" {
			if s.webui != nil {
				s.webui.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
			return
		}
		if s.webui != nil {
			r.URL.Path = "/"
			s.webui.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
	})

	r.Get("/auth/login", s.auth.HandleLogin)
	r.Get("/auth/oidc/callback", s.auth.HandleCallback)
	r.Get("/auth/callback", s.auth.HandleCallback)
	r.Post("/auth/logout", s.auth.HandleLogout)

	r.Get("/healthz", s.handleHealthz)
	r.Get("/api/config/options", s.handleOptions)
	r.Get("/api/me", s.handleMe)
	r.Get("/api/vms", s.handleListVMs)
	r.Post("/api/vms", s.handleCreateVM)
	r.Get("/api/vms/{id}", s.handleGetVM)
	r.Patch("/api/vms/{id}", s.handlePatchVM)
	r.Delete("/api/vms/{id}", s.handleDeleteVM)
	r.Post("/api/vms/{id}/restore", s.handleRestoreVM)
	r.Post("/api/vms/{id}/delete-now", s.handleDeleteNowVM)
	r.Post("/api/vms/{id}/power", s.handlePowerVM)
	r.Post("/api/vms/{id}/tasks/pause", s.handlePauseVMTasks)
	r.Post("/api/vms/{id}/tasks/resume", s.handleResumeVMTasks)
	r.Post("/api/vms/{id}/tasks/{task_id}/retry", s.handleRetryVMTask)
	r.Get("/api/ssh-keys", s.handleListSSHKeys)
	r.Post("/api/ssh-keys", s.handleCreateSSHKey)
	r.Delete("/api/ssh-keys/{id}", s.handleDeleteSSHKey)
	r.Get("/api/security-groups", s.handleListSecurityGroups)
	r.Post("/api/security-groups", s.handleCreateSecurityGroup)
	r.Get("/api/security-groups/{name}", s.handleGetSecurityGroup)
	r.Patch("/api/security-groups/{name}", s.handlePatchSecurityGroup)
	r.Delete("/api/security-groups/{name}", s.handleDeleteSecurityGroup)
	r.Get("/api/templates", s.handleListTemplates)
	r.Get("/api/admin/vms", s.handleAdminListVMs)
	r.Get("/api/admin/vm-tasks", s.handleAdminListVMTasks)
	r.Get("/api/admin/users", s.handleAdminListUsers)
	r.Get("/api/admin/security-groups", s.handleAdminListSecurityGroups)
	r.Get("/api/admin/ssh-keys", s.handleAdminListSSHKeys)
	r.Get("/api/admin/maintenance-tasks", s.handleAdminListMaintenanceTasks)
	r.Patch("/api/admin/vms/{id}", s.handleAdminPatchVM)
	r.Post("/api/admin/vms/{id}/adopt", s.handleAdminAdoptVM)
	r.Patch("/api/admin/vms/{id}/quota-exempt", s.handleAdminSetQuotaExempt)

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.PingContext(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
	})
}

func (s *Server) handleOptions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.options)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := s.auth.CurrentUser(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": err.Error(),
		})
		return
	}

	quota := s.effectiveQuota(user)
	count, cpu, memory, storage, err := s.listUserVMUsage(r.Context(), user.Username)
	if err != nil {
		s.logger.Warn("failed to load user vm usage", "username", user.Username, "error", err)
		count, cpu, memory, storage = 0, 0, 0, 0
	}
	sgCount, err := s.countUserSecurityGroups(r.Context(), user.Username)
	if err != nil {
		s.logger.Warn("failed to load user security group count", "username", user.Username, "error", err)
		sgCount = 0
	}

	writeJSON(w, http.StatusOK, userEnvelope{
		Username: user.Username,
		Email:    user.Email,
		Name:     user.Name,
		Groups:   parseJSONStrings(user.GroupsJSON),
		IsAdmin:  user.IsAdmin,
		Quota: quotaEnvelope{
			Number:        quota.Number,
			CPU:           quota.CPU,
			Memory:        quota.Memory,
			Storage:       quota.Storage,
			SecurityGroup: quota.SecurityGroup,
			UESTC:         quota.UESTC,
		},
		Usage: usageEnvelope{
			Count:         count,
			CPU:           cpu,
			Memory:        memory,
			Storage:       storage,
			SecurityGroup: sgCount,
		},
	})
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(ww, r)
			logger.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.statusCode,
				"duration", time.Since(start),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (s *statusRecorder) WriteHeader(statusCode int) {
	s.statusCode = statusCode
	s.ResponseWriter.WriteHeader(statusCode)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, "encode json response", http.StatusInternalServerError)
	}
}
