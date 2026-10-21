package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadLegacyRDSAlias(t *testing.T) {
	for _, source := range []string{"file", "environment"} {
		t.Run(source, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			value := `{"StorageMedia":"rds","MySQL":{"DSN":"test:pass@tcp(localhost:3306)/packets"}}`
			if source == "environment" {
				value = `{"StorageMedia":"lfs","MySQL":{"DSN":"test:pass@tcp(localhost:3306)/packets"}}`
				t.Setenv("STORAGE_MEDIA", "rds")
			}
			if err := os.WriteFile(path, []byte(value), 0600); err != nil {
				t.Fatal(err)
			}
			if err := Load(path); err != nil {
				t.Fatalf("supported legacy rds configuration rejected: %v", err)
			}
			if got := Get().StorageMedia; got != "mysql" {
				t.Fatalf("rds must select canonical mysql storage, got %q", got)
			}
		})
	}
}

func TestLegacyRDSAliasRequiresDSN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"StorageMedia":"rds","MySQL":{"DSN":""}}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := *Get()
	if err := Load(path); err == nil || !strings.Contains(err.Error(), "MySQL.DSN") {
		t.Fatalf("rds must apply the mysql DSN requirement, got %v", err)
	}
	if *Get() != before {
		t.Fatal("invalid rds load changed the published snapshot")
	}
}

func TestLoadPrecedenceAndCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"StorageMedia":"lfs","PacketsFilePath":"custom-packets"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	got := Get()
	if got.PacketsFilePath != "custom-packets" {
		t.Fatalf("explicit Load overwritten: %+v", got)
	}
	if got.MySQL.MaxOpen != 20 || got.MySQL.QueryTimeoutMs != 3000 {
		t.Fatalf("missing defaults: %+v", got.MySQL)
	}
	got.PacketsFilePath = "mutated"
	if Get().PacketsFilePath != "custom-packets" {
		t.Fatal("caller mutated shared config")
	}
}

func TestInvalidConfigPreservesLastSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	for _, value := range []string{
		`{"StorageMedia":"typo"}`,
		`{"StorageMedia":"mysql","MySQL":{"DSN":""}}`,
		`{"MySQL":{"MaxOpen":-1}}`,
		`{"MySQL":{"MaxIdle":100}}`,
		`{"PacketsFilePath":""}`,
		`{"HostPorts":"bad-address"}`,
		`{"StorageMeda":"mysql"}`,
		`{"StorageMedia":null}`,
		`{"MySQL":{"MaxOpen":null}}`,
		`{"mysql":{"MaxOpen":null}}`,
		`{"WriteToken":null}`,
		`{"WriteToken":"secret "}`,
		`{"WriteToken":"two words"}`,
		`null`,
	} {
		before := *Get()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if err := Load(path); err == nil {
			t.Errorf("accepted invalid config: %s", value)
		}
		if *Get() != before {
			t.Errorf("failed load changed snapshot for %s", value)
		}
	}
}

func TestEnvironmentOverridesAndExplicitMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"PacketsFilePath":"from-file"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PACKET_CONFIG", path)
	t.Setenv("PACKETS_FILE_PATH", "from-env")
	t.Setenv("PACKET_HOST_PORTS", "127.0.0.1:18080")
	t.Setenv("PACKET_WRITE_TOKEN", "test-token")
	if err := Load(""); err != nil {
		t.Fatal(err)
	}
	got := Get()
	if got.PacketsFilePath != "from-env" || got.HostPorts != "127.0.0.1:18080" || got.WriteToken != "test-token" {
		t.Fatalf("environment overrides ignored: %+v", got)
	}
	if err := Load(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("missing explicit file accepted")
	}
}
