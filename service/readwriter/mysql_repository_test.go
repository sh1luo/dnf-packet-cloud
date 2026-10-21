package readwriter

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/config"

	mysqldriver "github.com/go-sql-driver/mysql"
	mysqlgorm "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type sqlStep struct {
	query   string
	args    []driver.Value
	columns []string
	rows    [][]driver.Value
	id      int64
	err     error
}

// A database/sql driver exercises GORM's actual SQL, transaction and binding
// behavior without requiring a daemon or adding a mock library dependency.
type storageSQLScript struct {
	mu       sync.Mutex
	t        *testing.T
	steps    []sqlStep
	closed   bool
	connects int
}

func (s *storageSQLScript) take(query string, args []driver.NamedValue) sqlStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.steps) == 0 {
		s.t.Errorf("unexpected SQL: %s", query)
		return sqlStep{err: errors.New("unexpected SQL")}
	}
	step := s.steps[0]
	s.steps = s.steps[1:]
	if !strings.Contains(query, step.query) {
		s.t.Errorf("SQL %q does not contain %q", query, step.query)
	}
	if step.args != nil {
		values := make([]driver.Value, len(args))
		for i, arg := range args {
			values[i] = arg.Value
		}
		if !reflect.DeepEqual(values, step.args) {
			s.t.Errorf("SQL args %v, want %v for %s", values, step.args, query)
		}
	}
	return step
}

func (s *storageSQLScript) Connect(context.Context) (driver.Conn, error) {
	s.mu.Lock()
	s.connects++
	s.mu.Unlock()
	return &scriptConn{s: s}, nil
}
func (s *storageSQLScript) Driver() driver.Driver { return scriptDriver{s} }

type scriptDriver struct{ s *storageSQLScript }

func (d scriptDriver) Open(string) (driver.Conn, error) { return &scriptConn{s: d.s}, nil }

type scriptConn struct{ s *storageSQLScript }

func (c *scriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected Prepare")
}
func (c *scriptConn) Close() error {
	c.s.mu.Lock()
	defer c.s.mu.Unlock()
	c.s.closed = true
	return nil
}
func (c *scriptConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *scriptConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	name := "BEGIN"
	if opts.ReadOnly {
		name = "BEGIN READ ONLY"
		if opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
			c.s.t.Error("read snapshot must use repeatable read")
		}
	}
	if err := c.s.take(name, nil).err; err != nil {
		return nil, err
	}
	return scriptTx{c.s}, nil
}
func (c *scriptConn) Ping(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		c.s.t.Error("ping lacks deadline")
	}
	return c.s.take("PING", nil).err
}
func (c *scriptConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		c.s.t.Error("query lacks deadline")
	}
	step := c.s.take(query, args)
	return scriptResult{id: step.id}, step.err
}
func (c *scriptConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if _, ok := ctx.Deadline(); !ok {
		c.s.t.Error("query lacks deadline")
	}
	step := c.s.take(query, args)
	return &scriptRows{columns: step.columns, rows: step.rows}, step.err
}

type scriptTx struct{ s *storageSQLScript }

func (tx scriptTx) Commit() error   { return tx.s.take("COMMIT", nil).err }
func (tx scriptTx) Rollback() error { return tx.s.take("ROLLBACK", nil).err }

type scriptResult struct{ id int64 }

func (r scriptResult) LastInsertId() (int64, error) { return r.id, nil }
func (r scriptResult) RowsAffected() (int64, error) { return 1, nil }

type scriptRows struct {
	columns []string
	rows    [][]driver.Value
}

func (r *scriptRows) Columns() []string { return r.columns }
func (r *scriptRows) Close() error      { return nil }
func (r *scriptRows) Next(dest []driver.Value) error {
	if len(r.rows) == 0 {
		return io.EOF
	}
	copy(dest, r.rows[0])
	r.rows = r.rows[1:]
	return nil
}

func scriptedSQLDB(t *testing.T, steps ...sqlStep) (*sql.DB, *storageSQLScript) {
	t.Helper()
	script := &storageSQLScript{t: t, steps: steps}
	db := sql.OpenDB(script)
	t.Cleanup(func() {
		_ = db.Close()
		script.mu.Lock()
		defer script.mu.Unlock()
		if len(script.steps) != 0 {
			t.Errorf("%d unexecuted SQL steps, next %s", len(script.steps), script.steps[0].query)
		}
	})
	return db, script
}

