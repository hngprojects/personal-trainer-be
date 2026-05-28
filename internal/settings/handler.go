// Package settings backs the admin Settings page (general defaults,
// trainer rules, category catalog) plus the client-facing
// /categories list.
//
// Three routes are hand-wired in routes.go and documented in api.yaml
// for Swagger:
//
//   GET    /api/v1/admin/settings           — read settings + categories
//   PUT    /api/v1/admin/settings           — partial update of the four scalar settings
//   POST   /api/v1/admin/categories         — add a category (admin)
//   DELETE /api/v1/admin/categories/{id}    — remove a category (admin)
//   GET    /api/v1/categories               — public-ish list (auth required, same as /config/zoom)
//
// The four scalar settings live on a single-row admin_settings table
// (singleton-locked at the DB level). Categories live on their own
// table the admin can edit through the chip UI. Trainer
// specializations are still validated against the hardcoded CHECK
// constraint in migration 000037 — migrating them to reference this
// categories table is a deliberate follow-up so this PR stays
// focused.
package settings

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	db "github.com/hngprojects/personal-trainer-be/internal/repository/db"
)

// hard caps so a malformed PUT can't drift settings into nonsensical
// values that the FE then has to defensively handle. The DB CHECK
// constraints enforce the same bounds — these are the friendlier 400
// path so the error tells the client what's wrong before Postgres does.
const (
	minSessionMinutes = 5
	maxSessionMinutes = 480
	minTrainersList   = 1
	maxTrainersList   = 100
	maxCategoryName   = 60
)

// slug must be URL-safe: lowercase letters, digits, dashes only.
// Kept narrow on purpose — admins type these once and clients see
// them in URLs / query params, so we don't want surprises like
// emoji or whitespace landing in the catalog.
var slugRegex = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type Handler struct {
	q   *db.Queries
	log *slog.Logger
}

func NewHandler(q *db.Queries, log *slog.Logger) *Handler {
	return &Handler{q: q, log: log}
}

// Register wires the three groups. authMw enforces "must be logged
// in"; the admin routes additionally get adminMw (typically
// middleware.SuperAdminOnly) so the role gate is on the route itself
// instead of relying on oapi-codegen's middleware chain.
func (h *Handler) Register(group *gin.RouterGroup, authMw gin.HandlerFunc, adminMw gin.HandlerFunc) {
	group.GET("/admin/settings", authMw, adminMw, h.getSettings)
	group.PUT("/admin/settings", authMw, adminMw, h.updateSettings)
	group.POST("/admin/categories", authMw, adminMw, h.createCategory)
	group.DELETE("/admin/categories/:id", authMw, adminMw, h.deleteCategory)
	// Client-facing catalog. authMw only — every signed-in user can
	// read this; it's effectively the same trust level as /config/zoom.
	group.GET("/categories", authMw, h.listCategories)
}

// ─── DTOs ───────────────────────────────────────────────────────────

type settingsResponse struct {
	DefaultSessionDurationMin int32              `json:"default_session_duration_min"`
	MaxTrainersDisplayed      int32              `json:"max_trainers_displayed"`
	RequireVideoBeforeListing bool               `json:"require_video_before_listing"`
	AutoAssignTrainer         bool               `json:"auto_assign_trainer"`
	Categories                []categoryResponse `json:"categories"`
	UpdatedAt                 string             `json:"updated_at"`
}

type categoryResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Slug string    `json:"slug"`
}

// updateSettingsRequest uses pointers everywhere so "field absent"
// (no change) is distinguishable from "field present with default
// zero" (set to 0/false). The COALESCE in UpdateAdminSettings reads
// NULL as "leave existing"; nil pointers map to NULL.
type updateSettingsRequest struct {
	DefaultSessionDurationMin *int32 `json:"default_session_duration_min,omitempty"`
	MaxTrainersDisplayed      *int32 `json:"max_trainers_displayed,omitempty"`
	RequireVideoBeforeListing *bool  `json:"require_video_before_listing,omitempty"`
	AutoAssignTrainer         *bool  `json:"auto_assign_trainer,omitempty"`
}

type createCategoryRequest struct {
	Name string `json:"name" binding:"required"`
	// Slug optional — handler derives it from Name when absent.
	Slug string `json:"slug,omitempty"`
}

// ─── Handlers ───────────────────────────────────────────────────────

func (h *Handler) getSettings(c *gin.Context) {
	ctx := c.Request.Context()
	s, err := h.q.GetAdminSettings(ctx)
	if err != nil {
		h.log.Error("admin settings: load failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load settings", api.CodeServerError))
		return
	}
	cats, err := h.q.ListCategories(ctx)
	if err != nil {
		h.log.Error("admin settings: list categories failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load categories", api.CodeServerError))
		return
	}
	out := settingsResponse{
		DefaultSessionDurationMin: s.DefaultSessionDurationMin,
		MaxTrainersDisplayed:      s.MaxTrainersDisplayed,
		RequireVideoBeforeListing: s.RequireVideoBeforeListing,
		AutoAssignTrainer:         s.AutoAssignTrainer,
		Categories:                catsToResponse(cats),
		UpdatedAt:                 s.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, out))
}

