package header

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"reflect"
	"testing"
)

func TestByteOrder(t *testing.T) {
	data := []interface{}{1, 2, "231"}
	buffer := new(bytes.Buffer)
	for _, value := range data {
		_ = binary.Write(buffer, binary.BigEndian, value)
	}
	fmt.Println(buffer.Bytes())
	buffer = bytes.NewBuffer(buffer.Bytes())
	var result []interface{}
	for {
		var value interface{}
		err := binary.Read(buffer, binary.BigEndian, &value)
		if err != nil && err == io.EOF {
			break
		}
		if reflect.TypeOf(value).Kind() == reflect.String {
			fmt.Println(111)
		}
		result = append(result, value)
	}
	fmt.Println(result)
}
