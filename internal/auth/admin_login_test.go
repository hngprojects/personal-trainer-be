package auth_test

import (
	"context"
	"database/sql"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"golang.org/x/crypto/bcrypt"
)

type fakeAdminUserRepo struct {
	user              *db.User
	role              *db.GetUserRoleRow
	userErr           error
	roleErr           error
	createUserErr     error
	createRoleErr     error
	createUserRoleErr error
}

func init() {
	gin.SetMode(gin.TestMode)
}

func (f *fakeAdminUserRepo) GetUserRole(_ context.Context, email string) (*db.GetUserRoleRow, error) {
	return f.role, f.roleErr
}

func (f *fakeAdminUserRepo) GetUserByEmail(_ context.Context, email string) (*db.User, error) {
	return f.user, f.userErr
}

func (f *fakeAdminUserRepo) Create(_ context.Context, email string, name string, password string) (*db.User, error) {
	if f.createUserErr != nil {
		return nil, f.createUserErr
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), auth.BcryptSaltRound)
	if err != nil {
		return nil, err
	}
	return &db.User{
		ID:    uuid.New(),
		Email: email,
		Name:  name,
		Password: sql.NullString{
			Valid:  true,
			String: string(hashedPassword),
		},
	}, nil
}

func (f *fakeAdminUserRepo) CreateRole(_ context.Context, roleName string) (*db.Role, error) {
	if f.createRoleErr != nil {
		return nil, f.createRoleErr
	}
	return &db.Role{ID: uuid.New(), Name: roleName}, nil
}

func (f *fakeAdminUserRepo) CreateRoleForUser(_ context.Context, userID uuid.UUID, roleID uuid.UUID) (*db.UserRole, error) {
	if f.createUserRoleErr != nil {
		return nil, f.createUserRoleErr
	}
	return &db.UserRole{ID: uuid.New(), UserID: userID, RoleID: roleID}, nil
}
