package main

import (
	"bufio"
	"flag"
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
		slog.Error("token script can only run in development and staging")
		os.Exit(1)
	}

	userIDFlag := flag.String("user-id", "", "user ID to generate token for")
	tokenTypeFlag := flag.String("type", "access", "token type(refresh or access)")

	flag.Parse()

	userID := *userIDFlag

	if userID == "" {
		reader := bufio.NewReader(os.Stdin)

		fmt.Print("Enter user Id: ")

		input, _ := reader.ReadString('\n')
		userID = strings.TrimSpace(input)
	}

	if userID == "" {
		userID = uuid.NewString()
	}

	tokenType := auth.TokenType(*tokenTypeFlag)

	switch tokenType {
	case auth.AccessToken, auth.RefreshToken:
		// valid
	default:
		slog.Error("invalid token type", "type", tokenType)
		os.Exit(1)
	}
	generatedToken, err := auth.GenerateJWTToken(userID, tokenType)
	if err != nil {
		slog.Error("failed to generate token", "err", err)
		return
	}

	fmt.Println("token:\n", generatedToken)
}
