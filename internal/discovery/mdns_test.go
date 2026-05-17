package discovery

import (
	"testing"
	"time"

	"github.com/hashicorp/mdns"
)

func TestMDNSDiscovery(t *testing.T) {
	testPort := 9999
	// 1. 启动一个模拟的 mDNS Server
	server, err := StartServer(testPort, "")
	if err != nil {
		t.Fatalf("Failed to start mDNS server: %v", err)
	}
	defer server.Shutdown()

	// 2. 创建一个通道来接收发现结果
	entriesCh := make(chan *mdns.ServiceEntry, 1)

	// 3. 构造查询参数
	params := mdns.DefaultParams(ServiceType)
	params.Entries = entriesCh
	params.DisableIPv6 = true // 某些测试环境下关闭 IPv6 更稳定
	// 缩短超时时间用于测试
	params.Timeout = 2 * time.Second

	// 4. 执行查询
	err = mdns.Query(params)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	// 5. 验证是否找到了我们自己启动的服务
	select {
	case entry := <-entriesCh:
		t.Logf("Successfully discovered node: %s at %s:%d", entry.Name, entry.AddrV4, entry.Port)
		if entry.Port != testPort {
			t.Errorf("Expected port %d, got %d", testPort, entry.Port)
		}
	case <-time.After(3 * time.Second):
		t.Error("Discovery timed out: no nodes found")
	}
}

func TestDiscoverNodes(t *testing.T) {
	// 1. 启动一个真实的节点供发现
	testPort := 8888
	server, _ := StartServer(testPort, "")
	defer server.Shutdown()

	// 2. 创建一个同步通道，用于在测试中获取异步结果
	resultChan := make(chan *mdns.ServiceEntry, 1)

	// 3. 调用重构后的函数，将结果转发到 resultChan
	DiscoverNodes(func(entry *mdns.ServiceEntry) {
		// 我们只关注测试启动的那个节点
		if entry.Port == testPort {
			resultChan <- entry
		}
	}, "")

	// 4. 验证结果
	select {
	case entry := <-resultChan:
		t.Logf("Validated DiscoverNodes: Found %s", entry.Name)
	case <-time.After(5 * time.Second):
		t.Fatal("DiscoverNodes failed to find the test node within timeout")
	}
}
