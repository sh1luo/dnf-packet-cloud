// Code generated by hertz generator.

package main

import (
	"github.com/bytedance/sonic"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/robfig/cron/v3"
	"log"
	"os"
	"packet_cloud/biz/model"
	"time"
)

func main() {
	h := server.Default()

	h.LoadHTMLGlob("html/**/*")

	autoSave()

	register(h)
	h.Spin()
}

func autoSave() {
	c := cron.New()
	//c.AddFunc("6 6 * * 4", func() {
	c.AddFunc("* * * * *", func() {
		filename := time.Now().Format("20060102-15-04-saved")
		packets, err := model.ReadPackets()
		if err != nil {
			log.Println("[ReadPackets] read packets error:", err)
			return
		}

		bs, err := sonic.Marshal(packets)
		if err != nil {
			log.Println(err)
			return
		}

		// 备份文件
		err = os.WriteFile(filename, bs, 0644)
		if err != nil {
			log.Println(err)
			return
		}

		// 重置当前文件
		err = os.WriteFile("packets", []byte("[]"), 0644)
		if err != nil {
			log.Println(err)
			return
		}

		log.Println("备份数据文件成功")
	})

	c.Start()
}
