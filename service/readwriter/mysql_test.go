package readwriter

import (
	"os"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestMySQLChildIdentityIsScopedToParent(t *testing.T) {
	child, err := schema.Parse(&UserPacketModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	ordinal := child.LookUpField("Ordinal")
	if len(child.PrimaryFields) != 2 || !child.LookUpField("CloudPacketID").PrimaryKey || ordinal == nil || !ordinal.PrimaryKey {
		t.Fatalf("child identity must be (cloud_packet_id,ordinal): %v", child.PrimaryFieldDBNames)
	}
	if child.LookUpField("ID").AutoIncrement || child.LookUpField("ID").PrimaryKey {
		t.Fatal("child IDs must never be automatically assigned")
	}
}

func TestMySQLParentIdentityIsAutoIncrement(t *testing.T) {
	parent, err := schema.Parse(&CloudPacketModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if !parent.LookUpField("ID").AutoIncrement {
		t.Fatal("parent ID must be allocated by the database")
	}
}

func TestMySQLForeignKeyMatchesSQLSchema(t *testing.T) {
	parent, err := schema.Parse(&CloudPacketModel{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	if name := parent.Relationships.Relations["UserPackets"].ParseConstraint().Name; name != "fk_user_packets_cloud_packet_id" {
		t.Fatalf("AutoMigrate must not create a duplicate FK: %s", name)
	}
}

func TestMySQLSchemaHasOrdinalIdentity(t *testing.T) {
	b, err := os.ReadFile("../../db/schema.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"`id` INT NOT NULL AUTO_INCREMENT", "`ordinal` INT NOT NULL", "PRIMARY KEY (`cloud_packet_id`,`ordinal`)"} {
		if !strings.Contains(string(b), fragment) {
			t.Errorf("schema missing %s", fragment)
		}
	}
}
