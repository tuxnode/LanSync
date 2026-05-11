package discovery

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	ServiceType = "_lansync._tcp"
	MaxNodes    = 100
)

type Discover interface {
	StartServer(port int) (*mdns.Server, error)
	DiscoverNodes(handle func(*mdns.ServiceEntry))
}

// Local Server: port
func StartServer(port int) (*mdns.Server, error) {
	host, _ := os.Hostname()

	// 额外处理hostname
	host = strings.ReplaceAll(host, ".", "-")

	// defien mdns server mete data
	service, err := mdns.NewMDNSService(
		host, ServiceType, "", "", port, nil, []string{"v=1.0"},
	)
	if err != nil {
		return nil, err
	}

	config := &mdns.Config{Zone: service}

	server, err := mdns.NewServer(config)
	if err != nil {
		return nil, err
	}

	log.Print("StartServer: mdns server start")
	return server, nil
}

// Discover Nodes
func DiscoverNodes(handle func(*mdns.ServiceEntry)) {
	entriesChan := make(chan *mdns.ServiceEntry, MaxNodes)

	go func() {
		for entry := range entriesChan {
			fmt.Printf("[Discovery] Found Node: %s, IP: %s, Port: %d\n",
				entry.Name, entry.AddrV4, entry.Port)
			handle(entry)
		}
	}()

	params := mdns.DefaultParams(ServiceType)
	params.Entries = entriesChan
	params.Timeout = time.Second * 2

	err := mdns.Query(params)
	if err != nil {
		log.Printf("DiscoverNodes: Query Failt, %v\n", err)
	}
}
