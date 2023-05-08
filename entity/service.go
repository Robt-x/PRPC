package entity

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *methodType) NumCalls() uint64 {
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArg() (argv reflect.Value, argIsPtr bool) {
	argIsPtr = m.ArgType.Kind() == reflect.Pointer
	if argIsPtr {
		argv = reflect.New(m.ArgType.Elem()).Elem()
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return
}

func (m *methodType) newReply() reflect.Value {
	replytp := m.ReplyType.Elem()
	replyv := reflect.New(replytp)
	switch replytp.Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(replytp))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(replytp, 0, 0))
	}
	return replyv
}

type service struct {
	name   string
	typ    reflect.Type
	rcvr   reflect.Value
	method map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)
	s.typ = reflect.TypeOf(rcvr)
	s.rcvr = reflect.ValueOf(rcvr)
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mtyp := method.Type
		if mtyp.NumIn() != 3 || mtyp.NumOut() != 1 {
			continue
		}
		if mtyp.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}
		argType, replyType := mtyp.In(1), mtyp.In(2)
		if !isExportedOrBuiltinType(argType) || !isExportedOrBuiltinType(replyType) {
			continue
		}
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
	}
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	atomic.AddUint64(&m.numCalls, 1)
	F := m.method.Func
	returnValue := F.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValue[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}

func isExportedOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}
