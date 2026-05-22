package auth

import (
	"log/slog"

	"github.com/hngprojects/personal-trainer-be/internal/api"
)

// toAuthUserType maps the DB-level users.role string to the OpenAPI
// enum the login responses return.
//
// "super_admin" collapses to api.AuthUserUserTypeAdmin on purpose:
// front-end clients never need to distinguish between admin and
// super-admin at sign-in (privilege escalation is enforced
// server-side by SuperAdminOnly middleware on /admin/* routes), and
// keeping the enum closed means existing switch-statements on the FE
// can't forget a case. If a future product call needs the
// distinction visible, add a new enum value here AND in api.yaml.
//
// Empty or unknown roles fall back to Client to preserve the
// previous hardcoded behaviour — the warn log lets us notice a new
// role slipping through without it manifesting as a 500.
func toAuthUserType(role string) api.AuthUserUserType {
	switch role {
	case "client":
		return api.AuthUserUserTypeClient
	case "trainer":
		return api.AuthUserUserTypeTrainer
	case "admin", "super_admin":
		return api.AuthUserUserTypeAdmin
	default:
		slog.Warn("toAuthUserType: unknown user role, falling back to client", "role", role)
		return api.AuthUserUserTypeClient
	}
}
