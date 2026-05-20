package routes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/auth"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// trainersStore now carries the raw *sql.DB so the admin-create handler can
// run the user/trainer/benefits inserts inside one transaction. The existing
// callers that only need *db.Queries continue to use the .q field.
type trainersStore struct {
	db *sql.DB
	q  *db.Queries
}

func newTrainersStore(rawDB *sql.DB, q *db.Queries) *trainersStore {
	return &trainersStore{db: rawDB, q: q}
}

// allowedTrainerSpecializations mirrors the CHECK constraint added in
// migration 000037. Kept here as a Go-side allow-list so we can return a
// clean 400 instead of letting the DB raise a constraint violation.
var allowedTrainerSpecializations = map[string]struct{}{
	"yoga":      {},
	"speed":     {},
	"cardio":    {},
	"endurance": {},
	"strength":  {},
}

const (
	// Hard cap on the display-picture file. Mirrors the 5 MiB profile used by
	// the trainer gallery + user avatar endpoints.
	trainerDisplayPictureMaxBytes = 5 << 20 // 5 MiB

	// Wire-level cap on the whole multipart request: room for the picture +
	// generous overhead for the text fields and multipart boundaries.
	trainerCreateMaxRequestBytes = 10 << 20 // 10 MiB

	trainerDisplayPictureField = "display_picture"

	// Length of the auto-generated trainer password emailed to the trainer.
	// Matches the admin invite flow; 16 chars from the friendly charset is
	// well past any practical brute-force horizon against bcrypt.
	trainerGeneratedPasswordLen = 16

	// Max training_styles allowed by the DB CHECK + product spec.
	trainerTrainingStylesMax = 4
)

