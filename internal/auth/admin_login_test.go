package auth_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/handlers"
	"github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

type fakeAdminUser struct {
	findUser    *db.User
	findUserErr error
}

type fakeAdminUserRole struct {
	// hasRole is the default response when hasRolePerRole is nil — it
	// applies regardless of which role name is asked about. The existing
	// tests set this and expect both UserHasRole("admin") and
	// UserHasRole("super_admin") to return the same thing.
	hasRole bool
	// hasRolePerRole, when non-nil, overrides hasRole on a per-role
	// basis. Lets the regression tests below distinguish "user has
	// admin but not super_admin" from "user has both" — a distinction
	// the original single-bool fake could not express, which is why
	// the role-check bug here went undetected for so long.
	hasRolePerRole map[string]bool
	hasRoleErr     error
}

func (f *fakeAdminUser) Create(_ context.Context, email, name, password string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUser) CreateEmailUser(_ context.Context, email string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUser) FindByEmail(_ context.Context, email string) (*db.User, error) {
	return f.findUser, f.findUserErr
}

func (f *fakeAdminUser) FindByEmailAndProvider(_ context.Context, email string, _ string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUser) MarkVerified(_ context.Context, email string) (*db.User, error) {
	return nil, nil
}

func (f *fakeAdminUser) LookupRoleIDs(_ context.Context, _ uuid.UUID) (auth.RoleIDs, error) {
	return auth.RoleIDs{}, nil
}

func (f *fakeAdminUserRole) UserHasRole(_ context.Context, _ uuid.UUID, roleName string) (bool, error) {
	if f.hasRoleErr != nil {
		return false, f.hasRoleErr
	}
	if f.hasRolePerRole != nil {
		return f.hasRolePerRole[roleName], nil
	}
	return f.hasRole, nil
}

func newAdminTestHandler(t *testing.T, user auth.UserRepository, role auth.RoleRepository) *handlers.AdminLoginHandler {
	t.Helper()
	service := auth.NewAdminLoginService(user, role, discardLog)
	return handlers.NewAdminLogin(service, discardLog)
}

func createAdminHandler(t *testing.T) handlers.AdminLoginHandler {
	hashedPassword := mustHashPassword(t, "user2020")
	userRepo := &fakeAdminUser{
		findUser: &db.User{
			ID:    uuid.New(),
			Email: "testadminemail@yahoomail.com",
			Name:  "test-admin veteran",
			Password: sql.NullString{
				Valid:  true,
				String: hashedPassword,
			},
		},
	}
	roleRepo := &fakeAdminUserRole{
		hasRole: true,
	}
	handler := newAdminTestHandler(t, userRepo, roleRepo)
	return *handler
}

func createAdminErrHandler(t *testing.T, userRepo auth.UserRepository, roleRepo auth.RoleRepository) *handlers.AdminLoginHandler {
	return newAdminTestHandler(t, userRepo, roleRepo)
}

func sendAdminRequest(t *testing.T, h *handlers.AdminLoginHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	relativePath := "/auth/admin/log-in"
	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST(relativePath, h.Login)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, relativePath, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, request)
	return w
}

