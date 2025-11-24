package readwriter

import (
    "github.com/bytedance/sonic"
    "github.com/pkg/errors"
    "github.com/robfig/cron/v3"
    "log"
    "os"
    "packet_cloud/biz/model/hertz/packet"
    cfg "packet_cloud/config"
    "sync"
    "time"
)

var (
    fileRelativePath = "./packets"
)

var (
	syncLock sync.RWMutex
)

type LocalFileSystem struct {
}

func (s *LocalFileSystem) ReadPacket() ([]*packet.CloudPacket, error) {
    packets := make([]*packet.CloudPacket, 0)

	syncLock.RLock()
	defer syncLock.RUnlock()

    fileRelativePath = cfg.Get().PacketsFilePath
    bytes, err := os.ReadFile(fileRelativePath)
	if err != nil {
		return nil, err
	}
	err = sonic.Unmarshal(bytes, &packets)
	if err != nil {
		return nil, err
	}
	return packets, nil
}

func (s *LocalFileSystem) SavePacket(packets []*packet.CloudPacket) error {
    bytes, err := sonic.Marshal(packets)
    if err != nil {
        return err
    }

	syncLock.Lock()
	defer syncLock.Unlock()

    fileRelativePath = cfg.Get().PacketsFilePath
    err = os.WriteFile(fileRelativePath, bytes, 0644)
	if err != nil {
		return err
	}
	return nil
}

func (s *LocalFileSystem) Backup() error {

	var (
		c      = cron.New()
		backup = time.Now().Format("20060102-15-04")
	)

	_, err := c.AddFunc("6 6 * * 4", func() {
		packets, err := ReadPacket(LFS)
		if err != nil {
			log.Println("[Backup] read packets error:", err)
			return
		}

		bs, err := sonic.Marshal(packets)
		if err != nil {
			log.Println("[Backup] marshal packets error:", err)
			return
		}

		// 备份文件
		err = os.WriteFile(backup, bs, 0644)
		if err != nil {
			log.Println(err)
			return
		}

		// 重置当前文件
		err = os.WriteFile(fileRelativePath, []byte("[]"), 0644)
		if err != nil {
			log.Println(err)
			return
		}

		log.Println("备份数据文件成功")
	})

	if err != nil {
		return errors.Wrap(err, "add cron error")
	}

	c.Start()

	return nil
}
