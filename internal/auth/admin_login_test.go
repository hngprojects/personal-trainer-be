package auth_test

import (
	"context"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	gin.SetMode(gin.TestMode)
}

const (
	adminRoleName = "admin"
	MinCost       = 12
)

type fakeAdminUserRepo struct {
	findUser    *db.User
	findUserErr error
}

type fakeAdminUserRoleRepo struct {
	hasRole    bool
	hasRoleErr error
}

func (f *fakeAdminUserRepo) FindByEmail(_ context.Context, email string) (*db.User, error) {
	return f.findUser, f.findUserErr
}

func (f *fakeAdminUserRepo) FindByEmailAndProvider(_ context.Context, _, _ string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUserRepo) Create(_ context.Context, _, _, _ string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUserRepo) CreateEmailUser(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUserRepo) MarkVerified(_ context.Context, _ string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUserRoleRepo) UserHasRole(_ context.Context, userID uuid.UUID, _ string) (bool, error) {
	return f.hasRole, f.hasRoleErr
}

// func (f *fakeAdminUserRoleRepo) UserHasAdminRole (_ context.Context, userID uuid.UUID)(*db.)

func (f *fakeAdminUserRepo) MustHashPassword(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), MinCost)
	if err != nil {
		t.Fatalf("error occured during password hash: %v", err)
	}
	return string(hash)
}

func (f *fakeAdminUserRepo) newAdminTestHandle(t *testing.T, userRepo *fakeAdminUserRepo, roleRepo *fakeAdminUserRoleRepo) *handlers.AdminLoginHandler {
	t.Helper()
	service := auth.NewAdminLoginService(userRepo, roleRepo, discardLog)
	return handlers.NewAdminLogin(service, discardLog)
}
