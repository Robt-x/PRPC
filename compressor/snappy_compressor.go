package compressor

import (
	"bytes"
	"github.com/golang/snappy"
	"io"
	"io/ioutil"
)

type SnappyCompressor struct{}

func (_ SnappyCompressor) Zip(data []byte) ([]byte, error) {
	buf := bytes.NewBuffer(nil)
	writer := snappy.NewBufferedWriter(buf)
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

func (_ SnappyCompressor) UnZip(data []byte) ([]byte, error) {
	reader := snappy.NewReader(bytes.NewBuffer(data))
	data, err := ioutil.ReadAll(reader)
	if err != nil || err != io.EOF || err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return data, err
}
