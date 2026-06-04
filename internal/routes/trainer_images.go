package routes

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
)

const (
	// Per-file hard cap. Same as the avatar limit — gallery images and
	// avatars have the same size profile from a UX perspective.
	trainerImageMaxBytes = 5 << 20 // 5 MiB

	// Hard limit on total images a trainer can have. The handler counts
	// existing + incoming and rejects the whole request if the sum exceeds
	// this — partial success would be a confusing UX.
	trainerImagesMaxTotal = 5

	trainerImagesMultipartField = "images"

	// Wire-level cap so a malicious client can't stream gigabytes before
	// we reject. 5 files × 5 MiB plus generous multipart overhead.
	trainerImagesMaxRequestBytes = 30 << 20 // 30 MiB
)

// imageAcceptedMIMEs mirrors the avatar policy: JPEG/PNG/WebP/HEIC. HEIC
// >5MiB would need decoding which requires CGO; since the per-file cap is
// 5 MiB anyway, we accept HEIC as-is (no decode/compression needed).
var imageAcceptedMIMEs = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	"image/heic": ".heic",
}

// POST /trainers/{id}/images
func (s *routerImpl) UploadTrainerImages(c *gin.Context, id openapi_types.UUID) {
	if s.trainerImageUploader == nil {
		s.logger.Warn("upload trainer images: image uploader not configured")
		c.JSON(http.StatusServiceUnavailable, api.NewError("trainer image upload is not configured on this server", api.CodeServerError))
		return
	}
	if s.trainers == nil {
		s.logger.Warn("upload trainer images: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	// Pre-flight: trainer exists?
	if _, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("upload trainer images: trainer not found", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		s.logger.Warn("upload trainer images: failed to look up trainer", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	// Bound the body before parsing multipart.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trainerImagesMaxRequestBytes)

	if err := c.Request.ParseMultipartForm(trainerImagesMaxRequestBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.logger.Warn("upload trainer images: request too large", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("request exceeds %d-byte limit", trainerImagesMaxRequestBytes), api.CodeBadRequest))
			return
		}
		s.logger.Warn("upload trainer images: invalid multipart form", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}

	files := c.Request.MultipartForm.File[trainerImagesMultipartField]
	if len(files) == 0 {
		s.logger.Warn("upload trainer images: missing images files", "trainerID", trainerID.String())
		c.JSON(http.StatusBadRequest, api.NewError("missing 'images' files in multipart form", api.CodeBadRequest))
		return
	}
	if len(files) > trainerImagesMaxTotal {
		s.logger.Warn("upload trainer images: too many images per request", "trainerID", trainerID.String(), "count", len(files))
		c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("at most %d images per request", trainerImagesMaxTotal), api.CodeBadRequest))
		return
	}

	// Enforce the per-trainer 5-image cap: existing + incoming must be ≤ 5.
	// Atomic-ish — there's a TOCTOU window between this count and the
	// worker's INSERT, but worst case the worker inserts a 6th row, which
	// is fine because the UI never shows more than 5. The unique index on
	// (trainer_id, position) prevents corruption.
	existing, err := s.trainers.q.CountTrainerImages(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("upload trainer images: failed to count existing images", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not check existing images", api.CodeServerError))
		return
	}
	if int(existing)+len(files) > trainerImagesMaxTotal {
		s.logger.Warn("upload trainer images: trainer image limit exceeded", "trainerID", trainerID.String(), "existing", existing)
		c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("trainer already has %d images; can upload at most %d more", existing, trainerImagesMaxTotal-int(existing)), api.CodeBadRequest))
		return
	}

	// Read + validate every file BEFORE enqueueing any — all-or-nothing.
	// A failure on file #3 of 5 should not leave files 1 and 2 partially
	// uploaded as the admin tries to fix and re-upload.
	type pending struct {
		bytes     []byte
		mime      string
		objectKey string
		publicURL string
	}
	queue := make([]pending, 0, len(files))
	for _, fh := range files {
		if fh.Size > trainerImageMaxBytes {
			s.logger.Warn("upload trainer images: file exceeds size limit", "trainerID", trainerID.String(), "filename", fh.Filename, "size", fh.Size)
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file %q exceeds %d-byte limit", fh.Filename, trainerImageMaxBytes), api.CodeBadRequest))
			return
		}
		f, err := fh.Open()
		if err != nil {
			s.logger.Warn("upload trainer images: could not open file", "trainerID", trainerID.String(), "filename", fh.Filename, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("could not open uploaded file: "+err.Error(), api.CodeBadRequest))
			return
		}
		raw, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			s.logger.Warn("upload trainer images: could not read file", "trainerID", trainerID.String(), "filename", fh.Filename, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("could not read uploaded file: "+err.Error(), api.CodeBadRequest))
			return
		}

		mime, err := detectTrainerImage(raw)
		if err != nil {
			s.logger.Warn("upload trainer images: unsupported image format", "trainerID", trainerID.String(), "filename", fh.Filename, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		ext := imageAcceptedMIMEs[mime]
		objectKey := path.Join("trainer-images", trainerID.String(), uuid.NewString()+ext)
		publicURL := strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey

		queue = append(queue, pending{
			bytes:     raw,
			mime:      mime,
			objectKey: objectKey,
			publicURL: publicURL,
		})
	}

	// All validations passed — enqueue atomically. EnqueueBatch is
	// all-or-nothing: if the queue can't fit every job in the batch,
	// nothing is accepted and we return 503. Without this, a mid-batch
	// queue-full would leave us with partial acceptance — the client
	// would see 503 but workers would still be processing the first half,
	// breaking retry idempotence.
	jobs := make([]uploads.TrainerImageJob, 0, len(queue))
	urls := make([]string, 0, len(queue))
	for _, p := range queue {
		jobs = append(jobs, uploads.TrainerImageJob{
			TrainerID:   trainerID,
			ObjectKey:   p.objectKey,
			PublicURL:   p.publicURL,
			ContentType: p.mime,
			Bytes:       p.bytes,
		})
		urls = append(urls, p.publicURL)
	}
	if err := s.trainerImageUploader.EnqueueBatch(jobs); err != nil {
		if errors.Is(err, uploads.ErrQueueFull) || errors.Is(err, uploads.ErrUploaderClosed) {
			s.logger.Warn("upload trainer images: upload service busy", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusServiceUnavailable, api.NewError("image upload service is busy, please retry shortly", api.CodeServerError))
			return
		}
		s.logger.Warn("upload trainer images: could not enqueue upload", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("could not enqueue upload", api.CodeServerError))
		return
	}

	c.JSON(http.StatusAccepted, api.NewSuccess("Images upload accepted", api.CodeAccepted, map[string]interface{}{
		"image_urls": urls,
		"status":     "processing",
	}))
}

// GET /trainers/{id}/images
func (s *routerImpl) ListTrainerImages(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("list trainer images: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	trainerID := uuid.UUID(id)
	// Verify the trainer exists so a missing trainer returns 404 (matches the
	// OpenAPI spec and the upload/delete handlers' behaviour) instead of a
	// 200 with an empty array — which would mask a typo in the URL.
	if _, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("list trainer images: trainer not found", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		s.logger.Warn("list trainer images: failed to look up trainer", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}
	rows, err := s.trainers.q.ListTrainerImages(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("list trainer images: failed to list images", "trainerID", trainerID.String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list images", api.CodeServerError))
		return
	}
	out := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		out = append(out, map[string]interface{}{
			"id":        r.ID.String(),
			"image_url": r.ImageUrl,
			"position":  r.Position,
		})
	}
	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_IMAGES_FETCHED", api.CodeOK, out))
}

// DELETE /trainers/{id}/images/{image_id}
func (s *routerImpl) DeleteTrainerImage(c *gin.Context, id openapi_types.UUID, imageID openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("delete trainer image: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	rows, err := s.trainers.q.DeleteTrainerImage(c.Request.Context(), db.DeleteTrainerImageParams{
		ID:        uuid.UUID(imageID),
		TrainerID: uuid.UUID(id),
	})
	if err != nil {
		s.logger.Warn("delete trainer image: failed to delete image", "imageID", uuid.UUID(imageID).String(), "trainerID", uuid.UUID(id).String(), "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete image", api.CodeServerError))
		return
	}
	if rows == 0 {
		s.logger.Warn("delete trainer image: image not found", "imageID", uuid.UUID(imageID).String(), "trainerID", uuid.UUID(id).String())
		c.JSON(http.StatusNotFound, api.NewError("image not found for this trainer", api.CodeNotFound))
		return
	}
	// NOTE: we intentionally don't delete the underlying MinIO object here.
	// Same policy as avatars — orphan objects accumulate until a cleanup job
	// runs. Simpler now, can add a sweep later.
	c.Status(http.StatusNoContent)
}

// detectTrainerImage is the same MIME-sniff logic as the avatar handler.
// Kept inline rather than shared because the two handlers may diverge in
// what they accept (the avatar handler refuses HEIC > 5MiB; trainer images
// here never exceed 5MiB so the special case doesn't apply).
func detectTrainerImage(raw []byte) (string, error) {
	if len(raw) < 12 {
		return "", errors.New("file is too small to be a valid image")
	}
	// HEIC sniff first — http.DetectContentType doesn't know HEIC.
	if isHEIC(raw) {
		return "image/heic", nil
	}
	sniffed := http.DetectContentType(raw)
	if i := strings.Index(sniffed, ";"); i >= 0 {
		sniffed = sniffed[:i]
	}
	sniffed = strings.TrimSpace(sniffed)
	if _, ok := imageAcceptedMIMEs[sniffed]; !ok {
		return "", errors.New("unsupported image format. Accepted: jpeg, png, webp, heic")
	}
	return sniffed, nil
}
