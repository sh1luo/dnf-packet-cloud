package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"packet_cloud/biz/handler"
	packet "packet_cloud/biz/model/hertz/packet"
	"packet_cloud/internal/storage"
	"packet_cloud/web"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestManagementTemplateAndGeneratedRoutes(t *testing.T) {
	repo, err := storage.NewLocalFileSystem(filepath.Join(t.TempDir(), "packets"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	_, err = repo.Append(context.Background(), []*packet.CloudPacket{{
		Name: `<script>alert("x")</script>`, Region: "跨1", Channel: "1", Uploader: "tester", Time: "2026",
		UserPackets: []*packet.UserPacket{{Content: "never-render-content"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := server.New()
	// ut.PerformRequest bypasses the HTTP protocol initialization that normally
	// installs the HTML renderer on each RequestContext.
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		c.HTMLRender = web.Renderer()
		c.Next(ctx)
	})
	h.Use(handler.WithRepository(repo), handler.AuthorizeWrites("never-render-secret"))
	web.Register(h)
	register(h)
	r := ut.PerformRequest(h.Engine, "GET", "/v1/packet/edit", nil)
	if r.Code != 200 {
		t.Fatalf("page: %d %s", r.Code, r.Body)
	}
	body := r.Body.String()
	if !strings.Contains(body, "&lt;script&gt;") || strings.Contains(body, `id="admin-token"`) ||
		strings.Contains(body, "never-render-content") || strings.Contains(body, "never-render-secret") {
		t.Fatal("page failed escaping, summary or secret isolation")
	}
	p, err := repo.Get(context.Background(), 1)
	if err != nil || p.UserPackets[0].Content != "never-render-content" {
		t.Fatalf("page mutated details: %v %v", p, err)
	}
	r = ut.PerformRequest(h.Engine, "GET", "/healthz", nil)
	if r.Code != 200 {
		t.Fatalf("healthz: %d", r.Code)
	}
}

func TestEmbeddedAssetsOutsideRepository(t *testing.T) {
	t.Chdir(t.TempDir())
	h := server.New()
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		c.HTMLRender = web.Renderer()
		c.Next(ctx)
	})
	web.Register(h)
	h.GET("/page", func(_ context.Context, c *app.RequestContext) {
		c.HTML(200, "packet/online_edit.html", map[string]any{"packets": []*packet.CloudPacket{}})
	})
	r := ut.PerformRequest(h.Engine, "GET", "/page", nil)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "云端数据包管理") {
		t.Fatalf("embedded page: %d %s", r.Code, r.Body)
	}
	r = ut.PerformRequest(h.Engine, "GET", "/favicon.ico", nil)
	if r.Code != 200 || r.Body.Len() == 0 {
		t.Fatalf("embedded icon: %d", r.Code)
	}
	r = ut.PerformRequest(h.Engine, "HEAD", "/favicon.ico", nil)
	if r.Code != 200 || r.Body.Len() != 0 {
		t.Fatalf("embedded icon HEAD: %d, bytes=%d", r.Code, r.Body.Len())
	}
}
