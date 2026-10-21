package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/internal/config"
	"strings"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	mysqlgorm "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type CloudPacketModel struct {
	ID          int32             `gorm:"primaryKey;autoIncrement;column:id;type:int"`
	Region      string            `gorm:"column:region;type:varchar(32);index:idx_region"`
	Name        string            `gorm:"column:name;type:varchar(64)"`
	Channel     string            `gorm:"column:channel;type:varchar(32);index:idx_channel"`
	Uploader    string            `gorm:"column:uploader;type:varchar(64);index:idx_uploader;index:idx_uploader_time"`
	Time        string            `gorm:"column:time;type:varchar(32);index:idx_time;index:idx_uploader_time"`
	UserPackets []UserPacketModel `gorm:"foreignKey:CloudPacketID;references:ID;constraint:fk_user_packets_cloud_packet_id,OnUpdate:CASCADE,OnDelete:CASCADE;"`
}

func (CloudPacketModel) TableName() string {
	return "cloud_packets"
}

type UserPacketModel struct {
	CloudPacketID int32  `gorm:"primaryKey;autoIncrement:false;column:cloud_packet_id;type:int;index:idx_cloud_packet_id"`
	Ordinal       int32  `gorm:"primaryKey;autoIncrement:false;column:ordinal;type:int"`
	ID            int32  `gorm:"autoIncrement:false;column:id;type:int"`
	Name          string `gorm:"column:name;type:varchar(64)"`
	Content       string `gorm:"column:content;type:longtext"`
	Size          int32  `gorm:"column:size"`
	SendTiming    string `gorm:"column:send_timing;type:varchar(32)"`
}

func (UserPacketModel) TableName() string {
	return "user_packets"
}

type MySQLStorage struct {
	db           *gorm.DB
	sqlDB        *sql.DB
	queryTimeout time.Duration
}

// NewMySQLStorage owns one primary pool. ReadDSN and the legacy cache settings
// are intentionally unused: all reads promise primary read-after-write behavior.
func NewMySQLStorage(c cfg.MySQLConfig) (*MySQLStorage, error) {
	if strings.TrimSpace(c.DSN) == "" {
		return nil, errors.New("MySQL DSN is required")
	}
	driverConfig, err := mysqldriver.ParseDSN(c.DSN)
	if err != nil {
		return nil, errors.New("invalid MySQL DSN")
	}
	timeout := time.Duration(positiveOr(c.QueryTimeoutMs, 3000)) * time.Millisecond
	driverConfig.Timeout = boundedTimeout(driverConfig.Timeout, timeout)
	driverConfig.ReadTimeout = boundedTimeout(driverConfig.ReadTimeout, timeout)
	driverConfig.WriteTimeout = boundedTimeout(driverConfig.WriteTimeout, timeout)
	connector, err := mysqldriver.NewConnector(driverConfig)
	if err != nil {
		return nil, fmt.Errorf("create MySQL connector: %w", err)
	}
	return initializeMySQLStorage(sql.OpenDB(connector), c, timeout)
}

