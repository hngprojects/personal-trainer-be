package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

type Server struct {
	cfg *config.Config
	log *slog.Logger
	db  *sql.DB
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB) *Server {
	return &Server{cfg: cfg, log: log, db: db}
}

// serverImpl satisfies api.ServerInterface by delegating to domain handlers.
type serverImpl struct {
	google *auth.GoogleHandler
}

func (s *serverImpl) HandleGoogleLogin(c *gin.Context) {
	s.google.HandleGoogleLogin(c)
}

func (s *serverImpl) HandleGoogleCallback(c *gin.Context, params api.HandleGoogleCallbackParams) {
	s.google.HandleGoogleCallback(c, params.State, params.Code)
}

// HandleLocalAuth is not yet implemented — placeholder for local auth handler.
func (s *serverImpl) HandleLocalAuth(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
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
	userRepo := auth.NewPostgresUserRepo(queries)
	googleHandler := auth.NewGoogleHandler(s.cfg, userRepo, s.log)

	api.RegisterHandlersWithOptions(r, &serverImpl{google: googleHandler}, api.GinServerOptions{
		ErrorHandler: func(c *gin.Context, err error, statusCode int) {
			c.JSON(statusCode, gin.H{"error": err.Error()})
		},
	})

	return r
}

func (s *Server) buildMailer() email.Mailer {
	if s.cfg.SMTPHost == "" || s.cfg.Env == "development" {
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
