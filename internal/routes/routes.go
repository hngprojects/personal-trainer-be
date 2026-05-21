package routes

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/admin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/booking_session"
	"github.com/hngprojects/personal-trainer-be/internal/bookings"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/contact"
	"github.com/hngprojects/personal-trainer-be/internal/dev"
	"github.com/hngprojects/personal-trainer-be/internal/discovery"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/health"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/observability"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	reviewsvc "github.com/hngprojects/personal-trainer-be/internal/reviews"
	"github.com/hngprojects/personal-trainer-be/internal/root"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
	"github.com/hngprojects/personal-trainer-be/pkg/meeting"
	"github.com/hngprojects/personal-trainer-be/pkg/ratelimit"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
	"github.com/hngprojects/personal-trainer-be/pkg/video"
	appzoom "github.com/hngprojects/personal-trainer-be/pkg/zoom"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
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

	// avatarUploader is the background worker pool for profile-picture
	// uploads. nil if MinIO env vars weren't supplied — handlers return 503.
	// Held here (not just on routerImpl) so Close() can drain it on shutdown.
	avatarUploader *uploads.AvatarUploader

	// videoUploader is the background worker pool for trainer-intro-video
	// uploads. nil if MinIO env vars or ffmpeg are missing — handler 503s.
	// Held here so Close() can drain it on shutdown.
	videoUploader *uploads.VideoUploader

	// trainerImageUploader is the background worker pool for trainer
	// gallery images (up to 5 per trainer). Same nil-if-misconfigured
	// behaviour as the others.
	trainerImageUploader *uploads.TrainerImageUploader

	// trainerDisplayPictureUploader handles the optional display picture
	// uploaded as part of POST /trainers. Distinct from avatarUploader
	// (which writes users.avatar_url) so a trainer's client-facing display
	// picture can differ from their personal user avatar.
	trainerDisplayPictureUploader *uploads.TrainerDisplayPictureUploader
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

// Close performs cleanup for the router. The Redis client itself is owned by
// cmd/server/main.go, but background workers we constructed during Routes()
// are ours to drain.
func (s *Router) Close() {
	if s.avatarUploader != nil {
		// Closes the job channel, waits for in-flight uploads to land.
		// Bounded by per-attempt timeout × max attempts × workers — at most
		// a few minutes worst case.
		s.avatarUploader.Stop()
	}
	if s.videoUploader != nil {
		// Same drain semantics. Video transcodes can take minutes per job
		// so worst-case shutdown is correspondingly longer.
		s.videoUploader.Stop()
	}
	if s.trainerImageUploader != nil {
		s.trainerImageUploader.Stop()
	}
	if s.trainerDisplayPictureUploader != nil {
		s.trainerDisplayPictureUploader.Stop()
	}
}

