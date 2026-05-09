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
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

type Router struct {
	cfg           *config.Config
	log           *slog.Logger
	db            *sql.DB
	redis         *appredis.Client
	globalLimiter ratelimit.RateLimiter
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB, redisClient *appredis.Client) *Router {
	var rawRedis *redis.Client
	if redisClient != nil {
		rawRedis = redisClient.Raw()
	}
	return &Router{
		cfg:           cfg,
		log:           log,
		db:            db,
		redis:         redisClient,
		globalLimiter: ratelimit.New(rawRedis, "rl:global", 100, time.Minute),
	}
}

// Close performs cleanup for the router.
// Currently a no-op as Redis connection lifecycle is managed by the caller.
func (s *Router) Close() {
	// No cleanup needed - Redis connection is managed by cmd/server/main.go
}

type routerImpl struct {
	google   *auth.GoogleHandler
	local    *auth.LocalHandler
	root     *root.RootHandler
	health   *health.HealthHandler
	waitlist *waitlist.WaitlistHandler
	logout   *auth.LogoutHandler
	trainers *trainersStore
}

func (s *Router) Routes() *gin.Engine {
	if s.cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.SetTrustedProxies(nil)

	r.Use(
		common.RequestIDMiddleware(),
		middleware.CORS(s.cfg.FrontendURL),
		middleware.SecurityHeaders(),
		middleware.Logger(s.log),
		middleware.Recover(s.log),
		middleware.RateLimit(s.globalLimiter, s.log),
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
		var (
			q               *db.Queries
			googleHandler   *auth.GoogleHandler
			localHandler    *auth.LocalHandler
			waitlistHandler *waitlist.WaitlistHandler
			logout          *auth.LogoutHandler
		)

		var rawRedis *redis.Client
		if s.redis != nil {
			rawRedis = s.redis.Raw()
			logout = auth.NewLogoutHandler(s.redis, s.log)
		}
		if s.db != nil {
			q = db.New(s.db)

			usersRepo := auth.NewPostgresUserRepo(q)
			googleHandler = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)

			sessionsRepo := auth.NewPostgresSessionRepo(q)
			codesRepo := auth.NewPostgresVerificationCodeRepo(q)
			localAuthRepo := auth.NewPostgresLocalAuthRepo(s.db)
			mailer := s.buildMailer()
			verifyLimiter := ratelimit.New(rawRedis, "rl:auth:verify", 5, 15*time.Minute)
			registerLimiter := ratelimit.New(rawRedis, "rl:auth:register", 3, 15*time.Minute)
			localHandler = auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, localAuthRepo, mailer, s.log, s.cfg.OTPSecret, verifyLimiter, registerLimiter)

			waitlistRepo := waitlist.NewPostgresWaitlistRepo(q)
			waitlistHandler = waitlist.NewWaitlistHandler(waitlistRepo, s.log)
		} else {
			s.log.Warn("database not configured — auth, waitlist and trainers endpoints may be unavailable")
		}

		impl := &routerImpl{
			google:   googleHandler,
			local:    localHandler,
			root:     root.NewRootHandler(s.log),
			health:   health.NewHealthHandler(s.log),
			waitlist: waitlistHandler,
			logout:   logout,
			trainers: newTrainersStore(q),
		}

		authMw := middleware.AuthMiddleware(s.redis)
		var adminOnly api.MiddlewareFunc
		if q != nil {
			adminOnly = middleware.TrainersAdminOnly(q)
		}

		api.RegisterHandlersWithOptions(v1, impl, api.GinServerOptions{
			Middlewares: []api.MiddlewareFunc{
				func(c *gin.Context) {
					if _, requiresAuth := c.Get(string(api.BearerAuthScopes)); requiresAuth {
						authMw(c)
						if c.IsAborted() {
							return
						}
					}
					if adminOnly != nil {
						adminOnly(c)
					}
				},
			},
		})
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
