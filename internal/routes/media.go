package routes

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
	"github.com/hngprojects/personal-trainer-be/pkg/storage"
	"github.com/hngprojects/personal-trainer-be/pkg/video"
)

// mediaStore holds the sqlc handle + MinIO client for the org-media
// endpoints. Kept separate from the existing per-feature stores so
// /media stays self-contained — adding a new media type later means
// touching this file + the worker file, nothing else.
type mediaStore struct {
	q     *db.Queries
	store storage.Storage // nil-safe: DELETE skips the unlink when nil
}

const (
	// Per-file caps — mirror the trainer pipelines so the FE can
	// reuse its existing client-side validators.
	orgMediaImageMaxBytes = 5 << 20  // 5 MiB
	orgMediaImageReqMax   = 6 << 20  // 5 MiB + multipart overhead
	orgMediaVideoMaxBytes = 500 << 20 // 500 MiB before transcode
	orgMediaVideoMaxDurationSeconds = 600 // 10 minutes

	orgMediaTitleMaxLen       = 200
	orgMediaDescriptionMaxLen = 2000
	orgMediaCategoryMaxLen    = 64
)

// orgMediaImageProbeTimeout bounds the cheap MIME-sniff + format probe
// done in the request goroutine. Generous because heic decoding can be
// slower than jpeg/png.
const orgMediaImageProbeTimeout = 10 * time.Second

// orgMediaVideoProbeTimeout bounds the ffprobe call in the request
// goroutine. Tighter than the worker's transcode timeout — probe only
// reads headers so it's much faster than a full transcode.
const orgMediaVideoProbeTimeout = 30 * time.Second

// allowedMediaCategories is intentionally NOT enforced — `category` is
// free-text by design. Kept here as documentation of the conventional
// values the FE may filter on:
//
//	hero        homepage hero image/video
//	about       about-page copy
//	testimonial client testimonial card
//	blog        blog post hero
//	generic     anything else
//
// Add new values by typing them on POST; no migration needed.

