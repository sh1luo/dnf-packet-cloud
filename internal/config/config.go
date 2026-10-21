// Package config loads and validates the application's runtime configuration.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

type MySQLConfig struct {
	DSN                string `json:"DSN"`
	ReadDSN            string `json:"ReadDSN"`
	MaxOpen            int    `json:"MaxOpen"`
	MaxIdle            int    `json:"MaxIdle"`
	ConnMaxLifetimeMin int    `json:"ConnMaxLifetimeMin"`
	QueryCacheTTLms    int    `json:"QueryCacheTTLms"`
	SlowQueryMs        int    `json:"SlowQueryMs"`
	QueryTimeoutMs     int    `json:"QueryTimeoutMs"`
}

type Config struct {
	StorageMedia    string      `json:"StorageMedia"`
	PacketsFilePath string      `json:"PacketsFilePath"`
	HostPorts       string      `json:"HostPorts"`
	WriteToken      string      `json:"WriteToken"`
	MySQL           MySQLConfig `json:"MySQL"`
}

var (
	mu sync.RWMutex
	c  = defaults()
)

func defaults() Config {
	return Config{
		StorageMedia: "lfs", PacketsFilePath: "./packets", HostPorts: "127.0.0.1:8080",
		MySQL: MySQLConfig{MaxOpen: 20, MaxIdle: 10, ConnMaxLifetimeMin: 30,
			QueryCacheTTLms: 500, SlowQueryMs: 200, QueryTimeoutMs: 3000},
	}
}

// Load merges a file over defaults, applies environment overrides, then publishes
// an immutable snapshot. An empty path uses PACKET_CONFIG or the optional default
// config/config.json; explicitly selected files must exist.
func Load(path string) error {
	explicit := path != ""
	if path == "" {
		path = os.Getenv("PACKET_CONFIG")
		explicit = path != ""
	}
	if path == "" {
		path = "config/config.json"
	}
	x := defaults()
	b, err := os.ReadFile(path)
	if err != nil && (explicit || !errors.Is(err, os.ErrNotExist)) {
		return fmt.Errorf("read configuration: %w", err)
	}
	if err == nil {
		if len(bytes.TrimSpace(b)) == 0 || bytes.TrimSpace(b)[0] != '{' {
			return errors.New("configuration must be a JSON object")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(b, &fields); err != nil {
			return fmt.Errorf("decode configuration: %w", err)
		}
		if err := rejectNull(fields); err != nil {
			return err
		}
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&x); err != nil {
			return fmt.Errorf("decode configuration: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return errors.New("configuration must contain one JSON object")
		}
	}
	for name, target := range map[string]*string{
		"STORAGE_MEDIA": &x.StorageMedia, "PACKETS_FILE_PATH": &x.PacketsFilePath,
		"PACKET_HOST_PORTS": &x.HostPorts, "PACKET_WRITE_TOKEN": &x.WriteToken,
		"MYSQL_DSN": &x.MySQL.DSN, "MYSQL_READ_DSN": &x.MySQL.ReadDSN,
	} {
		if value, exists := os.LookupEnv(name); exists {
			*target = value
		}
	}
	// Existing deployments use rds as the MySQL storage alias.
	if x.StorageMedia == "rds" {
		x.StorageMedia = "mysql"
	}
	if err := x.Validate(); err != nil {
		return err
	}
	mu.Lock()
	c = x
	mu.Unlock()
	return nil
}

// Get returns a copy so callers cannot mutate process-wide configuration.
func Get() *Config {
	mu.RLock()
	copy := c
	mu.RUnlock()
	return &copy
}

func rejectNull(fields map[string]json.RawMessage) error {
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("configuration field %s must not be null", key)
		}
		if strings.EqualFold(key, "MySQL") {
			var nested map[string]json.RawMessage
			if err := json.Unmarshal(value, &nested); err != nil {
				return fmt.Errorf("MySQL must be an object: %w", err)
			}
			if err := rejectNull(nested); err != nil {
				return err
			}
		}
	}
	return nil
}

func (x Config) Validate() error {
	for _, char := range x.WriteToken {
		if char <= 32 || char >= 127 {
			return errors.New("WriteToken must contain only visible ASCII characters without whitespace")
		}
	}
	if x.StorageMedia != "lfs" && x.StorageMedia != "mysql" {
		return errors.New("StorageMedia must be lfs or mysql")
	}
	if x.StorageMedia == "lfs" && strings.TrimSpace(x.PacketsFilePath) == "" {
		return errors.New("PacketsFilePath must not be empty")
	}
	if x.StorageMedia == "mysql" && strings.TrimSpace(x.MySQL.DSN) == "" {
		return errors.New("MySQL.DSN is required for mysql storage")
	}
	_, port, err := net.SplitHostPort(x.HostPorts)
	if err != nil {
		return errors.New("HostPorts must be a host:port address")
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return errors.New("HostPorts port must be between 1 and 65535")
	}
	m := x.MySQL
	if m.MaxOpen < 1 || m.MaxIdle < 0 || m.MaxIdle > m.MaxOpen ||
		m.ConnMaxLifetimeMin < 1 || m.QueryCacheTTLms < 0 || m.SlowQueryMs < 0 || m.QueryTimeoutMs < 1 {
		return errors.New("invalid MySQL pool or timeout settings")
	}
	return nil
}
