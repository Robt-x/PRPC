package rpcerrors

import "errors"

var (
	InvalidSequenceError        = errors.New("invalid sequence number in response")
	UnexpectedChecksumError     = errors.New("unexpected checksum")
	NotFoundCompressorError     = errors.New("not found compressor")
	CompressorTypeMismatchError = errors.New("request and response Compressor type mismatch")
	ErrShutdownError            = errors.New("connection is shut down")
	UnmarshalError              = errors.New("an error occurred in Unmarshal")
	RepeatRequestError          = errors.New("repeat Request")
	DialTimeoutError            = errors.New("rpc client: connect timeout")
)
