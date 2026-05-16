package routes

import (
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/admin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/booking_session"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/contact"
	"github.com/hngprojects/personal-trainer-be/internal/dev"
	"github.com/hngprojects/personal-trainer-be/internal/discovery"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	reviewsvc "github.com/hngprojects/personal-trainer-be/internal/reviews"
	"github.com/hngprojects/personal-trainer-be/internal/root"
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
	appzoom "github.com/hngprojects/personal-trainer-be/pkg/zoom"
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
	googleMobile  *auth.MobileGoogleHandler
	local         *auth.LocalHandler
	root          *root.RootHandler
	adminLogin    *handlers.AdminLoginHandler
	health        *health.HealthHandler
	waitlist      *waitlist.WaitlistHandler
	logout        *auth.LogoutHandler
	refresh       *auth.RefreshHandler
	passwordReset *auth.PasswordResetHandler
	trainers      *trainersStore
	users         *usersStore
	reviews       *reviewsvc.Service
	admin         *admin.Handler
	contact       *contact.Handler
	discovery     *discovery.Handler
	dev           *dev.Handler
	bookingSession booking_session.SessionHandler
}

func (s *Router) Routes() *gin.Engine {
	if s.cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	_ = r.SetTrustedProxies(nil) // nil cannot fail; explicit discard for errcheck

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
			adminLoginService := auth.NewAdminLoginService(usersRepo, rolesRepo, s.log)
			codesRepo := auth.NewPostgresVerificationCodeRepo(q)
			localAuthRepo := auth.NewPostgresLocalAuthRepo(s.db)
			passwordResetRepo := auth.NewPostgresPasswordResetRepo(s.db)

			bookingSessionRepo := booking_session.NewPostgresBookingSessionRepo(q)
			bookingSessionService := booking_session.NewSessionService(bookingSessionRepo, s.log)
			mailer := s.buildMailer()

			impl.adminLogin = handlers.NewAdminLogin(adminLoginService, s.log)
			impl.google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)
			impl.googleMobile = auth.NewMobileGoogleHandler(s.cfg, usersRepo, sessionsRepo, s.log)
			impl.waitlist = waitlist.NewWaitlistHandler(waitlistRepo, s.log, mailer)
			impl.contact = contact.NewHandler(q, s.log, mailer)
			impl.trainers = newTrainersStore(q)
			impl.users = newUsersStore(q)

			var meetingProvider meeting.Provider = meeting.NoOp{}
			if s.cfg.ZoomAccountID != "" {
				meetingProvider = appzoom.New(s.cfg.ZoomAccountID, s.cfg.ZoomClientID, s.cfg.ZoomClientSecret)
			}
			discoveryRepo := discovery.NewPostgresRepo(q)
			impl.discovery = discovery.NewHandler(discoveryRepo, meetingProvider, mailer, s.cfg.NotificationEmail, s.log)
			impl.reviews = reviewsvc.NewService(s.db, q, s.log)
			impl.bookingSession = booking_session.NewSessionHandler(bookingSessionService, *s.redis, s.log)
			// Rate limiters are Redis-backed. When Redis is unavailable we wire
			// in AllowAllLimiter (always-allow) so the auth endpoints stay up
			// instead of returning 503 across the board. Real Redis-Allow errors
			// at request time already fail open; this matches that behaviour for
			// the "no backend at all" startup case.
			var (
				verifyLimiter   ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				registerLimiter ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				forgotLimiter   ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				forgotIPLimiter ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				resetLimiter    ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				resetIPLimiter  ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				refreshLimiter  ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
			)
			if s.redis != nil {
				rawRedis := s.redis.Raw()
				verifyLimiter = ratelimit.New(rawRedis, "rl:auth:verify", 5, 15*time.Minute)
				registerLimiter = ratelimit.New(rawRedis, "rl:auth:register", 3, 15*time.Minute)
				forgotLimiter = ratelimit.New(rawRedis, "rl:auth:forgot-password", 3, 15*time.Minute)
				forgotIPLimiter = ratelimit.New(rawRedis, "rl:auth:forgot-password:ip", 10, 15*time.Minute)
				resetLimiter = ratelimit.New(rawRedis, "rl:auth:reset-password", 5, 15*time.Minute)
				resetIPLimiter = ratelimit.New(rawRedis, "rl:auth:reset-password:ip", 20, 15*time.Minute)
				refreshLimiter = ratelimit.New(rawRedis, "rl:auth:refresh", 10, 1*time.Minute)
			} else {
				s.log.Warn("redis is not configured — auth rate limits disabled (using no-op limiters)")
			}

			impl.local = auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, localAuthRepo, mailer, s.log, s.cfg.OTPSecret, verifyLimiter, registerLimiter)
			impl.passwordReset = auth.NewPasswordResetHandler(usersRepo, rolesRepo, passwordResetRepo, mailer, s.log, s.cfg.OTPSecret, forgotLimiter, forgotIPLimiter, resetLimiter, resetIPLimiter)
			impl.refresh = auth.NewRefreshHandler(s.redis, s.log, refreshLimiter)
			impl.admin = admin.NewHandler(usersRepo.(auth.AdminUserRepository), mailer, s.log)
		} else {
			s.log.Warn("database not configured — auth, waitlist and trainers endpoints may be unavailable")
		}
		if s.cfg.Env == "development" {
			impl.dev = dev.NewDevHandler()
		}

		var authRedis appredis.RedisClient
		if s.redis != nil {
			authRedis = s.redis
		}
		authMw := middleware.AuthMiddleware(authRedis)
		var trainersAdminOnly api.MiddlewareFunc
		var superAdminOnly api.MiddlewareFunc
		if q != nil {
			trainersAdminOnly = middleware.TrainersAdminOnly(q)
			superAdminOnly = middleware.SuperAdminOnly(q)
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
					if trainersAdminOnly != nil {
						trainersAdminOnly(c)
						if c.IsAborted() {
							return
						}
					}
					if superAdminOnly != nil {
						superAdminOnly(c)
					}
				},
			},
		})
	}

	return r
}

// buildMailer picks a mailer in this order:
//  1. Resend, if both RESEND_API_KEY and RESEND_FROM are set (works in any env).
//  2. SMTP, if SMTP_HOST is set.
//  3. LogMailer — silent in development (expected), warns in any other env.
//
// Setting a real mailer explicitly takes precedence over the development
// default, so devs can opt into live email delivery for end-to-end testing.
func (s *Router) buildMailer() email.Mailer {
	if s.cfg.ResendAPIKey != "" && s.cfg.ResendFrom != "" {
		s.log.Info("using Resend mailer", "from", s.cfg.ResendFrom)
		return email.NewResendMailer(s.cfg.ResendAPIKey, s.cfg.ResendFrom)
	}
	if s.cfg.SMTPHost != "" {
		s.log.Info("using SMTP mailer", "host", s.cfg.SMTPHost, "from", s.cfg.SMTPFrom)
		return email.NewSMTPMailer(
			s.cfg.SMTPHost,
			s.cfg.SMTPPort,
			s.cfg.SMTPUser,
			s.cfg.SMTPPassword,
			s.cfg.SMTPFrom,
		)
	}
	if s.cfg.Env != "development" {
		s.log.Warn("no mailer configured (Resend or SMTP) — falling back to log mailer; verification emails will NOT be delivered")
	}
	return email.NewLogMailer()
}
