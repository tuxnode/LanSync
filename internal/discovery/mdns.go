package discovery

import (
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	ServiceType = "_lansync._tcp"
	MaxNodes    = 100
)

func findBestInterface() *net.Interface {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				return &iface
			}
		}
	}
	return nil
}

func quietLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

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

	config := &mdns.Config{
		Zone:   service,
		Iface:  findBestInterface(),
		Logger: quietLogger(),
	}

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
	params.DisableIPv6 = true
	params.Interface = findBestInterface()
	params.Logger = quietLogger()

	err := mdns.Query(params)
	close(entriesChan)

	if err != nil {
		log.Printf("DiscoverNodes: Query Failed: %v", err)
	}
}
