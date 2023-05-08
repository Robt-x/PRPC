package codec

import (
	"PRPC/header"
	"PRPC/logger"
	"bufio"
	"encoding/gob"
	"io"
	"log"
)

type clientGobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func NewClientGobCodec(conn io.ReadWriteCloser, opt *Consult) ClientCodec {
	buf := bufio.NewWriter(conn)
	return &clientGobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (c *clientGobCodec) WriteRequest(r *header.RPCHeader, param any) error {
	defer func() {
		_ = c.buf.Flush()
	}()
	if err := c.enc.Encode(r); err != nil {
		_ = c.Close()
		log.Fatal("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := c.enc.Encode(param); err != nil {
		_ = c.Close()
		log.Fatal("rpc codec: gob error encoding header:", err)
		return err
	}
	return nil
}
func (c *clientGobCodec) ReadResponseHeader(r *header.RPCHeader) error {
	return c.dec.Decode(r)
}
func (c *clientGobCodec) ReadResponseBody(param any) error {
	return c.dec.Decode(param)
}
func (c *clientGobCodec) Close() error {
	err := c.conn.Close()
	if err != nil {
		logger.Error(err)
		return err
	}
	logger.Info("close connection successfully")
	return err
}

type serverGobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func NewServerGobCodec(conn io.ReadWriteCloser, opt *Consult) ServerCodec {
	buf := bufio.NewWriter(conn)
	return &serverGobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (s *serverGobCodec) ReadRequestHeader(r *header.RPCHeader) error {
	return s.dec.Decode(r)
}
func (s *serverGobCodec) ReadRequestBody(param any) error {
	return s.dec.Decode(param)
}
func (s *serverGobCodec) WriteResponse(r *header.RPCHeader, param any) error {
	defer func() {
		_ = s.buf.Flush()
	}()

	if err := s.enc.Encode(r); err != nil {
		_ = s.Close()
		log.Fatal("rpc codec: gob error encoding header:", err)
		return err
	}
	if err := s.enc.Encode(param); err != nil {
		_ = s.Close()
		log.Fatal("rpc codec: gob error encoding header:", err)
		return err
	}
	return nil
}
func (s *serverGobCodec) Close() error {
	err := s.conn.Close()
	if err != nil {
		logger.Error(err)
		return err
	}
	logger.Info("close connection successfully")
	return err
}
