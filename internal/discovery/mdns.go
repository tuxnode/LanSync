package discovery

import (
	"io"
	"log"
	"net"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/mdns"
)

const (
	ServiceType = "_lansync._tcp"
	MaxNodes    = 100
)

// InterfaceInfo 网卡简略信息，供 GUI 展示选择。
type InterfaceInfo struct {
	Name string
	Addr string
}

// ListInterfaces 列出所有可用（UP、非回环、有 IPv4）网卡。
func ListInterfaces() []InterfaceInfo {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var result []InterfaceInfo
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
				result = append(result, InterfaceInfo{
					Name: iface.Name,
					Addr: ipnet.IP.String(),
				})
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// FindInterfaceByName 按名称查找网卡，name 为空时返回 nil。
func FindInterfaceByName(name string) *net.Interface {
	if name == "" {
		return nil
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil
	}
	return iface
}

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

func resolveIface(ifaceName string) *net.Interface {
	if ifaceName != "" {
		iface := FindInterfaceByName(ifaceName)
		if iface != nil {
			return iface
		}
		log.Printf("StartServer: 指定网卡 %s 未找到，回退自动检测", ifaceName)
	}
	return findBestInterface()
}

// StartServer 启动 mDNS 广播服务。ifaceName 为空时自动检测最优网卡。
func StartServer(port int, ifaceName string) (*mdns.Server, error) {
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
		Iface:  resolveIface(ifaceName),
		Logger: quietLogger(),
	}

	server, err := mdns.NewServer(config)
	if err != nil {
		return nil, err
	}

	log.Printf("StartServer: mDNS server started on port %d", port)
	return server, nil
}

// DiscoverNodes 查询 mDNS 发现的节点。ifaceName 为空时自动检测最优网卡。
func DiscoverNodes(handle func(*mdns.ServiceEntry), ifaceName string) {
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
	params.Interface = resolveIface(ifaceName)
	params.Logger = quietLogger()

	err := mdns.Query(params)
	close(entriesChan)

	if err != nil {
		log.Printf("DiscoverNodes: Query Failed: %v", err)
	}
}
