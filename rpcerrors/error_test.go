package rpcerrors

import (
	"fmt"
	"testing"
)

func a() error {
	return RepeatRequestError
}

func Test(t *testing.T) {
	fmt.Println(RepeatRequestError == a())
}
