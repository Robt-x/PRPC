package registry

import (
	"PRPC/logger"
	"errors"
	"io/ioutil"
	"math"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type SelectMode uint

type Discovery interface {
	Refresh() error
	Update(service string, servers []string) error
	Get(service string, mode SelectMode) (string, error)
	GetAll(service string) ([]string, error)
}

type ServiceDiscovery struct {
	r             *rand.Rand
	mu            sync.RWMutex
	services      map[string][]string
	idx           map[string]int
	registryAddr  string
	discoveryAddr string
	timeout       time.Duration
	lastUpdate    time.Time
}

func NewServiceDiscovery(registryAddr string, timeout time.Duration) *ServiceDiscovery {
	if timeout == 0 {
		timeout = defaultUpdateTimeout
	}
	d := &ServiceDiscovery{
		r:            rand.New(rand.NewSource(time.Now().UnixNano())),
		services:     make(map[string][]string),
		idx:          make(map[string]int),
		registryAddr: registryAddr,
		timeout:      timeout,
	}
	go d.Subscribe()
	err := d.Refresh()
	if err != nil {
		return nil
	}
	return d
}

func (d *ServiceDiscovery) Update(serviceType string, services []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(services) == 0 {
		return nil
	}
	if d.services[serviceType] == nil {
		d.idx[serviceType] = d.r.Intn(math.MaxInt32 - 1)
	}
	d.services[serviceType] = services
	d.lastUpdate = time.Now()
	return nil
}

func (d *ServiceDiscovery) Refresh() error {
	d.mu.Lock()
	if d.lastUpdate.Add(d.timeout).After(time.Now()) {
		d.mu.Unlock()
		return nil
	}
	d.mu.Unlock()
	if d.registryAddr == "" {
		logger.Error("rpc client: registryAddr should not be empty")
		return errors.New("rpc client: registryAddr should not be empty")
	}
	c := http.Client{}
	req, _ := http.NewRequest("GET", d.registryAddr, nil)
	req.Header.Set("DiscoverType", "ServiceDiscover")
	resp, err := c.Do(req)
	if err != nil {
		return nil
	}
	body, _ := ioutil.ReadAll(resp.Body)
	services := string(body)
	if services == "" {
		logger.Error("rpc discovery services should not be empty")
		return errors.New("rpc discovery services should not be empty")
	}
	serviceTypes := strings.Split(services, ";")
	for _, serviceList := range serviceTypes {
		parts := strings.Split(serviceList, "+")
		name, ls := parts[0], strings.Split(parts[1], ",")
		err = d.Update(name, ls)
	}
	return nil
}

func (d *ServiceDiscovery) Get(serviceType string, mode SelectMode) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := len(d.services[serviceType])
	if n == 0 {
		return "", errors.New("rpc discovery: no available services")
	}
	switch mode {
	case Random:
		return d.services[serviceType][d.r.Intn(n)], nil
	case RoundRobin:
		host := d.services[serviceType][d.idx[serviceType]%n]
		d.idx[serviceType] = (d.idx[serviceType] + 1) % n
		return host, nil
	default:
		return "", errors.New("rpc discovery: not supported select mode")
	}
}

func (d *ServiceDiscovery) GetAll(serviceType string) ([]string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	hosts := make([]string, len(d.services))
	copy(hosts, d.services[serviceType])
	return hosts, nil
}

func (d *ServiceDiscovery) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch req.Method {
	case "POST":
		body, _ := ioutil.ReadAll(req.Body)
		services := string(body)
		serviceTypes := strings.Split(services, ";")
		for _, serviceList := range serviceTypes {
			parts := strings.Split(serviceList, "+")
			name, ls := parts[0], strings.Split(parts[1], ",")
			_ = d.Update(name, ls)
		}
	}
}

func (d *ServiceDiscovery) Subscribe() {
	l, _ := net.Listen("tcp", ":0")
	http.Handle(defaultDiscoveryPath, d)
	s := strings.Split(l.Addr().String(), ":")
	d.discoveryAddr = "http://localhost:" + s[len(s)-1]
	c := http.Client{}
	req, _ := http.NewRequest("POST", d.registryAddr, nil)
	req.Header.Set("RegisterType", "Discovery")
	req.Header.Set("Operate", "subscribe")
	req.Header.Set("Addr", d.discoveryAddr+defaultDiscoveryPath)
	resp, err := c.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		logger.Error("rpc discovery: register error")
		return
	}
	_ = http.Serve(l, nil)
}
