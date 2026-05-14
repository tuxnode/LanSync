package discovery

import (
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

func StartServer(port int) (*mdns.Server, error) {
	host, err := os.Hostname()
	if err != nil {
		log.Printf("StartServer: os.Hostname failed: %v, using fallback", err)
		host = "lansync-node"
	}
	host = strings.ReplaceAll(host, ".", "-")

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

	log.Printf("StartServer: mDNS server started on port %d", port)
	return server, nil
}

func DiscoverNodes(handle func(*mdns.ServiceEntry)) {
	entriesChan := make(chan *mdns.ServiceEntry, MaxNodes)

	go func() {
		for entry := range entriesChan {
			log.Printf("[Discovery] Found Node: %s, IP: %s, Port: %d",
				entry.Name, entry.AddrV4, entry.Port)
			handle(entry)
		}
	}()

	params := mdns.DefaultParams(ServiceType)
	params.Entries = entriesChan
	params.Timeout = time.Second * 2

	err := mdns.Query(params)
	close(entriesChan)

	if err != nil {
		log.Printf("DiscoverNodes: Query Failed: %v", err)
	}
}
