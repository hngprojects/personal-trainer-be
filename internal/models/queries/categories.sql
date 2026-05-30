-- name: ListCategories :many
-- Used by both the admin settings page (which shows them as chips
-- with × delete) and the client-facing /categories endpoint. Stable
-- order by name so the FE doesn't have to sort.
SELECT id, name, slug, created_at, updated_at
FROM categories
ORDER BY name ASC;

-- name: CreateCategory :one
-- name is the display label; slug is the URL-safe identifier. Both
-- enforced UNIQUE at the schema level — the handler maps 23505
-- (unique violation) to 409 Conflict so the admin sees a clean
-- "already exists" message instead of a 500.
INSERT INTO categories (name, slug)
VALUES ($1, $2)
RETURNING id, name, slug, created_at, updated_at;

-- name: DeleteCategory :execrows
-- Returns the affected row count so the handler can 404 cleanly when
-- the id doesn't exist rather than returning 204 in both cases (which
-- would make "already gone" indistinguishable from "really gone now").
DELETE FROM categories WHERE id = $1;
