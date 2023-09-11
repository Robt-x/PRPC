package header

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
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

func Test_Header(t *testing.T) {
	h := &RequestHeader{}
	htype := reflect.ValueOf(h).Elem().Type()
	fmt.Println(htype.NumField())
	ftype := htype.Field(3).Type
	v := reflect.New(ftype).Elem()
	b := uint64(1)
	v.Set(reflect.ValueOf(b))
	fmt.Println(v.Interface())
}

func Test_Mac(t *testing.T) {
	conn, err := net.Dial("tcp", "10.128.240.242:80")
	defer conn.Close()
	if err != nil {
		fmt.Println(err)
	}
	h := RequestHeader{
		CompressType: 2,
		Method:       "ADD不是",
		ID:           33,
	}
	data := h.Marshal()
	var w io.Writer = bufio.NewWriter(conn)
	err = sendFrame(w, data)
	w.(*bufio.Writer).Flush()
	if err != nil {
		fmt.Println(err)
	}
	select {}
}

func sendFrame(w io.Writer, data []byte) (err error) {
	var size [binary.MaxVarintLen64]byte
	if data == nil || len(data) == 0 {
		n := binary.PutUvarint(size[:], uint64(0))
		if err = write(w, size[:n]); err != nil {
			return err
		}
		return
	}
	if err = binary.Write(w, binary.BigEndian, uint64(len(data))); err != nil {
		return
	}
	if err = write(w, data); err != nil {
		return
	}
	return
}
func write(w io.Writer, data []byte) error {
	for index := 0; index < len(data); {
		n, err := w.Write(data[index:])
		if _, ok := err.(net.Error); !ok {
			return err
		}
		index += n
	}
	return nil
}
