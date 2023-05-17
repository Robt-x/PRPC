package codec

import (
	"PRPC/compressor"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/pool"
	"PRPC/rpcerrors"
	"PRPC/serializer"
	"bufio"
	"fmt"
	"hash/crc32"
	"io"
	"log"
	"sync"
)

type clientProtoCodec struct {
	r          io.Reader
	w          io.Writer
	c          io.Closer
	compressor compressor.CompressType
	serializer serializer.Serializer
	response   header.ResponseHeader
}

func NewClientProtoCodec(conn io.ReadWriteCloser, opt *Consult) ClientCodec {
	defer logger.Info("rpc client: codec-connection bound successfully")
	return &clientProtoCodec{
		r:          bufio.NewReader(conn),
		w:          bufio.NewWriter(conn),
		c:          conn,
		compressor: opt.compressor,
		serializer: serializer.Proto,
	}
}

func (c *clientProtoCodec) WriteRequest(r *header.RPCHeader, param any) error {
	if _, ok := compressor.Compressors[c.compressor]; !ok {
		log.Println(rpcerrors.NotFoundCompressorError)
		return rpcerrors.NotFoundCompressorError
	}
	reqBody, err := c.serializer.Marshal(param)
	if err != nil {
		log.Println(err)
		return err
	}
	compressedBody, err := compressor.Compressors[c.compressor].Zip(reqBody)
	if err != nil {
		return err
	}
	h := pool.RequestPool.Get().(*header.RequestHeader)
	defer func() {
		h.ResetHeader()
		pool.RequestPool.Put(h)
	}()
	h.ID = r.Seq
	h.Method = r.ServiceMethod
	h.RequestLen = uint32(len(compressedBody))
	h.CompressType = compressor.CompressType(c.compressor)
	h.Checksum = crc32.ChecksumIEEE(compressedBody)
	if err := sendFrame(c.w, h.Marshal()); err != nil {
		return err
	}
	if err := write(c.w, compressedBody); err != nil {
		return err
	}
	err = c.w.(*bufio.Writer).Flush()
	if err != nil {
		return err
	}
	return err
}

func (c *clientProtoCodec) ReadResponseHeader(r *header.RPCHeader) error {
	c.response.ResetHeader()
	data, err := recvFrame(c.r)
	if err != nil {
		return err
	}
	err = c.response.UnMarshal(data)
	if err != nil {
		return err
	}
	r.Seq = c.response.ID
	r.Error = c.response.Error
	return err
}

func (c *clientProtoCodec) ReadResponseBody(param any) error {
	if param == nil {
		if c.response.ResponseLen != 0 {
			if err := read(c.r, make([]byte, c.response.ResponseLen)); err != nil {
				return err
			}
		}
	}
	respBody := make([]byte, c.response.ResponseLen)
	err := read(c.r, respBody)
	if err != nil {
		return err
	}
	if c.response.Checksum != 0 {
		if crc32.ChecksumIEEE(respBody) != c.response.Checksum {
			return rpcerrors.UnexpectedChecksumError
		}
	}
	if _, ok := compressor.Compressors[c.response.GetCompressType()]; !ok {
		return rpcerrors.CompressorTypeMismatchError
	}
	resp, err := compressor.Compressors[c.response.GetCompressType()].UnZip(respBody)
	if err != nil {
		return err
	}
	return c.serializer.UnMarshal(resp, param)
}

func (c *clientProtoCodec) Close() error {
	err := c.c.Close()
	if err != nil {
		logger.Error(err)
		return err
	}
	logger.Info("rpc client: close connection successfully")
	return nil
}

type reqCtx struct {
	requestID   uint64
	compareType compressor.CompressType
}

type serverProtoCodec struct {
	r          io.Reader
	w          io.Writer
	c          io.Closer
	request    header.RequestHeader
	serializer serializer.Serializer
	compressor compressor.CompressType
	mutex      sync.Mutex
	pending    map[uint64]*reqCtx
}

func NewServerProtoCodec(conn io.ReadWriteCloser, opt *Consult) ServerCodec {
	defer logger.Info("rpc server: codec-connection bound successfully")
	return &serverProtoCodec{
		r:          bufio.NewReader(conn),
		w:          bufio.NewWriter(conn),
		c:          conn,
		compressor: opt.compressor,
		serializer: serializer.Proto,
		pending:    make(map[uint64]*reqCtx),
	}
}

func (s *serverProtoCodec) ReadRequestHeader(r *header.RPCHeader) error {
	s.request.ResetHeader()
	data, err := recvFrame(s.r)
	if err != nil {
		return err
	}
	err = s.request.UnMarshal(data)
	if err != nil {
		return err
	}
	s.mutex.Lock()
	s.pending[s.request.ID] = &reqCtx{s.request.ID, s.request.GetCompressType()}
	r.ServiceMethod = s.request.Method
	r.Seq = s.request.ID
	s.mutex.Unlock()
	return err

}

func (s *serverProtoCodec) ReadRequestBody(param any) error {
	if param == nil {
		if s.request.RequestLen != 0 {
			if err := read(s.r, make([]byte, s.request.RequestLen)); err != nil {
				return err
			}
		}
	}
	reqBody := make([]byte, s.request.RequestLen)
	err := read(s.r, reqBody)
	if err != nil {
		return err
	}
	//fmt.Println(s.request.ID, "read", reqBody)
	if s.request.Checksum != 0 {
		if crc32.ChecksumIEEE(reqBody) != s.request.Checksum {
			log.Printf("UnexpectedChecksumError")
			return rpcerrors.UnexpectedChecksumError
		}
	}
	if _, ok := compressor.Compressors[s.request.GetCompressType()]; !ok {
		fmt.Println(rpcerrors.CompressorTypeMismatchError)
		return rpcerrors.CompressorTypeMismatchError
	}
	body, err := compressor.Compressors[s.request.GetCompressType()].UnZip(reqBody)
	if err != nil {
		return err
	}
	return s.serializer.UnMarshal(body, param)
}

func (s *serverProtoCodec) WriteResponse(r *header.RPCHeader, param any) error {
	s.mutex.Lock()
	reqCtx, ok := s.pending[r.Seq]
	if !ok {
		s.mutex.Unlock()
		return rpcerrors.InvalidSequenceError
	}
	delete(s.pending, r.Seq)
	s.mutex.Unlock()
	if r.Error != "" {
		param = nil
	}
	if _, ok := compressor.Compressors[reqCtx.compareType]; !ok {
		return rpcerrors.CompressorTypeMismatchError
	}
	var respBody []byte
	var err error
	if param != nil {
		respBody, err = s.serializer.Marshal(param)
		if err != nil {
			return err
		}
	}

	compressBody, err := compressor.Compressors[reqCtx.compareType].Zip(respBody)
	if err != nil {
		return err
	}
	h := pool.ResponsePool.Get().(*header.ResponseHeader)
	defer func() {
		h.ResetHeader()
		pool.ResponsePool.Put(h)
	}()
	h.ID = reqCtx.requestID
	h.Error = r.Error
	h.ResponseLen = uint32(len(compressBody))
	h.CompressType = reqCtx.compareType
	h.Checksum = crc32.ChecksumIEEE(compressBody)
	if err = sendFrame(s.w, h.Marshal()); err != nil {
		return err
	}
	if err = write(s.w, compressBody); err != nil {
		return err
	}
	err = s.w.(*bufio.Writer).Flush()
	return nil
}

func (s *serverProtoCodec) Close() error {
	err := s.c.Close()
	if err != nil {
		logger.Error(err)
		return err
	}
	logger.Info("rpc server: close connection successfully")
	return err
}
