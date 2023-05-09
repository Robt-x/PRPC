package PRPC

import (
	"PRPC/entity"
	"PRPC/logger"
	"PRPC/message"
	"PRPC/registry"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func startRegistry(addr chan string, wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":0")
	registry.HandleHTTP()
	go func() {
		ticker := time.Tick(time.Second * 2)
		for range ticker {
			registry.RegistryState()
		}
	}()
	wg.Done()
	s := strings.Split(l.Addr().String(), ":")
	addr <- s[len(s)-1]
	_ = http.Serve(l, nil)
}
func startServer(regisAddr string, wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":0")
	server := entity.NewServer()
	err := server.Register(&message.ArithService{})
	server.HandleHTTP()
	if err != nil {
		fmt.Println(err)
	}
	server.UpdateToRegistry(regisAddr, "http@"+l.Addr().String())
	wg.Done()
	_ = http.Serve(l, nil)
}
func foo(xc *entity.XClient, ctx context.Context, typ, serviceMethod string, args *message.ArithRequest) {
	var reply message.ArithResponse
	var err error
	switch typ {
	case "call":
		err = xc.Call(ctx, serviceMethod, args, &reply)
	case "broadcast":
		err = xc.Broadcast(ctx, serviceMethod, args, &reply)
	}
	if err != nil {
		logger.Errorf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.A, args.B, reply.C)
	}
}
func call(registryaddr string) {
	d := registry.NewRegistryDiscovery(registryaddr, 0)
	xc := entity.NewClientWithDiscovery(d, registry.Random, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "call", "ArithService.Add", &message.ArithRequest{A: int64(i), B: int64(i * i)})
		}(i)
	}
	wg.Wait()
}
func Test(t *testing.T) {
	log.SetFlags(0)
	logger.SetLevel(logger.ErrorLevel)
	addr := make(chan string)
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(addr, &wg)
	wg.Wait()
	time.Sleep(time.Second)
	port := <-addr
	registryAddr := "http://localhost:" + port + "/_prpc_/registry"
	wg.Add(1)
	go startServer(registryAddr, &wg)
	//go startServer(registryAddr, &wg)
	wg.Wait()
	time.Sleep(2 * time.Second)
	call(registryAddr)
	select {}
}