func scriptedMySQL(t *testing.T, steps ...sqlStep) *MySQLStorage {
	t.Helper()
	db, _ := scriptedSQLDB(t, steps...)
	orm, err := gorm.Open(mysqlgorm.New(mysqlgorm.Config{Conn: db, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &MySQLStorage{db: orm, sqlDB: db, queryTimeout: time.Second}
}

func TestMySQLListOnlySelectsMetadata(t *testing.T) {
	s := scriptedMySQL(t, sqlStep{
		query:   "SELECT `id`,`region`,`name`,`channel`,`uploader`,`time` FROM `cloud_packets` ORDER BY id ASC",
		columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(3), "cloud"}},
	})
	out, err := s.List(context.Background())
	if err != nil || len(out) != 1 || out[0].Id != 3 || len(out[0].UserPackets) != 0 {
		t.Fatalf("metadata query: %v %v", out, err)
	}
}

func TestMySQLGetUsesConsistentTargetSnapshotAndChildOrder(t *testing.T) {
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN READ ONLY"},
		sqlStep{query: "WHERE id = ?", columns: []string{"id", "name"}, rows: [][]driver.Value{{int64(8), "cloud"}}},
		sqlStep{query: "WHERE `user_packets`.`cloud_packet_id` = ? ORDER BY ordinal ASC",
			columns: []string{"cloud_packet_id", "ordinal", "id", "content"},
			rows:    [][]driver.Value{{int64(8), int64(0), int64(0), "first"}, {int64(8), int64(1), int64(0), "second"}}},
		sqlStep{query: "COMMIT"},
	)
	got, err := s.Get(context.Background(), 8)
	if err != nil || len(got.UserPackets) != 2 || got.UserPackets[0].Content != "first" ||
		got.UserPackets[1].Content != "second" || got.UserPackets[1].Id != 0 {
		t.Fatalf("ordered target: %v %v", got, err)
	}
}

func TestMySQLGetMissingReturnsSentinel(t *testing.T) {
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN READ ONLY"},
		sqlStep{query: "WHERE id = ?", columns: []string{"id"}},
		sqlStep{query: "ROLLBACK"},
	)
	if _, err := s.Get(context.Background(), 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing target: %v", err)
	}
}

func TestMySQLAppendIsIncrementalAndPreservesChildIDs(t *testing.T) {
	p := samplePacket(999)
	p.UserPackets = append(p.UserPackets, &packet.UserPacket{Id: 0, Name: "again", Content: "second"})
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN"},
		sqlStep{query: "INSERT INTO `cloud_packets`", id: 21},
		sqlStep{query: "INSERT INTO `user_packets` (`cloud_packet_id`,`ordinal`,`id`,`name`,`content`,`size`,`send_timing`) VALUES",
			args: []driver.Value{int64(21), int64(0), int64(0), "child", "payload", int64(7), "s",
				int64(21), int64(1), int64(0), "again", "second", int64(0), ""}},
		sqlStep{query: "INSERT INTO `cloud_packets`", id: 22},
		sqlStep{query: "INSERT INTO `user_packets`"},
		sqlStep{query: "COMMIT"},
	)
	ids, err := s.Append(context.Background(), []*packet.CloudPacket{p, samplePacket(1000)})
	if err != nil || !reflect.DeepEqual(ids, []int32{21, 22}) {
		t.Fatalf("incremental append: %v %v", ids, err)
	}
	if p.Id != 999 || p.UserPackets[0].Id != 0 || p.UserPackets[1].Id != 0 {
		t.Fatal("append mutated caller IDs")
	}
}

func TestMySQLAppendRollsBackWholeBatchOnChildFailure(t *testing.T) {
	failure := errors.New("child insertion failed")
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN"},
		sqlStep{query: "INSERT INTO `cloud_packets`", id: 21},
		sqlStep{query: "INSERT INTO `user_packets`", err: failure},
		sqlStep{query: "ROLLBACK"},
	)
	ids, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)})
	if ids != nil || !errors.Is(err, failure) {
		t.Fatalf("failed batch returned IDs or lost error: %v %v", ids, err)
	}
}

