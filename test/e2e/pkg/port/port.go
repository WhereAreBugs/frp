package port

import (
	"fmt"
	"net"
	"strconv"
	"sync"

	"k8s.io/apimachinery/pkg/util/sets"
)

type Allocator struct {
	reserved sets.Set[int]
	used     sets.Set[int]
	mu       sync.Mutex
}

// NewAllocator return a port allocator for testing.
// Example: from: 10, to: 20, mod 4, index 1
// Reserved ports: 13, 17
func NewAllocator(from int, to int, mod int, index int) *Allocator {
	pa := &Allocator{
		reserved: sets.New[int](),
		used:     sets.New[int](),
	}

	for i := from; i <= to; i++ {
		if i%mod == index {
			pa.reserved.Insert(i)
		}
	}
	return pa
}

func (pa *Allocator) Get() int {
	return pa.GetByName("")
}

func (pa *Allocator) GetByName(portName string) int {
	var builder *nameBuilder
	if portName == "" {
		builder = &nameBuilder{}
	} else {
		var err error
		builder, err = unmarshalFromName(portName)
		if err != nil {
			fmt.Println(err, portName)
			return 0
		}
	}

	pa.mu.Lock()
	defer pa.mu.Unlock()

	probeTCP := func(host string, port int) error {
		l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		return l.Close()
	}
	probeUDP := func(host string, port int) error {
		udpAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err != nil {
			return err
		}
		udpConn, err := net.ListenUDP("udp", udpAddr)
		if err != nil {
			return err
		}
		return udpConn.Close()
	}

	// Probe on multiple loopback/any addresses to reduce false positives on platforms
	// where 0.0.0.0/127.0.0.1 binding semantics differ.
	probeHosts := []string{"127.0.0.1", "0.0.0.0", "::1"}

	for i := 0; i < 20; i++ {
		port := pa.getByRange(builder.rangePortFrom, builder.rangePortTo)
		if port == 0 {
			return 0
		}

		ok := true
		for _, host := range probeHosts {
			if err := probeTCP(host, port); err != nil {
				ok = false
				break
			}
			if err := probeUDP(host, port); err != nil {
				ok = false
				break
			}
		}
		if !ok {
			// Maybe not controlled by us, mark it used.
			pa.used.Insert(port)
			continue
		}

		pa.used.Insert(port)
		pa.reserved.Delete(port)
		return port
	}
	return 0
}

func (pa *Allocator) getByRange(from, to int) int {
	if from <= 0 {
		port, _ := pa.reserved.PopAny()
		return port
	}

	// choose a random port between from - to
	ports := pa.reserved.UnsortedList()
	for _, port := range ports {
		if port >= from && port <= to {
			return port
		}
	}
	return 0
}

func (pa *Allocator) Release(port int) {
	if port <= 0 {
		return
	}

	pa.mu.Lock()
	defer pa.mu.Unlock()

	if pa.used.Has(port) {
		pa.used.Delete(port)
		pa.reserved.Insert(port)
	}
}
