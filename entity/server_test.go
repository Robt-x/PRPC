package entity

import (
	"fmt"
)

//type Foo struct{}
//
//type Args struct{ Num1, Num2 int }
//
//func (f Foo) Sum(args Args, reply *int) error {
//	*reply = args.Num1 + args.Num2
//	return nil
//}
//
func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}

//func startServer(addr chan string) {
//	if err := Register(&Foo{}); err != nil {
//		logger.Fatal("register error:", err)
//	}
//	// pick a free port
//	l, err := net.Listen("tcp", ":0")
//	if err != nil {
//		logger.Fatal("network error:", err)
//	}
//	logger.Println("start rpc server on", l.addr())
//	addr <- l.addr().String()
//	Accept(l)
//}
//func TestNewService(t *testing.T) {
//	logger.SetFlags(0)
//	addr := make(chan string)
//	go startServer(addr)
//	client, _ := Dial("tcp", <-addr)
//	defer func() { _ = client.Close() }()
//
//	time.Sleep(time.Second)
//	// send request & receive response
//	var wg sync.WaitGroup
//	for i := 0; i < 5; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			args := &Args{Num1: i, Num2: i * i}
//			ctx, _ := context.WithTimeout(context.Background(), time.Second*10)
//			var reply int
//			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
//				logger.Fatal("call Foo.Sum error:", err)
//			}
//			logger.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
//		}(i)
//	}
//	wg.Wait()
//}
//
//func TestMethodType_Call(t *testing.T) {
//	var foo Foo
//	s := newService(&foo)
//	mType := s.method["Sum"]
//	argv := mType.newArg()
//	replyv := mType.newReply()
//
//	argv.Set(reflect.ValueOf(Args{Num1: 1, Num2: 3}))
//	err := s.call(mType, argv, replyv)
//	fmt.Println(replyv.Elem().Interface())
//	_assert(err == nil && *replyv.Interface().(*int) == 4 && mType.NumCalls() == 1, "failed to call Foo.Sum")
//}
