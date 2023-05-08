package serializer

import (
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
)

type ProtoSerializer struct {
}

var NotImplementProtoMessageError = errors.New("param does not implement proto.Message")
var Proto = ProtoSerializer{}

func (_ ProtoSerializer) Marshal(message interface{}) ([]byte, error) {
	var body proto.Message
	if message == nil {
		return []byte{}, nil
	}
	var ok bool
	if body, ok = message.(proto.Message); !ok {
		return nil, NotImplementProtoMessageError
	}
	return proto.Marshal(body)
}

func (_ ProtoSerializer) UnMarshal(data []byte, message interface{}) error {
	var body proto.Message
	if message == nil {
		return nil
	}
	var ok bool
	if body, ok = message.(proto.Message); !ok {
		fmt.Println(NotImplementProtoMessageError)
		return NotImplementProtoMessageError
	}
	err := proto.Unmarshal(data, body)
	return err
}
