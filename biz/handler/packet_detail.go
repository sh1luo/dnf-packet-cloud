package handler

import (
	"context"
	"strconv"

	packet "packet_cloud/biz/model/hertz/packet"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// GetPacketDetail 返回管理界面的明文详情，保留旧客户端的加密读取协议。
func GetPacketDetail(ctx context.Context, c *app.RequestContext) {
	c.Header("Cache-Control", "no-store")

	// 与旧读取接口保持相同的路径及重复查询参数校验语义。
	id, err := strconv.ParseInt(c.Param("id"), 10, 32)
	if err != nil || id < 1 {
		JSONError(c, consts.StatusBadRequest, "id 必须为有效的正整数")
		return
	}
	for _, queryID := range c.QueryArgs().PeekAll("id") {
		legacyID, parseErr := strconv.ParseInt(string(queryID), 10, 32)
		if parseErr != nil || legacyID != id {
			JSONError(c, consts.StatusBadRequest, "查询参数 id 必须与路径 id 一致")
			return
		}
	}

	p, err := Repository(c).Get(ctx, int32(id))
	if err != nil {
		storageError(c, err)
		return
	}
	c.JSON(consts.StatusOK, struct {
		Code   int                 `json:"code"`
		Msg    string              `json:"msg"`
		Packet *packet.CloudPacket `json:"packet"`
	}{
		Code: 0, Msg: "获取云数据包成功", Packet: p,
	})
}
