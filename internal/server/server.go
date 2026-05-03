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
	"github.com/hngprojects/personal-trainer-be/internal/repository"
	"github.com/hngprojects/personal-trainer-be/internal/service"
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

	if s.db != nil {
		queries := db.New(s.db)
		mailer := s.buildMailer()

		userRepo := repository.NewUserRepository(queries)
		sessionRepo := repository.NewSessionRepository(queries)
		codeRepo := repository.NewVerificationCodeRepository(queries)
		authSvc := service.NewAuthService(userRepo, sessionRepo, codeRepo, mailer)
		auth := handlers.NewAuthHandler(authSvc, s.cfg)

		r.POST("/auth/register", auth.InitiateSignUp)
		r.POST("/auth/register/verify", auth.VerifyCode)
		r.POST("/auth/register/complete", auth.CompleteSignUp)
		r.POST("/auth/login", auth.SignIn)
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
