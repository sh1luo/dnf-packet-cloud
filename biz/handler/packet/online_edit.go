package packet

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"log"
	"net/http"
	"packet_cloud/biz/handler"
)

// OnlineEdit .
// @router /edit [GET]
func OnlineEdit(ctx context.Context, c *app.RequestContext) {
	packets, err := handler.Repository(c).List(ctx)
	if err != nil {
		log.Println("[OnlineEdit] read file error", err)
		c.String(http.StatusInternalServerError, "数据包加载失败，请稍后刷新重试。")
		return
	}
	authEnabled, _ := c.Get("authEnabled")
	c.HTML(http.StatusOK, "packet/online_edit.html", utils.H{"packets": packets, "authEnabled": authEnabled})
}