func (h *Handler) updateSettings(c *gin.Context) {
	var req updateSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	if req.DefaultSessionDurationMin != nil {
		v := *req.DefaultSessionDurationMin
		if v < minSessionMinutes || v > maxSessionMinutes {
			c.JSON(http.StatusBadRequest, api.NewError("default_session_duration_min must be between 5 and 480", api.CodeBadRequest))
			return
		}
	}
	if req.MaxTrainersDisplayed != nil {
		v := *req.MaxTrainersDisplayed
		if v < minTrainersList || v > maxTrainersList {
			c.JSON(http.StatusBadRequest, api.NewError("max_trainers_displayed must be between 1 and 100", api.CodeBadRequest))
			return
		}
	}

	params := db.UpdateAdminSettingsParams{
		DefaultSessionDurationMin: nullInt32(req.DefaultSessionDurationMin),
		MaxTrainersDisplayed:      nullInt32(req.MaxTrainersDisplayed),
		RequireVideoBeforeListing: nullBool(req.RequireVideoBeforeListing),
		AutoAssignTrainer:         nullBool(req.AutoAssignTrainer),
	}
	updated, err := h.q.UpdateAdminSettings(c.Request.Context(), params)
	if err != nil {
		h.log.Error("admin settings: update failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update settings", api.CodeServerError))
		return
	}
	cats, err := h.q.ListCategories(c.Request.Context())
	if err != nil {
		// Settings persisted fine; just couldn't re-load the catalog.
		// Return what we have so the client doesn't think the PUT
		// failed.
		h.log.Warn("admin settings: list categories failed after update", "err", err)
	}
	out := settingsResponse{
		DefaultSessionDurationMin: updated.DefaultSessionDurationMin,
		MaxTrainersDisplayed:      updated.MaxTrainersDisplayed,
		RequireVideoBeforeListing: updated.RequireVideoBeforeListing,
		AutoAssignTrainer:         updated.AutoAssignTrainer,
		Categories:                catsToResponse(cats),
		UpdatedAt:                 updated.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
	c.JSON(http.StatusOK, api.NewSuccess("settings updated", api.CodeOK, out))
}

func (h *Handler) createCategory(c *gin.Context) {
	var req createCategoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		c.JSON(http.StatusBadRequest, api.NewError("name is required", api.CodeBadRequest))
		return
	}
	if len(name) > maxCategoryName {
		c.JSON(http.StatusBadRequest, api.NewError("name is too long (max 60 chars)", api.CodeBadRequest))
		return
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(name)
	}
	if !slugRegex.MatchString(slug) {
		c.JSON(http.StatusBadRequest, api.NewError("slug must be lowercase letters, digits, and single dashes (e.g. weight-loss)", api.CodeBadRequest))
		return
	}

	row, err := h.q.CreateCategory(c.Request.Context(), db.CreateCategoryParams{
		Name: name,
		Slug: slug,
	})
	if err != nil {
		// 23505 = unique_violation on either name or slug. Convert to a
		// clean 409 so the FE can show "already exists" instead of a
		// generic 500.
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			c.JSON(http.StatusConflict, api.NewError("a category with this name or slug already exists", api.CodeConflict))
			return
		}
		h.log.Error("admin settings: create category failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to create category", api.CodeServerError))
		return
	}
	c.JSON(http.StatusCreated, api.NewSuccess("category created", api.CodeCreated, categoryResponse{
		ID: row.ID, Name: row.Name, Slug: row.Slug,
	}))
}

func (h *Handler) deleteCategory(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid category id", api.CodeBadRequest))
		return
	}
	n, err := h.q.DeleteCategory(c.Request.Context(), id)
	if err != nil {
		h.log.Error("admin settings: delete category failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to delete category", api.CodeServerError))
		return
	}
	if n == 0 {
		c.JSON(http.StatusNotFound, api.NewError("category not found", api.CodeNotFound))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("category deleted", api.CodeOK, nil))
}

func (h *Handler) listCategories(c *gin.Context) {
	cats, err := h.q.ListCategories(c.Request.Context())
	if err != nil {
		h.log.Error("list categories failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to load categories", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, gin.H{
		"items": catsToResponse(cats),
	}))
}

// ─── helpers ────────────────────────────────────────────────────────

func catsToResponse(rows []db.Category) []categoryResponse {
	out := make([]categoryResponse, 0, len(rows))
	for _, r := range rows {
		out = append(out, categoryResponse{ID: r.ID, Name: r.Name, Slug: r.Slug})
	}
	return out
}

func nullInt32(p *int32) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: *p, Valid: true}
}

func nullBool(p *bool) sql.NullBool {
	if p == nil {
		return sql.NullBool{}
	}
	return sql.NullBool{Bool: *p, Valid: true}
}

// slugify produces a default slug from a display name. Lowercases,
// collapses whitespace into single dashes, strips anything else. Good
// enough for the common case ("Weight loss" → "weight-loss"); admins
// who want something different can pass `slug` explicitly.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		case r == ' ' || r == '-' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := b.String()
	return strings.TrimRight(out, "-")
}
