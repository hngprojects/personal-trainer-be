-- name: CreateUser :one
INSERT INTO users (email, name, auth_provider)
VALUES ($1, $2, $3)
ON CONFLICT (email, auth_provider) DO UPDATE
    SET updated_at = NOW()
RETURNING *;

-- name: GetUserByEmailAndProvider :one
SELECT *
FROM users
WHERE email = $1 AND auth_provider = $2
LIMIT 1;

-- name: GetUserByID :one
SELECT *
FROM users
WHERE id = $1
LIMIT 1;

-- name: GetUserRoleByID :one
SELECT role
FROM users
WHERE id = $1
LIMIT 1;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1
LIMIT 1;

-- name: UpsertAdminUser :one
INSERT INTO users (email, name, password, auth_provider, role, is_active)
VALUES ($1, $2, $3, 'local', 'admin', true)
ON CONFLICT (email, auth_provider) DO UPDATE
   SET password   = EXCLUDED.password,
       name       = EXCLUDED.name,
       role       = 'admin',
       is_active  = true,
       updated_at = NOW()
RETURNING *;

-- name: UpdateUserAvatar :execrows
-- Partial avatar-only update. Kept separate from UpdateUserOnboarding so the
-- background avatar worker can't race a concurrent profile edit and clobber
-- name/gender/etc with stale values. Returns affected row count so the worker
-- can distinguish "updated cleanly" from "user was deleted between upload and
-- DB write" — the latter must be persisted as a terminal failure or we silently
-- orphan the object in storage.
UPDATE users
SET avatar_url = sqlc.arg(avatar_url),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: UpdateUserOnboarding :one
UPDATE users
SET
    name           = COALESCE(NULLIF(sqlc.arg(name)::text, ''), name),
    gender         = COALESCE(NULLIF(sqlc.arg(gender)::text, ''), gender),
    fitness_goals  = CASE WHEN sqlc.arg(fitness_goals)::text[] IS NULL THEN fitness_goals ELSE sqlc.arg(fitness_goals)::text[] END,
    fitness_level  = COALESCE(NULLIF(sqlc.arg(fitness_level)::text, ''), fitness_level),
    avatar_url     = COALESCE(NULLIF(sqlc.arg(avatar_url)::text, ''), avatar_url),
    updated_at     = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;