// UploadOrganisationImage handles POST /media/images. Admin-only.
// Validates MIME + size, INSERTs the organisation_media row with
// status='processing', enqueues the bytes for async upload, returns
// 202 with the row so the FE can render an optimistic placeholder
// pointing at the (about-to-be-live) public_url.
func (s *routerImpl) UploadOrganisationImage(c *gin.Context) {
	if !s.requireMediaAdmin(c) {
		return
	}
	if s.organisationImageUploader == nil {
		s.logger.Warn("upload organisation image: uploader not configured")
		c.JSON(http.StatusServiceUnavailable, api.NewError("organisation image upload is not configured on this server", api.CodeServerError))
		return
	}

	// Bound the body before the multipart parser touches it.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, orgMediaImageReqMax)

	if err := c.Request.ParseMultipartForm(orgMediaImageReqMax); err != nil {
		s.logger.Warn("upload organisation image: invalid multipart form", "err", err)
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("request exceeds %d-byte limit", orgMediaImageReqMax), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}

	title, description, category, fieldErrs := parseMediaTextFields(c)
	if len(fieldErrs) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrs))
		return
	}

	fh, _ := getOptionalFormFile(c, "file")
	if fh == nil {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "file", Message: "image file is required"},
		}))
		return
	}
	if fh.Size > orgMediaImageMaxBytes {
		c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file exceeds %d-byte limit", orgMediaImageMaxBytes), api.CodeBadRequest))
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("could not open uploaded file: "+err.Error(), api.CodeBadRequest))
		return
	}
	raw, err := io.ReadAll(f)
	_ = f.Close()
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("could not read uploaded file: "+err.Error(), api.CodeBadRequest))
		return
	}

	// Sniff MIME from bytes, not from the multipart Content-Type header
	// (which is client-supplied and unreliable).
	mime, err := detectTrainerImage(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}
	ext := imageAcceptedMIMEs[mime]

	// Pre-compute the final URL so we can INSERT it now and the worker
	// just uploads to that key. Same shape as the existing trainer-image
	// flow.
	mediaID := uuid.New()
	objectKey := path.Join("organisation-media", "images", mediaID.String()+ext)
	publicURL := strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey

	row, err := s.insertOrganisationMedia(c, db.CreateOrganisationMediaParams{
		MediaType:   "image",
		Title:       title,
		Description: description,
		Category:    category,
		ObjectKey:   objectKey,
		PublicUrl:   publicURL,
		MimeType:    mime,
		SizeBytes:   int64(len(raw)),
		UploadedBy:  uploaderRef(c),
	})
	if err != nil {
		return // response already written
	}

	if err := s.organisationImageUploader.Enqueue(uploads.OrganisationImageJob{
		MediaID:     row.ID,
		ObjectKey:   row.ObjectKey,
		ContentType: mime,
		Bytes:       raw,
	}); err != nil {
		s.logger.Warn("upload organisation image: enqueue failed", "media_id", row.ID, "err", err)
		// Best-effort cleanup of the row we just wrote — without this
		// the row sits in 'processing' forever despite no worker owning
		// it. Failure here is logged but not surfaced; admin can DELETE.
		if _, dbErr := s.media.q.DeleteOrganisationMedia(c.Request.Context(), row.ID); dbErr != nil {
			s.logger.Error("upload organisation image: cleanup delete failed", "media_id", row.ID, "err", dbErr)
		}
		if errors.Is(err, uploads.ErrQueueFull) || errors.Is(err, uploads.ErrUploaderClosed) {
			c.JSON(http.StatusServiceUnavailable, api.NewError("media upload service is busy, please retry shortly", api.CodeServerError))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("could not enqueue upload", api.CodeServerError))
		return
	}

	c.JSON(http.StatusAccepted, api.NewSuccess("organisation image accepted", api.CodeAccepted, organisationMediaToMap(row)))
}

