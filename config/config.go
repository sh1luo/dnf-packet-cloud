package config

import (
	"encoding/json"
	"os"
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
	MySQL           MySQLConfig `json:"MySQL"`
}

var (
	once sync.Once
	c    *Config
)

func Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var x Config
	if err := json.Unmarshal(b, &x); err != nil {
		return err
	}
	c = &x
	return nil
}

func Get() *Config {
	once.Do(func() {
		p := "config/config.json"
		b, err := os.ReadFile(p)
		if err != nil {
			c = &Config{StorageMedia: "lfs", PacketsFilePath: "./packets", MySQL: MySQLConfig{MaxOpen: 20, MaxIdle: 10, ConnMaxLifetimeMin: 30, QueryCacheTTLms: 500, SlowQueryMs: 200, QueryTimeoutMs: 3000}}
			return
		}
		var x Config
		if err := json.Unmarshal(b, &x); err != nil {
			c = &Config{StorageMedia: "lfs", PacketsFilePath: "./packets", MySQL: MySQLConfig{MaxOpen: 20, MaxIdle: 10, ConnMaxLifetimeMin: 30, QueryCacheTTLms: 500, SlowQueryMs: 200, QueryTimeoutMs: 3000}}
			return
		}
		c = &x
	})
	return c
}
