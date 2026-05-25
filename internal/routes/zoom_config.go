// Public-config + universal-link endpoints needed by mobile/web clients
// to know how to join a Zoom meeting and to claim the deep-link domain.
//
//   GET /api/v1/config/zoom                    (auth required, JSON)
//   GET /.well-known/apple-app-site-association (no auth, JSON, no .json suffix per Apple spec)
//   GET /.well-known/assetlinks.json            (no auth, JSON)
//
// /config/zoom is what the mobile app polls on launch (or caches) to
// decide whether to use the SDK or to open the raw Zoom URL. Without
// it the app would have to bake the join_mode into a build flag, which
// would force a release to flip the flag.
package routes

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/hngprojects/personal-trainer-be/internal/api"
	"github.com/hngprojects/personal-trainer-be/internal/config"
	"github.com/hngprojects/personal-trainer-be/pkg/zoom"
)

type zoomConfigRoutes interface {
	register(group *gin.RouterGroup, authMw gin.HandlerFunc)
}

type zoomConfigHandler struct {
	cfg    *config.Config
	signer *zoom.SDKSigner // may be nil; sdk_key still surfaced from cfg
	log    *slog.Logger
}

func newZoomConfigHandler(cfg *config.Config, signer *zoom.SDKSigner, log *slog.Logger) zoomConfigRoutes {
	return &zoomConfigHandler{cfg: cfg, signer: signer, log: log}
}

func (h *zoomConfigHandler) register(group *gin.RouterGroup, authMw gin.HandlerFunc) {
	// authMw — even though the data isn't sensitive, gating it behind
	// auth keeps it out of public scrapers and lets us 401-by-default
	// from the mobile app's network layer when the user isn't signed in.
	group.GET("/config/zoom", authMw, h.read)
}

func (h *zoomConfigHandler) read(c *gin.Context) {
	c.JSON(http.StatusOK, api.NewSuccess("ok", api.CodeOK, gin.H{
		// "link" → open the raw join_url in Zoom; "sdk" → call
		// /sessions/{id}/join-info and host the call in-app.
		"join_mode":             h.cfg.ZoomJoinMode,
		"sdk_configured":        h.signer.IsConfigured(),
		"sdk_key":               h.cfg.ZoomSDKKey,
		"universal_link_domain": h.cfg.UniversalLinkDomain,
	}))
}

// registerWellKnown adds the AASA + assetlinks.json routes. These MUST
// live at the root of the domain (not under /api/v1) — iOS and Android
// hit absolute paths.
//
// We serve them generated from config rather than as static files so
// rotating an app team ID / signing cert is a single env-var change,
// not a deploy artefact swap.
func registerWellKnown(r *gin.Engine, cfg *config.Config, log *slog.Logger) {
	// AASA — Apple App Site Association. NO .json extension per Apple's
	// docs (they fetch without one and require Content-Type
	// application/json).
	r.GET("/.well-known/apple-app-site-association", func(c *gin.Context) {
		if cfg.IOSAppBundleID == "" || cfg.IOSAppTeamID == "" {
			c.JSON(http.StatusNotFound, gin.H{}) // not configured = no claim
			return
		}
		appID := cfg.IOSAppTeamID + "." + cfg.IOSAppBundleID
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, gin.H{
			"applinks": gin.H{
				"apps": []string{},
				"details": []gin.H{
					{
						// New-style "appIDs"; older iOS reads "appID" but
						// xcode-tooling on iOS 14+ prefers the array form.
						"appIDs": []string{appID},
						"components": []gin.H{
							{
								"/": "/sessions/*/join",
								"comment": "deep-link into the FitCall app's in-call screen",
							},
						},
					},
				},
			},
		})
	})

	r.GET("/.well-known/assetlinks.json", func(c *gin.Context) {
		if cfg.AndroidAppPackage == "" || cfg.AndroidAppSHA256 == "" {
			c.JSON(http.StatusNotFound, []gin.H{})
			return
		}
		c.Header("Content-Type", "application/json")
		c.JSON(http.StatusOK, []gin.H{
			{
				"relation": []string{"delegate_permission/common.handle_all_urls"},
				"target": gin.H{
					"namespace":                "android_app",
					"package_name":             cfg.AndroidAppPackage,
					"sha256_cert_fingerprints": []string{cfg.AndroidAppSHA256},
				},
			},
		})
	})

	log.Info("universal-link static files registered",
		"aasa", cfg.IOSAppBundleID != "" && cfg.IOSAppTeamID != "",
		"assetlinks", cfg.AndroidAppPackage != "" && cfg.AndroidAppSHA256 != "",
	)
}