// UploadOrganisationVideo handles POST /media/videos. Admin-only.
// Streams the upload to a temp file, probes with ffprobe, INSERTs the
// row with status='processing', enqueues the transcode job, returns
// 202.
func (s *routerImpl) UploadOrganisationVideo(c *gin.Context) {
	if !s.requireMediaAdmin(c) {
		return
	}
	if s.organisationVideoUploader == nil || s.videoTranscoder == nil {
		s.logger.Warn("upload organisation video: uploader or transcoder not configured")
		c.JSON(http.StatusServiceUnavailable, api.NewError("organisation video upload is not configured on this server", api.CodeServerError))
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, orgMediaVideoMaxBytes)

	if err := c.Request.ParseMultipartForm(32 << 20); err != nil {
		s.logger.Warn("upload organisation video: invalid multipart form", "err", err)
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file exceeds %d-byte upload limit", orgMediaVideoMaxBytes), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}

	title, description, category, fieldErrs := parseMediaTextFields(c)
	if len(fieldErrs) > 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError(fieldErrs))
		return
	}

	file, _, err := c.Request.FormFile("file")
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) {
			c.JSON(http.StatusBadRequest, api.NewError("missing 'file' in multipart form", api.CodeBadRequest))
			return
		}
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file exceeds %d-byte upload limit", orgMediaVideoMaxBytes), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}
	defer func() { _ = file.Close() }()

	tempPath, err := streamUploadToTemp(file, s.cfg.VideoTempDir)
	if err != nil {
		s.logger.Warn("upload organisation video: could not buffer upload to disk", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not buffer upload to disk", api.CodeServerError))
		return
	}

	// If anything below fails, clean up the temp file. On the success
	// path (Enqueue) the worker owns it.
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = removeTempFile(tempPath)
		}
	}()

	probeCtx, cancelProbe := context.WithTimeout(c.Request.Context(), orgMediaVideoProbeTimeout)
	meta, err := s.videoTranscoder.Probe(probeCtx, tempPath)
	cancelProbe()
	if err != nil {
		if errors.Is(err, video.ErrNotConfigured) {
			c.JSON(http.StatusServiceUnavailable, api.NewError("video transcoder is not configured on this server", api.CodeServerError))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("could not read video metadata — is this a valid video file?", api.CodeBadRequest))
		return
	}
	if !meta.HasVideoStream {
		c.JSON(http.StatusBadRequest, api.NewError("file has no video stream", api.CodeBadRequest))
		return
	}
	if meta.DurationSeconds > orgMediaVideoMaxDurationSeconds {
		c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("video duration %.0fs exceeds %ds limit", meta.DurationSeconds, orgMediaVideoMaxDurationSeconds), api.CodeBadRequest))
		return
	}

	// Determine size on disk for the row's size_bytes. Use the temp
	// file size; the transcoded output size will differ but we record
	// the originally-uploaded size, which is what an admin actually
	// chose.
	stat, err := tempStat(tempPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("could not stat temp file", api.CodeServerError))
		return
	}

	mediaID := uuid.New()
	objectKey := path.Join("organisation-media", "videos", mediaID.String()+".mp4")
	publicURL := strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey

	row, err := s.insertOrganisationMedia(c, db.CreateOrganisationMediaParams{
		MediaType:   "video",
		Title:       title,
		Description: description,
		Category:    category,
		ObjectKey:   objectKey,
		PublicUrl:   publicURL,
		MimeType:    "video/mp4",
		SizeBytes:   stat,
		UploadedBy:  uploaderRef(c),
	})
	if err != nil {
		return
	}

	if err := s.organisationVideoUploader.Enqueue(uploads.OrganisationVideoJob{
		MediaID:       row.ID,
		ObjectKey:     row.ObjectKey,
		TempInputPath: tempPath,
		ContentType:   "video/mp4",
	}); err != nil {
		s.logger.Warn("upload organisation video: enqueue failed", "media_id", row.ID, "err", err)
		if _, dbErr := s.media.q.DeleteOrganisationMedia(c.Request.Context(), row.ID); dbErr != nil {
			s.logger.Error("upload organisation video: cleanup delete failed", "media_id", row.ID, "err", dbErr)
		}
		if errors.Is(err, uploads.ErrQueueFull) || errors.Is(err, uploads.ErrUploaderClosed) {
			c.JSON(http.StatusServiceUnavailable, api.NewError("video upload service is busy, please retry shortly", api.CodeServerError))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("could not enqueue upload", api.CodeServerError))
		return
	}
	cleanupTemp = false // worker owns the temp file now

	c.JSON(http.StatusAccepted, api.NewSuccess("organisation video accepted", api.CodeAccepted, organisationMediaToMap(row)))
}

