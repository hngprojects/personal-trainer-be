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
	hasRole    bool
	hasRoleErr error
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

func (f *fakeAdminUserRole) UserHasRole(_ context.Context, userID uuid.UUID, roleName string) (bool, error) {
	return f.hasRole, f.hasRoleErr
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
	response := decodeBody(t, w)
	if response["status"] != "success" {
		t.Errorf("expected success, got %s: %s", response["status"], w.Body.String())
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
	response := decodeBody(t, w)
	if response["status"] != "error" {
		t.Errorf("expected error, got %s: %s", response["status"], w.Body.String())
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
	response := decodeBody(t, w)
	if response["status"] != "error" {
		t.Errorf("expected error, got %s: %s", response["status"], w.Body.String())
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
	response := decodeBody(t, w)
	if response["status"] != "error" {
		t.Errorf("expected error, got %s: %s", response["status"], w.Body.String())
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
	response := decodeBody(t, w)
	if response["status"] != "error" {
		t.Errorf("expected error, got %s: %s", response["status"], w.Body.String())
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
	if response["status"] != "success" {
		t.Errorf("expected success, got %s: %s", response["status"], w.Body.String())
	}
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
