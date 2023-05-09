package entity

import (
	"PRPC/codec"
	rpcErrors "PRPC/errors"
	"PRPC/header"
	"PRPC/logger"
	"PRPC/pool"
	"PRPC/registry"
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"reflect"
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
	Error         error
	Done          chan *Call
}

func (call *Call) done() {
	call.Done <- call
	logger.Infof("rpc client: Call of %s %d done", call.ServiceMethod, call.Seq)
}

type Client struct {
	cc       codec.ClientCodec
	opt      *codec.Consult
	sending  sync.Mutex
	mu       sync.Mutex
	pending  map[uint64]*Call
	header   header.RPCHeader
	seq      uint64
	closing  bool
	shutdown bool
}

type newClientFunc func(conn net.Conn, opt *codec.Consult) (*Client, error)

type clientResult struct {
	client *Client
	err    error
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""
	if err := client.cc.WriteRequest(&client.header, call.Args); err != nil {
		if call := client.removeCall(seq); call != nil {
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
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
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

func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		logger.Error("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		logger.Error("rpc client: call failed: " + ctx.Err().Error())
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case <-call.Done:
		return call.Error
	}
}

func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		logger.Error(rpcErrors.ErrShutdownError)
		return 0, rpcErrors.ErrShutdownError
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	logger.Infof("rpc client: Call of %s %d registered", call.ServiceMethod, call.Seq)
	return call.Seq, nil
}

func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	logger.Infof("rpc client: remove Call of %s %d from client", call.ServiceMethod, call.Seq)
	return call
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

func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		logger.Error(rpcErrors.ErrShutdownError)
		return rpcErrors.ErrShutdownError
	}
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
	client := &Client{
		seq:     1,
		cc:      CodecFunc(conn, opt),
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	logger.Info("rpc client: client created successfully")
	return client, nil
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

func dialTimeout(f newClientFunc, network, address string, opt *codec.Consult) (client *Client, err error) {
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
		logger.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		logger.Info("rpc client: connection creation successfully")
		return result.client, result.err
	}
}

func Dial(network, address string, opt *codec.Consult) (client *Client, err error) {
	opt, err = parseOptions(opt)
	if err != nil {
		return nil, err
	}
	return dialTimeout(NewClient, network, address, opt)
}

func HTTPDial(network, address string, opt *codec.Consult) (*Client, error) {
	opt, err := parseOptions(opt)
	if err != nil {
		return nil, err
	}
	return dialTimeout(NewHTTPClient, network, address, opt)
}

func XDial(rpcAddr string, opt *codec.Consult) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return HTTPDial("tcp", addr, opt)
	default:
		return Dial(protocol, addr, opt)
	}
}

func parseOptions(opt *codec.Consult) (*codec.Consult, error) {
	if opt == nil {
		return codec.DefaultConsult, nil
	}
	opt.MagicNumber = codec.DefaultConsult.MagicNumber
	if opt.CodecType == "" {
		opt.CodecType = codec.DefaultConsult.CodecType
	}
	return opt, nil
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

func (xc *XClient) call(rpcAddr string, ctx context.Context, serviceMethod string, arg, reply interface{}) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, arg, reply)
}

func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(rpcAddr, ctx, serviceMethod, args, reply)
}

func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex // protect e and replyDone
	var e error
	replyDone := reply == nil // if reply is nil, don't need to set value
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		if e == nil {
			cancel()
		}
	}()
	for _, rpcAddr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var cloneReply interface{}
			if reply != nil {
				cloneReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(rpcAddr, ctx, serviceMethod, args, cloneReply)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel()
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(cloneReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
