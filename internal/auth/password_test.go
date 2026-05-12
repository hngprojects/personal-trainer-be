package auth_test

import (
	"strings"
	"testing"

	"github.com/hngprojects/personal-trainer-be/internal/auth"
)

func TestHashAndCheckPassword_RoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("Test1234!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if hash == "Test1234!" {
		t.Fatal("hash equals plaintext")
	}
	if err := auth.CheckPassword(hash, "Test1234!"); err != nil {
		t.Fatalf("check valid password: %v", err)
	}
	if err := auth.CheckPassword(hash, "wrong"); err == nil {
		t.Fatal("check accepted wrong password")
	}
}

func TestHashPassword_DifferentSalts(t *testing.T) {
	a, err := auth.HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.HashPassword("same")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two hashes of same password are identical — bcrypt salt missing")
	}
}

func TestGenerateRandomPassword_LengthFloor(t *testing.T) {
	pwd, err := auth.GenerateRandomPassword(4)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(pwd) < 12 {
		t.Errorf("expected length >= 12 for short request, got %d", len(pwd))
	}
}

func TestGenerateRandomPassword_RequestedLength(t *testing.T) {
	pwd, err := auth.GenerateRandomPassword(20)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(pwd) != 20 {
		t.Errorf("expected length 20, got %d", len(pwd))
	}
}

func TestGenerateRandomPassword_Distinct(t *testing.T) {
	seen := make(map[string]struct{})
	for i := 0; i < 200; i++ {
		p, err := auth.GenerateRandomPassword(16)
		if err != nil {
			t.Fatal(err)
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate password generated after %d iterations: %q", i, p)
		}
		seen[p] = struct{}{}
	}
}

func TestGenerateRandomPassword_NoConfusables(t *testing.T) {
	const confusables = "0OIl1"
	for i := 0; i < 100; i++ {
		p, err := auth.GenerateRandomPassword(32)
		if err != nil {
			t.Fatal(err)
		}
		if strings.ContainsAny(p, confusables) {
			t.Fatalf("password contains confusable char: %q", p)
		}
	}
}
