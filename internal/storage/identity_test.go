package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"packet_cloud/biz/model/hertz/packet"
)

func TestLFSRejectsInvalidPersistedIdentity(t *testing.T) {
	for _, ids := range [][]int32{{7, 7}, {0}, {-1}} {
		t.Run(fmt.Sprint(ids), func(t *testing.T) {
			packets := make([]*packet.CloudPacket, len(ids))
			for i, id := range ids {
				packets[i] = samplePacket(id)
			}
			raw, err := json.Marshal(packets)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "packets.json")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewLocalFileSystem(path); !errors.Is(err, ErrCorruptStorage) {
				t.Fatalf("stored IDs %v must be corrupt, got %v", ids, err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(raw) {
				t.Fatalf("invalid stored identity changed on disk: %v", err)
			}
		})
	}
}

func TestLFSIdentityCorruptionCannotBeOverwritten(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`[{"id":7,"name":"first"},{"id":7,"name":"second"}]`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteRange(context.Background(), 7, 7); !errors.Is(err, ErrCorruptStorage) {
		t.Fatalf("delete overwrote duplicate identity: %v", err)
	}
	if _, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)}); !errors.Is(err, ErrCorruptStorage) {
		t.Fatalf("append overwrote duplicate identity: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(raw) {
		t.Fatalf("failed mutation altered corrupt snapshot: %v", err)
	}
}

func TestLFSLegacySaveRejectsInvalidIdentity(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, packets := range [][]*packet.CloudPacket{
		{samplePacket(7), samplePacket(7)}, {samplePacket(0)}, {samplePacket(-1)},
	} {
		if err := s.SavePacket(packets); !errors.Is(err, ErrInvalidPacket) {
			t.Fatalf("invalid replacement identity accepted: %v", err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("failed replacement changed disk: %v", err)
	}
	ids, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0), samplePacket(0)})
	if err != nil || !reflect.DeepEqual(ids, []int32{2, 3}) {
		t.Fatalf("append must still ignore duplicate incoming IDs: %v %v", ids, err)
	}
}
