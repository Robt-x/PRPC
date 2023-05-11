package PRPC

import (
	"PRPC/entity"
	"PRPC/logger"
	"PRPC/message"
	"PRPC/registry"
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

type Foo struct {
}

func (f *Foo) Sub() {
	time.Sleep(time.Second * 5)
}

func startRegistry(addr chan string, wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":0")
	registry.HandleHTTP()
	//go func() {
	//	ticker := time.Tick(time.Second * 2)
	//	for range ticker {
	//		registry.RegistryState()
	//	}
	//}()
	wg.Done()
	s := strings.Split(l.Addr().String(), ":")
	fmt.Printf("registry addr: %s\n", l.Addr().String())
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
	fmt.Printf("server addr: %s\n", l.Addr().String())
	wg.Done()
	//go func() {
	//	ticker := time.NewTicker(time.Second * 10)
	//	<-ticker.C
	//	_ = server.Register(&Foo{})
	//	server.UpdateToRegistry(regisAddr, "http@"+l.Addr().String())
	//	ticker = time.NewTicker(time.Second * 5)
	//	<-ticker.C
	//	server.RemoveFromRegistry("Foo")
	//}()
	//go func() {
	//	ticker := time.Tick(time.Second * 5)
	//	for range ticker {
	//		server.RemoveFromRegistry("ArithService")
	//	}
	//}()
	_ = http.Serve(l, nil)
	//server.Accept(l)
}
func call(registryAddr string) {
	d := registry.NewServiceDiscovery(registryAddr, 0)
	xc := entity.NewClientWithDiscovery(d, registry.Random, nil)
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			var reply message.ArithResponse
			args := &message.ArithRequest{A: int64(i), B: int64(2 * i)}
			err := xc.Call("ArithService.Add", args, &reply)
			if err != nil {
				logger.Errorf("%s error: %v", "ArithService.Add", err)
			} else {
				log.Printf("%s success: %d + %d = %d", "ArithService.Add", args.A, args.B, reply.C)
			}
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
	time.Sleep(20 * time.Second)
}

func TestRegistryToServer(t *testing.T) {
	log.SetFlags(0)
	logger.SetLevel(logger.ErrorLevel)
	addr := make(chan string)
	var wg sync.WaitGroup
	wg.Add(1)
	go startRegistry(addr, &wg)
	wg.Wait()
	time.Sleep(time.Second)
	wg.Add(3)
	port := <-addr
	registryAddr := "http://localhost:" + port + "/_prpc_/registry"
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	go startServer(registryAddr, &wg)
	wg.Wait()
	select {}
}

func TestDiscovery(t *testing.T) {
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
	wg.Wait()
	time.Sleep(time.Second)
	d := registry.NewServiceDiscovery(registryAddr, 0)
	go d.Subscribe()
	time.Sleep(20 * time.Second)
}

func TestSnowFlake(t *testing.T) {
	g := entity.Generator{}
	err := g.Init("2021-12-03", 1)
	if err != nil {
		return
	}
	g1id := g.GenID()
	g2 := entity.Generator{}
	err = g.Init("2021-12-03", int64(g1id))
	if err != nil {
		return
	}
	fmt.Println(g1id, g2.GenID())
}

func Test_repeat(t *testing.T) {
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
	wg.Wait()
	time.Sleep(2 * time.Second)
	d := registry.NewServiceDiscovery(registryAddr, 0)
	xc := entity.NewClientWithDiscovery(d, registry.Random, nil)
	var reply message.ArithResponse
	var err error
	args := &message.ArithRequest{A: int64(1), B: int64(5)}
	err = xc.Call("ArithService.Add", args, &reply)
	if err != nil {
		logger.Errorf("%s error: %v", "ArithService.Add", err)
	} else {
		log.Printf("%s success: %d + %d = %d", "ArithService.Add", args.A, args.B, reply.C)
	}
	time.Sleep(20 * time.Second)
}

var rd *redis.Client = redis.NewClient(&redis.Options{
	Addr:     "127.0.0.1:6379",
	Password: "",
	DB:       0,
})

func Test_redis(t *testing.T) {
	err := rd.SetNX(context.Background(), "k1", "v1", 0).Err()
	if err != nil {
		fmt.Println("set err")
		return
	}
	rd.Del(context.Background(), "k1")
	res := rd.SetNX(context.Background(), "k1", "v1", 0)
	if res.Val() {
		fmt.Println("add success")
	} else {
		fmt.Println("already exist")
	}
}
