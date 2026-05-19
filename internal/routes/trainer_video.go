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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
	"github.com/hngprojects/personal-trainer-be/pkg/video"
)

const (
	// Hard wire-level cap for video uploads. Anything bigger we reject at the
	// multipart parser before allocating a temp file. 500 MiB covers 10 min of
	// 4K phone footage; if a real video is larger than this, something is
	// off (uncompressed, wrong codec, etc.).
	videoMaxUploadBytes = 500 << 20 // 500 MiB

	// Hard duration cap enforced by ffprobe before queuing. Trainers'
	// intro videos shouldn't be longer than this; rejects accidental
	// webinar uploads loudly instead of silently transcoding for an hour.
	videoMaxDurationSeconds = 10 * 60

	videoMultipartFieldName = "video"

	// Time budget for the ffprobe pre-flight check. Reading metadata from
	// a 500 MiB file is fast — seconds — but enforce a ceiling so a hung
	// ffprobe can't pin the request.
	videoProbeTimeout = 30 * time.Second
)

// POST /trainers/{id}/intro-video
//
// Generated method name = UploadTrainerIntroVideo; wired by oapi-codegen.
func (s *routerImpl) UploadTrainerIntroVideo(c *gin.Context, id openapi_types.UUID) {
	if s.videoUploader == nil || s.videoTranscoder == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("video upload is not configured on this server", api.CodeServerError))
		return
	}
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	// Verify the trainer exists before doing any expensive multipart parsing.
	// Cheap DB lookup; saves us writing a 500 MiB temp file just to discover
	// the trainerID was made up.
	if _, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	// Bound the body the multipart parser will read so a malicious caller
	// can't stream gigabytes before we reject it.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, videoMaxUploadBytes)

	file, _, err := c.Request.FormFile(videoMultipartFieldName)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) || strings.Contains(err.Error(), "missing form body") {
			c.JSON(http.StatusBadRequest, api.NewError("missing 'video' file in multipart form", api.CodeBadRequest))
			return
		}
		// Typed check beats substring matching — the stdlib's message can
		// drift and we'd silently start returning 400 for oversize uploads.
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file exceeds %d-byte upload limit", videoMaxUploadBytes), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}
	defer func() { _ = file.Close() }()

	// Stream the upload to a temp file rather than buffering in memory —
	// the worker needs a path for ffmpeg anyway, and disk-backed jobs let
	// the queue depth × worker count be sized for throughput without
	// blowing up RAM.
	tempPath, err := streamUploadToTemp(file, s.cfg.VideoTempDir)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("could not buffer upload to disk", api.CodeServerError))
		return
	}

	// If anything below fails, the temp file is our responsibility to clean
	// up. On the success path (Enqueue) the worker owns it.
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	// Pre-flight probe: confirm it's actually a video and the duration is
	// inside our limit. Cheap (reads only the file header / format box).
	probeCtx, cancel := context.WithTimeout(c.Request.Context(), videoProbeTimeout)
	meta, err := s.videoTranscoder.Probe(probeCtx, tempPath)
	cancel()
	if err != nil {
		// Server-config problem (no ffmpeg/ffprobe on host) ≠ client-supplied
		// garbage. Distinguish the two so the caller sees the right status:
		// 503 means "try again later / not us"; 400 means "fix your upload".
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
	if meta.DurationSeconds > videoMaxDurationSeconds {
		c.JSON(http.StatusBadRequest, api.NewError(fmt.Sprintf("video duration %.0fs exceeds %ds limit", meta.DurationSeconds, videoMaxDurationSeconds), api.CodeBadRequest))
		return
	}

	// Build the deterministic public URL. Worker writes this verbatim to
	// trainers.intro_video_url on success; we return the same string in
	// the 202 so the frontend can optimistically display it before the
	// transcode finishes.
	objectKey := path.Join("videos", "trainers", trainerID.String(), uuid.NewString()+".mp4")
	publicURL := strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey

	if err := s.videoUploader.Enqueue(uploads.VideoJob{
		TrainerID:     trainerID,
		ObjectKey:     objectKey,
		PublicURL:     publicURL,
		TempInputPath: tempPath,
		ContentType:   "video/mp4", // always mp4 after transcode
	}); err != nil {
		if errors.Is(err, uploads.ErrQueueFull) || errors.Is(err, uploads.ErrUploaderClosed) {
			c.JSON(http.StatusServiceUnavailable, api.NewError("video upload service is busy, please retry shortly", api.CodeServerError))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("could not enqueue upload", api.CodeServerError))
		return
	}

	// Worker now owns the temp file — don't delete it on our way out.
	cleanupTemp = false

	c.JSON(http.StatusAccepted, api.NewSuccess("Video upload accepted", api.CodeAccepted, map[string]interface{}{
		"intro_video_url": publicURL,
		"status":          "processing",
	}))
}

// streamUploadToTemp copies the multipart file body into a uniquely-named
// file in dir (defaulting to os.TempDir if empty) and returns its path.
// Caller is responsible for deleting the file unless ownership has been
// transferred to the worker via Enqueue.
func streamUploadToTemp(src io.Reader, dir string) (string, error) {
	if dir == "" {
		dir = os.TempDir()
	}
	tmp, err := os.CreateTemp(dir, "video-upload-*.bin")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("copy upload to temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("close temp file: %w", err)
	}
	return tmp.Name(), nil
}


// GET /trainers/{id}/intro-video/stream
//
// Returns a 302 redirect to the MinIO-hosted MP4. We deliberately do NOT
// proxy the bytes through this Go server — the Go process would otherwise
// become a bottleneck (every video byte copied through it costs latency,
// CPU, and memory). MinIO (and the nginx that fronts it on prod) is a
// purpose-built byte-server: it handles HTTP Range requests natively, which
// is what every video player uses to seek and to start playback before the
// whole file has downloaded. The transcoder also writes the MP4 with
// `+faststart` so the `moov` atom is at the start of the file and playback
// begins as soon as the player has the first few hundred KB.
//
// Why a backend endpoint at all rather than just exposing the URL in the
// trainer response?
//   - Decouples the public URL shape from the storage location. If we ever
//     swap MinIO for S3 / Cloudflare R2 / etc., the URL the frontend uses
//     (/trainers/{id}/intro-video/stream) stays the same — only the
//     redirect target changes.
//   - Easy place to add auth later (right now the bucket is public, but if
//     we move to private + presigned URLs, this is the natural hook).
//   - Lets us return 404 with a JSON body when there's no video — friendlier
//     than the bare MinIO 404 page.
func (s *routerImpl) StreamTrainerIntroVideo(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	t, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewError("trainer not found", api.CodeNotFound))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}
	if !t.IntroVideoUrl.Valid || t.IntroVideoUrl.String == "" {
		c.JSON(http.StatusNotFound, api.NewError("trainer has no intro video", api.CodeNotFound))
		return
	}

	// 302 (not 301) so the browser doesn't cache the redirect forever — the
	// URL could change if we move buckets/CDN. Players follow 302 just as
	// happily as 301 for video URLs.
	c.Redirect(http.StatusFound, t.IntroVideoUrl.String)
}
