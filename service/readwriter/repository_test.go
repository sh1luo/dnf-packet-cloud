package readwriter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/config"
)

func TestOpenSelectsBackendAndReturnsErrors(t *testing.T) {
	for _, media := range []string{"lfs", "file", ""} {
		repo, err := Open(cfg.Config{StorageMedia: media, PacketsFilePath: filepath.Join(t.TempDir(), "packets")})
		if err != nil {
			t.Fatalf("open %q: %v", media, err)
		}
		if _, ok := repo.(*LocalFileSystem); !ok {
			t.Fatalf("open %q returned %T", media, repo)
		}
		_ = repo.Close()
	}
	for _, media := range []string{"mysql", "rds", "unsupported"} {
		repo, err := Open(cfg.Config{StorageMedia: media})
		if err == nil || repo != nil {
			t.Fatalf("open %q must return a real error and nil repository", media)
		}
	}
}

func TestLFSConcurrentAppendRetainsEveryPacket(t *testing.T) {
	s, _ := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(15), samplePacket(2)}); err != nil {
		t.Fatal(err)
	}
	const count = 40
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(999)})
			if err == nil && (len(ids) != 1 || ids[0] <= 15) {
				err = fmt.Errorf("bad allocated IDs: %v", ids)
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := s.List(context.Background())
	if err != nil || len(out) != count+2 {
		t.Fatalf("lost successful appends: count=%d err=%v", len(out), err)
	}
	ids := make(map[int32]bool)
	for _, p := range out {
		if ids[p.Id] {
			t.Fatalf("duplicate allocated ID %d", p.Id)
		}
		ids[p.Id] = true
	}
}

func TestLFSOwnsInboundAndReturnedGraphs(t *testing.T) {
	s, _ := legacyTestFileStore(t)
	p := samplePacket(500)
	p.UserPackets = append(p.UserPackets, &packet.UserPacket{Id: 0, Content: "second"})
	ids, err := s.Append(context.Background(), []*packet.CloudPacket{p})
	if err != nil {
		t.Fatal(err)
	}
	if p.Id != 500 || p.UserPackets[0].Id != 0 {
		t.Fatal("append mutated caller IDs")
	}
	p.Name = "changed input"
	p.UserPackets[0].Content = "changed input child"
	list, err := s.List(context.Background())
	if err != nil || len(list) != 1 || len(list[0].UserPackets) != 0 {
		t.Fatalf("list must contain metadata only: packets=%v err=%v", list, err)
	}
	list[0].Name = "changed list"
	got, err := s.Get(context.Background(), ids[0])
	if err != nil || got.Name != "original" || len(got.UserPackets) != 2 ||
		got.UserPackets[0].Content != "payload" || got.UserPackets[1].Content != "second" {
		t.Fatalf("stored graph was changed: packet=%v err=%v", got, err)
	}
	got.UserPackets[0].Content = "changed get"
	got, err = s.Get(context.Background(), ids[0])
	if err != nil || got.UserPackets[0].Content != "payload" {
		t.Fatal("get returned an alias")
	}
	if _, err := s.Get(context.Background(), -99); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing get: %v", err)
	}
}

func TestLFSExternalChangesInvalidateSnapshot(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	old, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(path), "external")
	b, err := json.Marshal([]*packet.CloudPacket{samplePacket(8)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(replacement, b, 0600); err != nil {
		t.Fatal(err)
	}
	// Same size and mtime still must invalidate on atomic external replacement.
	if err := os.Chtimes(replacement, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), 8)
	if err != nil || got.Id != 8 {
		t.Fatalf("external rename not loaded: %v %v", got, err)
	}
	ids, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)})
	if err != nil || !reflect.DeepEqual(ids, []int32{9}) {
		t.Fatalf("append ignored external maximum: %v %v", ids, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out, err := s.List(context.Background())
	if err != nil || out == nil || len(out) != 0 {
		t.Fatalf("external deletion not loaded: %v %v", out, err)
	}
}

func TestLFSCorruptFilesAreExplicitErrors(t *testing.T) {
	for _, raw := range []string{"", "{", "null", "{}", "[null]", `[{"user_packets":[null]}]`, "[] []"} {
		t.Run(raw, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "packets")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if s, err := NewLocalFileSystem(path); err == nil {
				_ = s.Close()
				t.Fatal("constructor accepted corrupt packet array")
			}
		})
	}
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[null]"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List(context.Background()); err == nil {
		t.Fatal("served stale cache after corrupt external edit")
	}
	if _, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(1)}); err == nil {
		t.Fatal("overwrote corrupt external file")
	}
	after, _ := os.ReadFile(path)
	if string(after) != "[null]" {
		t.Fatal("failed mutation changed corrupt file")
	}
}

