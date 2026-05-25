-- +goose Up
-- Organisation-level media library — images and videos that belong to
-- the org (used on landing pages, marketing copy, etc.), distinct from
-- the trainer-specific gallery (trainer_images) and intro-video
-- (trainers.intro_video_url) tables.
--
-- One table with a media_type discriminator (image | video) rather
-- than two parallel tables: the FE-facing schemas only differ on the
-- worker pipeline (images upload directly; videos transcode first), so
-- splitting the storage would mostly duplicate columns and require two
-- queries on the common "list everything" path.
--
-- status reflects the async pipeline:
--   processing - POST returned 202, worker hasn't finished
--   ready      - object in storage; public_url is reachable
--   failed     - worker exhausted retries; admin can DELETE + re-upload
CREATE TABLE IF NOT EXISTS organisation_media (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    media_type    TEXT         NOT NULL
        CHECK (media_type IN ('image', 'video')),
    title         TEXT         NOT NULL,
    description   TEXT,
    -- Free text up to 64 chars so admins can group entries
    -- ('hero', 'about', etc.) without us having to ship a migration
    -- every time they want a new bucket.
    category      TEXT         CHECK (category IS NULL OR length(category) <= 64),
    object_key    TEXT         NOT NULL,
    public_url    TEXT         NOT NULL,
    mime_type     TEXT         NOT NULL,
    size_bytes    BIGINT       NOT NULL CHECK (size_bytes >= 0),
    -- uploaded_by uses ON DELETE SET NULL so admin removals don't
    -- cascade-delete the media row. The org content stays; we just
    -- lose the audit pointer.
    uploaded_by   UUID         REFERENCES users(id) ON DELETE SET NULL,
    status        TEXT         NOT NULL DEFAULT 'processing'
        CHECK (status IN ('processing', 'ready', 'failed')),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- The list endpoint filters by media_type and/or category and sorts
-- newest-first; this composite index covers the common scan pattern
-- (type filter + created_at DESC) without bloating writes.
CREATE INDEX IF NOT EXISTS idx_organisation_media_type_created
    ON organisation_media(media_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_organisation_media_category
    ON organisation_media(category)
    WHERE category IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_organisation_media_status
    ON organisation_media(status);

-- +goose Down
DROP INDEX IF EXISTS idx_organisation_media_status;
DROP INDEX IF EXISTS idx_organisation_media_category;
DROP INDEX IF EXISTS idx_organisation_media_type_created;
DROP TABLE IF EXISTS organisation_media;
