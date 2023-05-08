package entity

import (
	"PRPC/codec"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/timewheel"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"time"
)

type Server struct {
	registry   string
	addr       string
	tw         *timewheel.TimeWheel
	serviceMap sync.Map
}

const (
	connected        = "200 Connected to TinyRPC"
	defaultRPCPath   = "/_pprc_"
	defaultDebugPath = "/debug/entity"
)

func NewServer() *Server {
	return &Server{}
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
	fmt.Println(111)
	if req.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", req.RemoteAddr, ": ", err.Error())
		return
	}
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	server.ServeConn(conn)
}

func (server *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, server)
	http.Handle(defaultDebugPath, debugHTTP{server})
	log.Println("rpc server debug path:", defaultDebugPath)
}

func (server *Server) ServeConn(conn io.ReadWriteCloser) {
	var opt codec.Consult
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		logger.Errorf("rpc server: options error: ", err)
		return
	}
	if opt.MagicNumber != codec.MagicNumber {
		logger.Errorf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	scodec := codec.NewServerCodecFuncMap[opt.CodecType]
	if scodec == nil {
		logger.Errorf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	server.serveCodec(scodec(conn, &opt), opt)
}

func (server *Server) serveCodec(sc codec.ServerCodec, opt codec.Consult) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(sc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(sc, req.h, invalidRequest, sending)
		}
		wg.Add(1)
		//go server.handleRequest(sc, req, wg, sending, opt.HandleTimeout)
		go server.handleRequest(sc, req, wg, sending, opt.HandleTimeout)
	}
	wg.Wait()
	_ = sc.Close()
}

type RPCRequest struct {
	h           *header.RPCHeader // header of RPCRequest
	argv, reply reflect.Value     // argv and reply of RPCRequest
	mtype       *methodType
	svc         *service
}

func (server *Server) readRequest(sc codec.ServerCodec) (*RPCRequest, error) {
	var h header.RPCHeader
	err := sc.ReadRequestHeader(&h)
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			logger.Error("rpc server: read header error:", err)
		}
		return nil, err
	}
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

func (server *Server) sendResponse(sc codec.ServerCodec, h *header.RPCHeader, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := sc.WriteResponse(h, body); err != nil {
		logger.Errorf("rpc server: write response error:", err)
	}
}

func (server *Server) handleRequest(sc codec.ServerCodec, req *RPCRequest, wg *sync.WaitGroup, sending *sync.Mutex, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	send := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.reply)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(sc, req.h, invalidRequest, sending)
			send <- struct{}{}
			return
		}
		server.sendResponse(sc, req.h, req.reply.Interface(), sending)
		send <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-send
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		server.sendResponse(sc, req.h, invalidRequest, sending)
	case <-called:
		<-send
	}
}

func (server *Server) Register(rcvr any) error {
	svr := newService(rcvr)
	if _, exist := server.serviceMap.LoadOrStore(svr.name, svr); exist {
		return errors.New("rpc: service already defined: " + svr.name)
	}
	return nil
}

func (server *Server) RemoveFromRegistry(svrName string) {
	_, loaded := server.serviceMap.LoadAndDelete(svrName)
	if !loaded {
		log.Printf("%s does not exist\n", svrName)
		return
	}
	if server.registry == "" {
		return
	}
	hClient := &http.Client{}
	req, _ := http.NewRequest("POST", server.registry, nil)
	req.Header.Set("X-Prpc-Server", server.addr)
	req.Header.Set("X-Prpc-Service", svrName)
	if _, err := hClient.Do(req); err != nil {
		log.Println("rpc server: register update err:", err)
	}
}

func (server *Server) UpdateToRegistry(registry string, addr string) {
	services := make([]string, 0)
	server.registry = registry
	server.addr = addr
	server.serviceMap.Range(func(key, value interface{}) bool {
		services = append(services, key.(string))
		return true
	})
	hClient := &http.Client{}
	req, _ := http.NewRequest("POST", server.registry, nil)
	req.Header.Set("X-Prpc-Server", server.addr)
	req.Header.Set("X-Prpc-Services", strings.Join(services, ","))
	if _, err := hClient.Do(req); err != nil {
		log.Println("rpc server: register update err:", err)
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

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

func HandleHTTP() {
	DefaultServer.HandleHTTP()
}

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}
