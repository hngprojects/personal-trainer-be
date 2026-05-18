package routes

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
)

type trainersStore struct {
	q *db.Queries
}

func newTrainersStore(q *db.Queries) *trainersStore { return &trainersStore{q: q} }

func trainerToMap(t db.Trainer) map[string]interface{} {
	out := map[string]interface{}{
		"id":                 t.ID.String(),
		"user_id":            t.UserID.String(),
		"calendly_connected": t.CalendlyConnected,
		"onboarding_status":  t.OnboardingStatus,
		"average_rating":     t.AverageRating,
		"total_reviews":      t.TotalReviews,
		"created_at":         t.CreatedAt,
		"updated_at":         t.UpdatedAt,
	}

	if t.Specialization.Valid {
		out["specialization"] = t.Specialization.String
	} else {
		out["specialization"] = nil
	}
	if t.Bio.Valid {
		out["bio"] = t.Bio.String
	} else {
		out["bio"] = nil
	}
	if t.YearsOfExperience.Valid {
		out["years_of_experience"] = t.YearsOfExperience.Int32
	} else {
		out["years_of_experience"] = nil
	}
	if t.IntroVideoUrl.Valid {
		out["intro_video_url"] = t.IntroVideoUrl.String
	} else {
		out["intro_video_url"] = nil
	}
	if t.DisplayPicture.Valid {
		out["display_picture"] = t.DisplayPicture.String
	} else {
		out["display_picture"] = nil
	}
	if t.CalendlyLink.Valid {
		out["calendly_link"] = t.CalendlyLink.String
	} else {
		out["calendly_link"] = nil
	}

	return out
}

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt32Ptr(i *int) sql.NullInt32 {
	if i == nil {
		return sql.NullInt32{Valid: false}
	}
	if *i > math.MaxInt32 || *i < math.MinInt32 {
		return sql.NullInt32{Valid: false}
	}
	return sql.NullInt32{Int32: int32(*i), Valid: true}
}

// GET /trainers?category=...
// 200 -> TrainersListResponse (data is []Trainer)
func (s *routerImpl) GetTrainers(c *gin.Context, params api.GetTrainersParams) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	category := ""
	if params.Category != nil {
		category = *params.Category
	}

	trainers, err := s.trainers.q.ListTrainers(c.Request.Context(), category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainers", api.CodeServerError))
		return
	}

	list := make([]interface{}, 0, len(trainers))
	for _, t := range trainers {
		list = append(list, trainerToMap(t))
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINERS_FETCHED", api.CodeOK, list))
}

const (
	// Per-file hard cap for the display picture. Same 5 MiB profile as the
	// trainer gallery images and the avatar handler.
	trainerDisplayPictureMaxBytes = 5 << 20 // 5 MiB

	// Wire-level cap on the whole multipart request. 5 MiB picture plus
	// generous headroom for the text fields and multipart overhead.
	trainerCreateMaxRequestBytes = 10 << 20 // 10 MiB

	trainerDisplayPictureField = "display_picture"
)

