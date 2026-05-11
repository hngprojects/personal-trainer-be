package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Role struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
}

type UserRole struct {
	UserID    uuid.UUID
	RoleID    uuid.UUID
	CreatedAt time.Time
}

const getRoleByName = `SELECT id, name, created_at FROM roles WHERE name = $1 LIMIT 1`

func (q *Queries) GetRoleByName(ctx context.Context, name string) (Role, error) {
	row := q.db.QueryRowContext(ctx, getRoleByName, name)
	var r Role
	err := row.Scan(&r.ID, &r.Name, &r.CreatedAt)
	return r, err
}

const assignUserRole = `
INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2)
ON CONFLICT (user_id, role_id) DO NOTHING
`

func (q *Queries) AssignUserRole(ctx context.Context, userID, roleID uuid.UUID) error {
	_, err := q.db.ExecContext(ctx, assignUserRole, userID, roleID)
	return err
}

const userHasRole = `
SELECT EXISTS (
    SELECT 1 FROM user_roles ur
    INNER JOIN roles r ON r.id = ur.role_id
    WHERE ur.user_id = $1 AND r.name = $2
)
`

func (q *Queries) UserHasRole(ctx context.Context, userID uuid.UUID, roleName string) (bool, error) {
	row := q.db.QueryRowContext(ctx, userHasRole, userID, roleName)
	var has bool
	err := row.Scan(&has)
	return has, err
}

const getUserRoles = `
SELECT r.id, r.name, r.created_at
FROM roles r
INNER JOIN user_roles ur ON ur.role_id = r.id
WHERE ur.user_id = $1
`

func (q *Queries) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]Role, error) {
	rows, err := q.db.QueryContext(ctx, getUserRoles, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var roles []Role
	for rows.Next() {
		var r Role
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}
