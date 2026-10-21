package readwriter

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"packet_cloud/biz/model/hertz/packet"
	cfg "packet_cloud/config"
)

var (
	ErrNotFound       = errors.New("packet not found")
	ErrInvalidPacket  = errors.New("invalid packet")
	ErrCorruptStorage = errors.New("corrupt packet storage")
	ErrIDExhausted    = errors.New("packet ID exhausted")
)

// Repository owns its resources and returns caller-owned packet graphs.
// List contains metadata only; Get includes the original ordered children.
type Repository interface {
	List(context.Context) ([]*packet.CloudPacket, error)
	Get(context.Context, int32) (*packet.CloudPacket, error)
	Append(context.Context, []*packet.CloudPacket) ([]int32, error)
	DeleteRange(context.Context, int32, int32) ([]int32, error)
	Close() error
}

func Open(c cfg.Config) (Repository, error) {
	switch strings.ToLower(strings.TrimSpace(c.StorageMedia)) {
	case "", "lfs", "file":
		s, err := NewLocalFileSystem(c.PacketsFilePath)
		if err != nil {
			return nil, err
		}
		return s, nil
	case "mysql", "rds":
		s, err := NewMySQLStorage(c.MySQL)
		if err != nil {
			return nil, err
		}
		return s, nil
	default:
		return nil, fmt.Errorf("unsupported storage media %q", c.StorageMedia)
	}
}

type StorageMedia int

const (
	LFS StorageMedia = iota
	MySQL
)

// ReadWriter is the legacy administrative API. Request handlers must use one
// startup-owned Repository so mutations never become detached full rewrites.
type ReadWriter interface {
	ReadPacket() ([]*packet.CloudPacket, error)
	SavePacket([]*packet.CloudPacket) error
	Backup() error
}

func legacyRepository(media StorageMedia) (Repository, error) {
	c := *cfg.Get()
	if media == MySQL {
		c.StorageMedia = "mysql"
	} else if media != LFS {
		return nil, fmt.Errorf("unsupported storage media %d", media)
	}
	return Open(c)
}

func ReadPacket(media StorageMedia) ([]*packet.CloudPacket, error) {
	repo, err := legacyRepository(media)
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	return repo.(ReadWriter).ReadPacket()
}

func SavePacket(packets []*packet.CloudPacket, media StorageMedia) error {
	repo, err := legacyRepository(media)
	if err != nil {
		return err
	}
	defer repo.Close()
	return repo.(ReadWriter).SavePacket(packets)
}

func validatePackets(packets []*packet.CloudPacket) error {
	for i, p := range packets {
		if p == nil {
			return fmt.Errorf("%w: cloud packet %d is nil", ErrInvalidPacket, i)
		}
		for j, child := range p.UserPackets {
			if child == nil {
				return fmt.Errorf("%w: cloud packet %d child %d is nil", ErrInvalidPacket, i, j)
			}
		}
	}
	return nil
}

func clonePackets(packets []*packet.CloudPacket) []*packet.CloudPacket {
	out := make([]*packet.CloudPacket, len(packets))
	for i, p := range packets {
		out[i] = proto.Clone(p).(*packet.CloudPacket)
	}
	return out
}

func packetMetadata(p *packet.CloudPacket) *packet.CloudPacket {
	return &packet.CloudPacket{
		Id: p.Id, Region: p.Region, Name: p.Name, Channel: p.Channel,
		Uploader: p.Uploader, Time: p.Time, UserPackets: []*packet.UserPacket{},
	}
}
