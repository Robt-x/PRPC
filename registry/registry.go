package registry

import (
	"PRPC/logger"
	"PRPC/timewheel"
	"bytes"
	"container/list"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultRegistryPath             = "/_prpc_/registry"
	defaultBeatPath                 = "/_prpc_/server"
	defaultDiscoveryPath            = "/_prpc_/discovery"
	defaultBeatTimeout              = time.Second * 5
	defaultUpdateTimeout            = time.Second * 5
	Random               SelectMode = iota
	RoundRobin
)

type ServerItem struct {
	Addr     string
	t        *timewheel.Timer
	services map[string]*list.Element
	start    time.Time
}

type Registry struct {
	aliveTimeout time.Duration
	mu           sync.Mutex
	tw           *timewheel.TimeWheel
	serviceMap   map[string]*list.List
	servers      map[string]*ServerItem
	subscribers  map[string]*timewheel.Timer
}

func New(timeout time.Duration) *Registry {
	rg := &Registry{
		aliveTimeout: timeout,
		tw:           timewheel.NewTimeWheel(time.Second, 60),
		serviceMap:   make(map[string]*list.List),
		servers:      make(map[string]*ServerItem),
		subscribers:  make(map[string]*timewheel.Timer),
	}
	rg.tw.Start()
	return rg
}

var DefaultRegistry = New(defaultBeatTimeout)

func (r *Registry) RegistryState() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for service, ptr := range r.serviceMap {
		fmt.Print(service + ": ")
		for p := ptr.Front(); p != nil; p = p.Next() {
			item := p.Value.(*ServerItem)
			fmt.Print(item.Addr + "\t")
		}
		fmt.Println()
	}
}

func (r *Registry) addServer(addr string, services []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]
	if s == nil {
		item := &ServerItem{
			Addr:     addr,
			services: make(map[string]*list.Element),
			start:    time.Now(),
		}
		item.t = timewheel.NewTimer(defaultBeatTimeout, func() {
			parts := strings.Split(addr, "@")
			_, addr := parts[0], parts[1]
			port := strings.Split(addr, "[::]:")[1]
			url := "http://localhost:" + port + defaultBeatPath
			c := http.Client{}
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("BEAT", "Ping")
			resp, err := c.Do(req)
			if err != nil || resp.Header.Get("BEAT") != "Pong" {
				r.removeServer(item.Addr)
			}
			item.start = time.Now()
		}, true)
		r.tw.AddTaskByTimer(item.t)
		r.servers[addr] = item
	} else {
		s.start = time.Now()
	}
	r.addServices(addr, services)
}

func (r *Registry) removeServer(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	server := r.servers[addr]
	if server == nil {
		logger.Infof("rpc register: %s server does not exist", addr)
		return
	}
	delete(r.servers, addr)
	for k, elem := range server.services {
		r.serviceMap[k].Remove(elem)
	}
	r.tw.RemoveTask(server.t)
}

func (r *Registry) addServices(addr string, services []string) {
	item := r.servers[addr]
	for _, service := range services {
		if _, ok := item.services[service]; !ok {
			var elem *list.Element
			sl := r.serviceMap[service]
			if sl != nil {
				elem = sl.PushBack(item)
			} else {
				sl = list.New()
				elem = sl.PushBack(item)
				r.serviceMap[service] = sl
			}
			item.services[service] = elem
			logger.Infof("registryAddr: ", service, " register from ", addr)
		}
	}
}

func (r *Registry) removeService(addr string, service string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.servers[addr]
	if item == nil {
		logger.Errorf("registryAddr: no available services from ", addr)
		return
	}
	if elem, ok := item.services[service]; ok {
		r.serviceMap[service].Remove(elem)
		logger.Infof("registryAddr: ", service, " removed from ", addr)
		if r.serviceMap[service].Len() == 0 {
			delete(r.serviceMap, service)
		}
	}
}

func (r *Registry) aliveServer(service string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	ls := r.serviceMap[service]
	for cur := ls.Front(); cur != nil; cur = cur.Next() {
		s := cur.Value.(*ServerItem)
		if r.aliveTimeout == 0 || s.start.Add(r.aliveTimeout).After(time.Now()) {
			alive = append(alive, s.Addr)
		} else {
			for k, elem := range s.services {
				r.serviceMap[k].Remove(elem)
			}
			delete(r.servers, s.Addr)
		}
	}
	sort.Strings(alive)
	return alive
}

func (r *Registry) allAliveServer() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	for addr, s := range r.servers {
		if r.aliveTimeout == 0 || s.start.Add(r.aliveTimeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

func (r *Registry) addSubscriber(subscriber string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exist := r.subscribers[subscriber]; exist {
		return
	}
	t := r.tw.AddTask(defaultUpdateTimeout, func() {
		services := make([]string, 0)
		r.mu.Lock()
		for k, _ := range r.serviceMap {
			services = append(services, k)
		}
		r.mu.Unlock()
		body := ""
		for _, s := range services {
			body += s + "+" + strings.Join(r.aliveServer(s), ",") + ";"
		}
		body = body[:len(body)-1]
		c := http.Client{}
		req, _ := http.NewRequest("POST", subscriber, bytes.NewBuffer([]byte(body)))
		_, err := c.Do(req)
		if err != nil {
			r.removeSubscriber(subscriber)
		}
	}, true)
	r.subscribers[subscriber] = t
}

func (r *Registry) removeSubscriber(subscriber string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, exist := r.subscribers[subscriber]; exist {
		r.tw.RemoveTask(t)
		delete(r.subscribers, subscriber)
	}

}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		switch req.Header.Get("DiscoverType") {
		case "ServiceDiscover":
			services := make([]string, 0)
			r.mu.Lock()
			for k, _ := range r.serviceMap {
				services = append(services, k)
			}
			r.mu.Unlock()
			body := ""
			for _, s := range services {
				body += s + "+" + strings.Join(r.aliveServer(s), ",") + ";"
			}
			_, _ = w.Write([]byte(body[:len(body)-1]))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	case "POST":
		switch req.Header.Get("RegisterType") {
		case "Discovery":
			subscriber := req.Header.Get("Addr")
			operate := req.Header.Get("Operate")
			switch operate {
			case "subscribe":
				r.addSubscriber(subscriber)
				w.WriteHeader(http.StatusOK)
			case "Unsubscribe":
				r.removeSubscriber(subscriber)
				w.WriteHeader(http.StatusOK)
			}
		case "Server":
			addr := req.Header.Get("Server")
			Operate := req.Header.Get("Operate")
			btServices, _ := ioutil.ReadAll(req.Body)
			services := string(btServices)
			switch Operate {
			case "Register":
				r.addServer(addr, strings.Split(services, ","))
			case "UnRegister":
				serviceLs := strings.Split(services, ",")
				for _, service := range serviceLs {
					r.removeService(addr, service)
				}
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registryAddr path: ", registryPath)
}

func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultRegistryPath)
}

func RegistryState() {
	DefaultRegistry.RegistryState()
}
