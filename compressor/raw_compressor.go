package compressor

type RawCompressor struct{}

func (_ *RawCompressor) Zip(data []byte) ([]byte, error) {
	return data, nil
}

func (_ *RawCompressor) UnZip(data []byte) ([]byte, error) {
	return data, nil
}
