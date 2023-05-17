package codec

import (
	"PRPC/compressor"
	"PRPC/header"
	"io"
	"time"
)

type Type string

const (
	GobType   Type = "application/gob"
	ProtoType Type = "application/proto"
)
const MagicNumber = 0x3bef5c

type ClientCodec interface {
	WriteRequest(*header.RPCHeader, any) error
	ReadResponseHeader(*header.RPCHeader) error
	ReadResponseBody(any) error
	Close() error
}

type ServerCodec interface {
	ReadRequestHeader(*header.RPCHeader) error
	ReadRequestBody(any) error
	WriteResponse(*header.RPCHeader, any) error
	Close() error
}

type NewClientCodecFunc func(io.ReadWriteCloser, *Consult) ClientCodec

type NewServerCodecFunc func(io.ReadWriteCloser, *Consult) ServerCodec

type Consult struct {
	MagicNumber    uint64 // MagicNumber marks this's a geerpc request
	CodecType      Type
	compressor     compressor.CompressType
	ConnectTimeout time.Duration
	CallTimeout    time.Duration
	Retry          uint64
	NodeID         int64
	HandleTimeout  time.Duration
}

var DefaultConsult = &Consult{
	MagicNumber:    MagicNumber,
	CodecType:      GobType,
	compressor:     compressor.Raw,
	Retry:          3,
	CallTimeout:    time.Second,
	ConnectTimeout: time.Second,
	HandleTimeout:  time.Second,
}

var NewClientCodecFuncMap map[Type]NewClientCodecFunc

var NewServerCodecFuncMap map[Type]NewServerCodecFunc

func init() {
	NewClientCodecFuncMap = make(map[Type]NewClientCodecFunc)
	NewServerCodecFuncMap = make(map[Type]NewServerCodecFunc)

	NewClientCodecFuncMap[GobType] = NewClientGobCodec
	NewClientCodecFuncMap[ProtoType] = NewClientProtoCodec
	NewServerCodecFuncMap[GobType] = NewServerGobCodec
	NewServerCodecFuncMap[ProtoType] = NewServerProtoCodec
}
