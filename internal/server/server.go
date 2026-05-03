package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/db"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/service"
)

type Server struct {
	cfg *config.Config
	log *slog.Logger
	db  *sql.DB
}

<<<<<<< HEAD
func New(cfg *config.Config, log *slog.Logger, db *sql.DB) *Server {
	return &Server{cfg: cfg, log: log, db: db}
}

func (s *Server) Routes() http.Handler {
	if s.cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(
=======
func New(cfg *config.Config, log *slog.Logger, sqlDB *sql.DB) *Server {
	return &Server{cfg: cfg, log: log, db: sqlDB}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	// public routes
	health := handlers.NewHealthHandler()
	mux.HandleFunc("GET /health", health.Check)
	mux.HandleFunc("GET /{$}", health.Root)

	// auth routes — protected
	queries := db.New(s.db)
	authService := service.NewAuthService(queries)
	authHandler := handlers.NewLocalAuthHandler(authService)

	authMiddleware := middleware.Auth(s.db)

	mux.Handle("POST /api/v1/auth/logout",
		authMiddleware(http.HandlerFunc(authHandler.Logout)),
	)
	mux.Handle("PUT /api/v1/auth/change-password",
		authMiddleware(http.HandlerFunc(authHandler.ChangePassword)),
	)

	chain := middleware.Chain(
		middleware.Recover(s.log),
>>>>>>> feat: wire db connection and auth routes in server and main
		middleware.Logger(s.log),
		middleware.Recover(s.log),
	)
<<<<<<< HEAD

	health := handlers.NewHealthHandler()
	r.GET("/", health.Root)
	r.GET("/health", health.Check)

	return r
}
=======
	return chain(mux)
}
>>>>>>> feat: wire db connection and auth routes in server and main
