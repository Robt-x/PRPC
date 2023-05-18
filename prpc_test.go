package PRPC

import (
	"PRPC/codec"
	"PRPC/entity"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/message"
	"PRPC/registry"
	"context"
	"fmt"
	"github.com/go-redis/redis/v8"
	"log"
	"net"
	"net/http"
	"net/rpc"
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
	xc := entity.NewClientWithDiscovery(d, registry.Random, &codec.Consult{
		MagicNumber:    codec.MagicNumber,
		CodecType:      codec.ProtoType,
		Retry:          2,
		CallTimeout:    time.Second,
		ConnectTimeout: time.Second,
		HandleTimeout:  time.Second,
	})
	defer func() { _ = xc.Close() }()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
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
	start := time.Now().UTC().UnixNano() / int64(time.Microsecond)
	call(registryAddr)
	end := time.Now().UTC().UnixNano() / int64(time.Microsecond)
	fmt.Printf("%d ns\n", end-start)
}
func startServerWithoutRegistry(addr chan string, wg *sync.WaitGroup) {
	l, _ := net.Listen("tcp", ":0")
	server := entity.NewServer()
	_ = server.Register(&message.ArithService{})
	//server.HandleHTTP()
	fmt.Printf("server addr: %s\n", l.Addr().String())
	wg.Done()
	addr <- l.Addr().String()
	//_ = http.Serve(l, nil)
	server.Accept(l)
}
func BenchmarkB(b *testing.B) {
	log.SetFlags(0)
	logger.SetLevel(logger.ErrorLevel)
	addr := make(chan string)
	var wg sync.WaitGroup
	wg.Add(1)
	go startServerWithoutRegistry(addr, &wg)
	wg.Wait()
	c, _ := entity.Dial("tcp", <-addr, &codec.Consult{
		MagicNumber:    codec.MagicNumber,
		CodecType:      codec.ProtoType,
		Retry:          2,
		CallTimeout:    time.Second,
		ConnectTimeout: time.Second,
		HandleTimeout:  time.Second,
	})
	var reply message.ArithResponse
	args := &message.ArithRequest{A: int64(10), B: int64(2 * 10)}
	for i := 0; i < b.N; i++ {
		_ = c.Call("ArithService.Add", args, &reply)
	}
	_ = c.Close()
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
	id := g.GenID()
	h := header.RequestHeader{ID: id}
	data := h.Marshal()
	h.ResetHeader()
	_ = h.UnMarshal(data)
	fmt.Println(h.ID)
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

func BenchmarkRPC(b *testing.B) {
	addr := make(chan string)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		l, _ := net.Listen("tcp", ":0")
		s := rpc.NewServer()
		_ = s.Register(&message.ArithService{})
		fmt.Printf("server addr: %s\n", l.Addr().String())
		wg.Done()
		addr <- l.Addr().String()
		s.Accept(l)
	}()
	wg.Wait()
	c, _ := rpc.Dial("tcp", <-addr)
	var reply message.ArithResponse
	args := &message.ArithRequest{A: int64(10), B: int64(2 * 10)}
	for i := 0; i < b.N; i++ {
		_ = c.Call("ArithService.Add", args, &reply)
	}
	_ = c.Close()
}

func Test_IP(t *testing.T) {
	var ipv4 string
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	// 取第一个非lo的网卡IP
	for _, addr := range addrs {
		// 这个网络地址是IP地址: ipv4, ipv6
		ipNet, isIpNet := addr.(*net.IPNet)
		if isIpNet && !ipNet.IP.IsLoopback() {
			// 跳过IPV6
			if ipNet.IP.To4() != nil {
				ipv4 = ipNet.IP.String() // 192.168.1.1
				return
			}
		}
	}
	fmt.Printf("ipv4 is <%s>", ipv4)
}
