package registry

import (
	"PRPC/entity"
	"PRPC/message"
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
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
	HandleHTTP()
	go func() {
		ticker := time.Tick(time.Second * 2)
		for range ticker {
			RegistryState()
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
	if err != nil {
		fmt.Println(err)
	}
	server.UpdateToRegistry(regisAddr, "tcp@"+l.Addr().String())
	HearBeat(regisAddr, "tcp@"+l.Addr().String(), 0)
	wg.Done()
	server.Accept(l)
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
		log.Printf("%s %s error: %v", typ, serviceMethod, err)
	} else {
		log.Printf("%s %s success: %d + %d = %d", typ, serviceMethod, args.A, args.B, reply.C)
	}
}
func call(registry string) {
	d := NewServiceDiscovery(registry, 0)
	xc := entity.NewClientWithDiscovery(d, Random, nil)
	defer func() { _ = xc.Close() }()
	// send request & receive response
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
func broadcast(registry string) {
	d := NewServiceDiscovery(registry, 0)
	xc := entity.NewClientWithDiscovery(d, Random, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			foo(xc, context.Background(), "broadcast", "ArithService.Add", &message.ArithRequest{A: int64(i), B: int64(i * i)})
		}(i)
	}
	wg.Wait()
}
func Test(t *testing.T) {
	log.SetFlags(0)
	addr := make(chan string)
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(addr, &wg)
	wg.Wait()
	time.Sleep(time.Second)
	port := <-addr
	registryAddr := "http://localhost:" + port + "/_prpc_/registryAddr"
	wg.Add(2)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()
	time.Sleep(time.Second)
	call(registryAddr)
	//broadcast(registryAddr)
	select {}
}

func Test2(t *testing.T) {
	rd := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379", // url
		Password: "",
		DB:       0, // 0号数据库
	})
	result, err := rd.Ping(context.Background()).Result()
	if err != nil {
		fmt.Println("ping err :", err)
		return
	}
	fmt.Println(result)
}
