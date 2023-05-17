package entity

import (
	"PRPC/codec"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/rpcerrors"
	"PRPC/timewheel"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/go-redis/redis/v8"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	connected        = "200 Connected to TinyRPC"
	defaultRPCPath   = "/_prpc_/server"
	defaultDebugPath = "/debug/entity"
)

type RPCRequest struct {
	h           *header.RPCHeader // header of RPCRequest
	argv, reply reflect.Value     // argv and reply of RPCRequest
	mtype       *methodType
	svc         *service
}

type Server struct {
	mu           sync.Mutex
	sending      sync.Mutex
	RegistryAddr string
	Addr         string
	tw           *timewheel.TimeWheel
	rd           *redis.Client
	WorkIDs      []bool
	serviceMap   sync.Map
}

func NewServer() *Server {
	IDs := make([]bool, 1024)
	s := &Server{
		rd: redis.NewClient(&redis.Options{
			Addr:     "127.0.0.1:6379",
			Password: "",
			DB:       0,
		}),
		tw:      timewheel.NewTimeWheel(time.Millisecond, 512),
		WorkIDs: IDs,
	}
	s.tw.Start()
	return s
}

var DefaultServer = NewServer()

var invalidRequest = struct{}{}

func (server *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			logger.Errorf("rpc server: accept err:", err)
		}
		logger.Info("rpc server: connection accepted successfully")
		go server.ServeConn(conn)
	}
}

func (server *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		ping := req.Header.Get("BEAT")
		if ping != "Ping" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("BEAT", "Pong")
	case "CONNECT":
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			logger.Errorf("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
			return
		}
		_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
		go server.ServeConn(conn)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT or GET\n")
	}
}

func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server rpc path:", defaultRPCPath)
}

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	var opt codec.Consult
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		logger.Errorf("rpc server: options error: ", err)
		return
	}
	server.mu.Lock()
	opt.NodeID = -1
	for i, f := range server.WorkIDs {
		if !f {
			opt.NodeID = int64(i)
			break
		}
	}
	if opt.NodeID != -1 {
		server.WorkIDs[opt.NodeID] = true
	}
	server.mu.Unlock()
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		logger.Error("rpc client: options error: ", err)
		_ = conn.Close()
		return
	}
	if opt.NodeID == -1 {
		logger.Errorf("rpc server: no available WorkID")
		return
	}
	if opt.MagicNumber != codec.MagicNumber {
		logger.Errorf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	codecFunc := codec.NewServerCodecFuncMap[opt.CodecType]
	if codecFunc == nil {
		logger.Errorf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	server.serveCodec(codecFunc(conn, &opt), opt)
}

func (server *Server) serveCodec(sc codec.ServerCodec, opt codec.Consult) {
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(sc, opt)
		if err != nil {
			if req == nil {
				break
			}
			if err != rpcerrors.RepeatRequestError {
				req.h.Error = err.Error()
				server.sendResponse(sc, req.h, invalidRequest)
			}
			logger.Errorf("repeat request %d", req.h.Seq)
			continue
		}
		wg.Add(1)
		go server.handleRequest(sc, req, wg, opt.HandleTimeout)
	}
	wg.Wait()
	_ = sc.Close()
}

func (server *Server) readRequest(sc codec.ServerCodec, opt codec.Consult) (*RPCRequest, error) {
	var h header.RPCHeader
	err := sc.ReadRequestHeader(&h)
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			logger.Error("rpc server: read header error:", err)
		}
		return nil, err
	}
	server.mu.Lock()
	res := server.rd.SetNX(context.Background(), strconv.FormatUint(h.Seq, 10), "0", opt.HandleTimeout)
	if !res.Val() {
		_ = sc.ReadRequestBody(nil)
		server.mu.Unlock()
		return &RPCRequest{h: &h}, rpcerrors.RepeatRequestError
	}
	logger.Infof("rpc server: add seq %d to redis\n", h.Seq)
	server.mu.Unlock()
	req := &RPCRequest{
		h: &h,
	}
	req.svc, req.mtype, err = server.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	var argIsPtr bool
	req.reply = req.mtype.newReply()
	req.argv, argIsPtr = req.mtype.newArg()
	if err = sc.ReadRequestBody(req.argv.Addr().Interface()); err != nil {
		logger.Errorf("rpc server: read argv err:", err)
	}
	if argIsPtr {
		req.argv = req.argv.Addr()
	}
	return req, nil
}

func (server *Server) sendResponse(sc codec.ServerCodec, h *header.RPCHeader, body interface{}) {
	server.sending.Lock()
	defer server.sending.Unlock()
	if err := sc.WriteResponse(h, body); err != nil {
		logger.Errorf("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(sc codec.ServerCodec, req *RPCRequest, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	calling := make(chan error)
	t := server.tw.AddTask(timeout, func() {
		calling <- errors.New(fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout))
	}, false)
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.reply)
		if server.tw.RemoveTask(t) {
			calling <- err
		}
	}()
	err := <-calling
	if err != nil {
		req.h.Error = err.Error()
		server.sendResponse(sc, req.h, invalidRequest)
	} else {
		server.sendResponse(sc, req.h, req.reply.Interface())
	}
}

func (server *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		logger.Error(err)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svcv, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		logger.Error(err)
		return
	}
	svc = svcv.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
		logger.Error(err)
	}
	return
}

func (server *Server) Register(rcvr any) error {
	svr := newService(rcvr)
	if _, exist := server.serviceMap.LoadOrStore(svr.name, svr); exist {
		return errors.New("rpc: service already defined: " + svr.name)
	}
	return nil
}

func (server *Server) RemoveFromRegistry(service string) {
	if server.RegistryAddr == "" {
		return
	}
	_, loaded := server.serviceMap.LoadAndDelete(service)
	if !loaded {
		logger.Errorf("%s does not exist\n", service)
		return
	}
	hClient := &http.Client{}
	body := []byte(service)
	req, _ := http.NewRequest("POST", server.RegistryAddr, bytes.NewBuffer(body))
	req.Header.Set("RegisterType", "Server")
	req.Header.Set("Server", server.Addr)
	req.Header.Set("Operate", "UnRegister")
	if _, err := hClient.Do(req); err != nil {
		logger.Errorf("rpc server: register update err:", err)
	}
}

func (server *Server) UpdateToRegistry(registry string, addr string) {
	server.RegistryAddr = registry
	server.Addr = addr
	services := make([]string, 0)
	server.serviceMap.Range(func(key, value interface{}) bool {
		services = append(services, key.(string))
		return true
	})
	body := []byte(strings.Join(services, ","))
	hClient := &http.Client{}
	req, _ := http.NewRequest("POST", server.RegistryAddr, bytes.NewBuffer(body))
	req.Header.Set("RegisterType", "Server")
	req.Header.Set("Server", server.Addr)
	req.Header.Set("Operate", "Register")
	if _, err := hClient.Do(req); err != nil {
		log.Println("rpc server: register update err:", err)
	}
}

func UpdateToRegistry(registry string, addr string) {
	DefaultServer.UpdateToRegistry(registry, addr)
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func HandleHTTP() {
	DefaultServer.HandleHTTP()
}

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}
