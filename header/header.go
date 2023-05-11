package header

import (
	"PRPC/compressor"
	"PRPC/rpcerrors"
	"encoding/binary"
	"sync"
)

const (
	// MaxHeaderSize = 2 + 10 + 10 + 10 + 4 (10 refer to binary.MaxVarintLen64)
	MaxHeaderSize = 36
	Uint32Size    = 4
	Uint16Size    = 2
)

type RPCHeader struct {
	ServiceMethod string // format "Service.Method"
	Seq           uint64 // sequence number chosen by client
	Error         string
}

func (r *RPCHeader) ResetHeader() {
	r.Seq = 0
	r.ServiceMethod = ""
	r.Error = ""
}

type RequestHeader struct {
	sync.RWMutex
	CompressType compressor.CompressType
	Method       string
	ID           uint64
	RequestLen   uint32
	Checksum     uint32
}

func (r *RequestHeader) Marshal() []byte {
	r.RLock()
	defer r.RUnlock()
	idx := 0
	header := make([]byte, MaxHeaderSize+len(r.Method))
	binary.LittleEndian.PutUint16(header, uint16(r.CompressType))
	idx += Uint16Size
	idx += WriteString(header[idx:], r.Method)
	idx += binary.PutUvarint(header[idx:], r.ID)
	binary.LittleEndian.PutUint32(header[idx:], r.RequestLen)
	idx += Uint32Size
	binary.LittleEndian.PutUint32(header[idx:], r.Checksum)
	idx += Uint32Size
	return header[:idx]
}

func (r *RequestHeader) UnMarshal(data []byte) (err error) {
	r.Lock()
	defer r.Unlock()
	if len(data) == 0 {
		return rpcerrors.UnmarshalError
	}
	defer func() {
		if r := recover(); r != nil {
			err = rpcerrors.UnmarshalError
		}
	}()
	idx, size := 0, 0
	r.CompressType = compressor.CompressType(binary.LittleEndian.Uint16(data[idx:]))
	idx += Uint16Size
	r.Method, size = ReadString(data[idx:])
	idx += size
	r.ID, size = binary.Uvarint(data[idx:])
	idx += size
	r.RequestLen = binary.LittleEndian.Uint32(data[idx:])
	idx += Uint32Size
	r.Checksum = binary.LittleEndian.Uint32(data[idx:])
	return err
}

func (r *RequestHeader) GetCompressType() compressor.CompressType {
	r.RLock()
	defer r.RUnlock()
	return r.CompressType
}

func (r *RequestHeader) ResetHeader() {
	r.CompressType = 0
	r.ID = 0
	r.Method = ""
	r.RequestLen = 0
	r.Checksum = 0
}

type ResponseHeader struct {
	sync.RWMutex
	CompressType compressor.CompressType
	ID           uint64
	Error        string
	ResponseLen  uint32
	Checksum     uint32
}

func (r *ResponseHeader) Marshal() []byte {
	r.RLock()
	defer r.RUnlock()
	idx := 0
	header := make([]byte, MaxHeaderSize+len(r.Error))
	binary.LittleEndian.PutUint16(header, uint16(r.CompressType))
	idx += Uint16Size
	idx += binary.PutUvarint(header[idx:], r.ID)
	idx += WriteString(header[idx:], r.Error)
	binary.LittleEndian.PutUint32(header[idx:], r.ResponseLen)
	idx += Uint32Size
	binary.LittleEndian.PutUint32(header[idx:], r.Checksum)
	idx += Uint32Size
	return header[:idx]
}

func (r *ResponseHeader) UnMarshal(data []byte) (err error) {
	r.Lock()
	defer r.Unlock()
	if len(data) == 0 {
		return rpcerrors.UnmarshalError
	}
	defer func() {
		if r := recover(); r != nil {
			err = rpcerrors.UnmarshalError
		}
	}()
	idx, size := 0, 0
	r.CompressType = compressor.CompressType(binary.LittleEndian.Uint16(data[idx:]))
	idx += Uint16Size
	r.ID, size = binary.Uvarint(data[idx:])
	idx += size
	r.Error, size = ReadString(data[idx:])
	idx += size
	r.ResponseLen = binary.LittleEndian.Uint32(data[idx:])
	idx += Uint32Size
	r.Checksum = binary.LittleEndian.Uint32(data[idx:])
	return err
}

func (r *ResponseHeader) GetCompressType() compressor.CompressType {
	r.RLock()
	defer r.RUnlock()
	return r.CompressType
}

func (r *ResponseHeader) ResetHeader() {
	r.CompressType = 0
	r.ID = 0
	r.Error = ""
	r.ResponseLen = 0
	r.Checksum = 0
}

func ReadString(data []byte) (string, int) {
	idx := 0
	length, size := binary.Uvarint(data)
	idx += size
	str := string(data[idx : idx+int(length)])
	idx += len(str)
	return str, idx
}

func WriteString(data []byte, str string) int {
	idx := 0
	idx += binary.PutUvarint(data, uint64(len(str)))
	copy(data[idx:], str)
	idx += len(str)
	return idx
}
