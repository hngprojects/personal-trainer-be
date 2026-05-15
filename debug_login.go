package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"golang.org/x/crypto/bcrypt"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config load failed: %v", err)
	}

	dbConn, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db open failed: %v", err)
	}
	defer dbConn.Close()

	if err := dbConn.Ping(); err != nil {
		log.Fatalf("db ping failed: %v", err)
	}

	q := db.New(dbConn)

	// Step 1: Find user by email
	user, err := q.GetUserByEmail(context.Background(), "boss@x.com")
	if err != nil {
		fmt.Printf("❌ Step 1 FAILED: Could not find user: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Step 1 PASSED: Found user %s\n", user.Email)

	// Step 2: Check password is set
	if !user.Password.Valid {
		fmt.Println("❌ Step 2 FAILED: Password is NULL")
		os.Exit(1)
	}
	fmt.Printf("✓ Step 2 PASSED: Password is set (length: %d)\n", len(user.Password.String))

	// Step 3: Compare password
	password := "admin123"
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password.String), []byte(password)); err != nil {
		fmt.Printf("❌ Step 3 FAILED: Password mismatch: %v\n", err)
		fmt.Printf("   Hash from DB: %s\n", user.Password.String)
		fmt.Printf("   Password to check: %s\n", password)
		os.Exit(1)
	}
	fmt.Println("✓ Step 3 PASSED: Password matches")

	// Step 4: Check role
	roleRepo := db.New(dbConn)
	roles, err := roleRepo.GetUserRoles(context.Background(), user.ID)
	if err != nil {
		fmt.Printf("❌ Step 4 FAILED: Could not get roles: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Step 4 PASSED: Found %d role(s)\n", len(roles))
	for _, role := range roles {
		fmt.Printf("   - Role: %s\n", role.Name)
	}

	fmt.Println("\n✓ ALL CHECKS PASSED - Login should work!")
}
