package auth

import (
	"context"
	"log/slog"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
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

// buildAuthUser assembles the AuthUser response payload returned by
// every login handler. Centralised so the four flows (local sign-in,
// verify-email auto-login, google web, google mobile) populate the same
// fields the same way — including the role-specific trainer_id that
// the FE uses to call /trainers/{id}-style endpoints without a
// follow-up lookup.
//
// LookupRoleIDs is silent on no-trainer (returns RoleIDs{}, nil), so a
// non-trainer user just gets the field omitted via omitempty.
// A genuine DB failure surfaces as an error so the handler can decide
// whether to 500 or degrade.
func buildAuthUser(ctx context.Context, users UserRepository, user *db.User, log *slog.Logger) (api.AuthUser, error) {
	out := api.AuthUser{
		Id:              user.ID,
		Email:           user.Email,
		Name:            user.Name,
		UserType:        toAuthUserType(user.Role),
		ProfileComplete: user.Name != "",
	}
	roleIDs, err := users.LookupRoleIDs(ctx, user.ID)
	if err != nil {
		// Don't fail the login on this — the user still has a valid
		// JWT and can call /trainers/me to recover the trainer_id. Log
		// so we notice the underlying issue.
		log.Warn("buildAuthUser: role ID lookup failed, omitting trainer_id", "user_id", user.ID, "err", err)
		return out, nil
	}
	if roleIDs.TrainerID != nil {
		v := openapi_types.UUID(*roleIDs.TrainerID)
		out.TrainerId = &v
	}
	return out, nil
}
