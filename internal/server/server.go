package server

import (
	"database/sql"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/pkg/email"

	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type Server struct {
	cfg *config.Config
	log *slog.Logger
	db  *sql.DB
}

func New(cfg *config.Config, log *slog.Logger, db *sql.DB) *Server {
	return &Server{cfg: cfg, log: log, db: db}
}

type serverImpl struct {
	google *auth.GoogleHandler
}

func (s *serverImpl) HandleGoogleLogin(c *gin.Context) {
	s.google.HandleGoogleLogin(c)
}

func (s *serverImpl) HandleGoogleCallback(c *gin.Context, params api.HandleGoogleCallbackParams) {
	s.google.HandleGoogleCallback(c, params.State, params.Code)
}

func (s *serverImpl) HandleLocalAuth(c *gin.Context) {
	c.JSON(501, api.NewError("not implemented", api.CodeServerError))
}

func (s *serverImpl) Root(c *gin.Context) {
	c.JSON(200, api.NewSuccess("Personal Trainer API is running", api.CodeOK, nil))
}

func (s *serverImpl) HealthCheck(c *gin.Context) {
	c.JSON(200, api.NewSuccess("Service is healthy", api.CodeOK, map[string]interface{}{
		"timestamp": "2026-05-07T10:00:00Z",
	}))
}

func (s *Server) Routes() *gin.Engine {
	if s.cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()
	r.Use(
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
		usersRepo := auth.NewPostgresUserRepo(db.New(s.db))
		impl := &serverImpl{
			google: auth.NewGoogleHandler(s.cfg, usersRepo, s.log),
		}
		api.RegisterHandlers(v1, impl)
	}

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