package readwriter

import (
    "github.com/pkg/errors"
    "packet_cloud/biz/model/hertz/packet"
    cfg "packet_cloud/config"
)

type StorageMedia int

const (
	LFS StorageMedia = iota
	MySQL
)

type ReadWriter interface {
    ReadPacket() ([]*packet.CloudPacket, error)
    SavePacket([]*packet.CloudPacket) error
    Backup() error
}

func newReadWriter(media StorageMedia) ReadWriter {
    if cfg.Get().StorageMedia == "mysql" {
        return NewMySQLStorageFromConfig()
    }
    switch media {
    case LFS:
        return &LocalFileSystem{}
    case MySQL:
        return NewMySQLStorageFromConfig()
    default:
        
    }
    return nil
}

func ReadPacket(media StorageMedia) ([]*packet.CloudPacket, error) {
	rw := newReadWriter(media)
	if rw == nil {
		return nil, errors.New("readWriter is nil")
	}

	packets, err := rw.ReadPacket()
	if err != nil {
		return nil, errors.Wrapf(err, "read packet error")
	}

	return packets, nil
}

func SavePacket(packets []*packet.CloudPacket, media StorageMedia) error {
	rw := newReadWriter(media)
	if rw == nil {
		return errors.New("readWriter is nil")
	}

	err := rw.SavePacket(packets)
	if err != nil {
		return errors.Wrapf(err, "save packet error")
	}

	return nil
}
