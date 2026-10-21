// Package web embeds the administration page and static assets in the server.
package web

import (
	"context"
	"embed"
	"html/template"
	"net/http"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/app/server/render"
)

//go:embed templates/packet/*.html static/favicon.ico
var assets embed.FS

var templates = template.Must(template.ParseFS(assets, "templates/packet/*.html"))

// Renderer is also used by in-memory HTTP tests, which bypass Hertz's protocol
// initialization. All callers use the same immutable parsed templates.
func Renderer() render.HTMLRender {
	return render.HTMLProduction{Template: templates}
}

// Register installs the embedded page renderer and favicon routes.
func Register(h *server.Hertz) {
	h.SetHTMLTemplate(templates)
	icon, err := assets.ReadFile("static/favicon.ico")
	if err != nil {
		panic(err) // A missing bundled resource is a build defect.
	}
	h.GET("/favicon.ico", func(_ context.Context, c *app.RequestContext) {
		c.Data(http.StatusOK, "image/x-icon", icon)
	})
	h.HEAD("/favicon.ico", func(_ context.Context, c *app.RequestContext) {
		c.Status(http.StatusOK)
		c.Header("Content-Type", "image/x-icon")
	})
}
