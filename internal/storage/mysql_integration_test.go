package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/internal/config"

	mysqldriver "github.com/go-sql-driver/mysql"
	"google.golang.org/protobuf/proto"
)

// Explicit opt-in only:
//
//	TEST_MYSQL_DSN='packet_test:packet_test@tcp(127.0.0.1:33067)/packet_cloud_test?parseTime=true' \
//	  go test ./internal/storage -run '^TestMySQLIntegration$' -count=1 -v
//
// The database must already exist and end in "_test". Application config and
// MYSQL_DSN are never read. Tests clean only rows tagged with their unique uploader.
func TestMySQLIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set TEST_MYSQL_DSN to explicitly opt in to a dedicated *_test database")
	}
	parsed, err := mysqldriver.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid TEST_MYSQL_DSN")
	}
	if !strings.HasSuffix(parsed.DBName, "_test") {
		t.Fatal("refusing integration database without _test suffix")
	}
	s, err := NewMySQLStorage(cfg.MySQLConfig{DSN: dsn, QueryTimeoutMs: 10000, MaxOpen: 8})
	if err != nil {
		t.Fatalf("open dedicated MySQL: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	tag := fmt.Sprintf("storage-test-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		// Only this run's parents are deleted. Their children cascade.
		if err := s.db.WithContext(ctx).Where("uploader = ?", tag).Delete(&CloudPacketModel{}).Error; err != nil {
			t.Errorf("cleanup test-owned rows: %v", err)
		}
	})
	input := func(name string) *packet.CloudPacket {
		p := samplePacket(999)
		p.Name, p.Uploader = name, tag
		p.UserPackets = []*packet.UserPacket{
			{Id: 0, Content: "zero"}, {Id: 7, Content: "first seven"},
			{Id: 7, Content: "second seven"}, {Id: -1, Content: "negative"},
		}
		return p
	}
	ctx := context.Background()
	first := input("first")
	ids, err := s.Append(ctx, []*packet.CloudPacket{first, input("second")})
	if err != nil || len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("append: %v %v", ids, err)
	}
	if first.Id != 999 {
		t.Fatal("database changed inbound parent ID")
	}
	for _, id := range ids {
		got, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		for i := range first.UserPackets {
			if len(got.UserPackets) != len(first.UserPackets) || !proto.Equal(first.UserPackets[i], got.UserPackets[i]) {
				t.Fatalf("child IDs/order changed: %v", got)
			}
		}
		got.UserPackets[0].Content = "caller edit"
	}
	list, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range list {
		if len(p.UserPackets) != 0 {
			t.Fatal("list loaded child payloads")
		}
	}

	const count = 16
	var wg sync.WaitGroup
	newIDs := make(chan int32, count)
	failures := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			created, err := s.Append(ctx, []*packet.CloudPacket{input(fmt.Sprintf("concurrent-%d", i))})
			if err != nil {
				failures <- err
				return
			}
			newIDs <- created[0]
		}(i)
	}
	wg.Wait()
	close(newIDs)
	close(failures)
	for err := range failures {
		t.Errorf("concurrent append: %v", err)
	}
	seen := make(map[int32]bool)
	for id := range newIDs {
		if seen[id] {
			t.Errorf("duplicate parent ID %d", id)
		}
		seen[id] = true
		if _, err := s.Get(ctx, id); err != nil {
			t.Errorf("lost successful append %d: %v", id, err)
		}
	}
	if len(seen) != count {
		t.Fatalf("retained %d/%d concurrent appends", len(seen), count)
	}

	t.Run("batch rollback", func(t *testing.T) {
		bad := input("rollback-invalid")
		bad.UserPackets[0].Name = strings.Repeat("x", 65)
		before := int64(0)
		if err := s.db.Model(&CloudPacketModel{}).Where("uploader = ?", tag).Count(&before).Error; err != nil {
			t.Fatal(err)
		}
		created, err := s.Append(ctx, []*packet.CloudPacket{input("rollback-first"), bad})
		if err == nil || created != nil {
			t.Fatalf("invalid child must roll back batch: %v %v", created, err)
		}
		after := int64(0)
		if err := s.db.Model(&CloudPacketModel{}).Where("uploader = ?", tag).Count(&after).Error; err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Fatalf("failed batch leaked parents: before=%d after=%d", before, after)
		}
	})

	deleted, err := s.DeleteRange(ctx, ids[0], ids[0])
	if err != nil || len(deleted) != 1 || deleted[0] != ids[0] {
		t.Fatalf("delete target: %v %v", deleted, err)
	}
	if _, err := s.Get(ctx, ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted parent remains: %v", err)
	}
	var childCount int64
	if err := s.db.Model(&UserPacketModel{}).Where("cloud_packet_id = ?", ids[0]).Count(&childCount).Error; err != nil || childCount != 0 {
		t.Fatalf("deleted parent retained children: %d %v", childCount, err)
	}
	retained, err := s.Get(ctx, ids[1])
	if err != nil || len(retained.UserPackets) != 4 || retained.UserPackets[0].Content != "zero" {
		t.Fatalf("delete changed unrelated packet: %v %v", retained, err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.DeleteRange(cancelled, ids[1], ids[1]); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled delete was not rejected")
	}
	if _, err := s.Get(ctx, ids[1]); err != nil {
		t.Fatal("cancelled delete removed packet")
	}
}
