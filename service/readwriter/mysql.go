package readwriter

import (
    "context"
    "database/sql"
    "log"
    "os"
    "packet_cloud/biz/model/hertz/packet"
    "strconv"
    "strings"
    "time"

    _ "github.com/go-sql-driver/mysql"
)

type MySQLStorage struct {
    writeDB *sql.DB
    readDB  *sql.DB
    cache   []*packet.CloudPacket
    cacheAt time.Time
    cacheTTL time.Duration
    slowThreshold time.Duration
    queryTimeout time.Duration
}

func NewMySQLStorageFromEnv() *MySQLStorage {
    writeDSN := os.Getenv("MYSQL_DSN")
    if writeDSN == "" {
        return nil
    }
    readDSN := os.Getenv("MYSQL_READ_DSN")

    maxOpen := parseIntEnv("MYSQL_MAX_OPEN", 20)
    maxIdle := parseIntEnv("MYSQL_MAX_IDLE", 10)
    lifeMin := parseIntEnv("MYSQL_CONN_MAX_LIFETIME_MIN", 30)
    cacheTTLms := parseIntEnv("QUERY_CACHE_TTL_MS", 500)
    slowMS := parseIntEnv("SLOW_QUERY_MS", 200)
    qTimeoutMS := parseIntEnv("QUERY_TIMEOUT_MS", 3000)

    wdb := mustOpenWithRetry(writeDSN)
    if wdb == nil {
        return nil
    }
    wdb.SetMaxOpenConns(maxOpen)
    wdb.SetMaxIdleConns(maxIdle)
    wdb.SetConnMaxLifetime(time.Duration(lifeMin) * time.Minute)

    var rdb *sql.DB
    if readDSN != "" {
        rdb = mustOpenWithRetry(readDSN)
        if rdb == nil {
            rdb = wdb
        } else {
            rdb.SetMaxOpenConns(maxOpen)
            rdb.SetMaxIdleConns(maxIdle)
            rdb.SetConnMaxLifetime(time.Duration(lifeMin) * time.Minute)
        }
    } else {
        rdb = wdb
    }

    return &MySQLStorage{
        writeDB: wdb,
        readDB:  rdb,
        cacheTTL: time.Duration(cacheTTLms) * time.Millisecond,
        slowThreshold: time.Duration(slowMS) * time.Millisecond,
        queryTimeout: time.Duration(qTimeoutMS) * time.Millisecond,
    }
}

func (s *MySQLStorage) ReadPacket() ([]*packet.CloudPacket, error) {
    if s.cache != nil && time.Since(s.cacheAt) < s.cacheTTL {
        return s.cache, nil
    }

    ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
    start := time.Now()
    rows, err := s.readDB.QueryContext(ctx, `SELECT id, region, name, channel, uploader, time FROM cloud_packets ORDER BY id ASC`)
    cancel()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    cps := make([]*packet.CloudPacket, 0, 128)
    ids := make([]int32, 0, 128)
    for rows.Next() {
        var id int32
        var region, name, channel, uploader, tm string
        if err := rows.Scan(&id, &region, &name, &channel, &uploader, &tm); err != nil {
            return nil, err
        }
        cps = append(cps, &packet.CloudPacket{Id: id, Region: region, Name: name, Channel: channel, Uploader: uploader, Time: tm})
        ids = append(ids, id)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }

    if len(ids) > 0 {
        placeholders := make([]string, len(ids))
        args := make([]any, len(ids))
        for i, id := range ids {
            placeholders[i] = "?"
            args[i] = id
        }
        q := `SELECT id, cloud_packet_id, name, content, size, send_timing FROM user_packets WHERE cloud_packet_id IN (` + strings.Join(placeholders, ",") + `) ORDER BY cloud_packet_id, id`
        ctx2, cancel2 := context.WithTimeout(context.Background(), s.queryTimeout)
        urows, err := s.readDB.QueryContext(ctx2, q, args...)
        cancel2()
        if err != nil {
            return nil, err
        }
        defer urows.Close()
        upMap := make(map[int32][]*packet.UserPacket, len(ids))
        for urows.Next() {
            var id, cpID int32
            var name, content, send string
            var size int32
            if err := urows.Scan(&id, &cpID, &name, &content, &size, &send); err != nil {
                return nil, err
            }
            upMap[cpID] = append(upMap[cpID], &packet.UserPacket{Id: id, Name: name, Content: content, Size: size, SendTiming: send})
        }
        if err := urows.Err(); err != nil {
            return nil, err
        }
        for _, cp := range cps {
            cp.UserPackets = upMap[cp.Id]
        }
    }

    if dur := time.Since(start); dur > s.slowThreshold {
        log.Printf("slow query ReadPacket dur=%s", dur)
    }

    s.cache = cps
    s.cacheAt = time.Now()
    return cps, nil
}

func (s *MySQLStorage) SavePacket(packets []*packet.CloudPacket) error {
    ctx, cancel := context.WithTimeout(context.Background(), s.queryTimeout)
    tx, err := s.writeDB.BeginTx(ctx, nil)
    cancel()
    if err != nil {
        return err
    }
    _, err = tx.Exec(`DELETE FROM user_packets`)
    if err != nil {
        tx.Rollback()
        return err
    }
    _, err = tx.Exec(`DELETE FROM cloud_packets`)
    if err != nil {
        tx.Rollback()
        return err
    }
    cpStmt, err := tx.Prepare(`INSERT INTO cloud_packets (id, region, name, channel, uploader, time) VALUES (?, ?, ?, ?, ?, ?)`)
    if err != nil {
        tx.Rollback()
        return err
    }
    defer cpStmt.Close()
    upStmt, err := tx.Prepare(`INSERT INTO user_packets (id, cloud_packet_id, name, content, size, send_timing) VALUES (?, ?, ?, ?, ?, ?)`)
    if err != nil {
        tx.Rollback()
        return err
    }
    defer upStmt.Close()

    for _, cp := range packets {
        _, err = cpStmt.Exec(cp.Id, cp.Region, cp.Name, cp.Channel, cp.Uploader, cp.Time)
        if err != nil {
            tx.Rollback()
            return err
        }
        for _, up := range cp.UserPackets {
            _, err = upStmt.Exec(up.Id, cp.Id, up.Name, up.Content, up.Size, up.SendTiming)
            if err != nil {
                tx.Rollback()
                return err
            }
        }
    }
    if err = tx.Commit(); err != nil {
        return err
    }
    s.cache = packets
    s.cacheAt = time.Now()
    return nil
}

func (s *MySQLStorage) Backup() error {
    return nil
}

func mustOpenWithRetry(dsn string) *sql.DB {
    var db *sql.DB
    var err error
    backoff := []time.Duration{time.Millisecond * 200, time.Millisecond * 500, time.Second, time.Second * 2, time.Second * 5}
    for i := 0; i < len(backoff); i++ {
        db, err = sql.Open("mysql", dsn)
        if err == nil {
            pingCtx, cancel := context.WithTimeout(context.Background(), time.Second*2)
            err = db.PingContext(pingCtx)
            cancel()
            if err == nil {
                return db
            }
        }
        time.Sleep(backoff[i])
    }
    return nil
}

func parseIntEnv(key string, def int) int {
    v := os.Getenv(key)
    if v == "" {
        return def
    }
    n, err := strconv.Atoi(v)
    if err != nil {
        return def
    }
    return n
}
