-- Seed users + roles for local development/testing.
-- Run: psql "$DATABASE_URL" -f seed_users.sql

BEGIN;

-- 1) Ensure canonical roles exist (your migration seeds some already).
INSERT INTO roles (name) VALUES
  ('client'),
  ('trainer'),
  ('admin'),
  ('super_admin')
ON CONFLICT (name) DO NOTHING;

-- 2) Create users (local)
-- NOTE: users.password is left NULL by default here. You'll set it via setup-password or manually.
-- auth_provider must match what your code expects ("local").

INSERT INTO users (email, name, auth_provider, is_active, role)
VALUES
  ('superadmin@example.com', 'Super Admin', 'local', true, 'client'),
  ('admin@example.com',      'Admin User',  'local', true, 'client'),
  ('trainer@example.com',    'Trainer One', 'local', true, 'client'),
  ('client@example.com',     'Client One',  'local', true, 'client')
ON CONFLICT (email, auth_provider) DO UPDATE
  SET name = EXCLUDED.name,
      is_active = true,
      updated_at = NOW();

-- 3) Assign roles via user_roles (canonical)
WITH u AS (
  SELECT id, email FROM users WHERE auth_provider='local'
),
r AS (
  SELECT id, name FROM roles
)
INSERT INTO user_roles (user_id, role_id)
SELECT u.id, r.id
FROM u
JOIN r ON (
  (u.email='superadmin@example.com' AND r.name IN ('super_admin','admin')) OR
  (u.email='admin@example.com'      AND r.name='admin') OR
  (u.email='trainer@example.com'    AND r.name='trainer') OR
  (u.email='client@example.com'     AND r.name='client')
)
ON CONFLICT (user_id, role_id) DO NOTHING;

-- 4) Create trainer profile for trainer user (if not exists)
INSERT INTO trainers (
  user_id,
  specialization,
  bio,
  years_of_experience,
  intro_video_url,
  display_picture,
  calendly_connected,
  calendly_link,
  onboarding_status
)
SELECT
  u.id,
  'Weight loss',
  'Helping clients lose weight safely',
  5,
  NULL,
  NULL,
  false,
  NULL,
  'pending'
FROM users u
WHERE u.email='trainer@example.com' AND u.auth_provider='local'
AND NOT EXISTS (
  SELECT 1 FROM trainers t WHERE t.user_id = u.id
);

-- 5) Create a trainer invite token for /trainers/setup-password
-- You can use this token in Postman immediately.
-- Token: "dev-trainer-invite-token"
INSERT INTO trainer_invite_tokens (user_id, token, expires_at)
SELECT u.id, 'dev-trainer-invite-token', NOW() + INTERVAL '7 days'
FROM users u
WHERE u.email='trainer@example.com' AND u.auth_provider='local'
ON CONFLICT (token) DO UPDATE
  SET user_id = EXCLUDED.user_id,
      expires_at = EXCLUDED.expires_at,
      used_at = NULL;

COMMIT;

-- Convenience: print the user IDs you will need in Postman.
SELECT id AS user_id, email, name, is_active
FROM users
WHERE email IN (
  'superadmin@example.com',
  'admin@example.com',
  'trainer@example.com',
  'client@example.com'
)
ORDER BY email;

-- Convenience: show the trainer id too
SELECT t.id AS trainer_id, u.id AS user_id, u.email
FROM trainers t
JOIN users u ON u.id = t.user_id
WHERE u.email = 'trainer@example.com' AND u.auth_provider='local';

-- Convenience: show the invite token
SELECT token, user_id, expires_at, used_at
FROM trainer_invite_tokens
WHERE token = 'dev-trainer-invite-token';