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

-- name: GetUserByAppleSub :one
-- Apple Sign In uses the stable `sub` claim as the user identifier
-- because the `email` claim is only emitted on the first authorization.
-- A separate lookup keyed on apple_user_id is required so returning
-- users find their row on every subsequent sign-in.
SELECT *
FROM users
WHERE apple_user_id = $1
LIMIT 1;

-- name: CreateAppleUser :one
-- First-time Apple Sign In. Email may be empty or a Hide-My-Email
-- private relay address (privaterelay.appleid.com) — the relay value
-- is fine to store and email; Apple forwards it to the user's real
-- inbox. Name is also only present on the first authorization, so
-- empty strings are valid for both fields and we just don't display
-- placeholder names later.
INSERT INTO users (email, name, auth_provider, apple_user_id, is_active)
VALUES ($1, $2, 'apple', $3, true)
RETURNING *;

-- name: LinkAppleSubToUser :one
-- Backfill apple_user_id on an existing row when an account that was
-- previously created without one (e.g. through an earlier flow) signs
-- in for the first time. Only used by the handler when we find the
-- user by email-fallback; the primary lookup uses GetUserByAppleSub.
UPDATE users
SET apple_user_id = $2,
    updated_at = NOW()
WHERE id = $1
  AND apple_user_id IS NULL
RETURNING *;

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

-- name: UpsertTrainerUser :one
-- Mirror of UpsertAdminUser, used by POST /trainers (admin-creates-trainer).
-- The admin enters the trainer's email + name (+ optional gender +
-- phone_number); we provision a local-auth user with role='trainer'
-- and a generated password (hashed). Re-inviting the same email is
-- idempotent — the password rotates, the name is overwritten, gender
-- and phone_number are overwritten ONLY when the caller supplies a
-- non-empty value (NULLIF guard) so a re-invite that omits them keeps
-- existing data, and the role is forced back to 'trainer' so a
-- previously-suspended account is reactivated cleanly. The plaintext
-- password is mailed exactly once by the caller and never persisted.
INSERT INTO users (email, name, password, gender, phone_number, auth_provider, role, is_active)
VALUES (
    sqlc.arg(email),
    sqlc.arg(name),
    sqlc.arg(password),
    NULLIF(sqlc.arg(gender)::text, ''),
    NULLIF(sqlc.arg(phone_number)::text, ''),
    'local',
    'trainer',
    true
)
ON CONFLICT (email, auth_provider) DO UPDATE
   SET password     = EXCLUDED.password,
       name         = EXCLUDED.name,
       gender       = COALESCE(EXCLUDED.gender, users.gender),
       phone_number = COALESCE(EXCLUDED.phone_number, users.phone_number),
       role         = 'trainer',
       is_active    = true,
       updated_at   = NOW()
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

-- name: UpdateTrainerUserProfile :one
-- Updates the users-table fields a trainer can edit on their own profile.
-- Pass empty string for phone_number to leave it unchanged.
UPDATE users
SET
    phone_number = COALESCE(NULLIF(sqlc.arg(phone_number)::text, ''), phone_number),
    updated_at   = NOW()
WHERE id = sqlc.arg(id)
RETURNING *;

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

-- name: CountClients :one
SELECT COUNT(*) FROM users WHERE role = 'client' AND is_active = true;

-- name: ListClients :many
SELECT
    u.id,
    u.name,
    u.email,
    u.is_active,
    u.created_at,
    COALESCE(COUNT(b.id), 0)::BIGINT AS sessions_booked
FROM users u
LEFT JOIN bookings b ON b.client_id = u.id
WHERE u.role = 'client'
  AND (sqlc.narg(is_active)::boolean IS NULL OR u.is_active = sqlc.narg(is_active)::boolean)
GROUP BY u.id
ORDER BY u.created_at DESC
LIMIT sqlc.arg(lim)::BIGINT
OFFSET sqlc.arg(off)::BIGINT;

-- name: CountClients2 :one
SELECT COUNT(*)::BIGINT
FROM users
WHERE role = 'client'
  AND (sqlc.narg(is_active)::boolean IS NULL OR is_active = sqlc.narg(is_active)::boolean);

-- name: DeactivateSelf :one
UPDATE users SET is_active = false, updated_at = NOW()
WHERE users.id = $1 AND users.is_active = true
RETURNING users.id;

-- name: ReactivateSelf :one
UPDATE users SET is_active = true, updated_at = NOW()
WHERE users.id = $1 AND users.is_active = false
RETURNING users.id;

-- name: HardDeleteClient :execrows
-- Permanently deletes a client and all their data via FK cascade.
-- Admin-only. Role-guarded to prevent accidental deletion of admins/trainers.
-- Returns rows affected so caller can detect concurrent deletes or role mismatches.
DELETE FROM users WHERE users.id = $1 AND users.role = 'client';


-- name: GetClientByID :one
SELECT
    u.id,
    u.name,
    u.email,
    u.is_active,
    u.created_at,
    u.gender,
    u.fitness_goals,
    u.fitness_level,
    u.avatar_url,
    COALESCE(COUNT(b.id), 0)::BIGINT AS sessions_booked
FROM users u
LEFT JOIN bookings b ON b.client_id = u.id
WHERE u.id = sqlc.arg(id)::uuid
  AND u.role = 'client'
GROUP BY u.id;

-- name: DeactivateClient :one
-- Backfilled from internal/repository/db/users.sql.go where it was
-- hand-added (PR #283). Lifted into the sqlc source so future
-- `sqlc generate` runs don't wipe it. Disambiguated with table alias
-- so sqlc's parser is happy (Postgres handles the unaliased form fine
-- but the parser sqlc embeds is stricter).
UPDATE users u SET is_active = false, updated_at = NOW()
WHERE u.id = $1 AND u.role = 'client' AND u.is_active = true
RETURNING u.id;
