package readwriter

import (
    "os"
    "path/filepath"
    "testing"
    packet "packet_cloud/biz/model/hertz/packet"
)

func BenchmarkLFSReadPacket(b *testing.B) {
    dir := b.TempDir()
    fp := filepath.Join(dir, "packets.json")
    os.Setenv("PACKETS_FILE_PATH", fp)
    s := &LocalFileSystem{}
    data := make([]*packet.CloudPacket, 0, 1000)
    for i := 0; i < 1000; i++ {
        data = append(data, &packet.CloudPacket{Id: int32(i + 1), Region: "r", Name: "n", Channel: "c", Uploader: "u", Time: "t", UserPackets: []*packet.UserPacket{{Id: int32(i + 1), Name: "a", Content: "b", Size: 2, SendTiming: "s"}}})
    }
    _ = s.SavePacket(data)
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, _ = s.ReadPacket()
    }
}