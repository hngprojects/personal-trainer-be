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

	dbpkg "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/models"
	"github.com/hngprojects/personal-trainer-be/internal/repository"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

const (
	bcryptCost       = 12
	resetTokenExpiry = 30 * time.Minute
)

var (
	ErrEmailAlreadyExists = errors.New("email already registered")
	ErrInvalidCode        = errors.New("invalid or expired verification code")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrAccountNotActive   = errors.New("account not active")
	ErrWeakPassword       = errors.New("password must be 8-72 characters and contain at least one letter and one number")
	ErrInvalidResetToken  = errors.New("invalid or expired reset token")
)

var (
	hasNumber = regexp.MustCompile(`[0-9]`)
	hasLetter = regexp.MustCompile(`[a-zA-Z]`)
)

func validatePassword(password string) error {
	n := len([]byte(password))
	if n < 8 || n > 72 || !hasNumber.MatchString(password) || !hasLetter.MatchString(password) {
		return ErrWeakPassword
	}
	return nil
}

type AuthService struct {
	db          *sql.DB
	users       *repository.UserRepository
	sessions    *repository.SessionRepository
	codes       *repository.VerificationCodeRepository
	resetTokens *repository.PasswordResetRepository
	mailer      email.Mailer
}

func NewAuthService(
	db *sql.DB,
	users *repository.UserRepository,
	sessions *repository.SessionRepository,
	codes *repository.VerificationCodeRepository,
	resetTokens *repository.PasswordResetRepository,
	mailer email.Mailer,
) *AuthService {
	return &AuthService{db: db, users: users, sessions: sessions, codes: codes, resetTokens: resetTokens, mailer: mailer}
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
	if err := validatePassword(password); err != nil {
		return nil, err
	}

	vc, err := s.codes.FindByEmailAndCode(ctx, emailAddr, code)
	if err != nil || vc == nil || time.Now().After(vc.ExpiresAt) {
		return nil, ErrInvalidCode
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
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

func (s *AuthService) createSession(ctx context.Context, userID string) (*models.Session, error) {
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

func (s *AuthService) ForgotPassword(ctx context.Context, emailAddr string) error {
	user, err := s.users.FindByEmail(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}

	if !user.IsActive {
		return nil
	}

	token, err := generateToken()
	if err != nil {
		return err
	}

	prt := &models.PasswordResetToken{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(resetTokenExpiry),
	}
	if err := s.resetTokens.Save(ctx, prt); err != nil {
		return err
	}

	return s.mailer.Send(emailAddr, "Reset your password",
		fmt.Sprintf("Use this token to reset your password:\n\n%s\n\nThis token expires in 30 minutes. If you did not request a password reset, ignore this email.", token),
	)
}

func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}

	prt, err := s.resetTokens.FindByToken(ctx, token)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidResetToken
		}
		return err
	}
	if prt.UsedAt != nil || time.Now().After(prt.ExpiresAt) {
		return ErrInvalidResetToken
	}

	hashed, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	qtx := dbpkg.New(tx)
	if err := qtx.UpdateUserPassword(ctx, dbpkg.UpdateUserPasswordParams{
		ID:       prt.UserID,
		Password: sql.NullString{String: string(hashed), Valid: true},
	}); err != nil {
		return err
	}
	if err := qtx.MarkPasswordResetTokenUsed(ctx, token); err != nil {
		return err
	}
	if err := qtx.DeleteSessionsByUserID(ctx, prt.UserID); err != nil {
		return err
	}
	return tx.Commit()
}
