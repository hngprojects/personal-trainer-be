package routes

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	// Side-effect imports so image.Decode recognises these formats.
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
)

// heicSupportEnabled is false because decoding HEIC requires CGO (HEIC uses
// HEVC/AV1, neither of which has a pure-Go decoder, and our Makefile builds
// with CGO_ENABLED=0). The handler accepts HEIC bytes only when the file is
// already ≤5MB (no decode needed) and stores them as-is. HEIC files >5MB
// would require a decode-to-compress step that we can't perform without CGO,
// so those are rejected with a "convert client-side" message.
//
// To enable full HEIC support: set CGO_ENABLED=1 in the build, install
// libdav1d on the target host, add the goheif import and decode branch in
// compressToFit, and flip this flag.
const heicSupportEnabled = false

const (
	// Hard limit on what we'll read off the wire. Anything bigger gets 413.
	// We're willing to RECEIVE up to 50 MiB so a phone-camera HEIC can land
	// and we compress it down server-side; anything past that is almost
	// certainly not a real avatar.
	maxUploadBytes = 50 << 20 // 50 MiB

	// Files at or below this size are stored as-is. Files above are
	// re-encoded as JPEG with progressively lower quality.
	storeAsIsCeiling = 5 << 20 // 5 MiB

	// JPEG quality floor when compressing. We give up below this to avoid
	// shipping a heavily-degraded image; if we still can't fit under 5 MiB
	// at quality=30, the user has to send a smaller file.
	jpegQualityFloor = 30

	// maxPixelCount caps the decoded image dimensions. A pathological PNG/WebP
	// can be tiny on the wire (e.g. a few MiB of zeros that decompress to a
	// 20000x20000 bitmap = ~1.6 GiB of RGBA in memory). Refuse to decode
	// anything that would exceed this pixel budget; ~20 million pixels covers
	// all real-world phone-camera and 4K-screen sources (8K is ~33M, blocked).
	maxPixelCount = 20_000_000

	multipartFieldName = "picture"
)

// acceptedMIMEs maps the MIME we sniff from the bytes to a canonical file
// extension we'll use when constructing the object key.
var acceptedMIMEs = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
	// http.DetectContentType doesn't natively recognise HEIC; the handler
	// sniffs HEIC via the goheif decoder probe, see detectImage().
	"image/heic": ".heic",
}

// POST /users/me/profile/picture
//
// Generated method name = UploadProfilePicture; gin route is wired by the
// oapi-codegen middleware.
func (s *routerImpl) UploadProfilePicture(c *gin.Context) {
	if s.uploader == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("avatar storage is not configured on this server", api.CodeServerError))
		return
	}

	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	// Limit the body the multipart parser will read so a malicious client
	// can't stream gigabytes of "picture" into us before we reject it.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxUploadBytes)

	file, header, err := c.Request.FormFile(multipartFieldName)
	if err != nil {
		if errors.Is(err, http.ErrMissingFile) || strings.Contains(err.Error(), "missing form body") {
			c.JSON(http.StatusBadRequest, api.NewError("missing 'picture' file in multipart form", api.CodeBadRequest))
			return
		}
		// MaxBytesReader surfaces oversize as "http: request body too large".
		if strings.Contains(err.Error(), "request body too large") {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("file exceeds %d-byte upload limit", maxUploadBytes), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}
	defer func() { _ = file.Close() }()

	raw, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("could not read uploaded file", api.CodeBadRequest))
		return
	}

	// MIME-sniff from the bytes (don't trust header.Header.Get("Content-Type")).
	mimeType, err := detectImage(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	// Compress if oversize. Output of compression is always JPEG.
	bodyBytes := raw
	finalMIME := mimeType
	if len(bodyBytes) > storeAsIsCeiling {
		compressed, cerr := compressToFit(bodyBytes, mimeType, storeAsIsCeiling)
		if cerr != nil {
			c.JSON(http.StatusBadRequest, api.NewError("could not compress image to fit size limit: "+cerr.Error(), api.CodeBadRequest))
			return
		}
		bodyBytes = compressed
		finalMIME = "image/jpeg"
	}

	ext := acceptedMIMEs[finalMIME]
	objectKey := path.Join("avatars", userID.String(), uuid.NewString()+ext)
	publicURL := strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey

	// Enqueue the upload. ObjectKey is the bucket-relative path (passed to
	// Storage.PutObject); PublicURL is what gets written to users.avatar_url.
	// Keep them distinct — conflating them creates objects with full HTTP URLs
	// as keys inside the bucket.
	//
	// Bytes are passed by value (slice header copy); the underlying array
	// stays alive until the worker drops the job.
	if err := s.uploader.Enqueue(uploads.AvatarJob{
		UserID:      userID,
		ObjectKey:   objectKey,
		PublicURL:   publicURL,
		ContentType: finalMIME,
		Bytes:       bodyBytes,
	}); err != nil {
		if errors.Is(err, uploads.ErrQueueFull) || errors.Is(err, uploads.ErrUploaderClosed) {
			c.JSON(http.StatusServiceUnavailable, api.NewError("upload service is busy, please retry shortly", api.CodeServerError))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("could not enqueue upload", api.CodeServerError))
		return
	}

	_ = header // reserved for future filename/logging; quiet the linter

	c.JSON(http.StatusAccepted, api.NewSuccess("Avatar upload accepted", api.CodeAccepted, map[string]interface{}{
		"avatar_url": publicURL,
		"status":     "processing",
	}))
}

