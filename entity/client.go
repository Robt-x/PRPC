package entity

import (
	"PRPC/codec"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/pool"
	"PRPC/registry"
	rpcErrors "PRPC/rpcerrors"
	"PRPC/timewheel"
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultClientPath = "/_prpc_/clientDiscovery"
)

type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          any
	Reply         any
	tmr           *timewheel.Timer
	retries       uint64
	Error         error
	Done          chan *Call
}

func (call *Call) done() {
	call.Done <- call
}

type Client struct {
	cc        codec.ClientCodec
	opt       *codec.Consult
	sending   sync.Mutex
	mu        sync.Mutex
	tw        *timewheel.TimeWheel
	pending   map[uint64]*Call
	header    header.RPCHeader
	generator Generator
	closing   bool //主动关闭
	shutdown  bool //有错误发生时
}

type newClientFunc func(conn net.Conn, opt *codec.Consult) (*Client, error)

type clientResult struct {
	client *Client
	err    error
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = call.Seq
	client.header.Error = ""
	if err := client.cc.WriteRequest(&client.header, call.Args); err != nil {
		logger.Error(err)
		if call := client.removeCall(call.Seq); call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *Client) receive() {
	var err error
	var h = pool.RPCHeaderPool.Get().(*header.RPCHeader)
	defer func() {
		h.ResetHeader()
		pool.RPCHeaderPool.Put(h)
	}()
	for err == nil {
		h.ResetHeader()
		if err := client.cc.ReadResponseHeader(h); err != nil {
			break
		}
		call := client.removeCall(h.Seq)
		switch {
		case call == nil:
			err = client.cc.ReadResponseBody(nil)
			logger.Errorf("reply of %d discard", h.Seq)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			logger.Error(call.Error)
			err = client.cc.ReadResponseBody(nil)
			call.done()
		default:
			err = client.cc.ReadResponseBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	client.terminateCalls(err)
}

func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		logger.Error("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		retries:       1,
		Reply:         reply,
		Done:          done,
	}
	err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return call
	}
	t := client.tw.AddTask(client.opt.CallTimeout, func() {
		logger.Errorf("%d retrying with times %d.....", call.Seq, call.retries)
		if call.retries < client.opt.Retry {
			call.retries++
			client.send(call)
		} else {
			client.removeCall(call.Seq)
			call.Error = errors.New("call exceed max CallTimeout")
			call.done()
			return
		}
	}, true)
	call.tmr = t
	client.send(call)
	return call
}

func (client *Client) Call(serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	<-call.Done
	return call.Error
}

func (client *Client) registerCall(call *Call) error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		logger.Error(rpcErrors.ErrShutdownError)
		return rpcErrors.ErrShutdownError
	}
	call.Seq = client.generator.GenID()
	client.pending[call.Seq] = call
	logger.Infof("rpc client: %s Call with %d registered", call.ServiceMethod, call.Seq)
	return nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	if call, exist := client.pending[seq]; exist {
		client.tw.RemoveTask(call.tmr)
		delete(client.pending, seq)
		logger.Infof("rpc client: remove %s Call with seq %d from client", call.ServiceMethod, call.Seq)
		return call
	}
	logger.Errorf("rpc client: Call with seq %d does not exist", seq)
	return nil
}

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		logger.Error(rpcErrors.ErrShutdownError)
		return rpcErrors.ErrShutdownError
	}
	client.tw.Stop()
	client.closing = true
	return client.cc.Close()
}

func (client *Client) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.closing && !client.shutdown
}

func NewClient(conn net.Conn, opt *codec.Consult) (*Client, error) {
	CodecFunc := codec.NewClientCodecFuncMap[opt.CodecType]
	if CodecFunc == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		logger.Error("rpc client: codec error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		logger.Error("rpc client: options error: ", err)
		_ = conn.Close()
		return nil, err
	}
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		logger.Errorf("rpc server: options error: ", err)
		return nil, err
	}
	if opt.NodeID == -1 {
		logger.Errorf("rpc client: no available WorkID")
		return nil, errors.New("rpc client: no available WorkID")
	}
	client := &Client{
		cc:        CodecFunc(conn, opt),
		opt:       opt,
		pending:   make(map[uint64]*Call),
		generator: Generator{},
		tw:        timewheel.NewTimeWheel(time.Millisecond, 512),
	}
	client.tw.Start()
	err := client.generator.Init("2021-12-03", opt.NodeID)
	if err != nil {
		logger.Error(err)
		return nil, err
	}
	go client.receive()
	logger.Info("rpc client: client created successfully")
	return client, nil
}

func Dial(network, address string, opt *codec.Consult) (client *Client, err error) {
	return dialWithTimeout(NewClient, network, address, parseOptions(opt))
}

func NewHTTPClient(conn net.Conn, opt *codec.Consult) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
		logger.Error(err)
	}
	return nil, err
}

func HTTPDial(network, address string, opt *codec.Consult) (*Client, error) {
	return dialWithTimeout(NewHTTPClient, network, address, parseOptions(opt))
}

func dialWithTimeout(f newClientFunc, network, address string, opt *codec.Consult) (client *Client, err error) {
	conn, err := net.Dial(network, address)
	if err != nil {
		logger.Error("rpc client: connection creation failed")
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{
			client: client,
			err:    err,
		}
	}()
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.HandleTimeout):
		logger.Error(rpcErrors.DialTimeoutError)
		return nil, rpcErrors.DialTimeoutError
	case result := <-ch:
		logger.Infof("rpc client: connect to %s successfully", address)
		return result.client, result.err
	}
}

func XDial(rpcAddr string, opt *codec.Consult) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@Addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return HTTPDial("tcp", addr, opt)
	default:
		return Dial(protocol, addr, opt)
	}
}

func parseOptions(opt *codec.Consult) *codec.Consult {
	if opt == nil {
		return codec.DefaultConsult
	}
	opt.MagicNumber = codec.DefaultConsult.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = codec.DefaultConsult.CodecType
	}
	return opt
}

type XClient struct {
	d       registry.Discovery
	mode    registry.SelectMode
	opt     *codec.Consult
	mu      sync.Mutex
	clients map[string]*Client
}

func NewClientWithDiscovery(d registry.Discovery, mode registry.SelectMode, opt *codec.Consult) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*Client),
	}
}

func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for key, client := range xc.clients {
		_ = client.Close()
		delete(xc.clients, key)
	}
	return nil
}

func (xc *XClient) dial(rpcAddr string) (*Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
		go func() {
			ticker := time.After(time.Minute * 5)
			select {
			case <-ticker:
				_ = client.Close()
			}
		}()
	}
	return client, nil
}

func (xc *XClient) Call(serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(strings.Split(serviceMethod, ".")[0], xc.mode)
	if err != nil {
		return err
	}
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(serviceMethod, args, reply)
}