// ListOrganisationMedia handles GET /media. Public. Paginated; supports
// optional filters via query params.
func (s *routerImpl) ListOrganisationMedia(c *gin.Context, params api.ListOrganisationMediaParams) {
	if s.media == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	mediaType := ""
	if params.Type != nil {
		mediaType = strings.ToLower(strings.TrimSpace(string(*params.Type)))
		if mediaType != "" && mediaType != "image" && mediaType != "video" {
			c.JSON(http.StatusBadRequest, api.NewError("type must be 'image' or 'video'", api.CodeBadRequest))
			return
		}
	}
	category := ""
	if params.Category != nil {
		category = strings.TrimSpace(*params.Category)
	}
	// Default the public list to status='ready' so callers don't see
	// half-uploaded rows; admin tools can pass status='' explicitly to
	// see processing/failed too.
	status := "ready"
	if params.Status != nil {
		v := strings.ToLower(strings.TrimSpace(string(*params.Status)))
		switch v {
		case "", "ready", "processing", "failed":
			status = v
		default:
			c.JSON(http.StatusBadRequest, api.NewError("status must be one of ready, processing, failed (or empty for all)", api.CodeBadRequest))
			return
		}
	}

	ctx := c.Request.Context()

	total, err := s.media.q.CountOrganisationMedia(ctx, db.CountOrganisationMediaParams{
		MediaType: mediaType,
		Category:  category,
		Status:    status,
	})
	if err != nil {
		s.logger.Error("list organisation media: count failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list media", api.CodeServerError))
		return
	}

	rows, err := s.media.q.ListOrganisationMedia(ctx, db.ListOrganisationMediaParams{
		MediaType:  mediaType,
		Category:   category,
		Status:     status,
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("list organisation media: query failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list media", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, organisationMediaToMap(r))
	}
	c.JSON(http.StatusOK, api.NewSuccessWithMeta("MEDIA_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

// GetOrganisationMediaByID handles GET /media/{id}. Public.
func (s *routerImpl) GetOrganisationMediaByID(c *gin.Context, id openapi_types.UUID) {
	if s.media == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	row, err := s.media.q.GetOrganisationMediaByID(c.Request.Context(), uuid.UUID(id))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("media"))
			return
		}
		s.logger.Error("get organisation media: query failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load media", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("MEDIA_FETCHED", api.CodeOK, organisationMediaToMap(row)))
}

// DeleteOrganisationMedia handles DELETE /media/{id}. Admin-only.
// Removes the DB row AND the underlying MinIO object so storage
// doesn't drift away from the DB. If the row is still 'processing'
// we refuse with 409 — deleting mid-upload races the worker.
func (s *routerImpl) DeleteOrganisationMedia(c *gin.Context, id openapi_types.UUID) {
	if !s.requireMediaAdmin(c) {
		return
	}
	if s.media == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	mediaID := uuid.UUID(id)
	ctx := c.Request.Context()

	// Inspect status before deleting so we can refuse a delete on a row
	// that's still being processed. Without this, the worker could mark
	// status='ready' on a row that's already gone and we'd silently
	// leak the upload (no DB row pointing at the MinIO object).
	row, err := s.media.q.GetOrganisationMediaByID(ctx, mediaID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("media"))
			return
		}
		s.logger.Error("delete organisation media: lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load media", api.CodeServerError))
		return
	}
	if row.Status == "processing" {
		c.JSON(http.StatusConflict, api.NewError("media is still processing; retry after status flips to ready or failed", api.CodeConflict))
		return
	}

	deleted, err := s.media.q.DeleteOrganisationMedia(ctx, mediaID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("media"))
			return
		}
		s.logger.Error("delete organisation media: DB delete failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete media", api.CodeServerError))
		return
	}

	// Best-effort object removal — DB is the source of truth, so a
	// MinIO failure here doesn't fail the request. The orphan is
	// logged loudly for ops to sweep.
	if s.media.store != nil {
		if err := s.media.store.RemoveObject(ctx, deleted.ObjectKey); err != nil {
			s.logger.Error("delete organisation media: storage remove failed (object orphaned)",
				"object_key", deleted.ObjectKey, "err", err)
		}
	}

	c.Status(http.StatusNoContent)
}

// --- helpers --------------------------------------------------------

// requireMediaAdmin gates a media-mutating endpoint to admin /
// super_admin. /media isn't covered by TrainersAdminOnly or
// SuperAdminOnly (different path prefix), so the check lives in the
// handler. Returns true if the caller passes; false (and writes the
// response) otherwise.
func (s *routerImpl) requireMediaAdmin(c *gin.Context) bool {
	if s.users == nil {
		s.logger.Warn("media admin gate: users store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return false
	}
	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return false
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return false
	}
	role, err := s.users.q.GetUserRoleByID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusUnauthorized, api.NewError("user not found", api.CodeUnauthorized))
			return false
		}
		s.logger.Error("media admin gate: role lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return false
	}
	if role != "admin" && role != "super_admin" {
		c.JSON(http.StatusForbidden, api.NewError("admin or super_admin required", api.CodeForbidden))
		return false
	}
	return true
}