// POST /trainers
//
// Multipart create. The caller's user_id is taken from the JWT (a trainer can
// only create their own profile), the optional display_picture file is
// uploaded asynchronously, and the intro video is uploaded separately via
// POST /trainers/{id}/intro-video.
//
// Failure-mode contract for the picture: the trainer row is INSERTed
// synchronously and returned 201 immediately. The picture's eventual URL is
// included in the response (predicted from the freshly-minted trainer.id) so
// the client can optimistically display it; the URL will 404 for the second
// or two while the background worker uploads, then start resolving once the
// worker UPDATEs trainers.display_picture. On worker failure the column
// stays NULL — ops sees a loud log line; the client can re-upload via PATCH
// down the line (TODO: dedicated picture-replace endpoint).
func (s *routerImpl) CreateTrainer(c *gin.Context) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	// Auth: derive the user_id from the JWT. The middleware always populates
	// this for authed routes; an absence means the route was reached without
	// auth, which is a server config bug we surface as 401 rather than 500.
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}

	// 409 if the caller already has a trainer profile. trainers.user_id has
	// a UNIQUE constraint, so a second INSERT would 23505 anyway — we just
	// turn the race-free common case into a friendlier response. (A racing
	// concurrent create would still 23505 at INSERT time; we map that too
	// below.)
	if _, err := s.trainers.q.GetTrainerByUserID(c.Request.Context(), userID); err == nil {
		c.JSON(http.StatusConflict, api.NewError("trainer profile already exists for this user", api.CodeConflict))
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusInternalServerError, api.NewError("failed to check existing trainer profile", api.CodeServerError))
		return
	}

	// Bound the body before the multipart parser sees it.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trainerCreateMaxRequestBytes)

	if err := c.Request.ParseMultipartForm(trainerCreateMaxRequestBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("request exceeds %d-byte limit", trainerCreateMaxRequestBytes), api.CodeBadRequest))
			return
		}
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}

	// Read text fields. All optional.
	specialization := formStringPtr(c, "specialization")
	bio := formStringPtr(c, "bio")
	yearsOfExperience, err := formIntPtr(c, "years_of_experience")
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("years_of_experience must be an integer", api.CodeBadRequest))
		return
	}
	calendlyLink := formStringPtr(c, "calendly_link")
	calendlyConnected, err := formBool(c, "calendly_connected")
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("calendly_connected must be a boolean", api.CodeBadRequest))
		return
	}
	onboardingStatus := "pending"
	if v := c.Request.FormValue("onboarding_status"); v != "" {
		switch v {
		case "pending", "approved", "rejected", "suspended":
			onboardingStatus = v
		default:
			c.JSON(http.StatusBadRequest, api.NewError("onboarding_status must be one of pending, approved, rejected, suspended", api.CodeBadRequest))
			return
		}
	}

	// Optional picture. Validate up-front (size + MIME) so we can refuse with
	// the trainer row still un-INSERTed if the picture is bad. Also refuse
	// 503 if the uploader isn't configured and a picture WAS supplied — we
	// don't want to silently drop the upload after creating the row.
	var (
		pictureBytes []byte
		pictureMIME  string
		pictureExt   string
	)
	if fh, _ := getOptionalFormFile(c, trainerDisplayPictureField); fh != nil {
		if fh.Size > trainerDisplayPictureMaxBytes {
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("display_picture exceeds %d-byte limit", trainerDisplayPictureMaxBytes), api.CodeBadRequest))
			return
		}
		f, err := fh.Open()
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError("could not open display_picture: "+err.Error(), api.CodeBadRequest))
			return
		}
		raw, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError("could not read display_picture: "+err.Error(), api.CodeBadRequest))
			return
		}
		mime, err := detectTrainerImage(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		if s.trainerDisplayPictureUploader == nil {
			// Refuse rather than create the trainer with a silently-dropped
			// picture. The client can retry once the server is fixed; mid-state
			// is worse than the 503.
			c.JSON(http.StatusServiceUnavailable, api.NewError("display picture storage is not configured on this server", api.CodeServerError))
			return
		}
		pictureBytes = raw
		pictureMIME = mime
		pictureExt = imageAcceptedMIMEs[mime]
	}

	// INSERT trainer with display_picture NULL — the worker (if any) fills
	// it in once the object lands in MinIO.
	created, err := s.trainers.q.CreateTrainer(c.Request.Context(), db.CreateTrainerParams{
		UserID:            userID,
		Specialization:    specialization,
		Bio:               bio,
		YearsOfExperience: yearsOfExperience,
		IntroVideoUrl:     sql.NullString{Valid: false},
		DisplayPicture:    sql.NullString{Valid: false},
		CalendlyConnected: calendlyConnected,
		CalendlyLink:      calendlyLink,
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		// Concurrent create race past the 409 pre-check would land on the
		// users_id unique constraint here.
		if strings.Contains(err.Error(), "trainers_user_id_key") || strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			c.JSON(http.StatusConflict, api.NewError("trainer profile already exists for this user", api.CodeConflict))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create trainer", api.CodeServerError))
		return
	}

	// Predict the picture URL from the freshly-minted trainer.id and queue
	// the upload. If the queue is full, the row is already created — log
	// loudly and return the row without a picture URL (the client can
	// re-upload later). This is intentionally non-fatal: we accept a
	// "trainer exists, picture missing" state over rolling back the INSERT,
	// which would itself be fallible.
	var pictureURL string
	if pictureBytes != nil {
		objectKey := path.Join("trainer-display-pictures", created.ID.String(), uuid.NewString()+pictureExt)
		pictureURL = strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey
		if err := s.trainerDisplayPictureUploader.Enqueue(uploads.TrainerDisplayPictureJob{
			TrainerID:   created.ID,
			ObjectKey:   objectKey,
			PublicURL:   pictureURL,
			ContentType: pictureMIME,
			Bytes:       pictureBytes,
		}); err != nil {
			// Don't fail the request — the trainer is created. Drop the
			// optimistic URL from the response so the client doesn't try to
			// display a picture that will never appear.
			pictureURL = ""
		}
	}

	// Build response. trainerToMap reflects the DB row (display_picture NULL),
	// so overlay the predicted URL if we successfully queued the upload.
	payload := trainerToMap(created)
	if pictureURL != "" {
		payload["display_picture"] = pictureURL
		payload["display_picture_status"] = "processing"
	}
	c.JSON(http.StatusCreated, api.NewSuccess("TRAINER_CREATED", api.CodeCreated, payload))
}

