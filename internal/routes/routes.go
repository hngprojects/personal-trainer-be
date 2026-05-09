package routes

import (
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/root"
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
)

// Router holds the wrapped Redis client (*appredis.Client) — its method set
// matches the appredis.RedisClient interface that auth/middleware consumers
// expect. Code paths that need the raw go-redis client (rate limiter, which
// runs Lua scripts) call s.redis.Raw().
type Router struct {
	cfg           *config.Config
	log           *slog.Logger
	db            *sql.DB
	redis         *appredis.Client
	globalLimiter ratelimit.RateLimiter
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB, redisClient *appredis.Client) *Router {
	r := &Router{
		cfg:   cfg,
		log:   log,
		db:    db,
		redis: redisClient,
	}
	if redisClient != nil {
		r.globalLimiter = ratelimit.New(redisClient.Raw(), "rl:global", 100, time.Minute)
	}
	return r
}

// Close performs cleanup for the router.
// Currently a no-op as Redis connection lifecycle is managed by the caller.
func (s *Router) Close() {
	// No cleanup needed - Redis connection is managed by cmd/server/main.go
}

type routerImpl struct {
	google        *auth.GoogleHandler
	local         *auth.LocalHandler
	login         *auth.LoginHandler
	root          *root.RootHandler
	health        *health.HealthHandler
	waitlist      *waitlist.WaitlistHandler
	logout        *auth.LogoutHandler
	passwordReset *auth.PasswordResetHandler
	trainers      *trainersStore
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
	)
	if s.globalLimiter != nil {
		r.Use(middleware.RateLimit(s.globalLimiter, s.log))
	} else {
		s.log.Warn("global rate limiter disabled — redis is not configured")
	}

	spec, err := os.ReadFile("api.yaml")
	if err != nil {
		s.log.Warn("could not read api.yaml — /docs/spec will be unavailable", "err", err)
	}
	docs := handlers.NewDocsHandler(spec)
	r.GET("/docs", docs.UI)
	r.GET("/docs/spec", docs.Spec)

	v1 := r.Group("/api/v1")
	{
		impl := &routerImpl{
			root:   root.NewRootHandler(s.log),
			health: health.NewHealthHandler(s.log),
		}

		var q *db.Queries

		if s.redis != nil {
			impl.logout = auth.NewLogoutHandler(s.redis, s.log)
		}

		if s.db != nil {
			q = db.New(s.db)
			usersRepo := auth.NewPostgresUserRepo(q)
			waitlistRepo := waitlist.NewPostgresWaitlistRepo(q)
			sessionsRepo := auth.NewPostgresSessionRepo(q)
			rolesRepo := auth.NewPostgresRoleRepo(q)

			impl.google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)
			impl.waitlist = waitlist.NewWaitlistHandler(waitlistRepo, s.log)
			impl.login = auth.NewLoginHandler(usersRepo, sessionsRepo, rolesRepo, s.log)
			impl.trainers = newTrainersStore(q)

			// All remaining handlers depend on Redis-backed rate limiters.
			// If Redis is unavailable, ratelimit.New would hand back limiters
			// holding a nil *redis.Client and any Allow() call would panic.
			// Skip those handlers entirely when Redis isn't configured —
			// downstream forwarders return 503 from the routerImpl methods.
			if s.redis != nil {
				codesRepo := auth.NewPostgresVerificationCodeRepo(q)
				localAuthRepo := auth.NewPostgresLocalAuthRepo(s.db)
				passwordResetRepo := auth.NewPostgresPasswordResetRepo(s.db)

				mailer := s.buildMailer()
				rawRedis := s.redis.Raw()
				verifyLimiter := ratelimit.New(rawRedis, "rl:auth:verify", 5, 15*time.Minute)
				registerLimiter := ratelimit.New(rawRedis, "rl:auth:register", 3, 15*time.Minute)
				forgotLimiter := ratelimit.New(rawRedis, "rl:auth:forgot-password", 3, 15*time.Minute)
				forgotIPLimiter := ratelimit.New(rawRedis, "rl:auth:forgot-password:ip", 10, 15*time.Minute)
				resetLimiter := ratelimit.New(rawRedis, "rl:auth:reset-password", 5, 15*time.Minute)
				resetIPLimiter := ratelimit.New(rawRedis, "rl:auth:reset-password:ip", 20, 15*time.Minute)

				impl.local = auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, localAuthRepo, mailer, s.log, s.cfg.OTPSecret, verifyLimiter, registerLimiter)
				impl.passwordReset = auth.NewPasswordResetHandler(usersRepo, rolesRepo, passwordResetRepo, mailer, s.log, s.cfg.OTPSecret, forgotLimiter, forgotIPLimiter, resetLimiter, resetIPLimiter)
			} else {
				s.log.Warn("redis is not configured — rate-limited auth handlers (local register/verify, password reset) will return 503")
			}
		} else {
			s.log.Warn("database not configured — auth, waitlist and trainers endpoints may be unavailable")
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
