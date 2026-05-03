package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/service"
)

type Server struct {
	cfg *config.Config
	log *slog.Logger
	db  *sql.DB
}

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
		middleware.Logger(s.log),
		middleware.Recover(s.log),
	)

	health := handlers.NewHealthHandler()
	r.GET("/", health.Root)
	r.GET("/health", health.Check)

	queries := db.New(s.db)
	authService := service.NewAuthService(queries)
	authHandler := handlers.NewLocalAuthHandler(authService)

	auth := r.Group("/api/v1/auth")
	auth.Use(middleware.Auth(s.db))
	{
		auth.POST("/logout", authHandler.Logout)
		auth.PUT("/change-password", authHandler.ChangePassword)
	}

	return r
}