// formStringPtr returns *string for an optional text form field. Empty string
// is treated as "not supplied" so a client sending `bio=` doesn't store a
// literal empty string in the DB (we want NULL).
func formStringPtr(c *gin.Context, field string) sql.NullString {
	v := c.Request.FormValue(field)
	if v == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: v, Valid: true}
}

// formIntPtr parses an optional integer form field. Returns (NullInt32, nil)
// when absent; (NullInt32, err) on parse failure.
func formIntPtr(c *gin.Context, field string) (sql.NullInt32, error) {
	v := c.Request.FormValue(field)
	if v == "" {
		return sql.NullInt32{Valid: false}, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return sql.NullInt32{Valid: false}, err
	}
	if n > math.MaxInt32 || n < math.MinInt32 {
		return sql.NullInt32{Valid: false}, fmt.Errorf("value out of int32 range")
	}
	return sql.NullInt32{Int32: int32(n), Valid: true}, nil
}

// formBool parses an optional boolean form field. Default-false when absent —
// matches the schema's "calendly_connected defaults to false in DB" semantics.
func formBool(c *gin.Context, field string) (bool, error) {
	v := c.Request.FormValue(field)
	if v == "" {
		return false, nil
	}
	return strconv.ParseBool(v)
}

// getOptionalFormFile returns (nil, nil) when the file field is absent —
// distinguishing "no file supplied" from "supplied but unreadable" so the
// handler can treat the former as a valid, picture-less request.
func getOptionalFormFile(c *gin.Context, field string) (*multipart.FileHeader, error) {
	if c.Request.MultipartForm == nil {
		return nil, nil
	}
	files := c.Request.MultipartForm.File[field]
	if len(files) == 0 {
		return nil, nil
	}
	return files[0], nil
}

// GET /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
func (s *routerImpl) GetTrainerByID(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	t, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_FETCHED", api.CodeOK, trainerToMap(t)))
}

// PATCH /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
func (s *routerImpl) UpdateTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	var body api.UpdateTrainerRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	existing, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	specialization := existing.Specialization
	if body.Specialization != nil {
		specialization = sql.NullString{String: *body.Specialization, Valid: true}
	}

	years := existing.YearsOfExperience
	if body.YearsOfExperience != nil {
		years = sql.NullInt32{Int32: int32(*body.YearsOfExperience), Valid: true}
	}

	calendlyConnected := existing.CalendlyConnected
	if body.CalendlyConnected != nil {
		calendlyConnected = bool(*body.CalendlyConnected)
	}

	onboardingStatus := existing.OnboardingStatus
	if body.OnboardingStatus != nil {
		onboardingStatus = string(*body.OnboardingStatus)
	}

	bio := existing.Bio
	if body.Bio != nil {
		bio = sql.NullString{String: *body.Bio, Valid: true}
	}

	introVideoUrl := existing.IntroVideoUrl
	if body.IntroVideoUrl != nil {
		introVideoUrl = sql.NullString{String: *body.IntroVideoUrl, Valid: true}
	}

	displayPicture := existing.DisplayPicture
	if body.DisplayPicture != nil {
		displayPicture = sql.NullString{String: *body.DisplayPicture, Valid: true}
	}

	calendlyLink := existing.CalendlyLink
	if body.CalendlyLink != nil {
		calendlyLink = sql.NullString{String: *body.CalendlyLink, Valid: true}
	}

	updated, err := s.trainers.q.UpdateTrainer(c.Request.Context(), db.UpdateTrainerParams{
		ID:                trainerID,
		Specialization:    specialization,
		Bio:               bio,
		YearsOfExperience: years,
		IntroVideoUrl:     introVideoUrl,
		DisplayPicture:    displayPicture,
		CalendlyConnected: calendlyConnected,
		CalendlyLink:      calendlyLink,
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_UPDATED", api.CodeOK, trainerToMap(updated)))
}

// DELETE /trainers/{id}
// 204 -> no content
func (s *routerImpl) DeleteTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	_, err := s.trainers.q.DeleteTrainer(c.Request.Context(), trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete trainer", api.CodeServerError))
		return
	}

	c.Status(http.StatusNoContent)
}