func TestMySQLDeleteOnlyTouchesLockedTargets(t *testing.T) {
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN"},
		sqlStep{query: "WHERE id BETWEEN ? AND ? ORDER BY id ASC FOR UPDATE",
			columns: []string{"id"}, rows: [][]driver.Value{{int64(3)}, {int64(5)}}},
		sqlStep{query: "DELETE FROM `user_packets` WHERE cloud_packet_id IN (?,?)", args: []driver.Value{int64(3), int64(5)}},
		sqlStep{query: "DELETE FROM `cloud_packets` WHERE id IN (?,?)", args: []driver.Value{int64(3), int64(5)}},
		sqlStep{query: "COMMIT"},
	)
	ids, err := s.DeleteRange(context.Background(), 2, 6)
	if err != nil || !reflect.DeepEqual(ids, []int32{3, 5}) {
		t.Fatalf("targeted delete: %v %v", ids, err)
	}
}

func TestMySQLCancelledInvalidAndEmptyCallsDoNotQuery(t *testing.T) {
	s := scriptedMySQL(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Append(ctx, []*packet.CloudPacket{samplePacket(0)}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.DeleteRange(ctx, 1, 2); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Append(context.Background(), []*packet.CloudPacket{nil}); !errors.Is(err, ErrInvalidPacket) {
		t.Fatal(err)
	}
	if _, err := s.DeleteRange(context.Background(), 2, 1); !errors.Is(err, ErrInvalidPacket) {
		t.Fatal(err)
	}
	if ids, err := s.Append(context.Background(), nil); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("empty append: %v %v", ids, err)
	}
}

func TestMySQLMissingDSNNeverFallsBackToEnvironment(t *testing.T) {
	t.Setenv("MYSQL_DSN", "not a usable DSN")
	if s, err := NewMySQLStorage(cfg.MySQLConfig{}); s != nil || err == nil || !strings.Contains(err.Error(), "DSN") {
		t.Fatalf("missing DSN: %v %v", s, err)
	}
}

func TestMySQLInitializationFailureClosesPool(t *testing.T) {
	failure := errors.New("connection refused")
	db, script := scriptedSQLDB(t, sqlStep{query: "PING", err: failure})
	s, err := initializeMySQLStorage(db, cfg.MySQLConfig{}, time.Second)
	if s != nil || !errors.Is(err, failure) {
		t.Fatalf("initialization failure: %v %v", s, err)
	}
	if !script.closed {
		t.Fatal("failed initialization leaked opened connection")
	}
}

func TestMySQLMigrationFailureClosesPool(t *testing.T) {
	failure := errors.New("schema permission denied")
	db, script := scriptedSQLDB(t,
		sqlStep{query: "PING"},
		sqlStep{query: "information_schema", err: failure},
	)
	s, err := initializeMySQLStorage(db, cfg.MySQLConfig{}, time.Second)
	if s != nil || !errors.Is(err, failure) {
		t.Fatalf("migration failure: %v %v", s, err)
	}
	if !script.closed {
		t.Fatal("migration failure leaked connection")
	}
}

func TestMySQLExplicitZeroIdleDisablesIdleConnections(t *testing.T) {
	failure := errors.New("stop after schema inspection")
	db, script := scriptedSQLDB(t,
		sqlStep{query: "PING"},
		sqlStep{query: "information_schema", err: failure},
	)
	if _, err := initializeMySQLStorage(db, cfg.MySQLConfig{MaxIdle: 0}, time.Second); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if script.connects != 2 {
		t.Fatalf("MaxIdle=0 must close the ping connection before schema I/O; opened %d connections", script.connects)
	}
}

func TestMySQLAutoMigrationFailureClosesPool(t *testing.T) {
	failure := errors.New("CREATE permission denied")
	db, script := scriptedSQLDB(t,
		sqlStep{query: "PING"},
		sqlStep{query: "information_schema.COLUMNS", columns: []string{"TABLE_NAME", "COLUMN_NAME", "COLUMN_KEY", "EXTRA"}},
		sqlStep{query: "SELECT DATABASE()", columns: []string{"DATABASE()"}, rows: [][]driver.Value{{"packet_cloud_test"}}},
		sqlStep{query: "SELECT SCHEMA_NAME", columns: []string{"SCHEMA_NAME"}, rows: [][]driver.Value{{"packet_cloud_test"}}},
		sqlStep{query: "SELECT count(*)", columns: []string{"count(*)"}, rows: [][]driver.Value{{int64(0)}}},
		sqlStep{query: "CREATE TABLE `cloud_packets`", err: failure},
	)
	s, err := initializeMySQLStorage(db, cfg.MySQLConfig{MaxIdle: 1}, time.Second)
	if s != nil || !errors.Is(err, failure) {
		t.Fatalf("AutoMigrate must return failure: %v %v", s, err)
	}
	if !script.closed {
		t.Fatal("AutoMigrate failure leaked connection")
	}
}

func TestMySQLRejectsLegacySchemaBeforeAutoMigration(t *testing.T) {
	db, _ := scriptedSQLDB(t,
		sqlStep{query: "PING"},
		sqlStep{query: "information_schema.COLUMNS",
			columns: []string{"TABLE_NAME", "COLUMN_NAME", "COLUMN_KEY", "EXTRA"},
			rows: [][]driver.Value{
				{"cloud_packets", "id", "PRI", ""},
				{"user_packets", "id", "PRI", "auto_increment"},
				{"user_packets", "cloud_packet_id", "MUL", ""},
			}},
	)
	if s, err := initializeMySQLStorage(db, cfg.MySQLConfig{MaxIdle: 1}, time.Second); s != nil ||
		err == nil || !strings.Contains(err.Error(), "002_storage_identity.sql") {
		t.Fatalf("legacy schema must require explicit upgrade: %v %v", s, err)
	}
}

func TestMySQLIDExhaustionRollsBack(t *testing.T) {
	for _, insertErr := range []*mysqldriver.MySQLError{
		{Number: 1467, Message: "Failed to read auto-increment value from storage engine"},
		{Number: 1264, Message: "Out of range value for column 'id' at row 1"},
		{Number: 1062, Message: "Duplicate entry '2147483647' for key 'cloud_packets.PRIMARY'"},
	} {
		t.Run(fmt.Sprint(insertErr.Number), func(t *testing.T) {
			s := scriptedMySQL(t,
				sqlStep{query: "BEGIN"},
				sqlStep{query: "INSERT INTO `cloud_packets`", err: insertErr},
				sqlStep{query: "ROLLBACK"},
			)
			if ids, err := s.Append(context.Background(), []*packet.CloudPacket{samplePacket(0)}); ids != nil || !errors.Is(err, ErrIDExhausted) {
				t.Fatalf("exhaustion must return sentinel and no IDs: %v %v", ids, err)
			}
		})
	}
}

func TestMySQLDeleteRollsBackIfParentDeleteFails(t *testing.T) {
	failure := errors.New("delete failed")
	s := scriptedMySQL(t,
		sqlStep{query: "BEGIN"},
		sqlStep{query: "FOR UPDATE", columns: []string{"id"}, rows: [][]driver.Value{{int64(3)}}},
		sqlStep{query: "DELETE FROM `user_packets` WHERE cloud_packet_id IN (?)"},
		sqlStep{query: "DELETE FROM `cloud_packets` WHERE id IN (?)", err: failure},
		sqlStep{query: "ROLLBACK"},
	)
	if ids, err := s.DeleteRange(context.Background(), 3, 3); ids != nil || !errors.Is(err, failure) {
		t.Fatalf("delete failure returned IDs or lost error: %v %v", ids, err)
	}
}

func TestMySQLConnectionHandshakeHasDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	defer func() {
		_ = listener.Close()
		if conn := <-accepted; conn != nil {
			_ = conn.Close()
		}
	}()
	start := time.Now()
	s, err := NewMySQLStorage(cfg.MySQLConfig{
		DSN: "packet_test@tcp(" + listener.Addr().String() + ")/packet_cloud_test", QueryTimeoutMs: 40,
	})
	if s != nil || err == nil {
		t.Fatal("silent server must fail initialization")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("handshake ignored connect/query deadline: %v", elapsed)
	}
}