func TestAdminLogin_Success(t *testing.T) {
	t.Setenv(
		"JWT_SECRET", "admin-test-secret",
	)
	h := createAdminHandler(t)
	body := `{"email": "testadminemail@yahoomail.com", "password":"user2020"}`
	w := sendAdminRequest(t, &h, body)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_WrongPassword(t *testing.T) {
	t.Setenv(
		"JWT_SECRET", "admin-test-secret",
	)
	h := createAdminHandler(t)
	body := `{"email": "testadminemail@yahoomail.com", "password":"user2024"}`
	w := sendAdminRequest(t, &h, body)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_EmailNotFound(t *testing.T) {
	userRepo := &fakeAdminUser{
		findUserErr: auth.ErrNotFound,
	}
	roleRepo := &fakeAdminUserRole{
		hasRole: false,
	}
	h := createAdminErrHandler(t, userRepo, roleRepo)
	body := `{"email": "adminfakemail@proton.com", "password":"user2020"}`
	w := sendAdminRequest(t, h, body)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_UserNotAdmin(t *testing.T) {
	hashedPassword := mustHashPassword(t, "user2020")
	userRepo := &fakeAdminUser{
		findUser: &db.User{
			ID:    uuid.New(),
			Email: "testadminemail@yahoomail.com",
			Name:  "test-admin veteran",
			Password: sql.NullString{
				Valid:  true,
				String: hashedPassword,
			},
		},
	}
	roleRepo := &fakeAdminUserRole{
		hasRole: false,
	}
	h := createAdminErrHandler(t, userRepo, roleRepo)
	body := `{"email": "adminfakemail@proton.com", "password":"user2020"}`
	w := sendAdminRequest(t, h, body)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_MissingEmail(t *testing.T) {
	h := createAdminErrHandler(t, &fakeAdminUser{}, &fakeAdminUserRole{})
	body := `{"email":"", "password":"user2020"}`
	w := sendAdminRequest(t, h, body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_MissingPassword(t *testing.T) {
	h := createAdminErrHandler(t, &fakeAdminUser{}, &fakeAdminUserRole{})
	body := `{"email":"adminmissingpassword@g.co", "password":""}`
	w := sendAdminRequest(t, h, body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdminLogin_NullPassword(t *testing.T) {
	userRepo := &fakeAdminUser{
		findUser: &db.User{
			ID:    uuid.New(),
			Email: "userwithnullpassword@gmail.com",
			Name:  "null-password admin",
			Password: sql.NullString{
				Valid: false,
			},
		},
	}
	roleRepo := &fakeAdminUserRole{
		hasRole: true,
	}
	h := newAdminTestHandler(t, userRepo, roleRepo)
	body := `{"email":"userwithnullpassword@gmail.com", "password":"user2020"}`
	w := sendAdminRequest(t, h, body)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}

}

func TestAdminLogin_InvalidJSON(t *testing.T) {
	h := newAdminTestHandler(t, &fakeAdminUser{}, &fakeAdminUserRole{})

	w := sendAdminRequest(t, h, `hola, viola`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed JSON, got %d", w.Code)
	}
}

func TestAdminLogin_SuccessResponseShape(t *testing.T) {
	t.Setenv(
		"JWT_SECRET", "admin-test-secret",
	)
	h := createAdminHandler(t)
	body := `{"email": "testadminemail@yahoomail.com", "password":"user2020"}`
	w := sendAdminRequest(t, &h, body)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	response := decodeBody(t, w)
	data := response["data"]
	mappedData, ok := data.(map[string]any)
	if !ok {
		t.Errorf("data field missing or not an object")
	}
	for _, field := range []string{"access_token", "refresh_token", "expires_in"} {
		if _, ok := mappedData[field]; !ok {
			t.Errorf("could not find field %s", field)
		}
	}
	userData, ok := mappedData["user"].(map[string]any)
	if !ok {
		t.Errorf("user data missing")
	}
	for _, field := range []string{"email", "id", "name", "profile_complete", "user_type"} {
		if _, ok := userData[field]; !ok {
			t.Errorf("could not find field %s", field)
		}
	}
	if userData["email"] != "testadminemail@yahoomail.com" {
		t.Errorf("userData.email: want testadminemail@yahoomail.com, got %v", userData["email"])
	}
	if userData["profile_complete"] != true {
		t.Errorf("userData.profile_complete: want true, got %v", userData["profile_complete"])
	}
	if userData["user_type"] != "admin" {
		t.Errorf("userData.user_type: want true, got %v", userData["user_type"])
	}
}

// adminLoginWithRoles is a tiny helper for the role-distinguishing
// tests below. Keeps the per-test boilerplate (user fixture, JWT env,
// handler construction) out of the way so each test reads as just
// "given these roles, expect this status".
func adminLoginWithRoles(t *testing.T, isAdmin, isSuperAdmin bool) *httptest.ResponseRecorder {
	t.Helper()
	t.Setenv("JWT_SECRET", "admin-test-secret")
	hashed := mustHashPassword(t, "user2020")
	userRepo := &fakeAdminUser{
		findUser: &db.User{
			ID:       uuid.New(),
			Email:    "role-test@example.com",
			Name:     "Role Test",
			Password: sql.NullString{Valid: true, String: hashed},
		},
	}
	roleRepo := &fakeAdminUserRole{
		hasRolePerRole: map[string]bool{
			"admin":       isAdmin,
			"super_admin": isSuperAdmin,
		},
	}
	h := newAdminTestHandler(t, userRepo, roleRepo)
	return sendAdminRequest(t, h, `{"email":"role-test@example.com","password":"user2020"}`)
}

// Regression: a user with the `admin` role but NOT `super_admin` must
// be able to log in. Previously the role gate was `!isUserAdmin ||
// !isUserSuperAdmin`, which by De Morgan rejects anyone missing either
// role — so plain admins were locked out.
func TestAdminLogin_PlainAdminSucceeds(t *testing.T) {
	w := adminLoginWithRoles(t, true, false)
	if w.Code != http.StatusOK {
		t.Errorf("plain admin should log in (regression for require-both-roles bug); got %d: %s", w.Code, w.Body.String())
	}
}

// Mirror regression: super_admin without admin must also succeed.
func TestAdminLogin_PlainSuperAdminSucceeds(t *testing.T) {
	w := adminLoginWithRoles(t, false, true)
	if w.Code != http.StatusOK {
		t.Errorf("plain super_admin should log in (regression for require-both-roles bug); got %d: %s", w.Code, w.Body.String())
	}
}

// A user with BOTH roles must still succeed — the original happy path
// some env'd admin accounts will already be in.
func TestAdminLogin_BothRolesSucceeds(t *testing.T) {
	w := adminLoginWithRoles(t, true, true)
	if w.Code != http.StatusOK {
		t.Errorf("user with both roles should log in; got %d: %s", w.Code, w.Body.String())
	}
}

// A user with NEITHER role must be rejected. Sanity guard against an
// accidental flip the other way (e.g. someone "fixing" the OR by
// removing the gate entirely).
func TestAdminLogin_NeitherRoleFails(t *testing.T) {
	w := adminLoginWithRoles(t, false, false)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("user with neither role must not log in; got %d: %s", w.Code, w.Body.String())
	}
}
