package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"regexp"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/hngprojects/personal-trainer-be/internal/models"
	"github.com/hngprojects/personal-trainer-be/internal/repository"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

var (
	ErrEmailAlreadyExists = errors.New("email already registered")
	ErrInvalidCode        = errors.New("invalid or expired verification code")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountNotActive   = errors.New("account not active")
	ErrWeakPassword       = errors.New("password must be at least 8 characters and contain at least one number")
)

var hasNumber = regexp.MustCompile(`[0-9]`)

type AuthService struct {
	users    *repository.UserRepository
	sessions *repository.SessionRepository
	codes    *repository.VerificationCodeRepository
	mailer   email.Mailer
}

func NewAuthService(
	users *repository.UserRepository,
	sessions *repository.SessionRepository,
	codes *repository.VerificationCodeRepository,
	mailer email.Mailer,
) *AuthService {
	return &AuthService{users: users, sessions: sessions, codes: codes, mailer: mailer}
}

func (s *AuthService) InitiateSignUp(ctx context.Context, emailAddr string) error {
	existing, err := s.users.FindByEmail(ctx, emailAddr)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existing != nil && existing.IsActive {
		return ErrEmailAlreadyExists
	}

	if existing == nil {
		user := &models.User{Email: emailAddr}
		if err := s.users.Create(ctx, user); err != nil {
			return err
		}
	}

	code := fmt.Sprintf("%06d", mrand.IntN(1000000))
	vc := &models.VerificationCode{
		Email:     emailAddr,
		Code:      code,
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	if err := s.codes.Save(ctx, vc); err != nil {
		return err
	}

	return s.mailer.Send(emailAddr, "Verify your email",
		fmt.Sprintf("Your verification code is: %s\n\nThis code expires in 10 minutes.", code),
	)
}

func (s *AuthService) VerifyCode(ctx context.Context, emailAddr, code string) error {
	vc, err := s.codes.FindByEmailAndCode(ctx, emailAddr, code)
	if err != nil || vc == nil {
		return ErrInvalidCode
	}
	if time.Now().After(vc.ExpiresAt) {
		return ErrInvalidCode
	}
	return nil
}

func (s *AuthService) CompleteSignUp(ctx context.Context, emailAddr, name, code, password string) (*models.Session, error) {
	if len(password) < 8 || !hasNumber.MatchString(password) {
		return nil, ErrWeakPassword
	}

	vc, err := s.codes.FindByEmailAndCode(ctx, emailAddr, code)
	if err != nil || vc == nil || time.Now().After(vc.ExpiresAt) {
		return nil, ErrInvalidCode
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return nil, err
	}

	user, err := s.users.FindByEmail(ctx, emailAddr)
	if err != nil {
		return nil, err
	}

	if err := s.users.UpdatePassword(ctx, user.ID, string(hashed)); err != nil {
		return nil, err
	}

	if err := s.users.UpdateNameAndActivate(ctx, user.ID, name); err != nil {
		return nil, err
	}

	if err := s.codes.Delete(ctx, emailAddr); err != nil {
		return nil, err
	}

	return s.createSession(ctx, user.ID)
}

func (s *AuthService) SignIn(ctx context.Context, emailAddr, password string) (*models.Session, *models.User, error) {
	user, err := s.users.FindByEmail(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}

	if !user.IsActive {
		return nil, nil, ErrAccountNotActive
	}

	if user.Password == nil {
		return nil, nil, ErrInvalidCredentials
	}

	if err := bcrypt.CompareHashAndPassword([]byte(*user.Password), []byte(password)); err != nil {
		return nil, nil, ErrInvalidCredentials
	}

	session, err := s.createSession(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}

	return session, user, nil
}

func (s *AuthService) createSession(ctx context.Context, userID int64) (*models.Session, error) {
	token, err := generateToken()
	if err != nil {
		return nil, err
	}
	session := &models.Session{
		UserID:    userID,
		Token:     token,
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}
	return session, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
