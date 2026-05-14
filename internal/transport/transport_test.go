package transport_test

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/tuxnode/LanSync/internal/transport"
)

func TestNewTransport(t *testing.T) {
	TestTrans1 := transport.NewTransport(8888)
	TestTrans2 := transport.NewTransport(9999)

	if TestTrans1.MyID() == TestTrans2.MyID() {
		t.Errorf("NewTransport: UUID 构造出现相同")
	}

	if err := TestTrans1.Start(); err != nil && TestTrans1.Port() != 8888 {
		t.Errorf("NewTransport: 端口返回值不同")
	}

	if err := TestTrans2.Start(); err != nil && TestTrans2.Port() != 9999 {
		t.Errorf("NewTransport: 端口返回值不同")
	}

	TestTrans3 := transport.NewTransport(0)
	if err := TestTrans3.Start(); err != nil {
		t.Fatalf("TestNewTransport: Fail to Start Listening")
	}
	if TestTrans3.Port() <= 0 {
		t.Errorf("NewTransport: 构造端口为0的时候，端口小于等于0")
	}
	TestTrans1.Stop()
	TestTrans2.Stop()
	TestTrans3.Stop()
}

func TestServer(t *testing.T) {
	testTrans := transport.NewTransport(9999)
	testTrans.Start()

	// 检查端口是否可达
	conn, err := net.DialTimeout("tcp", "localhost:9999", time.Second)
	if err != nil {
		t.Fatalf("TestServer: 无法创建连接 %v", err)
	}
	conn.Close()

	// 测试占用端口后是否会返回error
	port := 19999
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		t.Fatalf("TestServer: 无法启动端口占用 %v", err)
	}
	portTrans := transport.NewTransport(port)
	if err := portTrans.Start(); err == nil {
		t.Errorf("TestServer: 启动被占用的端口期望返回error")
	}
	l.Close()
	testTrans.Stop()
}

func TestStop(t *testing.T) {
	tr := transport.NewTransport(9999)
	if err := tr.Start(); err != nil {
		t.Fatalf("TestStop: 无法启动服务 %v", err)
	}
	port := tr.Port()

	if err := tr.Stop(); err != nil {
		t.Fatalf("TestStop: 无法停止服务 %v", err)
	}

	_, err := net.DialTimeout("tcp", fmt.Sprintf(":%d", port), time.Second)
	if err == nil {
		t.Errorf("TestStop: 服务仍可连接，期望无法连接")
	}

	if tr.PeerCount() != 0 {
		t.Errorf("TestStop: 期望此时连接计数为0")
	}

	if err := tr.Stop(); err == nil {
		t.Errorf("TestStop: 两次Stop没有返回error")
	}

	tra := transport.NewTransport(0)
	if err := tra.Stop(); err == nil {
		t.Errorf("TestStop: 未Start直接Stop未触发error")
	}
}

func TestConnect(t *testing.T) {
	port := 1234
	tr := transport.NewTransport(port)

	if err := tr.Start(); err != nil {
		t.Fatalf("TestConnect: 启动服务失败 %v", err)
	}
	defer tr.Stop()

	// 传递错误的地址
	if err := tr.ConnectTo("Invalid Addr"); err == nil {
		t.Errorf("TestConnect: 传递无效地址未返回error，期望接收到error")
	}

	// 测试不可达地址
	start := time.Now()
	if err := tr.ConnectTo("192.0.2.1:9032"); err == nil {
		t.Errorf("TestConnect: 期望返回TimeOut Error")
	}
	elapsed := time.Since(start)

	if elapsed <= 500*time.Millisecond {
		t.Fatalf("TestConnect: 错误返回过快: %v", elapsed)
	}

	trB := transport.NewTransport(0)
	if err := trB.Start(); err != nil {
		t.Fatal(err)
	}
	defer trB.Stop()
	addr := fmt.Sprintf("127.0.0.1:%d", trB.Port())

	// A 连上 B
	trA := transport.NewTransport(0)
	if err := trA.Start(); err != nil {
		t.Fatal(err)
	}
	defer trA.Stop()

	if err := trA.ConnectTo(addr); err != nil {
		t.Fatal("首次连接失败:", err)
	}

	// 第二次连接同一地址 → 握手时 B 发现 PeerID 重复，返回 MsgHandShakeReject
	if err := trA.ConnectTo(addr); err == nil {
		t.Fatal("重复连接应返回 error")
	}
}
