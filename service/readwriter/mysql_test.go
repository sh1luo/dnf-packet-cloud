package readwriter

import (
	"testing"
	packet "packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/config"
	_ "github.com/go-sql-driver/mysql"
)

func TestMySQLReadWrite(t *testing.T) {
	x := cfg.Get()
	if x.MySQL.DSN == "" {
		t.Skip("mysql dsn missing")
	}
	s := NewMySQLStorageFromConfig()
	if s == nil {
		t.Skip("mysql not available")
	}
	w := s.writeDB
	if w == nil {
		t.Skip("no write db")
	}
	
    // Clean up
    if err := w.Exec("DELETE FROM user_packets").Error; err != nil {
        t.Logf("cleanup user_packets error: %v", err)
    }
    if err := w.Exec("DELETE FROM cloud_packets").Error; err != nil {
        t.Logf("cleanup cloud_packets error: %v", err)
    }

	in := []*packet.CloudPacket{{Id: 1, Region: "r", Name: "n", Channel: "c", Uploader: "u", Time: "t", UserPackets: []*packet.UserPacket{{Id: 10, Name: "a", Content: "b", Size: 2, SendTiming: "s"}}}}
	if err := s.SavePacket(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := s.ReadPacket()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(out) != 1 || out[0].Id != 1 || len(out[0].UserPackets) != 1 || out[0].UserPackets[0].Content != "b" {
		t.Fatalf("mismatch: %+v", out)
	}
}
