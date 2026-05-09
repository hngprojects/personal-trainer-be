package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const swaggerUI = `<!DOCTYPE html>
<html>
<head>
  <title>Personal Trainer API Docs</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
<div id="swagger-ui"></div>
<script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
  SwaggerUIBundle({
    url: "/docs/spec",
    dom_id: '#swagger-ui',
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIBundle.SwaggerUIStandalonePreset],
    layout: "BaseLayout",
    deepLinking: true,
  })
</script>
</body>
</html>`

type DocsHandler struct {
	spec []byte
}

// NewDocsHandler accepts the OpenAPI spec read once at startup — avoids CWD dependency at runtime.
func NewDocsHandler(spec []byte) *DocsHandler { return &DocsHandler{spec: spec} }

// GET /docs — renders Swagger UI loaded from CDN
func (h *DocsHandler) UI(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, swaggerUI)
}

// GET /docs/spec — serves the raw OpenAPI spec
func (h *DocsHandler) Spec(c *gin.Context) {
	c.Data(http.StatusOK, "application/x-yaml", h.spec)
}
