package routes

import (
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/root"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

type Router struct {
	cfg   *config.Config
	log   *slog.Logger
	db    *sql.DB
	redis *redis.Client
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB, redisClient *redis.Client) *Router {
	return &Router{cfg: cfg, log: log, db: db, redis: redisClient}
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
	r.SetTrustedProxies(nil)

	globalLimiter := ratelimit.New(s.redis, "rl:global", 100, time.Minute)

	r.Use(
		common.RequestIDMiddleware(),
		middleware.CORS(s.cfg.FrontendURL),
		middleware.SecurityHeaders(),
		middleware.Logger(s.log),
		middleware.Recover(s.log),
		middleware.RateLimit(globalLimiter, s.log),
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
		queries := db.New(s.db)
		usersRepo := auth.NewPostgresUserRepo(queries)
		sessionsRepo := auth.NewPostgresSessionRepo(queries)
		codesRepo := auth.NewPostgresVerificationCodeRepo(queries)
		localAuthRepo := auth.NewPostgresLocalAuthRepo(s.db)
		mailer := s.buildMailer()
		verifyLimiter := ratelimit.New(s.redis, "rl:auth:verify", 5, 15*time.Minute)
		registerLimiter := ratelimit.New(s.redis, "rl:auth:register", 3, 15*time.Minute)

		impl := &routerImpl{
			google: auth.NewGoogleHandler(s.cfg, usersRepo, s.log),
			local:  auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, localAuthRepo, mailer, s.log, s.cfg.OTPSecret, verifyLimiter, registerLimiter),
			root:   root.NewRootHandler(s.log),
			health: health.NewHealthHandler(s.log),
		}
		api.RegisterHandlers(v1, impl)
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