func initializeMySQLStorage(pool *sql.DB, c cfg.MySQLConfig, timeout time.Duration) (_ *MySQLStorage, err error) {
	defer func() {
		if err != nil {
			_ = pool.Close()
		}
	}()
	pool.SetMaxOpenConns(positiveOr(c.MaxOpen, 20))
	pool.SetMaxIdleConns(c.MaxIdle)
	pool.SetConnMaxLifetime(time.Duration(positiveOr(c.ConnMaxLifetimeMin, 30)) * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := pool.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect MySQL: %w", err)
	}
	db, err := gorm.Open(mysqlgorm.New(mysqlgorm.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("initialize MySQL: %w", err)
	}
	if err := checkMySQLSchema(ctx, pool); err != nil {
		return nil, err
	}
	if err := db.WithContext(ctx).AutoMigrate(&CloudPacketModel{}, &UserPacketModel{}); err != nil {
		return nil, fmt.Errorf("migrate MySQL storage: %w", err)
	}
	return &MySQLStorage{db: db, sqlDB: pool, queryTimeout: timeout}, nil
}

// Existing primary keys need the reviewed, versioned migration; AutoMigrate
// does not reliably replace a primary key or reconstruct lost child order.
func checkMySQLSchema(ctx context.Context, pool *sql.DB) error {
	rows, err := pool.QueryContext(ctx, `SELECT TABLE_NAME, COLUMN_NAME, COLUMN_KEY, EXTRA
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN ('cloud_packets', 'user_packets')`)
	if err != nil {
		return fmt.Errorf("inspect MySQL schema: %w", err)
	}
	defer rows.Close()
	var parent, child, parentAuto, cloudKey, ordinalKey, badChildKey bool
	for rows.Next() {
		var table, column, key, extra string
		if err := rows.Scan(&table, &column, &key, &extra); err != nil {
			return err
		}
		if table == "cloud_packets" {
			parent = true
			if column == "id" {
				parentAuto = strings.Contains(extra, "auto_increment")
			}
		}
		if table == "user_packets" {
			child = true
			switch {
			case column == "cloud_packet_id":
				cloudKey = key == "PRI"
			case column == "ordinal":
				ordinalKey = key == "PRI"
			case key == "PRI" || strings.Contains(extra, "auto_increment"):
				badChildKey = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if (parent && !parentAuto) || (child && (!cloudKey || !ordinalKey || badChildKey)) {
		return errors.New("legacy MySQL storage schema: apply db/migrations/002_storage_identity.sql before startup")
	}
	return nil
}

func (s *MySQLStorage) Close() error { return s.sqlDB.Close() }

func (s *MySQLStorage) List(ctx context.Context) ([]*packet.CloudPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	var models []CloudPacketModel
	if err := s.db.WithContext(ctx).Select("id", "region", "name", "channel", "uploader", "time").
		Order("id ASC").Find(&models).Error; err != nil {
		return nil, err
	}
	return modelsToPackets(models), nil
}

func (s *MySQLStorage) Get(ctx context.Context, id int32) (*packet.CloudPacket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	var model CloudPacketModel
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Preload("UserPackets", func(db *gorm.DB) *gorm.DB {
			return db.Order("ordinal ASC")
		}).Where("id = ?", id).Take(&model).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return modelsToPackets([]CloudPacketModel{model})[0], nil
}

func (s *MySQLStorage) Append(ctx context.Context, packets []*packet.CloudPacket) ([]int32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validatePackets(packets); err != nil {
		return nil, err
	}
	in := clonePackets(packets)
	ids := make([]int32, 0, len(in))
	if len(in) == 0 {
		return ids, nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, p := range in {
			model := CloudPacketModel{
				Region: p.Region, Name: p.Name, Channel: p.Channel, Uploader: p.Uploader, Time: p.Time,
			}
			if err := tx.Omit(clause.Associations).Create(&model).Error; err != nil {
				return parentInsertError(err)
			}
			if model.ID <= 0 {
				return ErrIDExhausted
			}
			children := make([]UserPacketModel, len(p.UserPackets))
			for i, child := range p.UserPackets {
				children[i] = UserPacketModel{
					CloudPacketID: model.ID, Ordinal: int32(i), ID: child.Id,
					Name: child.Name, Content: child.Content, Size: child.Size, SendTiming: child.SendTiming,
				}
			}
			if len(children) != 0 {
				if err := tx.CreateInBatches(&children, 200).Error; err != nil {
					return err
				}
			}
			ids = append(ids, model.ID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func parentInsertError(err error) error {
	var mysqlErr *mysqldriver.MySQLError
	// InnoDB reports exhausted auto-increment as 1467; signed INT overflow
	// can also surface as 167/1264 or a duplicate of the saturated maximum.
	if errors.As(err, &mysqlErr) && (mysqlErr.Number == 1467 || mysqlErr.Number == 167 ||
		(mysqlErr.Number == 1264 && strings.Contains(mysqlErr.Message, "'id'")) ||
		(mysqlErr.Number == 1062 && strings.Contains(mysqlErr.Message, "'2147483647'") &&
			strings.Contains(mysqlErr.Message, "PRIMARY"))) {
		return fmt.Errorf("%w: %v", ErrIDExhausted, err)
	}
	return err
}

func (s *MySQLStorage) DeleteRange(ctx context.Context, start, end int32) ([]int32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if start > end {
		return nil, fmt.Errorf("%w: range start exceeds end", ErrInvalidPacket)
	}
	ctx, cancel := context.WithTimeout(ctx, s.queryTimeout)
	defer cancel()
	ids := make([]int32, 0)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&CloudPacketModel{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id BETWEEN ? AND ?", start, end).Order("id ASC").Pluck("id", &ids).Error; err != nil {
			return err
		}
		// Chunk bind parameters while keeping all target deletions in one transaction.
		for offset := 0; offset < len(ids); offset += 500 {
			limit := offset + 500
			if limit > len(ids) {
				limit = len(ids)
			}
			targets := ids[offset:limit]
			if err := tx.Where("cloud_packet_id IN ?", targets).Delete(&UserPacketModel{}).Error; err != nil {
				return err
			}
			if err := tx.Where("id IN ?", targets).Delete(&CloudPacketModel{}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (s *MySQLStorage) ReadPacket() ([]*packet.CloudPacket, error) {
	ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
	defer cancel()
	var models []CloudPacketModel
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Preload("UserPackets", func(db *gorm.DB) *gorm.DB {
			return db.Order("ordinal ASC")
		}).Order("id ASC").Find(&models).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	return modelsToPackets(models), nil
}

// SavePacket no longer allows a stale full snapshot to delete unrelated rows.
func (s *MySQLStorage) SavePacket([]*packet.CloudPacket) error {
	return errors.New("MySQL full replacement is disabled; use Append and DeleteRange")
}

func (s *MySQLStorage) Backup() error {
	return errors.New("MySQL backups require database-native backup tooling")
}

func modelsToPackets(models []CloudPacketModel) []*packet.CloudPacket {
	packets := make([]*packet.CloudPacket, len(models))
	for i, m := range models {
		children := make([]*packet.UserPacket, len(m.UserPackets))
		for j, child := range m.UserPackets {
			children[j] = &packet.UserPacket{
				Id: child.ID, Name: child.Name, Content: child.Content, Size: child.Size, SendTiming: child.SendTiming,
			}
		}
		packets[i] = &packet.CloudPacket{
			Id: m.ID, Region: m.Region, Name: m.Name, Channel: m.Channel,
			Uploader: m.Uploader, Time: m.Time, UserPackets: children,
		}
	}
	return packets
}

func positiveOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func boundedTimeout(value, bound time.Duration) time.Duration {
	if value <= 0 || value > bound {
		return bound
	}
	return value
}
