package routes

import (
	"database/sql"
	"log/slog"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/admininvite"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/root"
	"github.com/hngprojects/personal-trainer-be/pkg/email"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Router struct {
	cfg *config.Config
	log *slog.Logger
	db  *sql.DB
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB) *Router {
	return &Router{cfg: cfg, log: log, db: db}
}

type routerImpl struct {
	google  *auth.GoogleHandler
	root    *root.RootHandler
	health  *health.HealthHandler
	users   auth.UserRepository
	invites *admininvite.Service
	log     *slog.Logger
}

func (s *Router) Routes() *gin.Engine {
	if s.cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(
		common.RequestIDMiddleware(),
		middleware.CORS(s.cfg.FrontendURL),
		middleware.SecurityHeaders(),
		middleware.Logger(s.log),
		middleware.Recover(s.log),
	)

	spec, err := os.ReadFile("api.yaml")
	if err != nil {
		s.log.Warn("could not read api.yaml — /docs/spec will be unavailable", "err", err)
	}
	docs := handlers.NewDocsHandler(spec)
	r.GET("/docs", docs.UI)
	r.GET("/docs/spec", docs.Spec)

	v1 := r.Group("/api/v1")
	{
		var google *auth.GoogleHandler
		var usersRepo auth.UserRepository
		var inviteSvc *admininvite.Service
		if s.db != nil {
			usersRepo = auth.NewPostgresUserRepo(db.New(s.db))
			google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)

			var mailer email.Mailer = email.NewLogMailer()
			if s.cfg.Env == "production" {
				mailer = email.NewSMTPMailer(s.cfg.SMTPHost, s.cfg.SMTPPort,
					s.cfg.SMTPUser, s.cfg.SMTPPassword, s.cfg.SMTPFrom)
			}
			inviteRepo := admininvite.NewPostgresRepo(db.New(s.db))
			inviteSvc = admininvite.NewService(s.db, inviteRepo, usersRepo, mailer, s.log, s.cfg.FrontendURL)
		} else {
			s.log.Warn("database not configured — auth endpoints unavailable")
		}

		impl := &routerImpl{
			google:  google,
			root:    root.NewRootHandler(s.log),
			health:  health.NewHealthHandler(s.log),
			users:   usersRepo,
			invites: inviteSvc,
			log:     s.log,
		}
		api.RegisterHandlers(v1, impl)
	}

	return r
}

// requireSuperAdmin gates the calling handler to users with the super_admin role.
// Used inline by handlers that oapi-codegen registered on the same group as
// public routes — splitting into multiple groups would conflict with the
// generated routing table.
func (s *routerImpl) requireSuperAdmin(c *gin.Context) bool {
	if s.users == nil {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable,
			api.NewError("service unavailable", api.CodeServerError))
		return false
	}
	middleware.AuthMiddleware()(c)
	if c.IsAborted() {
		return false
	}
	middleware.RequireRole(s.users, "super_admin")(c)
	return !c.IsAborted()
}
