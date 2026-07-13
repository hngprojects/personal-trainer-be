package feature_flags

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/common"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

// GetPublicFlags handles GET /api/v1/feature-flags — no auth required.
// Mobile clients call this on startup (and ideally before showing any
// feature gated by a flag) to learn which features are live. Response
// shape is a flat map of {key: bool} so adding a new flag is a
// non-breaking change for clients.
func (h *Handler) GetPublicFlags(c *gin.Context) {
	snapshot, err := h.svc.PublicSnapshot(c.Request.Context())
	if err != nil {
		h.log.Error("feature_flags: public snapshot failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to read feature flags", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("feature flags retrieved", api.CodeOK, snapshot))
}

// ListAdminFlags handles GET /api/v1/admin/feature-flags — admin only.
// Returns the full flag set including audit metadata so the admin UI
// can show "who set this when".
func (h *Handler) ListAdminFlags(c *gin.Context) {
	flags, err := h.svc.ListFlags(c.Request.Context())
	if err != nil {
		h.log.Error("feature_flags: admin list failed", "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to list feature flags", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("feature flags retrieved", api.CodeOK, flags))
}

type SetFlagRequest struct {
	Enabled bool   `json:"enabled"`
	Notes   string `json:"notes,omitempty"`
}

// SetFlag handles PUT /api/v1/admin/feature-flags/{key} — admin only.
// Upserts the row; if the key doesn't exist yet it's created, which
// keeps the endpoint stable when new flags are introduced server-side.
// The key is validated against a known-keys list so a typo can't
// silently create a no-op flag that the public read endpoint then
// dutifully serves to every mobile client forever.
func (h *Handler) SetFlag(c *gin.Context) {
	key := c.Param("key")
	if !isKnownKey(key) {
		h.log.Warn("feature_flags: rejected unknown key", "key", key)
		c.JSON(http.StatusBadRequest, api.NewError("unknown feature flag key", api.CodeBadRequest))
		return
	}

	var req SetFlagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, api.NewError("invalid request body", api.CodeBadRequest))
		return
	}

	// Capture who flipped the flag for the audit trail. Unauthenticated
	// shouldn't happen (this route is behind admin auth) but defend
	// against the context being malformed regardless.
	var updatedBy *uuid.UUID
	if v, ok := c.Get(string(common.ContextKeyUserID)); ok {
		if id, ok := v.(uuid.UUID); ok {
			updatedBy = &id
		}
	}

	flag, err := h.svc.SetFlag(c.Request.Context(), key, req.Enabled, updatedBy, req.Notes)
	if err != nil {
		h.log.Error("feature_flags: set failed", "key", key, "err", err)
		c.JSON(http.StatusInternalServerError, api.NewError("failed to update feature flag", api.CodeServerError))
		return
	}
	c.JSON(http.StatusOK, api.NewSuccess("feature flag updated", api.CodeOK, flag))
}

// knownKeys is the allowlist of flags the admin endpoint accepts.
// Add new flags here as you wire them — both to prevent typos creating
// orphan rows and to make the set of supported flags grep-able from
// one place.
func isKnownKey(key string) bool {
	switch key {
	case PaymentEnabled:
		return true
	default:
		return false
	}
}

// ErrUnknownKey is returned by the service layer when a caller looks up
// a key that doesn't exist. Exposed so tests can assert on it.
var ErrUnknownKey = errors.New("unknown feature flag key")