// detectImage inspects the leading bytes to determine the MIME. Returns one of
// the acceptedMIMEs keys on success. Distinguishes HEIC via goheif's probe
// since http.DetectContentType doesn't know it.
func detectImage(raw []byte) (string, error) {
	if len(raw) < 12 {
		return "", errors.New("file is too small to be a valid image")
	}

	// HEIC first — its ftyp box is at bytes 4..12 and unique enough that
	// goheif.ExtractExif (or just looking at the brand) is the cleanest
	// way to recognise it. The stdlib's DetectContentType returns
	// "application/octet-stream" for HEIC, so we check it explicitly.
	if isHEIC(raw) {
		return "image/heic", nil
	}

	sniffed := http.DetectContentType(raw)
	// DetectContentType returns things like "image/jpeg; charset=binary" —
	// trim parameters before lookup.
	if i := strings.Index(sniffed, ";"); i >= 0 {
		sniffed = sniffed[:i]
	}
	sniffed = strings.TrimSpace(sniffed)

	if _, ok := acceptedMIMEs[sniffed]; !ok {
		return "", errors.New("unsupported image format. Accepted: jpeg, png, webp, heic")
	}
	return sniffed, nil
}

// isHEIC checks for the HEIC/HEIF ftyp box. The first 4 bytes are the box
// size, then "ftyp", then a 4-char brand. HEIC brands include "heic", "heix",
// "hevc", "hevx", "mif1", and a few others. We accept the common subset.
func isHEIC(raw []byte) bool {
	if len(raw) < 12 {
		return false
	}
	if string(raw[4:8]) != "ftyp" {
		return false
	}
	brand := string(raw[8:12])
	switch brand {
	case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "heim", "heis":
		return true
	}
	return false
}

// compressToFit decodes the source image and re-encodes it as JPEG with
// progressively lower quality until the output is at or below targetSize.
// Returns the smallest output that fit; errors if even quality=jpegQualityFloor
// can't bring it under target.
func compressToFit(raw []byte, sourceMIME string, targetSize int) ([]byte, error) {
	if sourceMIME == "image/heic" && !heicSupportEnabled {
		// We accept HEIC uploads ≤5MB as-is (no decode required), but we
		// can't decompress oversized HEIC without a HEVC/AV1 decoder.
		// Surface a clear ask to convert client-side rather than a generic
		// "decode failed" so the mobile team knows exactly what to fix.
		return nil, errors.New("HEIC files larger than 5MB are not supported — please convert to JPEG before uploading")
	}

	// Read header-only to validate dimensions BEFORE allocating the full
	// pixel buffer. A small-on-wire-but-huge-when-decoded image (PNG of
	// repeating zeros decoding to 20000x20000) can OOM the worker; cheap
	// DecodeConfig parse rejects it before any allocation happens.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode source image config: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, errors.New("image has invalid dimensions")
	}
	if cfg.Width*cfg.Height > maxPixelCount {
		return nil, fmt.Errorf("image dimensions exceed limit (%dx%d > %d total pixels)", cfg.Width, cfg.Height, maxPixelCount)
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode source image: %w", err)
	}

	// Try descending qualities. Quality 85 is a sensible high-quality
	// starting point — most cameras shoot near this anyway.
	for quality := 85; quality >= jpegQualityFloor; quality -= 5 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, fmt.Errorf("encode jpeg at quality %d: %w", quality, err)
		}
		if buf.Len() <= targetSize {
			return buf.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("could not compress image under %d bytes even at jpeg quality %d", targetSize, jpegQualityFloor)
}