func TestLFSCancelledAndInvalidMutationsLeaveData(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Append(ctx, []*packet.CloudPacket{samplePacket(0)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled append: %v", err)
	}
	if _, err := s.DeleteRange(ctx, 1, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled delete: %v", err)
	}
	if _, err := s.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled list: %v", err)
	}
	if _, err := s.Get(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled get: %v", err)
	}
	for _, input := range [][]*packet.CloudPacket{{nil}, {{UserPackets: []*packet.UserPacket{nil}}}} {
		if _, err := s.Append(context.Background(), input); !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("invalid append: %v", err)
		}
	}
	if _, err := s.DeleteRange(context.Background(), 3, 1); !errors.Is(err, ErrInvalidPacket) {
		t.Fatalf("invalid delete range: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed mutations changed file")
	}
}

func TestLFSWriteFailurePreservesPreviousFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission-based failure injection requires a non-root user")
	}
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if _, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)}); err == nil {
		t.Fatal("append in unwritable directory succeeded")
	}
	if _, err := s.DeleteRange(context.Background(), 1, 1); err == nil {
		t.Fatal("delete in unwritable directory succeeded")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("failed write changed prior snapshot")
	}
	got, err := s.Get(context.Background(), 1)
	if err != nil || got.Name != "original" {
		t.Fatal("failed write changed cached snapshot")
	}
}

func TestLFSRangeDeleteAndExhaustion(t *testing.T) {
	s, _ := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(5), samplePacket(1), samplePacket(3)}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.DeleteRange(context.Background(), 3, 5)
	if err != nil || !reflect.DeepEqual(ids, []int32{3, 5}) {
		t.Fatalf("targeted range deletion: %v %v", ids, err)
	}
	got, err := s.Get(context.Background(), 1)
	if err != nil || got.UserPackets[0].Content != "payload" {
		t.Fatal("delete altered unrelated packet")
	}
	ids, err = s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)})
	if err != nil || ids[0] <= 5 {
		t.Fatalf("reused high-water ID: %v %v", ids, err)
	}
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(math.MaxInt32)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)}); !errors.Is(err, ErrIDExhausted) {
		t.Fatalf("ID overflow: %v", err)
	}
	out, err := s.List(context.Background())
	if err != nil || len(out) != 1 || out[0].Id != math.MaxInt32 {
		t.Fatal("overflow changed data")
	}
}

func TestLFSSeparateInstancesDoNotSharePathsOrLocks(t *testing.T) {
	first, _ := legacyTestFileStore(t)
	second, _ := legacyTestFileStore(t)
	if _, err := first.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)}); err != nil {
		t.Fatal(err)
	}
	out, err := second.List(context.Background())
	if err != nil || len(out) != 0 {
		t.Fatal("independent stores share data")
	}
}

func TestLFSEmptyBatchDoesNotWrite(t *testing.T) {
	s, path := legacyTestFileStore(t)
	ids, err := s.Append(context.Background(), nil)
	if err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("empty append: %v %v", ids, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("empty append wrote a snapshot")
	}
}

func TestLFSCancellationWhileWaitingDoesNotMutate(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	s.mu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := s.Append(ctx, []*packet.CloudPacket{samplePacket(0)})
		done <- err
	}()
	<-ctx.Done()
	s.mu.Unlock()
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("mutation ignored expired caller while waiting: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("cancelled waiting mutation changed data")
	}
}