// trainerToMap renders a trainers row for client consumption. Benefits are not
// part of the trainers row (they live in a sibling table), so handlers that
// want them on the response pass them via trainerToMapWithBenefits.
func trainerToMap(t db.Trainer) map[string]interface{} {
	out := map[string]interface{}{
		"id":                t.ID.String(),
		"user_id":           t.UserID.String(),
		"specializations":   specializationsOut(t.Specializations),
		"training_styles":   trainingStylesOut(t.TrainingStyles),
		"onboarding_status": t.OnboardingStatus,
		"average_rating":    t.AverageRating,
		"total_reviews":     t.TotalReviews,
		"created_at":        t.CreatedAt,
		"updated_at":        t.UpdatedAt,
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

	return out
}

// trainerToMapWithBenefits is the variant used by responses that already
// loaded the trainer's benefits — POST /trainers in particular, where the
// admin just supplied them and the client expects them back in the 201.
func trainerToMapWithBenefits(t db.Trainer, benefits []db.TrainerBenefit) map[string]interface{} {
	out := trainerToMap(t)
	out["benefits"] = benefitsOut(benefits)
	return out
}

// specializationsOut always returns a non-nil slice so the JSON encoder emits
// `[]` rather than `null` for trainers with no specializations set. The DB
// default is `'{}'` so this rarely triggers, but the marshaling guarantee
// matters for clients that assume the field is always present.
func specializationsOut(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func trainingStylesOut(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func benefitsOut(in []db.TrainerBenefit) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(in))
	for _, b := range in {
		out = append(out, map[string]interface{}{
			"id":       b.ID.String(),
			"position": b.Position,
			"title":    b.Title,
			"subtext":  b.Subtext,
		})
	}
	return out
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

// POST /trainers
//
// Admin-only. Admin enters the trainer's email + name + initial profile
// fields; the server provisions the user account (role='trainer'), inserts
// the trainer row, writes the benefits, optionally enqueues the display
// picture upload, and mails the generated credentials.
//
// Transactional contract: user upsert + trainer insert + benefit inserts run
// inside a single SQL transaction so a partial write can never leave a user
// row with no matching trainer (or a trainer with only half its benefits).
// The picture enqueue and the credentials email happen AFTER commit — they
// are non-fatal: a failure logs loudly and the admin can re-invite (the
// upsert is idempotent on email) or upload the picture later.
func (s *routerImpl) CreateTrainer(c *gin.Context) {
	if s.trainers == nil || s.mailer == nil {
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	// Fail closed if the only mailer available is the no-op LogMailer in any
	// non-development environment. The trainer create path rotates a real
	// password and is useless if the credentials email never leaves the
	// server — a 201 with "credentials emailed" would be a lie, and the
	// trainer would be permanently locked out. Configure Resend or SMTP
	// before enabling trainer provisioning on staging/prod.
	if s.cfg != nil && s.cfg.Env != "development" {
		if _, isLog := s.mailer.(*email.LogMailer); isLog {
			c.JSON(http.StatusServiceUnavailable, api.NewError("mailer is not configured for credential delivery on this environment", api.CodeServerError))
			return
		}
	}

	// Bound the body before the multipart parser touches it.
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

	emailAddr := strings.ToLower(strings.TrimSpace(c.Request.FormValue("email")))
	if emailAddr == "" || !common.IsValidEmail(emailAddr) || len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "valid email is required (max 255 chars)"},
		}))
		return
	}

	name := strings.TrimSpace(c.Request.FormValue("name"))
	if name == "" {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "name", Message: "name is required"},
		}))
		return
	}

	specializations, err := parseTrainerSpecializations(c.Request.MultipartForm.Value["specializations"])
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}
	if len(specializations) == 0 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "specializations", Message: "at least one specialization is required (yoga, speed, cardio, endurance, strength)"},
		}))
		return
	}

	trainingStyles, err := parseTrainerTrainingStyles(c.Request.MultipartForm.Value["training_styles"])
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	benefits, err := parseTrainerBenefits(c.Request.MultipartForm.Value["benefits"])
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	yearsOfExperience, err := formIntPtr(c, "years_of_experience")
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("years_of_experience must be an integer", api.CodeBadRequest))
		return
	}

	onboardingStatus := "pending"
	if v := strings.TrimSpace(c.Request.FormValue("onboarding_status")); v != "" {
		switch v {
		case "pending", "approved", "rejected", "suspended":
			onboardingStatus = v
		default:
			c.JSON(http.StatusBadRequest, api.NewError("onboarding_status must be one of pending, approved, rejected, suspended", api.CodeBadRequest))
			return
		}
	}

	// Optional picture — validate up-front so we can refuse with NO trainer
	// row created if the picture is bad / the uploader is missing.
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
			// picture. The admin can retry once storage is configured.
			c.JSON(http.StatusServiceUnavailable, api.NewError("display picture storage is not configured on this server", api.CodeServerError))
			return
		}
		pictureBytes = raw
		pictureMIME = mime
		pictureExt = imageAcceptedMIMEs[mime]
	}

	// Generate the password BEFORE opening the TX so the password-hash work
	// happens outside the DB lock window. bcrypt at cost 12 takes ~250ms;
	// holding a TX open for that is wasteful.
	plaintextPassword, err := auth.GenerateRandomPassword(trainerGeneratedPasswordLen)
	if err != nil {
		s.logger.Error("create trainer: generate password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	passwordHash, err := auth.HashPassword(plaintextPassword)
	if err != nil {
		s.logger.Error("create trainer: hash password failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// TX boundary. Everything that touches the DB lives here so a failure
	// rolls back to a consistent state — no orphaned user without a trainer
	// row, no trainer with half its benefits.
	ctx := c.Request.Context()
	tx, err := s.trainers.db.BeginTx(ctx, nil)
	if err != nil {
		s.logger.Error("create trainer: begin tx failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	qtx := s.trainers.q.WithTx(tx)

	user, err := qtx.UpsertTrainerUser(ctx, db.UpsertTrainerUserParams{
		Email:    emailAddr,
		Name:     name,
		Password: sql.NullString{String: passwordHash, Valid: true},
	})
	if err != nil {
		s.logger.Error("create trainer: upsert user failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// 409 if the user already had a trainer profile. We do this AFTER the
	// upsert (rather than a pre-check) so the read sees the committed row
	// inside our TX — concurrent admin calls for the same email won't both
	// pass a pre-check and then both INSERT.
	if existing, err := qtx.GetTrainerByUserID(ctx, user.ID); err == nil {
		_ = existing
		c.JSON(http.StatusConflict, api.NewError("a trainer profile already exists for this email", api.CodeConflict))
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		s.logger.Error("create trainer: lookup existing trainer failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	trainer, err := qtx.CreateTrainer(ctx, db.CreateTrainerParams{
		UserID:            user.ID,
		Specializations:   specializations,
		TrainingStyles:    trainingStyles,
		YearsOfExperience: yearsOfExperience,
		DisplayPicture:    sql.NullString{Valid: false},
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		// Race against a concurrent create for the same user_id — the unique
		// constraint catches it; map to 409.
		if strings.Contains(err.Error(), "trainers_user_id_key") {
			c.JSON(http.StatusConflict, api.NewError("a trainer profile already exists for this email", api.CodeConflict))
			return
		}
		s.logger.Error("create trainer: insert trainer failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create trainer", api.CodeServerError))
		return
	}

	insertedBenefits := make([]db.TrainerBenefit, 0, len(benefits))
	for i, b := range benefits {
		row, err := qtx.AddTrainerBenefit(ctx, db.AddTrainerBenefitParams{
			TrainerID: trainer.ID,
			Position:  int32(i + 1),
			Title:     b.title,
			Subtext:   b.subtext,
		})
		if err != nil {
			s.logger.Error("create trainer: insert benefit failed", "err", err, "position", i+1)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to write benefit", api.CodeServerError))
			return
		}
		insertedBenefits = append(insertedBenefits, row)
	}

	if err := tx.Commit(); err != nil {
		s.logger.Error("create trainer: commit failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	committed = true

	// Post-commit side effects. Both are non-fatal: the trainer row is
	// already real and queryable.
	var pictureURL string
	if pictureBytes != nil {
		objectKey := path.Join("trainer-display-pictures", trainer.ID.String(), uuid.NewString()+pictureExt)
		pictureURL = strings.TrimRight(s.cfg.MinioPublicBaseURL, "/") + "/" + objectKey
		if err := s.trainerDisplayPictureUploader.Enqueue(uploads.TrainerDisplayPictureJob{
			TrainerID:   trainer.ID,
			ObjectKey:   objectKey,
			PublicURL:   pictureURL,
			ContentType: pictureMIME,
			Bytes:       pictureBytes,
		}); err != nil {
			// Queue full / closed — log it but don't undo the trainer create.
			// The admin can upload the picture via a future replace endpoint.
			s.logger.Error("create trainer: enqueue picture failed", "err", err, "trainer_id", trainer.ID.String())
			pictureURL = ""
		}
	}

	if err := s.mailer.SendTrainerCredentials(emailAddr, plaintextPassword); err != nil {
		// Trainer exists and is queryable — but we couldn't tell them.
		// Surface a clear 500 so the admin retries (UpsertTrainerUser is
		// idempotent and will rotate the password on retry).
		s.logger.Error("create trainer: send credentials email failed", "err", err, "user_id", user.ID.String())
		c.JSON(http.StatusInternalServerError, api.NewError("trainer created but credentials email failed; please retry", api.CodeServerError))
		return
	}

	payload := trainerToMapWithBenefits(trainer, insertedBenefits)
	payload["email"] = user.Email
	payload["name"] = user.Name
	if pictureURL != "" {
		payload["display_picture"] = pictureURL
		payload["display_picture_status"] = "processing"
	}
	c.JSON(http.StatusCreated, api.NewSuccess("trainer provisioned; credentials emailed", api.CodeCreated, payload))
}

// parseTrainerSpecializations accepts both a single CSV field
// ("yoga,cardio") and a multi-valued field repeated for each entry. Either
// form is convenient depending on the admin frontend; both reduce to the
// same []string after dedup + catalog validation.
func parseTrainerSpecializations(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		for _, v := range strings.Split(entry, ",") {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "" {
				continue
			}
			if _, ok := allowedTrainerSpecializations[v]; !ok {
				return nil, fmt.Errorf("invalid specialization %q (allowed: yoga, speed, cardio, endurance, strength)", v)
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	if len(out) > 5 {
		return nil, fmt.Errorf("at most 5 specializations allowed")
	}
	return out, nil
}

// parseTrainerTrainingStyles accepts the same CSV-or-multi-valued shapes as
// specializations. Each style must be a single word (no whitespace); we
// lowercase + dedup. Empty input is valid (returns empty slice).
func parseTrainerTrainingStyles(raw []string) ([]string, error) {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, entry := range raw {
		for _, v := range strings.Split(entry, ",") {
			v = strings.ToLower(strings.TrimSpace(v))
			if v == "" {
				continue
			}
			if strings.ContainsAny(v, " \t\r\n") {
				return nil, fmt.Errorf("training_styles entries must be single words (no whitespace): %q", v)
			}
			if _, dup := seen[v]; dup {
				continue
			}
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	if len(out) > trainerTrainingStylesMax {
		return nil, fmt.Errorf("at most %d training_styles allowed", trainerTrainingStylesMax)
	}
	return out, nil
}

// parsedBenefit is the handler-side representation of one benefit row we're
// about to INSERT. We don't expose it on responses — those use db.TrainerBenefit.
type parsedBenefit struct {
	title, subtext string
}

// parseTrainerBenefits expects a list of JSON-ish strings, one per benefit,
// each containing the keys "title" and "subtext". For multipart admin
// frontends sending repeated `benefits` fields it's clearer than the bracket
// notation that some form libraries don't emit consistently. Examples of
// accepted values for one entry:
//
//	{"title":"Personalized plans","subtext":"Tailored to your goals"}
//	title=Personalized plans|subtext=Tailored to your goals
//
// We accept either form to avoid forcing the frontend to JSON-encode.
func parseTrainerBenefits(raw []string) ([]parsedBenefit, error) {
	out := make([]parsedBenefit, 0, len(raw))
	for i, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		title, subtext, err := parseSingleBenefit(entry)
		if err != nil {
			return nil, fmt.Errorf("benefit at index %d: %w", i, err)
		}
		out = append(out, parsedBenefit{title: title, subtext: subtext})
	}
	return out, nil
}

func parseSingleBenefit(entry string) (string, string, error) {
	// JSON form: {"title":"...","subtext":"..."}.
	//
	// `id` and `position` are also accepted but IGNORED — the OpenAPI schema
	// marks both as readOnly, but Swagger UI still includes them in its
	// generated form examples and the upstream `position: 0` value would
	// otherwise trip DisallowUnknownFields. The server is the source of
	// truth for both: id is the freshly-minted PK, position is the
	// submitted-order index. We declare them here so the decoder doesn't
	// reject the payload, then discard the values.
	if strings.HasPrefix(entry, "{") {
		var obj struct {
			Title    string  `json:"title"`
			Subtext  string  `json:"subtext"`
			ID       *string `json:"id,omitempty"`
			Position *int    `json:"position,omitempty"`
		}
		if err := jsonDecode(entry, &obj); err != nil {
			return "", "", fmt.Errorf("invalid JSON: %w", err)
		}
		if strings.TrimSpace(obj.Title) == "" || strings.TrimSpace(obj.Subtext) == "" {
			return "", "", fmt.Errorf("title and subtext are required")
		}
		return strings.TrimSpace(obj.Title), strings.TrimSpace(obj.Subtext), nil
	}
	// Pipe-separated form: title=...|subtext=...
	parts := strings.Split(entry, "|")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("expected JSON {title,subtext} or 'title=...|subtext=...' form")
	}
	kv := map[string]string{}
	for _, p := range parts {
		eq := strings.IndexByte(p, '=')
		if eq < 0 {
			return "", "", fmt.Errorf("malformed segment %q (missing '=')", p)
		}
		kv[strings.TrimSpace(p[:eq])] = strings.TrimSpace(p[eq+1:])
	}
	title, subtext := kv["title"], kv["subtext"]
	if title == "" || subtext == "" {
		return "", "", fmt.Errorf("title and subtext are required")
	}
	return title, subtext, nil
}

// jsonDecode is a tiny wrapper around encoding/json that rejects unknown
// fields — admins typo'ing "title" / "subtext" should fail fast instead of
// silently losing data on the way to the DB.
func jsonDecode(s string, v interface{}) error {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
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
	if n < 0 {
		return sql.NullInt32{Valid: false}, fmt.Errorf("must be non-negative")
	}
	return sql.NullInt32{Int32: int32(n), Valid: true}, nil
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
// 200 -> TrainerResponse (data is Trainer + benefits)
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

	benefits, err := s.trainers.q.ListTrainerBenefits(c.Request.Context(), trainerID)
	if err != nil {
		// Don't 500 if benefits fetch fails — the trainer row is more
		// important. Log and return without benefits.
		s.logger.Error("get trainer: list benefits failed", "err", err, "trainer_id", trainerID.String())
		benefits = nil
	}

	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_FETCHED", api.CodeOK, trainerToMapWithBenefits(t, benefits)))
}

// PATCH /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
//
// Doesn't touch benefits — those have their own future endpoints. The handler
// applies COALESCE-style "leave unchanged if NULL" semantics on the SQL side
// for every field; pass an empty array to clear specializations/training_styles.
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

	// Specializations / training_styles: pass-through if nil, otherwise
	// validate the new value against the catalog / cardinality rules.
	var specializations []string
	if body.Specializations != nil {
		strs := make([]string, 0, len(*body.Specializations))
		for _, s := range *body.Specializations {
			strs = append(strs, string(s))
		}
		validated, err := parseTrainerSpecializations(strs)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		specializations = validated
	}

	var trainingStyles []string
	if body.TrainingStyles != nil {
		validated, err := parseTrainerTrainingStyles(*body.TrainingStyles)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		trainingStyles = validated
	}

	years := existing.YearsOfExperience
	if body.YearsOfExperience != nil {
		if *body.YearsOfExperience < 0 {
			c.JSON(http.StatusBadRequest, api.NewError("years_of_experience must be non-negative", api.CodeBadRequest))
			return
		}
		years = sql.NullInt32{Int32: int32(*body.YearsOfExperience), Valid: true}
	}

	// onboarding_status uses sqlc.narg in the query (nullable) so a NULL
	// argument leaves the column untouched. We only set Valid=true when the
	// caller actually supplied a new value; otherwise we pass an
	// invalid-NullString which COALESCE folds back to the existing column.
	var onboardingStatus sql.NullString
	if body.OnboardingStatus != nil {
		onboardingStatus = sql.NullString{String: string(*body.OnboardingStatus), Valid: true}
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

	updated, err := s.trainers.q.UpdateTrainer(c.Request.Context(), db.UpdateTrainerParams{
		ID:                trainerID,
		Specializations:   specializations,
		TrainingStyles:    trainingStyles,
		Bio:               bio,
		YearsOfExperience: years,
		IntroVideoUrl:     introVideoUrl,
		DisplayPicture:    displayPicture,
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

// silenceUnusedImports keeps imports compiled in if certain code paths get
// pruned. Currently a no-op — referenced names below.
var (
	_ = context.Background
	_ = email.NewLogMailer
)
