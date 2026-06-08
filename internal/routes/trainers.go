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
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
	"github.com/hngprojects/personal-trainer-be/internal/uploads"
	"github.com/hngprojects/personal-trainer-be/pkg/email"
)

// trainerPhoneE164Regex validates the phone_number form field on
// POST /trainers. Same shape the discovery-call phone_callback path
// uses so trainers and that flow share one phone format.
var trainerPhoneE164Regex = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

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
		"total_reviews":     t.TotalReviews,
		"created_at":        t.CreatedAt,
		"updated_at":        t.UpdatedAt,
	}
	if t.AverageRating.Valid {
		out["average_rating"] = t.AverageRating.Float64
	} else {
		out["average_rating"] = nil
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

// GET /trainers?category=&page=&limit=
//
// Paginated. Each item now carries the trainer's display name + email
// (joined from users) so the FE doesn't have to round-trip per row.
// Returns pagination metadata under "meta".
func (s *routerImpl) GetTrainers(c *gin.Context, params api.GetTrainersParams) {
	if s.trainers == nil {
		s.logger.Warn("get trainers: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	category := ""
	onboarding_status := ""
	if params.Category != nil {
		category = *params.Category
	}
	if params.OnboardingStatus != nil {
		if !params.OnboardingStatus.Valid() {
			c.JSON(http.StatusBadRequest, api.NewError("invalid onboarding_status; should be approved, pending, rejected or suspended", api.CodeBadRequest))
			return
		}
		onboarding_status = string(*params.OnboardingStatus)
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	total, err := s.trainers.q.CountTrainersForList(ctx, category)
	if err != nil {
		s.logger.Error("count trainers failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainers", api.CodeServerError))
		return
	}

	trainers, err := s.trainers.q.ListTrainers(ctx, db.ListTrainersParams{
		Category:         category,
		PageLimit:        int32(limit),
		PageOffset:       int32((page - 1) * limit),
		OnboardingStatus: onboarding_status,
	})
	if err != nil {
		s.logger.Error("list trainers failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to get trainers", api.CodeServerError))
		return
	}

	list := make([]interface{}, 0, len(trainers))
	for _, t := range trainers {
		list = append(list, trainerListRowToMap(t))
	}

	meta := api.NewPaginationMeta(page, limit, int(total))
	c.JSON(http.StatusOK, api.NewSuccessWithMeta("TRAINERS_FETCHED", api.CodeOK, list, meta))
}

// trainerListRowToMap renders one paginated /trainers list row. Differs
// from trainerToMap (used by GetTrainerByID) only in that the SQL row
// also carries the joined users.name + users.email — exposed on the
// response as "name" / "email" so the FE can render the trainer's
// person without a second call.
func trainerListRowToMap(t db.ListTrainersRow) map[string]interface{} {
	out := map[string]interface{}{
		"id":                t.ID.String(),
		"user_id":           t.UserID.String(),
		"name":              t.TrainerName,
		"email":             t.TrainerEmail,
		"specializations":   specializationsOut(t.Specializations),
		"training_styles":   trainingStylesOut(t.TrainingStyles),
		"onboarding_status": t.OnboardingStatus,
		"total_reviews":     t.TotalReviews,
		"created_at":        t.CreatedAt,
		"updated_at":        t.UpdatedAt,
	}
	if t.AverageRating.Valid {
		out["average_rating"] = t.AverageRating.Float64
	} else {
		out["average_rating"] = nil
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
	if t.TrainerGender.Valid {
		out["gender"] = t.TrainerGender.String
	} else {
		out["gender"] = nil
	}
	if t.TrainerPhoneNumber.Valid {
		out["phone_number"] = t.TrainerPhoneNumber.String
	} else {
		out["phone_number"] = nil
	}
	return out
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
		s.logger.Warn("create trainer: trainers store or mailer not available")
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
			s.logger.Warn("create trainer: mailer is LogMailer on non-development environment")
			c.JSON(http.StatusServiceUnavailable, api.NewError("mailer is not configured for credential delivery on this environment", api.CodeServerError))
			return
		}
	}

	// Bound the body before the multipart parser touches it.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, trainerCreateMaxRequestBytes)

	if err := c.Request.ParseMultipartForm(trainerCreateMaxRequestBytes); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			s.logger.Warn("form request exceeds allowed limit", "err", err)
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("request exceeds %d-byte limit", trainerCreateMaxRequestBytes), api.CodeBadRequest))
			return
		}
		s.logger.Warn("invalid multipart form submitted", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid multipart form: "+err.Error(), api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(c.Request.FormValue("email")))
	if emailAddr == "" || !common.IsValidEmail(emailAddr) || len(emailAddr) > 255 {
		s.logger.Warn("create trainer: invalid email", "email", emailAddr)
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "valid email is required (max 255 chars)"},
		}))
		return
	}

	name := strings.TrimSpace(c.Request.FormValue("name"))
	if name == "" {
		s.logger.Warn("create trainer: name is required", "email", emailAddr)
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "name", Message: "name is required"},
		}))
		return
	}

	specializations, err := parseTrainerSpecializations(c.Request.MultipartForm.Value["specializations"])
	if err != nil {
		s.logger.Warn("create trainer: invalid specializations", "email", emailAddr, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}
	if len(specializations) == 0 {
		s.logger.Warn("create trainer: no specializations provided", "email", emailAddr)
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "specializations", Message: "at least one specialization is required (yoga, speed, cardio, endurance, strength)"},
		}))
		return
	}

	trainingStyles, err := parseTrainerTrainingStyles(c.Request.MultipartForm.Value["training_styles"])
	if err != nil {
		s.logger.Warn("create trainer: invalid training styles", "email", emailAddr, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	benefits, err := parseTrainerBenefits(c.Request.MultipartForm.Value["benefits"])
	if err != nil {
		s.logger.Warn("create trainer: invalid benefits", "email", emailAddr, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
		return
	}

	yearsOfExperience, err := formIntPtr(c, "years_of_experience")
	if err != nil {
		s.logger.Warn("create trainer: invalid years of experience", "email", emailAddr, "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("years_of_experience must be an integer", api.CodeBadRequest))
		return
	}

	// bio is optional — trim and treat blank as NULL so we don't store
	// literal empty strings. Cap at 2000 characters (not bytes) to match
	// the api.yaml maxLength annotation. utf8.RuneCountInString counts
	// code points so multibyte characters (emoji, accented letters, CJK)
	// aren't unfairly rejected; plain len() would let a trainer with a
	// 1500-character English bio pass but reject the same trainer's
	// 1500-character Japanese bio.
	var bio sql.NullString
	if v := strings.TrimSpace(c.Request.FormValue("bio")); v != "" {
		if utf8.RuneCountInString(v) > 2000 {
			s.logger.Warn("create trainer: bio too long", "email", emailAddr, "bioLength", utf8.RuneCountInString(v))
			c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
				{Field: "bio", Message: "bio must not exceed 2000 characters"},
			}))
			return
		}
		bio = sql.NullString{String: v, Valid: true}
	}

	onboardingStatus := "pending"
	if v := strings.TrimSpace(c.Request.FormValue("onboarding_status")); v != "" {
		switch v {
		case "pending", "approved", "rejected", "suspended":
			onboardingStatus = v
		default:
			s.logger.Warn("create trainer: invalid onboarding status", "email", emailAddr, "onboardingStatus", v)
			c.JSON(http.StatusBadRequest, api.NewError("onboarding_status must be one of pending, approved, rejected, suspended", api.CodeBadRequest))
			return
		}
	}

	// gender is optional. Closed enum (mirrored by the users_gender_valid
	// CHECK constraint added in migration 000047) so a typo on the admin
	// form gets a clean 400 instead of a constraint violation at TX commit.
	// Empty/missing -> store NULL (handled by NULLIF in the SQL).
	gender := ""
	if v := strings.TrimSpace(c.Request.FormValue("gender")); v != "" {
		switch strings.ToLower(v) {
		case "male", "female", "other", "prefer_not_to_say":
			gender = strings.ToLower(v)
		default:
			s.logger.Warn("create trainer: invalid gender", "email", emailAddr, "gender", v)
			c.JSON(http.StatusBadRequest, api.NewError("gender must be one of male, female, other, prefer_not_to_say", api.CodeBadRequest))
			return
		}
	}

	// phone_number is optional. Must be E.164 if supplied — same regex
	// the discovery-call phone_callback field uses, so trainers and
	// callbacks share one shape.
	phoneNumber := ""
	if v := strings.TrimSpace(c.Request.FormValue("phone_number")); v != "" {
		if !trainerPhoneE164Regex.MatchString(v) {
			s.logger.Warn("create trainer: invalid phone number", "email", emailAddr)
			c.JSON(http.StatusBadRequest, api.NewError("phone_number must be in E.164 format (e.g. +2348012345678)", api.CodeBadRequest))
			return
		}
		phoneNumber = v
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
			s.logger.Warn("create trainer: display picture too large", "email", emailAddr, "size", fh.Size)
			c.JSON(http.StatusRequestEntityTooLarge, api.NewError(fmt.Sprintf("display_picture exceeds %d-byte limit", trainerDisplayPictureMaxBytes), api.CodeBadRequest))
			return
		}
		f, err := fh.Open()
		if err != nil {
			s.logger.Warn("create trainer: could not open display picture", "email", emailAddr, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("could not open display_picture: "+err.Error(), api.CodeBadRequest))
			return
		}
		raw, err := io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			s.logger.Warn("create trainer: could not read display picture", "email", emailAddr, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError("could not read display_picture: "+err.Error(), api.CodeBadRequest))
			return
		}
		mime, err := detectTrainerImage(raw)
		if err != nil {
			s.logger.Warn("create trainer: unsupported image format", "email", emailAddr, "err", err)
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		if s.trainerDisplayPictureUploader == nil {
			// Refuse rather than create the trainer with a silently-dropped
			// picture. The admin can retry once storage is configured.
			s.logger.Warn("create trainer: display picture uploader not configured", "email", emailAddr)
			c.JSON(http.StatusServiceUnavailable, api.NewError("display picture storage is not configured on this server", api.CodeServerError))
			return
		}
		pictureBytes = raw
		pictureMIME = mime
		pictureExt = imageAcceptedMIMEs[mime]
	}

	// Account-setup flow is required: the trainer will mint their own
	// password via the emailed link. Refuse the create if the handler isn't
	// wired (configuration bug) rather than fall back to the old plaintext
	// path and leave a dangling password-less account.
	if s.accountSetup == nil {
		s.logger.Error("create trainer: account setup handler not wired")
		c.JSON(http.StatusServiceUnavailable, api.NewError("account setup is not configured on this server", api.CodeServerError))
		return
	}

	// TX boundary. Everything that touches the DB lives here so a failure
	// rolls back to a consistent state — no orphaned user without a trainer
	// row, no trainer with half its benefits.
	//
	// Note: the user row is created with password = NULL. The trainer sets
	// their own password by consuming the activation token mailed below.
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
		Email:       emailAddr,
		Name:        name,
		Password:    sql.NullString{Valid: false},
		Gender:      gender,
		PhoneNumber: phoneNumber,
	})
	if err != nil {
		s.logger.Error("create trainer: upsert user failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}

	// If this user was previously invited and has already activated their
	// account, a re-invite would silently overwrite their password via a
	// new token — confusing for the trainer and a privilege issue. Refuse
	// with 409 and tell the admin to use the forgot-password path instead.
	if activated, err := s.accountSetup.IsActivated(ctx, user.ID); err != nil {
		s.logger.Error("create trainer: token status lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	} else if activated {
		c.JSON(http.StatusConflict, api.NewError("this account is already activated; ask the trainer to use forgot-password instead", api.CodeConflict))
		return
	}

	// 409 if the user already had a trainer profile. We do this AFTER the
	// upsert (rather than a pre-check) so the read sees the committed row
	// inside our TX — concurrent admin calls for the same email won't both
	// pass a pre-check and then both INSERT.
	if existing, err := qtx.GetTrainerByUserID(ctx, user.ID); err == nil {
		_ = existing
		s.logger.Warn("create trainer: trainer profile already exists", "email", emailAddr, "userID", user.ID.String())
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
		Bio:               bio,
		YearsOfExperience: yearsOfExperience,
		DisplayPicture:    sql.NullString{Valid: false},
		OnboardingStatus:  onboardingStatus,
	})
	if err != nil {
		// Race against a concurrent create for the same user_id — the unique
		// constraint catches it; map to 409.
		if strings.Contains(err.Error(), "trainers_user_id_key") {
			s.logger.Warn("create trainer: unique constraint violation on user_id", "email", emailAddr, "userID", user.ID.String())
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

	// Issue the activation token + send the setup link. UpsertToken is
	// idempotent on user_id so retrying (via a future resend endpoint) rotates
	// the token cleanly.
	setupLinkSent := true
	if err := s.accountSetup.IssueAndSend(ctx, user.ID, emailAddr, name); err != nil {
		s.logger.Error("create trainer: issue setup link failed", "err", err, "user_id", user.ID.String())
		setupLinkSent = false
	}

	payload := trainerToMapWithBenefits(trainer, insertedBenefits)
	payload["email"] = user.Email
	payload["name"] = user.Name
	// Echo gender + phone_number from the freshly-upserted user row so
	// admins get a confirmation of what was saved. Sourced from the
	// returned User (post-NULLIF) — if the admin omitted them the
	// stored value is NULL and we emit JSON null.
	if user.Gender.Valid {
		payload["gender"] = user.Gender.String
	} else {
		payload["gender"] = nil
	}
	if user.PhoneNumber.Valid {
		payload["phone_number"] = user.PhoneNumber.String
	} else {
		payload["phone_number"] = nil
	}
	if pictureURL != "" {
		payload["display_picture"] = pictureURL
		payload["display_picture_status"] = "processing"
	}

	// Admin broadcast — staff dashboard surfaces new trainer onboarding
	// without anyone having to refresh. Keyed on the trainer record id
	// (not user id) so a re-invite of the SAME trainer that ran through
	// UpsertTrainerUser doesn't double-fire — the trainer row's id is
	// stable across re-invites.
	if s.notificationService != nil {
		if _, notifErr := s.notificationService.SendNotificationToAdmins(ctx,
			"New Trainer Created",
			name+" was added as a trainer.",
			"trainer-created-"+trainer.ID.String(),
		); notifErr != nil {
			s.logger.Warn("admin notification (trainer created) failed", "trainer_id", trainer.ID, "err", notifErr)
		}
	}

	if setupLinkSent {
		c.JSON(http.StatusCreated, api.NewSuccess("trainer provisioned; setup link emailed", api.CodeCreated, payload))
	} else {
		c.JSON(http.StatusCreated, api.NewSuccess("trainer provisioned; setup link could not be sent — use the resend endpoint to retry", api.CodeCreated, payload))
	}
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
// 200 -> TrainerResponse (data is Trainer + name/email + benefits)
//
// Uses the joined GetTrainerWithUserByID query so the response includes
// the trainer's display name + email without an extra users lookup.
func (s *routerImpl) GetTrainerByID(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("get trainer by id: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	s.renderTrainerProfileByID(c, uuid.UUID(id))
}

// ToggleTrainerAvailability handles PATCH /trainers/me/availability/toggle.
// Flips the trainer's global is_available flag — the "open/closed" sign.
// When false, clients see no bookable slots for this trainer; the slots are
// preserved so toggling back on instantly restores the schedule.
func (s *routerImpl) ToggleTrainerAvailability(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("toggle trainer availability: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID, ok := s.resolveTrainerIDFromJWT(c)
	if !ok {
		return
	}

	var body struct {
		IsAvailable bool `json:"is_available"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		s.logger.Warn("toggle trainer availability: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	result, err := s.trainers.q.ToggleTrainerAvailability(c.Request.Context(), db.ToggleTrainerAvailabilityParams{
		ID:          trainerID,
		IsAvailable: body.IsAvailable,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Error("toggle trainer availability: DB error", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update availability", api.CodeServerError))
		return
	}

	// Invalidate the booking-slots cache so clients immediately see the
	// updated availability instead of serving stale slots from Redis.
	if s.availability != nil {
		if err := s.availability.redis.Delete(c.Request.Context(), bookingSlotsCacheKey(trainerID)); err != nil {
			s.logger.Warn("toggle trainer availability: failed to invalidate booking slots cache", "trainerID", trainerID, "err", err)
		}
	}

	c.JSON(http.StatusOK, api.NewSuccess("AVAILABILITY_UPDATED", api.CodeOK, map[string]interface{}{
		"trainer_id":   result.ID.String(),
		"is_available": result.IsAvailable,
	}))
}

// PatchTrainersMe handles PATCH /trainers/me — lets a trainer update their
// own bio, years_of_experience, specializations, display_picture, and phone_number.
func (s *routerImpl) PatchTrainersMe(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("patch trainers me: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID, ok := s.resolveTrainerIDFromJWT(c)
	if !ok {
		return
	}

	var body api.PatchTrainersMeJSONRequestBody
	if err := c.ShouldBindJSON(&body); err != nil {
		s.logger.Warn("patch trainers me: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	ctx := c.Request.Context()

	existing, err := s.trainers.q.GetTrainerByID(ctx, trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Error("patch trainers me: failed to load trainer", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	// Specializations
	specializations := existing.Specializations
	if body.Specializations != nil {
		strs := make([]string, 0, len(*body.Specializations))
		for _, sp := range *body.Specializations {
			strs = append(strs, string(sp))
		}
		validated, err := parseTrainerSpecializations(strs)
		if err != nil {
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		specializations = validated
	}

	years := existing.YearsOfExperience
	if body.YearsOfExperience != nil {
		if *body.YearsOfExperience < 0 {
			c.JSON(http.StatusBadRequest, api.NewError("years_of_experience must be non-negative", api.CodeBadRequest))
			return
		}
		years = sql.NullInt32{Int32: int32(*body.YearsOfExperience), Valid: true}
	}

	bio := existing.Bio
	if body.Bio != nil {
		bio = sql.NullString{String: *body.Bio, Valid: true}
	}

	displayPicture := existing.DisplayPicture
	if body.DisplayPicture != nil {
		displayPicture = sql.NullString{String: *body.DisplayPicture, Valid: true}
	}

	updated, err := s.trainers.q.UpdateTrainer(ctx, db.UpdateTrainerParams{
		ID:                trainerID,
		Specializations:   specializations,
		TrainingStyles:    existing.TrainingStyles,
		Bio:               bio,
		YearsOfExperience: years,
		IntroVideoUrl:     existing.IntroVideoUrl,
		DisplayPicture:    displayPicture,
		OnboardingStatus:  sql.NullString{},
	})
	if err != nil {
		s.logger.Error("patch trainers me: update trainer failed", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update profile", api.CodeServerError))
		return
	}

	// Update phone_number on the users table if supplied.
	if body.PhoneNumber != nil {
		phoneVal := strings.TrimSpace(*body.PhoneNumber)
		if _, err := s.trainers.q.UpdateTrainerUserProfile(ctx, db.UpdateTrainerUserProfileParams{
			ID:          updated.UserID,
			PhoneNumber: phoneVal,
		}); err != nil {
			s.logger.Error("patch trainers me: update user phone failed", "userID", updated.UserID, "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to update phone number", api.CodeServerError))
			return
		}
	}

	payload, status, errResp := s.buildTrainerProfilePayload(c, updated.ID)
	if errResp != nil {
		s.logger.Warn("patch trainers me: post-update reload failed", "trainerID", trainerID, "status", status)
		c.JSON(status, errResp)
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_PROFILE_UPDATED", api.CodeOK, payload))
}

// GetTrainersMe handles GET /trainers/me — returns the trainer profile
// for the authenticated user. The FE uses this to learn its own
// trainer.id without ever needing it in the URL: login -> JWT ->
// GET /trainers/me -> read trainer.id from response.
//
// Returns 404 when the calling user has no trainer profile (e.g. a
// plain client hitting the endpoint).
func (s *routerImpl) GetTrainersMe(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("get trainers me: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	trainerID, ok := s.resolveTrainerIDFromJWT(c)
	if !ok {
		return
	}
	s.renderTrainerProfileByID(c, trainerID)
}

// resolveTrainerIDFromJWT pulls the trainer.id for the authenticated
// user, writing the appropriate error response (401 / 404 / 500) and
// returning ok=false if anything goes wrong. Shared by the /trainers/me
// family.
func (s *routerImpl) resolveTrainerIDFromJWT(c *gin.Context) (uuid.UUID, bool) {
	userIDVal, exists := c.Get(string(common.ContextKeyUserID))
	if !exists {
		s.logger.Warn("trainers/me: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return uuid.Nil, false
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("trainers/me: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return uuid.Nil, false
	}
	trainer, err := s.trainers.q.GetTrainerByUserID(c.Request.Context(), userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile for this user"))
			return uuid.Nil, false
		}
		s.logger.Error("trainers/me: trainer lookup failed", "userID", userID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return uuid.Nil, false
	}
	return trainer.ID, true
}

// renderTrainerProfileByID writes the same response shape as
// GetTrainerByID (trainer fields + name/email joined from users +
// benefits). Pulled out so /trainers/{id} and /trainers/me share the
// identical payload contract.
func (s *routerImpl) renderTrainerProfileByID(c *gin.Context, trainerID uuid.UUID) {
	payload, status, errResp := s.buildTrainerProfilePayload(c, trainerID)
	if errResp != nil {
		c.JSON(status, errResp)
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_FETCHED", api.CodeOK, payload))
}

// buildTrainerProfilePayload loads the trainer + joined user + benefits
// and returns the JSON map every "Trainer" response uses. Centralized
// so PATCH /trainers/{id} and GET /trainers/{id} can't drift on
// fields they include — both go through this builder. Returns the
// payload OR (status, errResp) for the error path so the caller picks
// its own success message.
func (s *routerImpl) buildTrainerProfilePayload(c *gin.Context, trainerID uuid.UUID) (map[string]interface{}, int, *api.ErrorResponse) {
	row, err := s.trainers.q.GetTrainerWithUserByID(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("failed to get trainer by ID", "err", err)
		if errors.Is(err, sql.ErrNoRows) {
			resp := api.NewNotFoundError("trainer")
			return nil, http.StatusNotFound, &resp
		}
		s.logger.Warn("render trainer profile: DB error", "trainerID", trainerID, "err", err)
		resp := api.NewError("failed to get trainer", api.CodeServerError)
		return nil, http.StatusInternalServerError, &resp
	}

	benefits, err := s.trainers.q.ListTrainerBenefits(c.Request.Context(), trainerID)
	if err != nil {
		// Don't 500 if benefits fetch fails — the trainer row is more
		// important. Log and return without benefits.
		s.logger.Error("get trainer: list benefits failed", "err", err, "trainer_id", trainerID.String())
		benefits = nil
	}

	payload := trainerToMap(db.Trainer{
		ID:                row.ID,
		UserID:            row.UserID,
		Bio:               row.Bio,
		YearsOfExperience: row.YearsOfExperience,
		IntroVideoUrl:     row.IntroVideoUrl,
		DisplayPicture:    row.DisplayPicture,
		OnboardingStatus:  row.OnboardingStatus,
		AverageRating:     row.AverageRating,
		TotalReviews:      row.TotalReviews,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
		Specializations:   row.Specializations,
		TrainingStyles:    row.TrainingStyles,
	})
	payload["name"] = row.TrainerName
	payload["email"] = row.TrainerEmail
	if row.TrainerGender.Valid {
		payload["gender"] = row.TrainerGender.String
	} else {
		payload["gender"] = nil
	}
	if row.TrainerPhoneNumber.Valid {
		payload["phone_number"] = row.TrainerPhoneNumber.String
	} else {
		payload["phone_number"] = nil
	}
	payload["benefits"] = benefitsOut(benefits)
	return payload, http.StatusOK, nil
}

// PATCH /trainers/{id}
// 200 -> TrainerResponse (data is Trainer)
//
// Doesn't touch benefits — those have their own future endpoints. The handler
// applies COALESCE-style "leave unchanged if NULL" semantics on the SQL side
// for every field; pass an empty array to clear specializations/training_styles.
func (s *routerImpl) UpdateTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("update trainer: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)

	var body api.UpdateTrainerRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		s.logger.Warn("update trainers endpoint got invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	existing, err := s.trainers.q.GetTrainerByID(c.Request.Context(), trainerID)
	if err != nil {
		s.logger.Warn("failed to get trainer by ID ", "trainerID", trainerID, "err", err)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Warn("update trainer: DB error fetching trainer", "trainerID", trainerID, "err", err)
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
			s.logger.Warn("update trainer: invalid specializations", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		specializations = validated
	}

	var trainingStyles []string
	if body.TrainingStyles != nil {
		validated, err := parseTrainerTrainingStyles(*body.TrainingStyles)
		if err != nil {
			s.logger.Warn("update trainer: invalid training styles", "trainerID", trainerID.String(), "err", err)
			c.JSON(http.StatusBadRequest, api.NewError(err.Error(), api.CodeBadRequest))
			return
		}
		trainingStyles = validated
	}

	years := existing.YearsOfExperience
	if body.YearsOfExperience != nil {
		if *body.YearsOfExperience < 0 {
			s.logger.Warn("update trainer: negative years of experience", "trainerID", trainerID.String(), "years", *body.YearsOfExperience)
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
		if !body.OnboardingStatus.Valid() {
			c.JSON(http.StatusBadRequest, api.NewError("invalid onboarding_status", api.CodeBadRequest))
			return
		}
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
		s.logger.Warn("error while updating trainer", "trainerID", trainerID, "err", err)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update trainer", api.CodeServerError))
		return
	}

	// Re-load via the joined builder so the PATCH response includes the
	// users-side fields (name, email, gender, phone_number, benefits) —
	// the UpdateTrainer query returns only the trainers row, which on
	// its own would omit those and silently drift from the contract
	// GET /trainers/{id} advertises.
	payload, status, errResp := s.buildTrainerProfilePayload(c, updated.ID)
	if errResp != nil {
		// Update succeeded; only the re-load failed. Surface that
		// distinctly so the FE knows the write landed.
		s.logger.Warn("update trainer: post-update reload failed", "trainerID", trainerID, "status", status)
		c.JSON(status, errResp)
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("TRAINER_UPDATED", api.CodeOK, payload))
}

// DELETE /trainers/{id}
// 204 -> no content
func (s *routerImpl) DeleteTrainer(c *gin.Context, id openapi_types.UUID) {
	if s.trainers == nil {
		s.logger.Warn("delete trainer: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID := uuid.UUID(id)
	ctx := c.Request.Context()

	// Soft-delete: set users.is_active = false for this trainer's user account.
	// Returns ErrNoRows if the trainer doesn't exist OR is already inactive.
	_, err := s.trainers.q.DeactivateTrainer(ctx, trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Disambiguate the three possible no-row states:
			//  1. trainer row missing                → 404
			//  2. user row missing or role mismatch  → 500 (data integrity)
			//  3. user already inactive              → 409
			trainer, lookupErr := s.trainers.q.GetTrainerByID(ctx, trainerID)
			if errors.Is(lookupErr, sql.ErrNoRows) {
				c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
				return
			}
			if lookupErr != nil {
				s.logger.Warn("delete trainer: lookup failed during disambiguate", "trainerID", trainerID, "err", lookupErr)
				c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch trainer", api.CodeServerError))
				return
			}
			user, userErr := s.trainers.q.GetUserByID(ctx, trainer.UserID)
			if errors.Is(userErr, sql.ErrNoRows) {
				s.logger.Error("delete trainer: trainer has no linked user (data integrity)", "trainerID", trainerID, "userID", trainer.UserID)
				c.JSON(http.StatusInternalServerError, api.NewError("trainer data integrity error", api.CodeServerError))
				return
			}
			if userErr != nil {
				s.logger.Warn("delete trainer: failed to fetch linked user", "trainerID", trainerID, "err", userErr)
				c.JSON(http.StatusInternalServerError, api.NewError("failed to fetch trainer account", api.CodeServerError))
				return
			}
			if user.Role != "trainer" {
				s.logger.Error("delete trainer: linked user has unexpected role (data integrity)", "trainerID", trainerID, "userID", trainer.UserID, "role", user.Role)
				c.JSON(http.StatusInternalServerError, api.NewError("trainer data integrity error", api.CodeServerError))
				return
			}
			// Trainer exists, user exists, role is correct — already inactive.
			c.JSON(http.StatusConflict, api.NewError("trainer is already deactivated", api.CodeConflict))
			return
		}
		s.logger.Warn("delete trainer: failed to deactivate", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to deactivate trainer", api.CodeServerError))
		return
	}

	c.JSON(http.StatusOK, api.NewSuccess("trainer deactivated successfully", api.CodeOK, nil))
}

// GET /trainers/me/sessions?page=&limit=
//
// Paginated list of bookings where the calling user is the trainer. The
// authenticated user_id is mapped to a trainers.id via GetTrainerByUserID
// — non-trainers get 404 (no trainer profile) rather than 403, mirroring
// the existing /trainers/me/* behaviour.
// ListTrainerSessions handles GET /trainers/sessions?trainer_id=...
//
// Trainer_id is supplied as a query parameter rather than derived from
// the JWT — the earlier GET /trainers/me/sessions used to resolve the
// trainer via GetTrainerByUserID and 404'd whenever the caller wasn't
// a trainer themselves (admin dashboards, etc.). With the explicit
// trainer_id, admins can query any trainer's schedule and the trainer
// themselves can pass their own id.
//
// Authz: caller must be EITHER the owner of the trainer profile
// (caller.user_id == trainers.user_id) OR admin / super_admin.
func (s *routerImpl) ListTrainerSessions(c *gin.Context, params api.ListTrainerSessionsParams) {
	if s.trainers == nil {
		s.logger.Warn("ListTrainerSessions: trainers store is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("ListTrainerSessions: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	callerUserID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("ListTrainerSessions: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	trainerID := uuid.UUID(params.TrainerId)
	if trainerID == uuid.Nil {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "trainer_id", Message: "trainer_id is required"},
		}))
		return
	}

	ctx := c.Request.Context()

	// Look up the target trainer first — needed both for the 404 path
	// and for the authz check (we compare callerUserID against
	// trainer.UserID).
	trainer, err := s.trainers.q.GetTrainerByID(ctx, trainerID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer"))
			return
		}
		s.logger.Error("ListTrainerSessions: trainer lookup failed", "trainerID", trainerID, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	// Authz: trainer owner OR admin/super_admin. Owner check is the
	// cheap path; only hit the roles table if the caller isn't the
	// owner.
	if trainer.UserID != callerUserID {
		role, err := s.trainers.q.GetUserRoleByID(ctx, callerUserID)
		if err != nil {
			s.logger.Error("ListTrainerSessions: caller role lookup failed", "callerUserID", callerUserID, "err", err)
			c.JSON(http.StatusInternalServerError, api.NewError("failed to verify caller", api.CodeServerError))
			return
		}
		if role != "admin" && role != "super_admin" {
			c.JSON(http.StatusForbidden, api.NewError("you are not authorized to view this trainer's sessions", api.CodeForbidden))
			return
		}
	}

	total, err := s.trainers.q.CountBookingsByTrainer(ctx, trainer.ID)
	if err != nil {
		s.logger.Error("count trainer bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	rows, err := s.trainers.q.ListBookingsByTrainer(ctx, db.ListBookingsByTrainerParams{
		TrainerID:  trainer.ID,
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("list trainer bookings failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, trainerBookingRowToMap(r))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("SESSIONS_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

// GetTrainersMeSessions handles GET /trainers/me/sessions — convenience
// variant of /trainers/sessions?trainer_id=... that resolves the
// trainer from the JWT, so a trainer never has to look up their own
// trainer.id. Returns 404 when the caller has no trainer profile.
func (s *routerImpl) GetTrainersMeSessions(c *gin.Context, params api.GetTrainersMeSessionsParams) {
	if s.trainers == nil {
		s.logger.Warn("GetTrainersMeSessions: trainers store is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	trainerID, ok := s.resolveTrainerIDFromJWT(c)
	if !ok {
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	total, err := s.trainers.q.CountBookingsByTrainer(ctx, trainerID)
	if err != nil {
		s.logger.Error("GetTrainersMeSessions: count failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	rows, err := s.trainers.q.ListBookingsByTrainer(ctx, db.ListBookingsByTrainerParams{
		TrainerID:  trainerID,
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("GetTrainersMeSessions: list failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list sessions", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, trainerBookingRowToMap(r))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("SESSIONS_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

// GetTrainersMeClients handles GET /trainers/me/clients — paginated list of
// distinct clients who have at least one booking with the authenticated trainer.
func (s *routerImpl) GetTrainersMeClients(c *gin.Context, params api.GetTrainersMeClientsParams) {
	if s.trainers == nil {
		s.logger.Warn("GetTrainersMeClients: trainers store is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}

	userIDVal, ok := c.Get(string(common.ContextKeyUserID))
	if !ok {
		s.logger.Warn("GetTrainersMeClients: missing authenticated user in context")
		c.JSON(http.StatusUnauthorized, api.NewError("missing authenticated user", api.CodeUnauthorized))
		return
	}
	userID, ok := userIDVal.(uuid.UUID)
	if !ok {
		s.logger.Warn("GetTrainersMeClients: invalid user id type in context")
		c.JSON(http.StatusUnauthorized, api.NewError("invalid user id", api.CodeUnauthorized))
		return
	}

	page, limit, ok := parsePagination(c, params.Page, params.Limit, s.logger)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	trainer, err := s.trainers.q.GetTrainerByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.logger.Warn("GetTrainersMeClients: trainer profile not found", "userID", userID)
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile for this user"))
			return
		}
		s.logger.Error("GetTrainersMeClients: get trainer by user id failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load trainer", api.CodeServerError))
		return
	}

	total, err := s.trainers.q.CountTrainerClients(ctx, trainer.ID)
	if err != nil {
		s.logger.Error("GetTrainersMeClients: count failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list clients", api.CodeServerError))
		return
	}

	rows, err := s.trainers.q.ListTrainerClients(ctx, db.ListTrainerClientsParams{
		TrainerID:  trainer.ID,
		PageLimit:  int32(limit),
		PageOffset: int32((page - 1) * limit),
	})
	if err != nil {
		s.logger.Error("GetTrainersMeClients: list failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list clients", api.CodeServerError))
		return
	}

	list := make([]map[string]interface{}, 0, len(rows))
	for _, r := range rows {
		list = append(list, trainerClientRowToMap(r))
	}

	c.JSON(http.StatusOK, api.NewSuccessWithMeta("CLIENTS_FETCHED", api.CodeOK, list, api.NewPaginationMeta(page, limit, int(total))))
}

func trainerClientRowToMap(r db.ListTrainerClientsRow) map[string]interface{} {
	m := map[string]interface{}{
		"client_id":      r.ClientID.String(),
		"client_name":    r.ClientName,
		"client_email":   r.ClientEmail,
		"total_bookings": r.TotalBookings,
	}
	if r.ClientAvatar.Valid {
		m["client_avatar"] = r.ClientAvatar.String
	}
	if r.ClientGender.Valid {
		m["client_gender"] = r.ClientGender.String
	}
	if len(r.ClientFitnessGoals) > 0 {
		m["client_fitness_goals"] = r.ClientFitnessGoals
	}
	if r.ClientFitnessLevel.Valid {
		m["client_fitness_level"] = r.ClientFitnessLevel.String
	}
	m["last_booking_date"] = r.LastBookingDate
	return m
}

func trainerBookingRowToMap(r db.ListBookingsByTrainerRow) map[string]interface{} {
	m := map[string]interface{}{
		"id":           r.ID.String(),
		"trainer_id":   r.TrainerID.String(),
		"client_id":    r.ClientID.String(),
		"client_name":  r.ClientName,
		"client_email": r.ClientEmail,
	}
	if r.SessionID.Valid {
		m["session_id"] = r.SessionID.UUID.String()
	}
	if r.ScheduledStart.Valid {
		m["scheduled_start"] = r.ScheduledStart.Time
	}
	if r.ScheduledEnd.Valid {
		m["scheduled_end"] = r.ScheduledEnd.Time
	}
	if r.Timezone.Valid {
		m["timezone"] = r.Timezone.String
	}
	if r.BookingStatus.Valid {
		m["booking_status"] = r.BookingStatus.String
	}
	if r.SessionPlatform.Valid {
		m["session_platform"] = r.SessionPlatform.String
	}
	if r.CreatedAt.Valid {
		m["created_at"] = r.CreatedAt.Time
	}
	if r.CancelledAt.Valid {
		m["cancelled_at"] = r.CancelledAt.Time
	}
	if r.ZoomMeetingLink.Valid {
		m["zoom_meeting_link"] = r.ZoomMeetingLink.String
	}
	if r.SessionPlatform.Valid {
		m["session_platform"] = r.SessionPlatform.String
	}
	if r.PhoneNumber.Valid {
		m["client_phone_number"] = r.PhoneNumber.String
	}
	if r.MessengerHandle.Valid {
		m["client_messenger_handle"] = r.MessengerHandle.String
	}
	return m
}

// ResendTrainerSetup handles POST /trainers/resend-setup. Rotates the
// activation token for the trainer identified by email and re-sends
// the setup link — the same email body POST /trainers issues on first
// invite. Identifies by email (not trainer.id) because the admin
// inputting "the trainer didn't get my invite, send it again" already
// knows the email, never the internal UUID.
//
// 404 is returned for both "no such email" and "user exists but isn't
// a trainer" — same message in both cases as a small guard against
// admins probing which emails are trainers. (Less critical here than
// on unauthenticated endpoints, but still good practice.)
//
// 409 if the trainer has already activated. The right recovery path
// for an activated trainer who's locked out is forgot-password, not
// another setup link.
func (s *routerImpl) ResendTrainerSetup(c *gin.Context) {
	if s.trainers == nil {
		s.logger.Warn("resend trainer setup: trainers store not available")
		c.JSON(http.StatusServiceUnavailable, api.NewError("service unavailable", api.CodeServerError))
		return
	}
	if s.accountSetup == nil {
		s.logger.Warn("resend trainer setup: account setup handler is nil")
		c.JSON(http.StatusServiceUnavailable, api.NewError("account setup is not configured on this server", api.CodeServerError))
		return
	}

	var req api.ResendTrainerSetupJSONRequestBody
	if err := c.ShouldBindJSON(&req); err != nil {
		s.logger.Warn("resend trainer setup: invalid request body", "err", err)
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	emailAddr := strings.ToLower(strings.TrimSpace(string(req.Email)))
	if emailAddr == "" || !common.IsValidEmail(emailAddr) || len(emailAddr) > 255 {
		c.JSON(http.StatusBadRequest, api.NewValidationError([]api.FieldError{
			{Field: "email", Message: "valid email is required"},
		}))
		return
	}

	ctx := c.Request.Context()

	user, err := s.trainers.q.GetUserByEmail(ctx, emailAddr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile for this email"))
			return
		}
		s.logger.Error("resend trainer setup: user lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up user", api.CodeServerError))
		return
	}

	// Same 404 for "user found but not a trainer" so an admin can't
	// distinguish unregistered emails from non-trainer accounts.
	if _, err := s.trainers.q.GetTrainerByUserID(ctx, user.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, api.NewNotFoundError("trainer profile for this email"))
			return
		}
		s.logger.Error("resend trainer setup: trainer lookup failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to look up trainer", api.CodeServerError))
		return
	}

	activated, err := s.accountSetup.IsActivated(ctx, user.ID)
	if err != nil {
		s.logger.Error("resend trainer setup: activation check failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("internal server error", api.CodeServerError))
		return
	}
	if activated {
		c.JSON(http.StatusConflict, api.NewError("this account is already activated; ask the trainer to use forgot-password instead", api.CodeConflict))
		return
	}

	if err := s.accountSetup.IssueAndSend(ctx, user.ID, user.Email, user.Name); err != nil {
		s.logger.Error("resend trainer setup: issue + send failed", "err", err, "user_id", user.ID)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to send setup link; please retry", api.CodeServerError))
		return
	}

	s.logger.Info("resend trainer setup: link resent", "user_id", user.ID, "email_domain", emailDomainOrEmpty(user.Email))

	c.JSON(http.StatusOK, api.NewSuccess("setup link resent", api.CodeOK, map[string]interface{}{
		"email": user.Email,
	}))
}

// emailDomainOrEmpty pulls the domain off an email for log fields,
// avoiding logging the local-part. Returns "" if the input has no '@'.
func emailDomainOrEmpty(addr string) string {
	if i := strings.IndexByte(addr, '@'); i >= 0 && i+1 < len(addr) {
		return addr[i+1:]
	}
	return ""
}

// silenceUnusedImports keeps imports compiled in if certain code paths get
// pruned. Currently a no-op — referenced names below.
var (
	_ = context.Background
	_ = email.NewLogMailer
)
