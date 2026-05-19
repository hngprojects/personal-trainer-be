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

	if cfg.Env == "production" {
		slog.Error("seed script can only run in development and staging")
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
		trainers   []seedTrainer
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
		trainers: []seedTrainer{
			{
				name:            "Sarah Connor",
				email:           "trainer1@example.com",
				bio:             "Certified personal trainer with 8 years of experience in strength training",
				specializations: []string{"strength", "weight loss"},
				yearsExperience: 8,
			},
			{
				name:            "Mike Johnson",
				email:           "trainer2@example.com",
				bio:             "Yoga instructor and wellness coach specializing in flexibility and mindfulness",
				specializations: []string{"yoga", "flexibility"},
				yearsExperience: 5,
			},
			{
				name:            "Emma Davis",
				email:           "trainer3@example.com",
				bio:             "HIIT and cardio specialist with certification in sports nutrition",
				specializations: []string{"cardio", "HIIT", "nutrition"},
				yearsExperience: 6,
			},
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

	for _, t := range seedData.trainers {
		trainerUser, err := queries.CreateUser(ctx, dbpkg.CreateUserParams{
			Email:        t.email,
			Name:         t.name,
			AuthProvider: "local",
		})
		if err != nil {
			slog.Error("failed to create trainer user", "email", t.email, "error", err)
			continue
		}

		specializations := strings.Join(t.specializations, ", ")
		trainer, err := queries.CreateTrainer(ctx, dbpkg.CreateTrainerParams{
			UserID:            trainerUser.ID,
			Specialization:    sql.NullString{String: specializations, Valid: true},
			Bio:               sql.NullString{String: t.bio, Valid: true},
			YearsOfExperience: sql.NullInt32{Int32: int32(t.yearsExperience), Valid: true},
			IntroVideoUrl:     sql.NullString{Valid: false},
			DisplayPicture:    sql.NullString{Valid: false},
			CalendlyConnected: false,
			CalendlyLink:      sql.NullString{Valid: false},
			OnboardingStatus:  "approved",
		})
		if err != nil {
			slog.Error("failed to create trainer record", "name", t.name, "error", err)
			continue
		}

		fmt.Printf("✓ Created trainer: %s - specializations: %s\n", trainer.ID, specializations)
	}

	fmt.Println("\n✓ Database seeding completed successfully!")
}

type seedUser struct {
	email    string
	name     string
	provider string
}

type seedTrainer struct {
	email           string
	name            string
	bio             string
	specializations []string
	yearsExperience int
}
