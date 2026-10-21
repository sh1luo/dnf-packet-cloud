package storage

import (
	"context"
	"path/filepath"
	"testing"

	"packet_cloud/biz/model/hertz/packet"
)

func BenchmarkLFSReadPacket(b *testing.B) {
	s := benchmarkFileStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.ReadPacket(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkLFSListMetadata(b *testing.B) {
	s := benchmarkFileStore(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := s.List(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkFileStore(b *testing.B) *LocalFileSystem {
	b.Helper()
	s, err := NewLocalFileSystem(filepath.Join(b.TempDir(), "packets.json"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = s.Close() })
	data := make([]*packet.CloudPacket, 1000)
	for i := range data {
		data[i] = samplePacket(int32(i + 1))
	}
	if err := s.SavePacket(data); err != nil {
		b.Fatal(err)
	}
	return s
}
