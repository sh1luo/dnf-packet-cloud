package readwriter

import (
	"context"
	"log"
	"os"
	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/config"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type CloudPacketModel struct {
	ID          int32             `gorm:"primaryKey;column:id"`
	Region      string            `gorm:"column:region;type:varchar(32);index:idx_region"`
	Name        string            `gorm:"column:name;type:varchar(64)"`
	Channel     string            `gorm:"column:channel;type:varchar(32);index:idx_channel"`
	Uploader    string            `gorm:"column:uploader;type:varchar(64);index:idx_uploader;index:idx_uploader_time"`
	Time        string            `gorm:"column:time;type:varchar(32);index:idx_time;index:idx_uploader_time"`
	UserPackets []UserPacketModel `gorm:"foreignKey:CloudPacketID;references:ID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (CloudPacketModel) TableName() string {
	return "cloud_packets"
}

type UserPacketModel struct {
	ID            int32  `gorm:"primaryKey;column:id"`
	CloudPacketID int32  `gorm:"column:cloud_packet_id;index:idx_cloud_packet_id"`
	Name          string `gorm:"column:name;type:varchar(64)"`
	Content       string `gorm:"column:content;type:longtext"`
	Size          int32  `gorm:"column:size"`
	SendTiming    string `gorm:"column:send_timing;type:varchar(32)"`
}

func (UserPacketModel) TableName() string {
	return "user_packets"
}

type MySQLStorage struct {
	writeDB       *gorm.DB
	readDB        *gorm.DB
	cache         []*packet.CloudPacket
	cacheAt       time.Time
	cacheTTL      time.Duration
	slowThreshold time.Duration
	queryTimeout  time.Duration
}

func NewMySQLStorageFromConfig() *MySQLStorage {
	writeDSN := cfg.Get().MySQL.DSN
	if writeDSN == "" {
		writeDSN = os.Getenv("MYSQL_DSN")
	}
	if writeDSN == "" {
		return nil
	}
	readDSN := cfg.Get().MySQL.ReadDSN
	if readDSN == "" {
		readDSN = os.Getenv("MYSQL_READ_DSN")
	}

	maxOpen := intOr(cfg.Get().MySQL.MaxOpen, 20)
	maxIdle := intOr(cfg.Get().MySQL.MaxIdle, 10)
	lifeMin := intOr(cfg.Get().MySQL.ConnMaxLifetimeMin, 30)
	cacheTTLms := intOr(cfg.Get().MySQL.QueryCacheTTLms, 500)
	slowMS := intOr(cfg.Get().MySQL.SlowQueryMs, 200)
	qTimeoutMS := intOr(cfg.Get().MySQL.QueryTimeoutMs, 3000)

	wdb := mustOpenWithRetry(writeDSN, maxOpen, maxIdle, lifeMin)
	if wdb == nil {
		return nil
	}

	// Auto Migrate
	if err := wdb.AutoMigrate(&CloudPacketModel{}, &UserPacketModel{}); err != nil {
		log.Printf("AutoMigrate error: %v", err)
	}

	var rdb *gorm.DB
	if readDSN != "" {
		rdb = mustOpenWithRetry(readDSN, maxOpen, maxIdle, lifeMin)
		if rdb == nil {
			rdb = wdb
		}
	} else {
		rdb = wdb
	}

	return &MySQLStorage{
		writeDB:       wdb,
		readDB:        rdb,
		cacheTTL:      time.Duration(cacheTTLms) * time.Millisecond,
		slowThreshold: time.Duration(slowMS) * time.Millisecond,
		queryTimeout:  time.Duration(qTimeoutMS) * time.Millisecond,
	}
}

func (s *MySQLStorage) ReadPacket() ([]*packet.CloudPacket, error) {
	if s.cache != nil && time.Since(s.cacheAt) < s.cacheTTL {
		return s.cache, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
	defer cancel()

	start := time.Now()
	var models []CloudPacketModel
	// Preload UserPackets to avoid N+1 query
	err := s.readDB.WithContext(ctx).Preload("UserPackets").Order("id ASC").Find(&models).Error
	if err != nil {
		return nil, err
	}

	if dur := time.Since(start); dur > s.slowThreshold {
		log.Printf("slow query ReadPacket dur=%s", dur)
	}

	packets := make([]*packet.CloudPacket, len(models))
	for i, m := range models {
		ups := make([]*packet.UserPacket, len(m.UserPackets))
		for j, um := range m.UserPackets {
			ups[j] = &packet.UserPacket{
				Id:         um.ID,
				Name:       um.Name,
				Content:    um.Content,
				Size:       um.Size,
				SendTiming: um.SendTiming,
			}
		}
		packets[i] = &packet.CloudPacket{
			Id:          m.ID,
			Region:      m.Region,
			Name:        m.Name,
			Channel:     m.Channel,
			Uploader:    m.Uploader,
			Time:        m.Time,
			UserPackets: ups,
		}
	}

	s.cache = packets
	s.cacheAt = time.Now()
	return packets, nil
}

func (s *MySQLStorage) SavePacket(packets []*packet.CloudPacket) error {
	ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
	defer cancel()

	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Clear existing data to match LFS overwrite behavior
		if err := tx.Exec("DELETE FROM user_packets").Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM cloud_packets").Error; err != nil {
			return err
		}

		if len(packets) == 0 {
			return nil
		}

		// Convert to models
		models := make([]CloudPacketModel, len(packets))
		for i, p := range packets {
			ums := make([]UserPacketModel, len(p.UserPackets))
			for j, up := range p.UserPackets {
				ums[j] = UserPacketModel{
					ID:            up.Id,
					CloudPacketID: p.Id,
					Name:          up.Name,
					Content:       up.Content,
					Size:          up.Size,
					SendTiming:    up.SendTiming,
				}
			}
			models[i] = CloudPacketModel{
				ID:          p.Id,
				Region:      p.Region,
				Name:        p.Name,
				Channel:     p.Channel,
				Uploader:    p.Uploader,
				Time:        p.Time,
				UserPackets: ums,
			}
		}

		// Batch insert
		// GORM handles associations automatically if configured correctly.
		// However, batch inserting with associations can be tricky.
		// For safety and simplicity given the full rewrite, we can insert cloud packets first, then user packets.
		// Or just let GORM handle it. GORM's Create supports batch insert with associations.
		if err := tx.Create(&models).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *MySQLStorage) Backup() error {
	return nil
}

func mustOpenWithRetry(dsn string, maxOpen, maxIdle, lifeMin int) *gorm.DB {
	var db *gorm.DB
	var err error
	backoff := []time.Duration{time.Millisecond * 200, time.Millisecond * 500, time.Second, time.Second * 2, time.Second * 5}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	}

	for i := 0; i < len(backoff); i++ {
		db, err = gorm.Open(mysql.Open(dsn), gormConfig)
		if err == nil {
			sqlDB, err := db.DB()
			if err == nil {
				sqlDB.SetMaxOpenConns(maxOpen)
				sqlDB.SetMaxIdleConns(maxIdle)
				sqlDB.SetConnMaxLifetime(time.Duration(lifeMin) * time.Minute)

				pingCtx, cancel := context.WithTimeout(context.Background(), time.Second*2)
				err = sqlDB.PingContext(pingCtx)
				cancel()
				if err == nil {
					return db
				}
			}
		}
		time.Sleep(backoff[i])
	}
	return nil
}

func intOr(x, def int) int {
	if x == 0 {
		return def
	}
	return x
}
