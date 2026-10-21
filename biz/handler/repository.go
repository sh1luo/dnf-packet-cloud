package handler

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"strings"

	"packet_cloud/service/readwriter"

	"github.com/cloudwego/hertz/pkg/app"
)

const repositoryKey = "packet.repository"

func WithRepository(repo readwriter.Repository) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Set(repositoryKey, repo)
		c.Next(ctx)
	}
}

func Repository(c *app.RequestContext) readwriter.Repository {
	value, exists := c.Get(repositoryKey)
	if !exists {
		panic("packet repository middleware is not installed")
	}
	return value.(readwriter.Repository)
}

// AuthorizeWrites leaves reads public and protects all mutation methods when
// a token is configured. Tokens are never rendered into the management page.
func AuthorizeWrites(token string) app.HandlerFunc {
	expected := sha256.Sum256([]byte(token))
	return func(ctx context.Context, c *app.RequestContext) {
		c.Set("authEnabled", token != "")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Cache-Control", "no-store")
		method := string(c.Method())
		if token != "" && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
			fields := strings.Fields(string(c.GetHeader("Authorization")))
			if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
				unauthorized(c)
				return
			}
			actual := sha256.Sum256([]byte(fields[1]))
			if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
				unauthorized(c)
				return
			}
		}
		c.Next(ctx)
	}
}

func unauthorized(c *app.RequestContext) {
	c.Header("WWW-Authenticate", "Bearer")
	JSONError(c, http.StatusUnauthorized, "请提供有效的写入令牌")
	c.Abort()
}

func JSONError(c *app.RequestContext, status int, message string) {
	c.JSON(status, map[string]any{"code": status, "msg": message})
}

func storageError(c *app.RequestContext, err error) {
	switch {
	case errors.Is(err, readwriter.ErrNotFound):
		JSONError(c, http.StatusNotFound, "数据包不存在")
	case errors.Is(err, readwriter.ErrInvalidPacket):
		JSONError(c, http.StatusBadRequest, "数据包字段不合法")
	case errors.Is(err, readwriter.ErrIDExhausted):
		JSONError(c, http.StatusConflict, "数据包 ID 已达到上限")
	default:
		log.Printf("packet storage operation failed: %v", err)
		JSONError(c, http.StatusInternalServerError, "存储操作失败，请稍后重试")
	}
}
