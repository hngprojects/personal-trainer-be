package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrEmailExists = errors.New("email already registered")
)

const providerLocal = "local"

// UserRepository defines what the auth feature needs from the users table.
type UserRepository interface {
	FindByEmail(ctx context.Context, email string) (*db.User, error)
	FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error)
	Create(ctx context.Context, email, name, provider string) (*db.User, error)
	CreateEmailUser(ctx context.Context, email string) (*db.User, error)
	MarkVerified(ctx context.Context, email string) (*db.User, error)
	// FindByAppleSub returns the user whose apple_user_id matches.
	// Apple Sign In uses this as the primary lookup because the email
	// claim is only present on the first authorization.
	FindByAppleSub(ctx context.Context, sub string) (*db.User, error)
	// CreateAppleUser provisions a new row from an Apple Sign In token.
	// email may be empty (subsequent sign-in) or a Hide-My-Email private
	// relay address — both are valid and stored verbatim.
	CreateAppleUser(ctx context.Context, email, name, sub string) (*db.User, error)
	// LookupRoleIDs returns the role-specific UUIDs associated with this
	// user — currently just trainer_id when the user has a trainers row.
	// Silent on missing trainer (most users aren't trainers): returns
	// RoleIDs{} rather than an error. Used by the login handlers to
	// aggregate ids into the auth response so the FE doesn't have to do
	// a follow-up lookup to learn its own trainer.id.
	LookupRoleIDs(ctx context.Context, userID uuid.UUID) (RoleIDs, error)
}

// RoleIDs aggregates the role-specific UUIDs for one user. Today only
// trainer_id exists (the only role with a separate ID table). Other
// roles (client, admin, super_admin) live entirely in users.role with
// no companion row, so they have no extra ID to surface.
//
// New role tables can extend this struct without breaking callers — the
// JSON encoder + the openapi schema both treat the new field as
// optional / omitempty.
type RoleIDs struct {
	// TrainerID is set iff the user has a trainers row. nil otherwise.
	TrainerID *uuid.UUID
}

// AdminUserRepository defines admin-specific user operations.
type AdminUserRepository interface {
	UpsertAdminUser(ctx context.Context, email, name, password string) (*db.User, error)
	FindByEmail(ctx context.Context, email string) (*db.User, error)
	FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error)
}

// TrainerUserRepository covers the user-provisioning side of the admin-creates-trainer
// flow. Mirrors AdminUserRepository but writes role='trainer'.
type TrainerUserRepository interface {
	UpsertTrainerUser(ctx context.Context, email, name, password string) (*db.User, error)
	FindByEmail(ctx context.Context, email string) (*db.User, error)
	FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error)
}

// SessionRepository defines what the auth feature needs from the sessions table.
type SessionRepository interface {
	Create(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*db.Session, error)
}

// VerificationCodeRepository defines what the auth feature needs from the verification_codes table.
type VerificationCodeRepository interface {
	Create(ctx context.Context, email, code string, expiresAt time.Time) error
	ConsumeByEmailAndCode(ctx context.Context, email, code string) (*db.VerificationCode, error)
	DeleteByEmail(ctx context.Context, email string) error
}

// RoleRepository defines what the auth feature needs from the roles / user_roles tables.
type RoleRepository interface {
	UserHasRole(ctx context.Context, userID uuid.UUID, roleName string) (bool, error)
}

// postgresRoleRepo implements RoleRepository.
type postgresRoleRepo struct {
	q *db.Queries
}

func NewPostgresRoleRepo(q *db.Queries) RoleRepository {
	return &postgresRoleRepo{q: q}
}

func (r *postgresRoleRepo) UserHasRole(ctx context.Context, userID uuid.UUID, roleName string) (bool, error) {
	return r.q.UserHasRole(ctx, db.UserHasRoleParams{UserID: userID, Name: roleName})
}

// postgresUserRepo implements UserRepository using sqlc-generated queries.
type postgresUserRepo struct {
	q *db.Queries
}

func NewPostgresUserRepo(q *db.Queries) UserRepository {
	return &postgresUserRepo{q: q}
}

