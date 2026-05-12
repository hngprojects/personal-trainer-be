package auth_test

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type fakeAdminUserRepo struct {
	user              *db.User
	role              *db.Role
	userErr           error
	roleErr           error
	createUserErr     error
	createRoleErr     error
	createUserRoleErr error
}

func init() {
	gin.SetMode(gin.TestMode)
}

func (f *fakeAdminUserRepo) GetUserRole(_ context.Context, email string) (*db.Role, error) {
	return f.role, f.roleErr
}

func (f *fakeAdminUserRepo) GetUserByEmail(_ context.Context, email string) (*db.User, error) {
	return f.user, f.userErr
}

func (f *fakeAdminUserRepo) CreateRole(_ context.Context, roleName string) (*db.Role, error) {
	if f.createRoleErr != nil {
		return nil, f.createRoleErr
	}
	return &db.Role{ID: uuid.New(), Name: roleName}, nil
}
