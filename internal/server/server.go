package server

import (
	"log/slog"
	"net/http"

	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/middleware"
	"github.com/hngprojects/personal-trainer-be/internal/repository"
)

type Server struct {
	cfg   *config.Config
	log   *slog.Logger
	store *repository.Store
}

func New(cfg *config.Config, log *slog.Logger, store *repository.Store) *Server {
	return &Server{
		cfg:   cfg,
		log:   log,
		store: store,
	}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	health := handlers.NewHealthHandler()
	mux.HandleFunc("GET /health", health.Check)
	mux.HandleFunc("GET /{$}", health.Root)

	auth := handlers.NewAuthHandler(
		s.cfg.GoogleClientID,
		s.cfg.GoogleClientSecret,
		s.cfg.GoogleRedirectURL,
		s.store,
	)
	mux.HandleFunc("GET /auth/google", auth.GoogleLogin)
	mux.HandleFunc("GET /auth/google/callback", auth.GoogleCallback)

	chain := middleware.Chain(
		middleware.Recover(s.log),
		middleware.Logger(s.log),
	)
	return chain(mux)
}
