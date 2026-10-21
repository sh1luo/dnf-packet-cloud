package handler

import (
	"fmt"
	"strings"
	"unicode/utf8"

	packet "packet_cloud/biz/model/hertz/packet"
)

// Match MySQL varchar character limits so both backends accept the same input.
func validateUpload(p *packet.CloudPacket) error {
	if p == nil {
		return fmt.Errorf("cloud_packet 不能为空")
	}
	for _, field := range []struct {
		name, value string
		max         int
	}{
		{"region", p.Region, 32}, {"name", p.Name, 64}, {"channel", p.Channel, 32},
		{"uploader", p.Uploader, 64}, {"time", p.Time, 32},
	} {
		if strings.TrimSpace(field.value) == "" || !utf8.ValidString(field.value) || utf8.RuneCountInString(field.value) > field.max {
			return fmt.Errorf("%s 必须为 1～%d 个字符", field.name, field.max)
		}
	}
	if p.UserPackets == nil || len(p.UserPackets) > 10000 {
		return fmt.Errorf("user_packets 必须是数组，且不超过 10000 项")
	}
	contentBytes := 0
	for i, child := range p.UserPackets {
		if child == nil {
			return fmt.Errorf("user_packets[%d] 不能为空", i)
		}
		if child.Id < 0 || child.Size < 0 || utf8.RuneCountInString(child.Name) > 64 ||
			utf8.RuneCountInString(child.SendTiming) > 32 || !utf8.ValidString(child.Content) {
			return fmt.Errorf("user_packets[%d] 字段不合法", i)
		}
		contentBytes += len(child.Content)
	}
	if contentBytes > 2*1024*1024 {
		return fmt.Errorf("单个云数据包内容总量不能超过 2 MiB")
	}
	return nil
}
