// routes.go
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
	"github.com/hngprojects/personal-trainer-be/internal/waitlist"
	appredis "github.com/hngprojects/personal-trainer-be/pkg/redis"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Router struct {
	cfg   *config.Config
	log   *slog.Logger
	db    *sql.DB
	redis *appredis.Client
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB, redis *appredis.Client) *Router {
	return &Router{cfg: cfg, log: log, db: db, redis: redis}
}

type routerImpl struct {
<<<<<<< HEAD
	google   *auth.GoogleHandler
	root     *root.RootHandler
	health   *health.HealthHandler
	waitlist *waitlist.WaitlistHandler
=======
	google *auth.GoogleHandler
	root   *root.RootHandler
	health *health.HealthHandler
	trainers *trainersStore
>>>>>>> 7c0dede (Refactored Trainer Management logic based on PR comments)
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
<<<<<<< HEAD
		var google *auth.GoogleHandler
		var waitlistHandler *waitlist.WaitlistHandler

=======
		var (
			google   *auth.GoogleHandler
			trainers *trainersStore
			q *db.Queries
		)
>>>>>>> 7c0dede (Refactored Trainer Management logic based on PR comments)
		if s.db != nil {
			q = db.New(s.db)

			usersRepo := auth.NewPostgresUserRepo(q)
			google = auth.NewGoogleHandler(s.cfg, usersRepo, s.log)
<<<<<<< HEAD

			waitlistRepo := waitlist.NewPostgresWaitlistRepo(db.New(s.db))
			waitlistHandler = waitlist.NewWaitlistHandler(waitlistRepo, s.log)
		} else {
			s.log.Warn("database not configured — auth and waitlist endpoints unavailable")
		}

		impl := &routerImpl{
			google:   google,
			root:     root.NewRootHandler(s.log),
			health:   health.NewHealthHandler(s.log),
			waitlist: waitlistHandler,
=======
			trainers = newTrainersStore(q)
		} else {
			s.log.Warn("database not configured — auth/trainers endpoints unavailable")
		}

		impl := &routerImpl{
			google: google,
			root:   root.NewRootHandler(s.log),
			health: health.NewHealthHandler(s.log),
			trainers: trainers,
>>>>>>> 7c0dede (Refactored Trainer Management logic based on PR comments)
		}
		opts := api.GinServerOptions{}
		if q != nil {
			adminOnly := middleware.TrainersAdminOnly(q)

			opts.Middlewares = []api.MiddlewareFunc{
				func(c *gin.Context) { adminOnly(c) }, 
			}
		}

		api.RegisterHandlersWithOptions(v1, impl, opts)
	}

	return r
}
