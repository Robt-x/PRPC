package pool

import (
	"PRPC/header"
	"sync"
)

var (
	RequestPool   sync.Pool
	ResponsePool  sync.Pool
	RPCHeaderPool sync.Pool
)

func init() {
	RequestPool = sync.Pool{
		New: func() any {
			return &header.RequestHeader{}
		},
	}
	ResponsePool = sync.Pool{
		New: func() any {
			return &header.ResponseHeader{}
		},
	}
	RPCHeaderPool = sync.Pool{
		New: func() any {
			return &header.RPCHeader{}
		},
	}
}
