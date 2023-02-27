// Code generated by hertz generator.

package packet

import (
	"context"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"log"
	"packet_cloud/biz/model"
	packet "packet_cloud/biz/model/hertz/packet"
)

// CreatePacketResponse .
// @router /v1/user/create [POST]
func CreatePacketResponse(ctx context.Context, c *app.RequestContext) {
	var err error
	var req packet.CreatePacketReq
	err = c.BindAndValidate(&req)
	if err != nil {
		c.String(consts.StatusBadRequest, err.Error())
		return
	}

	resp := new(packet.CreatePacketResp)

	p := &packet.Packet{
		ID:       req.ID,
		Region:   req.Region,
		Name:     req.Name,
		Content:  req.Content,
		Channel:  req.Channel,
		Uploader: req.Uploader,
		Time:     req.Time,
	}
	model.Mu.Lock()
	if len(model.Packets) > 0 {
		p.ID = model.Packets[len(model.Packets)-1].ID + 1
	} else {
		p.ID = 1
	}
	model.Packets = append(model.Packets, p)
	err = model.SaveFile(model.Packets)
	if err != nil {
		resp.Code = 501
		resp.Msg = "写入失败"
	}
	model.Mu.Unlock()

	resp.Code = 0
	resp.Msg = "上传成功"
	c.JSON(consts.StatusOK, resp)
}

// QueryPacketResponse .
// @router /v1/packet/query [POST]
func QueryPacketResponse(ctx context.Context, c *app.RequestContext) {
	var err error
	var req packet.QueryPacketsReq
	err = c.BindAndValidate(&req)
	if err != nil {
		c.String(consts.StatusBadRequest, err.Error())
		return
	}

	resp := new(packet.QueryPacketsResp)

	p, err := model.ReadFile()
	if err != nil {
		log.Println("read file error:", err)
	}

	resp.Packet = p
	resp.Code = 0
	resp.Msg = "获取云数据包成功"
	c.JSON(consts.StatusOK, resp)
}
