package routes

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/root"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
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
	google *auth.GoogleHandler
	local  *auth.LocalHandler
	root   *root.RootHandler
	health *health.HealthHandler
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
		var local *auth.LocalHandler
		if s.db != nil {
			queries := db.New(s.db)
			usersRepo := auth.NewPostgresUserRepo(queries)
			sessionsRepo := auth.NewPostgresSessionRepo(queries)
			codesRepo := auth.NewPostgresVerificationCodeRepo(queries)
			mailer := s.buildMailer()
			google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)
			local = auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, mailer, s.log)
		} else {
			s.log.Warn("database not configured — auth endpoints unavailable")
		}

		impl := &routerImpl{
			google: google,
			local:  local,
			root:   root.NewRootHandler(s.log),
			health: health.NewHealthHandler(s.log),
		}
		api.RegisterHandlers(v1, impl)
		v1.POST("/auth/register", impl.HandleRegister)
		v1.POST("/auth/verify-email", impl.HandleVerifyEmail)
	}

	return r
}

func (s *Router) buildMailer() email.Mailer {
	if s.cfg.Env == "development" {
		return email.NewLogMailer()
	}
	if s.cfg.SMTPHost == "" {
		s.log.Warn("SMTP not configured — falling back to log mailer; verification emails will NOT be delivered")
		return email.NewLogMailer()
	}
	return email.NewSMTPMailer(
		s.cfg.SMTPHost,
		s.cfg.SMTPPort,
		s.cfg.SMTPUser,
		s.cfg.SMTPPassword,
		s.cfg.SMTPFrom,
	)
}
