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

func Test_Mac(t *testing.T) {
	conn, err := net.Dial("tcp", "10.128.240.242:80")
	defer conn.Close()
	if err != nil {
		fmt.Println(err)
	}
	h := RequestHeader{
		CompressType: 2,
		Method:       "ADD",
		ID:           33,
	}
	err = sendFrame(conn, h.Marshal())
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(00000)
	select {}
}

//func sendFrame(w io.Writer, data []byte) (err error) {
//	sendBytes := make([]byte, 8)
//	binary.BigEndian.PutUint64(sendBytes, uint64(len(data)))
//	sendBytes = append(sendBytes, data...)
//	write(w, sendBytes)
//	return
//}

func sendFrame(w io.Writer, data []byte) (err error) {
	var size [binary.MaxVarintLen64]byte
	w = bufio.NewWriter(w)
	if data == nil || len(data) == 0 {
		n := binary.PutUvarint(size[:], uint64(0))
		if err = write(w, size[:n]); err != nil {
			return err
		}
		return
	}
	n := binary.PutUvarint(size[:], uint64(len(data)))
	if err = binary.Write(w, binary.BigEndian, size[:n]); err != nil {
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
