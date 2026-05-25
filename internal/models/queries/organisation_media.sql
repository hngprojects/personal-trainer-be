-- name: CreateOrganisationMedia :one
-- POST /media/{images,videos} writes the row with status='processing'
-- (the default) and hands the worker an upload job. Status is flipped
-- to 'ready' (or 'failed') by the worker after the upload + transcode
-- complete. The object_key + public_url are computed at handler time
-- and stay stable for the lifetime of the row — the worker writes to
-- exactly the key we already wrote into the DB.
INSERT INTO organisation_media (
    media_type,
    title,
    description,
    category,
    object_key,
    public_url,
    mime_type,
    size_bytes,
    uploaded_by
) VALUES (
    sqlc.arg(media_type),
    sqlc.arg(title),
    NULLIF(sqlc.arg(description)::text, ''),
    NULLIF(sqlc.arg(category)::text, ''),
    sqlc.arg(object_key),
    sqlc.arg(public_url),
    sqlc.arg(mime_type),
    sqlc.arg(size_bytes),
    sqlc.arg(uploaded_by)
)
RETURNING *;

-- name: GetOrganisationMediaByID :one
SELECT * FROM organisation_media WHERE id = $1;

-- name: ListOrganisationMedia :many
-- Public list with optional filters. Empty-string sentinels for
-- media_type / category mean "no filter" — keeps the handler signature
-- simple (a missing query param maps to "") without forcing dynamic
-- query construction. status defaults to 'ready' so callers don't see
-- half-uploaded rows; the admin endpoint can pass status='' to see
-- everything including in-flight + failed.
SELECT * FROM organisation_media
WHERE (sqlc.arg(media_type)::text = '' OR media_type = sqlc.arg(media_type)::text)
  AND (sqlc.arg(category)::text   = '' OR category   = sqlc.arg(category)::text)
  AND (sqlc.arg(status)::text     = '' OR status     = sqlc.arg(status)::text)
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_limit)
OFFSET sqlc.arg(page_offset);

-- name: CountOrganisationMedia :one
-- Total count for ListOrganisationMedia, applying the same filters so
-- pagination meta matches the body length.
SELECT COUNT(*) FROM organisation_media
WHERE (sqlc.arg(media_type)::text = '' OR media_type = sqlc.arg(media_type)::text)
  AND (sqlc.arg(category)::text   = '' OR category   = sqlc.arg(category)::text)
  AND (sqlc.arg(status)::text     = '' OR status     = sqlc.arg(status)::text);

-- name: UpdateOrganisationMediaStatus :execrows
-- Worker-only — flips status to 'ready' (success) or 'failed' (after
-- retries exhausted). Returns rowsAffected so the worker can
-- distinguish "wrote OK" from "row was deleted while we were
-- processing" and skip emitting a misleading success log.
UPDATE organisation_media
SET status = sqlc.arg(status),
    updated_at = NOW()
WHERE id = sqlc.arg(id);

-- name: DeleteOrganisationMedia :one
-- Returns the deleted row so the handler can pull the object_key and
-- remove the underlying MinIO object — without this, deletes would
-- orphan files in storage.
DELETE FROM organisation_media WHERE id = $1 RETURNING *;