type routerImpl struct {
	cfg                           *config.Config // exposed to handlers that need env-sourced values (e.g. MinIO public URL prefix)
	google                        *auth.GoogleHandler
	googleMobile                  *auth.MobileGoogleHandler
	local                         *auth.LocalHandler
	root                          *root.RootHandler
	adminLogin                    *handlers.AdminLoginHandler
	health                        *health.HealthHandler
	waitlist                      *waitlist.WaitlistHandler
	logout                        *auth.LogoutHandler
	refresh                       *auth.RefreshHandler
	passwordReset                 *auth.PasswordResetHandler
	accountSetup                  *auth.AccountSetupHandler
	trainers                      *trainersStore
	users                         *usersStore
	reviews                       *reviewsvc.Service
	admin                         *admin.Handler
	contact                       *contact.Handler
	bookings                      *bookingsStore
	paidReschedule                *bookings.Handler
	discovery                     *discovery.Handler
	availability                  *availabilityStore
	dev                           *dev.Handler
	booking                       bookings.BookingHandler
	bookingSlot                   bookings.BookingSlotHandler
	bookingSession                booking_session.SessionHandler
	uploader                      *uploads.AvatarUploader                // nil if MinIO env vars are missing → upload endpoint 503s
	videoUploader                 *uploads.VideoUploader                 // nil if MinIO env vars or ffmpeg are missing → upload endpoint 503s
	videoTranscoder               video.Transcoder                       // nil if ffmpeg is missing → upload endpoint 503s
	trainerImageUploader          *uploads.TrainerImageUploader          // nil if MinIO env vars are missing → upload endpoint 503s
	trainerDisplayPictureUploader *uploads.TrainerDisplayPictureUploader // nil if MinIO env vars are missing → POST /trainers with picture returns 503

	// log + mailer are shared dependencies that a handful of handlers
	// (notably POST /trainers for the credentials email) need direct access
	// to. Already constructed at Router level — passed through here so the
	// per-request handler methods don't need to dig back into the Router.
	logger *slog.Logger
	mailer email.Mailer
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
		otelgin.Middleware(s.cfg.ServiceName),
		middleware.CORS(s.cfg.FrontendURL),
		middleware.SecurityHeaders(),
		middleware.Logger(s.log),
		middleware.Recover(s.log),
	)

	metrics := observability.NewMetrics(s.cfg.ServiceName)
	r.Use(metrics.Middleware())
	r.GET("/metrics", metrics.Handler())

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
			cfg:    s.cfg,
			root:   root.NewRootHandler(s.log),
			health: health.NewHealthHandler(s.log),
			logger: s.log,
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

			bookingRepo := bookings.NewPostgresRepo(q)

			bookingSessionRepo := booking_session.NewPostgresBookingSessionRepo(q)
			bookingSessionService := booking_session.NewSessionService(bookingSessionRepo, s.log)
			mailer := s.buildMailer()
			impl.mailer = mailer

			impl.adminLogin = handlers.NewAdminLogin(adminLoginService, s.log)
			impl.google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)
			impl.googleMobile = auth.NewMobileGoogleHandler(s.cfg, usersRepo, sessionsRepo, s.log)
			impl.waitlist = waitlist.NewWaitlistHandler(waitlistRepo, s.log, mailer)
			impl.contact = contact.NewHandler(q, s.log, mailer)
			impl.trainers = newTrainersStore(s.db, q)
			impl.users = newUsersStore(q)
			var redisVal appredis.Client
			if s.redis != nil {
				redisVal = *s.redis
			}
			impl.availability = &availabilityStore{db: s.db, q: q, redis: redisVal}

			var meetingProvider meeting.Provider = meeting.NoOp{}
			if s.cfg.ZoomAccountID != "" {
				meetingProvider = appzoom.New(s.cfg.ZoomAccountID, s.cfg.ZoomClientID, s.cfg.ZoomClientSecret)
			}
			bookingSlotService := bookings.NewBookingSlotService(bookingRepo, s.log)
			bookingService := bookings.NewBookingService(bookingRepo, meetingProvider, mailer, s.log)
			discoveryRepo := discovery.NewPostgresRepo(q)
			impl.discovery = discovery.NewHandler(discoveryRepo, meetingProvider, mailer, s.cfg.NotificationEmail, s.log)
			bookingsRepo := bookings.NewPostgresRepo(q)
			impl.bookingSlot = bookings.NewBookingSlotHandler(bookingSlotService, redisVal, s.log)
			impl.booking = bookings.NewBookingHandler(bookingService, s.log)
			impl.paidReschedule = bookings.NewHandler(bookingsRepo, meetingProvider, mailer, s.log)
			impl.reviews = reviewsvc.NewService(s.db, q, s.log)
			impl.bookings = &bookingsStore{db: s.db, q: q}
			impl.bookingSession = booking_session.NewSessionHandler(bookingSessionService, *s.redis, s.log)

			// Avatar upload pipeline. Storage is built lazily — missing env
			// vars just leave impl.uploader nil and the handler returns 503,
			// rather than failing the whole server boot.
			//
			// MINIO_PUBLIC_BASE_URL is part of the required set: without it
			// the handler would still 202 but return a useless relative URI
			// (e.g. "/avatars/<uuid>/...") as the avatar_url. Better to refuse
			// to start the pipeline than to ship broken URLs to clients.
			switch {
			case s.cfg.MinioEndpoint == "":
				s.log.Warn("MINIO_ENDPOINT not set — avatar upload endpoint will return 503")
			case s.cfg.MinioPublicBaseURL == "":
				s.log.Warn("MINIO_PUBLIC_BASE_URL not set — avatar upload endpoint will return 503 to avoid handing clients relative URIs")
			default:
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				store, err := storage.NewMinioStorage(ctx, s.cfg.MinioEndpoint, s.cfg.MinioAccessKey, s.cfg.MinioSecretKey, s.cfg.MinioBucket, s.cfg.MinioUseSSL)
				cancel()
				if err != nil {
					s.log.Error("minio init failed — avatar upload endpoint will return 503", "err", err)
				} else {
					uploader := uploads.NewAvatarUploader(store, q, s.log, 100)
					uploader.Start(4)
					s.avatarUploader = uploader // stored on Router so Close() can drain
					impl.uploader = uploader
					s.log.Info("avatar upload pipeline started", "workers", 4, "queue", 100, "bucket", s.cfg.MinioBucket)

					// Video pipeline reuses the same MinIO client (different
					// path prefix inside the bucket). Requires ffmpeg on the
					// host; if missing, the video endpoint 503s but the rest
					// of the app keeps running.
					if transcoder, terr := video.NewFFmpegTranscoder(); terr != nil {
						s.log.Warn("ffmpeg not available - trainer-intro-video upload endpoint will return 503", "err", terr)
					} else {
						vUploader := uploads.NewVideoUploader(store, transcoder, q, s.log, 20)
						vUploader.Start(2)
						s.videoUploader = vUploader
						impl.videoUploader = vUploader
						impl.videoTranscoder = transcoder
						s.log.Info("video upload pipeline started", "workers", 2, "queue", 20, "bucket", s.cfg.MinioBucket)
					}

					// Trainer image gallery pipeline — same in-memory shape
					// as the avatar uploader (images are small enough to
					// hold in channel without disk overflow).
					tiUploader := uploads.NewTrainerImageUploader(store, q, s.log, 100)
					tiUploader.Start(4)
					s.trainerImageUploader = tiUploader
					impl.trainerImageUploader = tiUploader
					s.log.Info("trainer image upload pipeline started", "workers", 4, "queue", 100, "bucket", s.cfg.MinioBucket)

					// Trainer display-picture pipeline. One file per
					// trainer profile, capped at 5 MiB by the handler, so
					// a small queue is fine; this is light traffic compared
					// to the avatar endpoint.
					tdpUploader := uploads.NewTrainerDisplayPictureUploader(store, q, s.log, 50)
					tdpUploader.Start(2)
					s.trainerDisplayPictureUploader = tdpUploader
					impl.trainerDisplayPictureUploader = tdpUploader
					s.log.Info("trainer display-picture upload pipeline started", "workers", 2, "queue", 50, "bucket", s.cfg.MinioBucket)
				}
			}

			// Rate limiters are Redis-backed. When Redis is unavailable we wire
			// in AllowAllLimiter (always-allow) so the auth endpoints stay up
			// instead of returning 503 across the board. Real Redis-Allow errors
			// at request time already fail open; this matches that behaviour for
			// the "no backend at all" startup case.
			var (
				verifyLimiter      ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				registerLimiter    ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				forgotLimiter      ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				forgotIPLimiter    ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				resetLimiter       ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				resetIPLimiter     ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				refreshLimiter     ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
				setPasswordIPLimit ratelimit.RateLimiter = ratelimit.AllowAllLimiter{}
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
				setPasswordIPLimit = ratelimit.New(rawRedis, "rl:auth:set-password:ip", 20, 15*time.Minute)
			} else {
				s.log.Warn("redis is not configured — auth rate limits disabled (using no-op limiters)")
			}

			impl.local = auth.NewLocalHandler(usersRepo, sessionsRepo, codesRepo, localAuthRepo, mailer, s.log, s.cfg.OTPSecret, verifyLimiter, registerLimiter)
			impl.passwordReset = auth.NewPasswordResetHandler(usersRepo, rolesRepo, passwordResetRepo, mailer, s.log, s.cfg.OTPSecret, forgotLimiter, forgotIPLimiter, resetLimiter, resetIPLimiter)
			impl.refresh = auth.NewRefreshHandler(s.redis, s.log, refreshLimiter)
			impl.admin = admin.NewHandler(usersRepo.(auth.AdminUserRepository), mailer, s.log)
			impl.accountSetup = auth.NewAccountSetupHandler(
				auth.NewPostgresAccountSetupRepo(s.db),
				mailer,
				s.log,
				s.cfg.OTPSecret,
				s.cfg.FrontendURL,
				s.cfg.TrainerSetupTokenExpiryHours,
				setPasswordIPLimit,
			)
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
		authMw := middleware.AuthMiddleware(authRedis, s.log)
		refreshMw := middleware.AuthMiddlewareWithType(authRedis, "refresh", s.log)
		var trainersAdminOnly api.MiddlewareFunc
		var superAdminOnly api.MiddlewareFunc
		if q != nil {
			trainersAdminOnly = middleware.TrainersAdminOnly(q, s.log)
			superAdminOnly = middleware.SuperAdminOnly(q, s.log)
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
					if _, requiresRefreshAuth := c.Get(string(api.RefreshAuthScopes)); requiresRefreshAuth {
						refreshMw(c)
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
