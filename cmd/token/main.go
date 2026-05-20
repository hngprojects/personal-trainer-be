package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	if cfg.Env == "production" {
		slog.Error("seed script can only run in development and staging")
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter user Id: ")
	userId, _ := reader.ReadString('\n')
	userId = strings.TrimSpace(userId)

	if userId == "" {
		userId = uuid.NewString()
	}

	generatedToken, err := auth.GenerateJWTToken(userId, "access")
	if err != nil {
		slog.Error("failed to generate token", "err", err)
		return
	}

	fmt.Println("access token: \n", generatedToken)
}
