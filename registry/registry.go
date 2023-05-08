package registry

import (
	"PRPC/logger"
	"PRPC/timewheel"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	defaultPath                     = "/_prpc_/registry"
	defaultBeatTimeout              = time.Second * 5
	defaultUpdateTimeout            = time.Second * 5
	Random               SelectMode = iota
	RoundRobin
	P2C
	IPHash
	ConsistentHash
)

type SelectMode uint

type Discovery interface {
	Refresh(service string) error
	Update(servers []string) error
	Get(mode SelectMode) (string, error)
	GetAll() ([]string, error)
}

type ServicesDiscovery struct {
	r        *rand.Rand
	mu       sync.RWMutex
	services []string
	index    int
}

func NewServicesDiscovery(services []string) *ServicesDiscovery {
	d := &ServicesDiscovery{
		r:        rand.New(rand.NewSource(time.Now().UnixNano())),
		services: services,
	}
	d.index = d.r.Intn(math.MaxInt32 - 1)
	return d
}

func (d *ServicesDiscovery) Refresh() error {
	return nil
}

func (d *ServicesDiscovery) Update(services []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.services = services
	return nil
}

func (d *ServicesDiscovery) Get(mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.services)
	if n == 0 {
		return "", errors.New("rpc discovery: no available services")
	}
	switch mode {
	case Random:
		return d.services[d.r.Intn(n)], nil
	case RoundRobin:
		host := d.services[d.index%n]
		d.index = (d.index + 1) % n
		return host, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *ServicesDiscovery) GetAll() ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	hosts := make([]string, len(d.services))
	copy(hosts, d.services)
	return hosts, nil
}

type RegistryDiscovery struct {
	*ServicesDiscovery
	registry   string
	timeout    time.Duration
	lastUpdate time.Time
}

func NewRegistryDiscovery(registry string, timeout time.Duration) *RegistryDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	return &RegistryDiscovery{
		ServicesDiscovery: NewServicesDiscovery(make([]string, 0)),
		registry:          registry,
		timeout:           timeout,
	}
}

func (d *RegistryDiscovery) Update(services []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.services = services
	d.lastUpdate = time.Now()
	return nil
}

func (d *RegistryDiscovery) Refresh(service string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		return nil
	}
	logger.Infof("rpc registry: refresh servers from registry", d.registry)
	hc := http.Client{}
	req, _ := http.NewRequest("GET", d.registry, nil)
	if service != "" {
		req.Header.Set("X-Prpc-service", service)
	}
	resp, err := hc.Do(req)
	if err != nil {
		logger.Errorf("rpc registry refresh err:", err)
		return err
	}
	servers := strings.Split(resp.Header.Get("X-Prpc-Servers"), ",")
	if servers[0] == "" {
		logger.Error("rpc registry: no service found")
	}
	d.services = servers
	d.lastUpdate = time.Now()
	return nil
}

func (d *RegistryDiscovery) Get(mode SelectMode) (string, error) {
	err := d.Refresh("")
	if err != nil {
		return "", err
	}
	return d.ServicesDiscovery.Get(mode)
}

func (d *RegistryDiscovery) GetAll() ([]string, error) {
	if err := d.Refresh(""); err != nil {
		return nil, err
	}
	return d.ServicesDiscovery.GetAll()
}

type baseNode struct {
	Head    *linkNode
	Trailer *linkNode
}

func (b *baseNode) delete(n *linkNode) {
	pre := n.Pre
	next := n.Next
	if n == b.Trailer {
		if b.Head == n {
			b.Head = nil
			b.Trailer = nil
		} else {
			b.Trailer = pre
			pre.Next = nil
		}
	} else {
		if b.Head == n {
			b.Head = next
			next.Pre = nil
		} else {
			pre.Next = next
			next.Pre = pre
		}
	}
	n.Pre = nil
	n.Next = nil
}

func (b *baseNode) insert(n *linkNode) {
	if b.Head == nil {
		b.Head = n
		b.Trailer = n
	} else {
		b.Trailer.Next = n
		n.Pre = b.Trailer
		b.Trailer = n
	}
}

type linkNode struct {
	Item *ServerItem
	Pre  *linkNode
	Next *linkNode
}