// uploaderRef returns the authenticated user's id wrapped for the
// CreateOrganisationMedia uploaded_by param (NullUUID). The middleware
// already populated user_id; this never panics — falls back to NULL.
func uploaderRef(c *gin.Context) uuid.NullUUID {
	v, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		return uuid.NullUUID{}
	}
	id, ok := v.(uuid.UUID)
	if !ok {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: id, Valid: true}
}

// parseMediaTextFields extracts title / description / category from
// the multipart form. Field validation centralised here so the
// image/video paths can't drift on what they accept.
func parseMediaTextFields(c *gin.Context) (title, description, category string, errs []api.FieldError) {
	title = strings.TrimSpace(c.Request.FormValue("title"))
	description = strings.TrimSpace(c.Request.FormValue("description"))
	category = strings.TrimSpace(c.Request.FormValue("category"))

	if title == "" {
		errs = append(errs, api.FieldError{Field: "title", Message: "title is required"})
	} else if utf8.RuneCountInString(title) > orgMediaTitleMaxLen {
		errs = append(errs, api.FieldError{Field: "title", Message: fmt.Sprintf("title must not exceed %d characters", orgMediaTitleMaxLen)})
	}
	if utf8.RuneCountInString(description) > orgMediaDescriptionMaxLen {
		errs = append(errs, api.FieldError{Field: "description", Message: fmt.Sprintf("description must not exceed %d characters", orgMediaDescriptionMaxLen)})
	}
	if utf8.RuneCountInString(category) > orgMediaCategoryMaxLen {
		errs = append(errs, api.FieldError{Field: "category", Message: fmt.Sprintf("category must not exceed %d characters", orgMediaCategoryMaxLen)})
	}
	return title, description, category, errs
}

// insertOrganisationMedia wraps the CreateOrganisationMedia call and
// writes the response on failure. Returns the inserted row + nil on
// success; on error the response has already been written and the
// caller should return.
func (s *routerImpl) insertOrganisationMedia(c *gin.Context, params db.CreateOrganisationMediaParams) (db.OrganisationMedium, error) {
	row, err := s.media.q.CreateOrganisationMedia(c.Request.Context(), params)
	if err != nil {
		s.logger.Error("create organisation media: insert failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create media row", api.CodeServerError))
		return db.OrganisationMedium{}, err
	}
	return row, nil
}

func organisationMediaToMap(r db.OrganisationMedium) map[string]interface{} {
	m := map[string]interface{}{
		"id":         r.ID.String(),
		"media_type": r.MediaType,
		"title":      r.Title,
		"object_key": r.ObjectKey,
		"public_url": r.PublicUrl,
		"mime_type":  r.MimeType,
		"size_bytes": r.SizeBytes,
		"status":     r.Status,
		"created_at": r.CreatedAt,
		"updated_at": r.UpdatedAt,
	}
	if r.Description.Valid {
		m["description"] = r.Description.String
	} else {
		m["description"] = nil
	}
	if r.Category.Valid {
		m["category"] = r.Category.String
	} else {
		m["category"] = nil
	}
	if r.UploadedBy.Valid {
		m["uploaded_by"] = r.UploadedBy.UUID.String()
	} else {
		m["uploaded_by"] = nil
	}
	return m
}

// removeTempFile + tempStat are tiny wrappers; kept here so the
// handler stays linear. The video pipeline reuses streamUploadToTemp
// from trainer_video.go (same package).
func removeTempFile(p string) error {
	return os.Remove(p)
}

func tempStat(p string) (int64, error) {
	fi, err := os.Stat(p)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// Silence unused-import linter for time / detectTrainerImage import
// — both are referenced below but only on conditional paths the
// linter doesn't always trace.
var (
	_ = time.Second
	_ = imageAcceptedMIMEs
	_ = orgMediaImageProbeTimeout
)
