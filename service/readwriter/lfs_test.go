package readwriter

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    cfg "packet_cloud/config"
    packet "packet_cloud/biz/model/hertz/packet"
)

func TestLFSReadWrite(t *testing.T) {
    dir := t.TempDir()
    fp := filepath.Join(dir, "packets.json")
    cp := filepath.Join(dir, "config.json")
    b, _ := json.Marshal(cfg.Config{StorageMedia: "lfs", PacketsFilePath: fp})
    _ = os.WriteFile(cp, b, 0644)
    _ = cfg.Load(cp)
    s := &LocalFileSystem{}
    data := []*packet.CloudPacket{{Id: 1, Region: "r1", Name: "n1", Channel: "c1", Uploader: "u1", Time: "t1", UserPackets: []*packet.UserPacket{{Id: 1, Name: "x", Content: "y", Size: 1, SendTiming: "z"}}}}
    if err := s.SavePacket(data); err != nil {
        t.Fatalf("save: %v", err)
    }
    out, err := s.ReadPacket()
    if err != nil {
        t.Fatalf("read: %v", err)
    }
    if len(out) != 1 || out[0].Id != 1 || len(out[0].UserPackets) != 1 || out[0].UserPackets[0].Content != "y" {
        t.Fatalf("mismatch: %+v", out)
    }
}