type ServerItem struct {
	Addr     string
	t        *timewheel.Timer
	services map[string]*linkNode
	start    time.Time
}

type Registry struct {
	Timeout    time.Duration
	mu         sync.Mutex
	tw         *timewheel.TimeWheel
	serviceMap map[string]*baseNode
	servers    map[string]*ServerItem
}

func New(timeout time.Duration) *Registry {
	rg := &Registry{
		Timeout:    timeout,
		tw:         timewheel.NewTimeWheel(time.Second, 60),
		serviceMap: make(map[string]*baseNode),
		servers:    make(map[string]*ServerItem),
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
		p := ptr.Head
		for p != nil {
			item := p.Item
			fmt.Print(item.Addr + "\t")
			p = p.Next
		}
		fmt.Println()
	}
}

func (r *Registry) putServer(addr string, services []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.servers[addr]
	if s == nil {
		item := &ServerItem{
			Addr:     addr,
			services: make(map[string]*linkNode),
			start:    time.Now(),
		}
		item.t = timewheel.NewTimer(defaultBeatTimeout, func() {
			parts := strings.Split(addr, "@")
			_, addr := parts[0], parts[1]
			conn, err := net.Dial("tcp", addr)
			if err != nil {
				r.removeServer(item.Addr)
			}
			_ = conn.Close()
			item.start = time.Now()
		}, true)
		r.tw.AddTaskByTimer(item.t)
		r.servers[addr] = item
	} else {
		s.start = time.Now()
	}
	if services == nil {
		return
	}
	item := r.servers[addr]
	for _, service := range services {
		if _, ok := item.services[service]; !ok {
			node := &linkNode{
				Item: item,
				Pre:  nil,
				Next: nil,
			}
			item.services[service] = node
			if b, exist := r.serviceMap[service]; exist {
				b.insert(node)
			} else {
				r.serviceMap[service] = &baseNode{
					Head:    nil,
					Trailer: nil,
				}
				r.serviceMap[service].insert(node)
			}
			logger.Infof("registry: ", service, " register from ", addr)
		}
	}
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
	for k, node := range server.services {
		r.serviceMap[k].delete(node)
	}
	r.tw.RemoveTask(server.t)
}

func (r *Registry) removeServices(addr string, service string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item := r.servers[addr]
	if item == nil {
		logger.Errorf("registry: no available services from ", addr)
		return
	}
	if n, ok := item.services[service]; ok {
		r.serviceMap[service].delete(n)
		logger.Infof("registry: ", service, " removed from ", addr)
	}
}

func (r *Registry) aliveServer(service string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var alive []string
	list := r.serviceMap[service]
	for cur := list.Head; cur != nil; cur = cur.Next {
		s := cur.Item
		if r.Timeout == 0 || s.start.Add(r.Timeout).After(time.Now()) {
			alive = append(alive, s.Addr)
		} else {
			for k, n := range s.services {
				r.serviceMap[k].delete(n)
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
		if r.Timeout == 0 || s.start.Add(r.Timeout).After(time.Now()) {
			alive = append(alive, addr)
		} else {
			delete(r.servers, addr)
		}
	}
	sort.Strings(alive)
	return alive
}

func (r *Registry) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "GET":
		service := req.Header.Get("X-Prpc-service")
		if service == "" {
			w.Header().Set("X-Prpc-Servers", strings.Join(r.allAliveServer(), ","))
		} else {
			w.Header().Set("X-Prpc-Servers", strings.Join(r.aliveServer(service), ","))
		}
	case "POST":
		addr := req.Header.Get("X-Prpc-Server")
		services := req.Header.Get("X-Prpc-Services")
		service := req.Header.Get("X-Prpc-Service")
		if addr == "" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if services == "" {
			r.putServer(addr, nil)
		} else {
			r.putServer(addr, strings.Split(services, ","))
		}
		if service != "" {
			r.removeServices(addr, service)
		}
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (r *Registry) HandleHTTP(registryPath string) {
	http.Handle(registryPath, r)
	log.Println("rpc registry path: ", registryPath)
}

func HandleHTTP() {
	DefaultRegistry.HandleHTTP(defaultPath)
}

func RegistryState() {
	DefaultRegistry.RegistryState()
}
