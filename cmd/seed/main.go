// Command seed populates a local development database with a baseline
// admin account and a handful of client users. It is intentionally NOT
// part of any deploy artifact and refuses to run anywhere except the
// development environment — see the env check below.
//
// Trainer accounts are deliberately NOT seeded by this script. Trainers
// are provisioned end-to-end by an admin via POST /trainers (which
// generates a password, sends the credentials email, and writes the
// specializations / training styles / benefits with full schema-level
// validation). Duplicating that flow here would mean keeping the seed
// script in lockstep with every future change to the trainer schema —
// it's safer to have one source of truth.
//
// Usage (local dev only):
//
//	go run ./cmd/seed
//	# enter the desired admin password at the prompt
package main

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	dbpkg "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

func main() {
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Strict: development only. Staging and production must never run this
	// script — any seeded data on those environments has to go through the
	// real admin endpoints so it's auditable. Without this guard, a
	// misconfigured systemd unit (or a copy-pasted scp from a dev box) can
	// leave the seed binary in a restart loop trying to insert fixtures
	// against the wrong database.
	if cfg.Env != "development" {
		slog.Error("seed script can only run in the development environment",
			"got_env", cfg.Env,
			"hint", "set APP_ENV=development if you really mean to run this against your local DB",
		)
		os.Exit(1)
	}

	auth.Configure(cfg.JwtSecret)

	database, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}

	defer func() {
		if err := database.Close(); err != nil {
			slog.Error("failed to close database", "error", err)
			os.Exit(1)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := database.PingContext(ctx); err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter admin password: ")
	adminPassword, _ := reader.ReadString('\n')
	adminPassword = strings.TrimSpace(adminPassword)

	if adminPassword == "" {
		slog.Error("admin password cannot be empty")
		os.Exit(1)
	}

	hashedPassword, err := auth.HashPassword(adminPassword)
	if err != nil {
		slog.Error("failed to hash password", "error", err)
		os.Exit(1)
	}

	queries := dbpkg.New(database)

	seedData := struct {
		adminEmail string
		adminName  string
		users      []seedUser
	}{
		adminEmail: "admin@trainer.com",
		adminName:  "Admin User",
		users: []seedUser{
			{email: "client1@example.com", name: "John Doe", provider: "local"},
			{email: "client2@example.com", name: "Jane Smith", provider: "local"},
			{email: "client3@example.com", name: "Bob Johnson", provider: "google"},
			{email: "client4@example.com", name: "Alice Brown", provider: "google"},
			{email: "client5@example.com", name: "Charlie Wilson", provider: "local"},
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminUser, err := queries.UpsertAdminUser(ctx, dbpkg.UpsertAdminUserParams{
		Email:    seedData.adminEmail,
		Name:     seedData.adminName,
		Password: sql.NullString{String: hashedPassword, Valid: true},
	})
	if err != nil {
		slog.Error("failed to create admin user", "error", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Created admin user: %s (%s)\n", adminUser.Name, adminUser.Email)

	for _, u := range seedData.users {
		user, err := queries.CreateUser(ctx, dbpkg.CreateUserParams{
			Email:        u.email,
			Name:         u.name,
			AuthProvider: u.provider,
		})
		if err != nil {
			slog.Error("failed to create user", "email", u.email, "error", err)
			continue
		}

		fmt.Printf("✓ Created user: %s (%s)\n", user.Name, user.Email)
	}

	fmt.Println("\n✓ Database seeding completed successfully!")
	fmt.Println("  Trainer accounts are NOT seeded here — provision them via POST /trainers as an admin.")
}

type seedUser struct {
	email    string
	name     string
	provider string
}
