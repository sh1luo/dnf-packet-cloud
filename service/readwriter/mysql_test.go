package readwriter

import (
    "testing"
    packet "packet_cloud/biz/model/hertz/packet"
    _ "github.com/go-sql-driver/mysql"
    cfg "packet_cloud/config"
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
    dbname := "packet_cloud"
    _, _ = w.Exec("CREATE DATABASE IF NOT EXISTS `" + dbname + "` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci")
    _, _ = w.Exec("USE `" + dbname + "`")
    _, _ = w.Exec("CREATE TABLE IF NOT EXISTS cloud_packets (id INT NOT NULL, region VARCHAR(32) NOT NULL, name VARCHAR(64) NOT NULL, channel VARCHAR(32) NOT NULL, uploader VARCHAR(64) NOT NULL, time VARCHAR(32) NOT NULL, PRIMARY KEY (id)) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
    _, _ = w.Exec("CREATE TABLE IF NOT EXISTS user_packets (id INT NOT NULL, cloud_packet_id INT NOT NULL, name VARCHAR(64) NOT NULL, content LONGTEXT NOT NULL, size INT NOT NULL, send_timing VARCHAR(32) NOT NULL, PRIMARY KEY (id), INDEX idx_cloud_packet_id (cloud_packet_id), CONSTRAINT fk_user_packets_cloud_packet_id FOREIGN KEY (cloud_packet_id) REFERENCES cloud_packets(id) ON DELETE CASCADE ON UPDATE CASCADE) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
    _, _ = w.Exec("DELETE FROM user_packets")
    _, _ = w.Exec("DELETE FROM cloud_packets")
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
