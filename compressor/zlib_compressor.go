package compressor

import (
	"bytes"
	"compress/zlib"
	"io"
	"io/ioutil"
)

type ZlibCompressor struct{}

func (_ ZlibCompressor) Zip(data []byte) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	writer := zlib.NewWriter(buf)
	defer writer.Close()
	_, err := writer.Write(data)
	if err != nil {
		return nil, err
	}
	err = writer.Flush()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), err
}

func (_ ZlibCompressor) UnZip(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	data, err = ioutil.ReadAll(reader)
	if err != nil || err != io.EOF || err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return data, err
}
