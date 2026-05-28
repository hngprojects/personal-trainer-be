// Command seed populates a local development database with a baseline
// admin account, a handful of client users, and trainer accounts covering
// every onboarding status and every specialization.
//
// Unlike production, where trainers are provisioned end-to-end via
// POST /trainers (password generation + credentials email), the seed
// script creates them directly so you have a ready-to-use fixture set
// without standing up a mail server. All seeded trainer passwords are
// set to the same admin password you enter at the prompt.
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

	clients := []seedUser{
		{email: "client1@example.com", name: "John Doe", provider: "local"},
		{email: "client2@example.com", name: "Jane Smith", provider: "local"},
		{email: "client3@example.com", name: "Bob Johnson", provider: "google"},
		{email: "client4@example.com", name: "Alice Brown", provider: "google"},
		{email: "client5@example.com", name: "Charlie Wilson", provider: "local"},
	}

	trainers := []seedTrainer{
		{
			specializations:  []string{"yoga"},
			trainingStyles:   []string{"hatha", "vinyasa"},
			bio:              "Certified yoga instructor with a focus on mindful movement and breath control.",
			yearsOfExp:       3,
			gender:           "female",
			phoneNumber:      "+2348011110001",
			onboardingStatus: "pending",
		},
		{
			specializations:  []string{"cardio"},
			trainingStyles:   []string{"HIIT", "circuit training"},
			bio:              "High-energy cardio coach passionate about helping clients beat personal records.",
			yearsOfExp:       5,
			gender:           "male",
			phoneNumber:      "+2348011110002",
			onboardingStatus: "pending",
		},
		{
			specializations:  []string{"strength"},
			trainingStyles:   []string{"powerlifting", "functional strength"},
			bio:              "Strength coach specializing in progressive overload and injury-free lifting.",
			yearsOfExp:       4,
			gender:           "female",
			phoneNumber:      "+2348011110003",
			onboardingStatus: "pending",
		},

		{
			specializations:  []string{"speed"},
			trainingStyles:   []string{"sprint drills", "plyometrics"},
			bio:              "Former sprinter turned coach. Helps athletes shave seconds off their splits.",
			yearsOfExp:       7,
			gender:           "male",
			phoneNumber:      "+2348011110004",
			onboardingStatus: "approved",
		},
		{
			specializations:  []string{"yoga", "strength"},
			trainingStyles:   []string{"power yoga", "body-weight strength"},
			bio:              "Blends yoga mobility work with functional strength training for balanced athletes.",
			yearsOfExp:       6,
			gender:           "female",
			phoneNumber:      "+2348011110005",
			onboardingStatus: "approved",
		},
		{
			specializations:  []string{"cardio", "speed"},
			trainingStyles:   []string{"interval running", "agility ladders"},
			bio:              "Endurance and speed specialist. Coaches everyone from 5K beginners to competitive runners.",
			yearsOfExp:       9,
			gender:           "male",
			phoneNumber:      "+2348011110006",
			onboardingStatus: "approved",
		},

		{
			specializations:  []string{"strength"},
			trainingStyles:   []string{"bodybuilding"},
			bio:              "Bodybuilding enthusiast looking to transition into coaching.",
			yearsOfExp:       1,
			gender:           "male",
			phoneNumber:      "+2348011110007",
			onboardingStatus: "rejected",
		},
		{
			specializations:  []string{"cardio"},
			trainingStyles:   []string{"aerobics"},
			bio:              "Group aerobics instructor applying for one-on-one coaching certification.",
			yearsOfExp:       2,
			gender:           "female",
			phoneNumber:      "+2348011110008",
			onboardingStatus: "rejected",
		},
		{
			specializations:  []string{"yoga"},
			trainingStyles:   []string{"restorative yoga"},
			bio:              "Yoga practitioner with weekend certification seeking full trainer status.",
			yearsOfExp:       1,
			gender:           "male",
			phoneNumber:      "+2348011110009",
			onboardingStatus: "rejected",
		},

		{
			specializations:  []string{"speed"},
			trainingStyles:   []string{"track and field", "speed endurance"},
			bio:              "Track athlete and coach — account under review.",
			yearsOfExp:       5,
			gender:           "female",
			phoneNumber:      "+2348011110010",
			onboardingStatus: "suspended",
		},
		{
			specializations:  []string{"strength", "cardio"},
			trainingStyles:   []string{"CrossFit", "metabolic conditioning"},
			bio:              "CrossFit coach and competitive lifter — account under review.",
			yearsOfExp:       8,
			gender:           "male",
			phoneNumber:      "+2348011110011",
			onboardingStatus: "suspended",
		},
		{
			specializations:  []string{"yoga", "cardio"},
			trainingStyles:   []string{"yoga flow", "dance cardio"},
			bio:              "Movement-based fitness coach — account under review.",
			yearsOfExp:       4,
			gender:           "female",
			phoneNumber:      "+2348011110012",
			onboardingStatus: "suspended",
		},
	}

	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Admin
	adminUser, err := queries.UpsertAdminUser(ctx, dbpkg.UpsertAdminUserParams{
		Email:    "admin@trainer.com",
		Name:     "Admin User",
		Password: sql.NullString{String: hashedPassword, Valid: true},
	})
	if err != nil {
		slog.Error("failed to create admin user", "error", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Admin:   %s (%s)\n", adminUser.Name, adminUser.Email)

	// Clients
	fmt.Println()
	for _, u := range clients {
		user, err := queries.CreateUser(ctx, dbpkg.CreateUserParams{
			Email:        u.email,
			Name:         u.name,
			AuthProvider: u.provider,
		})
		if err != nil {
			slog.Error("failed to create client", "email", u.email, "error", err)
			continue
		}
		fmt.Printf("✓ Client:  %s (%s)\n", user.Name, user.Email)
	}

	fmt.Println()
	for i, t := range trainers {
		index := i + 1
		email := fmt.Sprintf("trainer-%d@example.com", index)
		name := fmt.Sprintf("trainer-%d", index)

		trainerUser, err := queries.UpsertTrainerUser(ctx, dbpkg.UpsertTrainerUserParams{
			Email:       email,
			Name:        name,
			Password:    sql.NullString{String: hashedPassword, Valid: true},
			Gender:      t.gender,
			PhoneNumber: t.phoneNumber,
		})
		if err != nil {
			slog.Error("failed to upsert trainer user", "email", email, "error", err)
			continue
		}

		trainer, err := queries.CreateTrainer(ctx, dbpkg.CreateTrainerParams{
			UserID:            trainerUser.ID,
			Specializations:   t.specializations,
			TrainingStyles:    t.trainingStyles,
			Bio:               sql.NullString{String: t.bio, Valid: t.bio != ""},
			YearsOfExperience: sql.NullInt32{Int32: int32(t.yearsOfExp), Valid: true},
			DisplayPicture:    sql.NullString{},
			OnboardingStatus:  t.onboardingStatus,
		})
		if err != nil {
			slog.Error("failed to create trainer profile", "email", email, "error", err)
			continue
		}

		fmt.Printf("✓ Trainer: %-12s (%-9s)  specs: %s\n",
			trainerUser.Name,
			trainer.OnboardingStatus,
			strings.Join(trainer.Specializations, ", "),
		)
	}

	fmt.Println("\n✓ Database seeding completed successfully!")
}

type seedUser struct {
	email    string
	name     string
	provider string
}

type seedTrainer struct {
	specializations  []string
	trainingStyles   []string
	bio              string
	yearsOfExp       int
	gender           string
	phoneNumber      string
	onboardingStatus string
}
