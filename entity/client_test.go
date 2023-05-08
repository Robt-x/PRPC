package entity

import (
	"PRPC/codec"
	"PRPC/message"
	"context"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

//func foo(xc *XClient, ctx context.Context, typ, serviceMethod string, args *Args) {
//	var reply int
//	var err error
//	switch typ {
//	case "call":
//		err = xc.Call(ctx, serviceMethod, args, &reply)
//	case "broadcast":
//		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
//	}
//	if err != nil {
//		log.Printf("%s %s error: %v", typ, serviceMethod, err)
//	} else {
//		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.Num1, args.Num2, reply)
//	}
//}
//func call(addr1, addr2 string) {
//	d := NewServicesDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := NewClientWithDiscovery(d, Random, nil)
//	defer func() { _ = xc.Close() }()
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			foo(xc, context.Background(), "call", "Foo.Sum", &Args{Num1: i, Num2: i * i})
//		}(i)
//	}
//	wg.Wait()
//}
//
//func broadcast(addr1, addr2 string) {
//	d := NewServicesDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := NewClientWithDiscovery(d, Random, nil)
//	defer func() { _ = xc.Close() }()
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			foo(xc, context.Background(), "broadcast", "Foo.Sum", &Args{Num1: i, Num2: i * i})
//			ctx, _ := context.WithTimeout(context.Background(), time.Second*2)
//			foo(xc, ctx, "broadcast", "Foo.Sleep", &Args{Num1: i, Num2: i * i})
//		}(i)
//	}
//	wg.Wait()
//}
func startServer(addrCh chan string) {
	l, _ := net.Listen("tcp", ":0")
	server := NewServer()
	err := server.Register(&message.ArithService{})
	if err != nil {
		fmt.Println(err)
	}
	addrCh <- l.Addr().String()
	server.Accept(l)
}

//func Test(t *testing.T) {
//	logger.SetFlags(0)
//	ch1 := make(chan string)
//	ch2 := make(chan string)
//	// start two servers
//	go startServer(ch1)
//	go startServer(ch2)
//
//	addr1 := <-ch1
//	addr2 := <-ch2
//	//
//	d := NewServicesDiscovery([]string{"tcp@" + addr1, "tcp@" + addr2})
//	xc := NewClientWithDiscovery(d, RoundRobin, nil)
//	defer func() { _ = xc.Close() }()
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int64) {
//			defer wg.Done()
//			reply := message.ArithResponse{}
//			arg := message.ArithRequest{A: i, B: i * i}
//			_ = xc.Broadcast(context.Background(), "ArithService.Add", &arg, &reply)
//		}(int64(i))
//	}
//	wg.Wait()
//}

func Test2(t *testing.T) {
	addr := make(chan string)
	go startServer(addr)
	client, _ := Dial("tcp", <-addr, &codec.Consult{
		MagicNumber: codec.MagicNumber,
		CodecType:   codec.ProtoType,
	})
	defer func() { _ = client.Close() }()

	time.Sleep(time.Second)
	// send request & receive response
	var wg sync.WaitGroup
	for i := 0; i < 1; i++ {
		wg.Add(1)
		go func(i int64) {
			defer wg.Done()
			reply := &message.ArithResponse{}
			arg := &message.ArithRequest{A: i + 1, B: i * i}
			_ = client.Call(context.Background(), "ArithService.Add", arg, reply)
			_assert(reply.C == i+1+i*i, "error value: "+strconv.Itoa(int(reply.C)))
		}(int64(i))
	}
	wg.Wait()
}

func Test3(t *testing.T) {
	addr := make(chan string)
	go startServer(addr)
	client, _ := XDial("tcp@"+<-addr, nil)
	fmt.Println(client.IsAvailable())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ticker := time.After(time.Second * 5)
		select {
		case <-ticker:
			fmt.Println("Terminate")
			_ = client.Close()
		}
		wg.Done()
	}()
	wg.Wait()
	fmt.Println(client.IsAvailable())
}
