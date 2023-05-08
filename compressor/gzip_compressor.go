package compressor

import (
	"bytes"
	"compress/gzip"
	"io"
	"io/ioutil"
)

type GzipCompressor struct{}

func (_ *GzipCompressor) Zip(data []byte) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	writer := gzip.NewWriter(buf)
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

func (_ *GzipCompressor) UnZip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err = ioutil.ReadAll(reader)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return data, err
}
