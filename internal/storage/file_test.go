package storage

import (
	"os"
	"path/filepath"
	"testing"

	"packet_cloud/biz/model/hertz/packet"
)

func legacyTestFileStore(t *testing.T) (*LocalFileSystem, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "packets.json")
	s, err := NewLocalFileSystem(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func samplePacket(id int32) *packet.CloudPacket {
	return &packet.CloudPacket{
		Id: id, Region: "r", Name: "original", Channel: "c", Uploader: "u", Time: "t",
		UserPackets: []*packet.UserPacket{{Id: 0, Name: "child", Content: "payload", Size: 7, SendTiming: "s"}},
	}
}

func TestLFSMissingIsEmpty(t *testing.T) {
	s, _ := legacyTestFileStore(t)
	out, err := s.ReadPacket()
	if err != nil || out == nil || len(out) != 0 {
		t.Fatalf("missing file must be an empty array: packets=%v err=%v", out, err)
	}
}

func TestLFSRejectsNilEntriesWithoutWriting(t *testing.T) {
	for _, input := range [][]*packet.CloudPacket{
		{nil},
		{{Id: 1, UserPackets: []*packet.UserPacket{nil}}},
	} {
		s, path := legacyTestFileStore(t)
		if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SavePacket(input); err == nil {
			t.Error("nil packet entry must return an error")
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Errorf("invalid save changed disk: err=%v", err)
		}
	}
}

func TestLFSBackupCreatesCopyWithoutChangingLiveData(t *testing.T) {
	s, path := legacyTestFileStore(t)
	if err := s.SavePacket([]*packet.CloudPacket{samplePacket(1)}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(path + ".backup-*")
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected immediate backup beside live data: paths=%v err=%v", matches, err)
	}
	backup, err := os.ReadFile(matches[0])
	if err != nil || string(backup) != string(before) {
		t.Fatalf("backup differs: err=%v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("backup changed live data: err=%v", err)
	}
}
