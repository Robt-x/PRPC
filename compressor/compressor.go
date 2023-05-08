package compressor

type CompressType uint16

const (
	Raw CompressType = iota
	Gzip
	Snappy
	Zlib
)

var Compressors = map[CompressType]Compressor{
	Raw:    &RawCompressor{},
	Gzip:   &GzipCompressor{},
	Snappy: &SnappyCompressor{},
	Zlib:   &ZlibCompressor{},
}

type Compressor interface {
	Zip([]byte) ([]byte, error)
	UnZip([]byte) ([]byte, error)
}