func (r *postgresUserRepo) FindByEmailAndProvider(ctx context.Context, email, provider string) (*db.User, error) {
	user, err := r.q.GetUserByEmailAndProvider(ctx, db.GetUserByEmailAndProviderParams{
		Email:        email,
		AuthProvider: provider,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) FindByEmail(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) Create(ctx context.Context, email, name, provider string) (*db.User, error) {
	user, err := r.q.CreateUser(ctx, db.CreateUserParams{
		Email:        email,
		Name:         name,
		AuthProvider: provider,
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) CreateEmailUser(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.CreateEmailUser(ctx, email)
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, ErrEmailExists
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) LookupRoleIDs(ctx context.Context, userID uuid.UUID) (RoleIDs, error) {
	// Trainer is the only role with a separate ID table today. Look it
	// up directly; sql.ErrNoRows just means "not a trainer" — that's
	// the common case, not a failure.
	trainer, err := r.q.GetTrainerByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RoleIDs{}, nil
		}
		return RoleIDs{}, err
	}
	id := trainer.ID
	return RoleIDs{TrainerID: &id}, nil
}

func (r *postgresUserRepo) FindByAppleSub(ctx context.Context, sub string) (*db.User, error) {
	user, err := r.q.GetUserByAppleSub(ctx, sql.NullString{String: sub, Valid: sub != ""})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) CreateAppleUser(ctx context.Context, email, name, sub string) (*db.User, error) {
	user, err := r.q.CreateAppleUser(ctx, db.CreateAppleUserParams{
		Email:       email,
		Name:        name,
		AppleUserID: sql.NullString{String: sub, Valid: sub != ""},
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) MarkVerified(ctx context.Context, email string) (*db.User, error) {
	user, err := r.q.MarkUserVerified(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// No rows updated means the user is already active — fetch and return current data.
			existing, fetchErr := r.q.GetUserByEmailAndProvider(ctx, db.GetUserByEmailAndProviderParams{
				Email:        email,
				AuthProvider: providerLocal,
			})
			if fetchErr != nil {
				if errors.Is(fetchErr, sql.ErrNoRows) {
					return nil, ErrNotFound
				}
				return nil, fetchErr
			}
			return &existing, nil
		}
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) UpsertAdminUser(ctx context.Context, email, name, password string) (*db.User, error) {
	user, err := r.q.UpsertAdminUser(ctx, db.UpsertAdminUserParams{
		Email:    email,
		Name:     name,
		Password: sql.NullString{String: password, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *postgresUserRepo) UpsertTrainerUser(ctx context.Context, email, name, password string) (*db.User, error) {
	user, err := r.q.UpsertTrainerUser(ctx, db.UpsertTrainerUserParams{
		Email:    email,
		Name:     name,
		Password: sql.NullString{String: password, Valid: true},
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// postgresSessionRepo implements SessionRepository using sqlc-generated queries.
type postgresSessionRepo struct {
	q *db.Queries
}

func NewPostgresSessionRepo(q *db.Queries) SessionRepository {
	return &postgresSessionRepo{q: q}
}

func (r *postgresSessionRepo) Create(ctx context.Context, userID uuid.UUID, token string, expiresAt time.Time) (*db.Session, error) {
	session, err := r.q.CreateSession(ctx, db.CreateSessionParams{
		UserID:    userID,
		Token:     token,
		ExpiresAt: expiresAt,
	})
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// LocalAuthRepository combines OTP consumption and user verification in one atomic transaction.
type LocalAuthRepository interface {
	ConsumeAndMarkVerified(ctx context.Context, email, hashedCode string) (*db.User, error)
}

type postgresLocalAuthRepo struct {
	db *sql.DB
}

func NewPostgresLocalAuthRepo(rawDB *sql.DB) LocalAuthRepository {
	return &postgresLocalAuthRepo{db: rawDB}
}

func (r *postgresLocalAuthRepo) ConsumeAndMarkVerified(ctx context.Context, email, hashedCode string) (*db.User, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	q := db.New(tx)

	_, err = q.ConsumeVerificationCode(ctx, email, hashedCode)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	user, err := q.MarkUserVerified(ctx, email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			existing, fetchErr := q.GetUserByEmailAndProvider(ctx, db.GetUserByEmailAndProviderParams{
				Email:        email,
				AuthProvider: providerLocal,
			})
			if fetchErr != nil {
				if errors.Is(fetchErr, sql.ErrNoRows) {
					return nil, ErrNotFound
				}
				return nil, fetchErr
			}
			if err := tx.Commit(); err != nil {
				return nil, err
			}
			return &existing, nil
		}
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &user, nil
}

// postgresVerificationCodeRepo implements VerificationCodeRepository.
type postgresVerificationCodeRepo struct {
	q *db.Queries
}

func NewPostgresVerificationCodeRepo(q *db.Queries) VerificationCodeRepository {
	return &postgresVerificationCodeRepo{q: q}
}

func (r *postgresVerificationCodeRepo) Create(ctx context.Context, email, code string, expiresAt time.Time) error {
	return r.q.CreateVerificationCode(ctx, db.CreateVerificationCodeParams{
		Email:     email,
		Code:      code,
		ExpiresAt: expiresAt,
	})
}

func (r *postgresVerificationCodeRepo) ConsumeByEmailAndCode(ctx context.Context, email, code string) (*db.VerificationCode, error) {
	vc, err := r.q.ConsumeVerificationCode(ctx, email, code)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &vc, nil
}

func (r *postgresVerificationCodeRepo) DeleteByEmail(ctx context.Context, email string) error {
	return r.q.DeleteVerificationCodes(ctx, email)
}
