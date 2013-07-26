package docker

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

var (
	testBuf     = []byte("Buffalo buffalo Buffalo buffalo buffalo buffalo Buffalo buffalo")
	testBufSize = len(testBuf)
)

type EchoServer interface {
	Run()
	Close() error
	LocalAddr() net.Addr
}

type TCPEchoServer struct {
	listener net.Listener
	testCtx  *testing.T
	stopped  bool
}

type UDPEchoServer struct {
	conn    net.PacketConn
	testCtx *testing.T
}

func NewEchoServer(t *testing.T, proto, address string) EchoServer {
	var server EchoServer

	if strings.HasPrefix(proto, "tcp") {
		listener, err := net.Listen(proto, address)
		if err != nil {
			t.Fatal(err)
		}
		server = &TCPEchoServer{listener: listener, testCtx: t}
	} else {
		socket, err := net.ListenPacket(proto, address)
		if err != nil {
			t.Fatal(err)
		}
		server = &UDPEchoServer{conn: socket, testCtx: t}
	}
	//	t.Logf("EchoServer listening on %v/%v\n", proto, server.LocalAddr().String())
	println("EchoServer listening on %v/%v\n", proto, server.LocalAddr().String())

	return server
}

func (server *TCPEchoServer) Run() {
	go func() {
		println("BEGIN OF RUN")
		defer println("END OF RUN")
		for !server.stopped {
			println("PRE ACCEPT")
			client, err := server.listener.Accept()
			println("POST ACCEPT")
			if err != nil {
				return
			}
			func(client net.Conn) {
				println("Enter subroutine RUN")
				defer println("Leaver subroutine RUN")
				//				server.testCtx.Logf("TCP client accepted on the EchoServer\n")
				_, err := io.Copy(client, client)
				//				server.testCtx.Logf("%v bytes echoed back to the client\n", written)
				if err != nil {
					server.testCtx.Logf("can't echo to the client: %v\n", err.Error())
				}
				client.Close()
			}(client)
		}
	}()
}

func (server *TCPEchoServer) LocalAddr() net.Addr { return server.listener.Addr() }
func (server *TCPEchoServer) Close() error        { server.stopped = true; return server.listener.Close() }

func (server *UDPEchoServer) Run() {
	go func() {
		readBuf := make([]byte, 1024)
		for {
			read, from, err := server.conn.ReadFrom(readBuf)
			if err != nil {
				return
			}
			server.testCtx.Logf("Writing UDP datagram back")
			for i := 0; i != read; {
				written, err := server.conn.WriteTo(readBuf[i:read], from)
				if err != nil {
					break
				}
				i += written
			}
		}
	}()
}

func (server *UDPEchoServer) LocalAddr() net.Addr { return server.conn.LocalAddr() }
func (server *UDPEchoServer) Close() error        { return server.conn.Close() }

func testProxyAt(t *testing.T, proto string, proxy Proxy, addr string) {
	go proxy.Run()
	defer proxy.Close()

	client, err := net.Dial(proto, addr)
	if err != nil {
		t.Fatalf("Can't connect to the proxy: %v", err)
	}
	defer client.Close()

	client.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err = client.Write(testBuf); err != nil {
		t.Fatal(err)
	}
	recvBuf := make([]byte, testBufSize)
	if _, err = client.Read(recvBuf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(testBuf, recvBuf) {
		t.Fatal(fmt.Errorf("Expected [%v] but got [%v]", testBuf, recvBuf))
	}
}

func testProxy(t *testing.T, proto string, proxy Proxy) {
	testProxyAt(t, proto, proxy, proxy.FrontendAddr().String())
}

func TestNetProxyTCP4Proxy(t *testing.T) {
	displayFdGoroutines(t)
	defer panic("ok")
	defer displayFdGoroutines(t)

	backend := NewEchoServer(t, "tcp", "127.0.0.1:0")

	backend.Run()

	c, _ := net.Dial("tcp", backend.LocalAddr().String())
	c.Write([]byte("Hello world!"))
	c.Close()

	time.Sleep(3 * time.Second)
	backend.Close()

	// frontendAddr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	// proxy, err := NewProxy(frontendAddr, backend.LocalAddr())
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// testProxy(t, "tcp", proxy)
}

func TestNetProxyTCP6Proxy(t *testing.T) {
	displayFdGoroutines(t)
	defer displayFdGoroutines(t)

	backend := NewEchoServer(t, "tcp", "[::1]:0")
	defer backend.Close()

	backend.Run()
	frontendAddr := &net.TCPAddr{IP: net.IPv6loopback, Port: 0}
	proxy, err := NewProxy(frontendAddr, backend.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	testProxy(t, "tcp", proxy)
}

func TestNetProxyTCPDualStackProxy(t *testing.T) {
	displayFdGoroutines(t)
	defer displayFdGoroutines(t)

	// If I understand `godoc -src net favoriteAddrFamily` (used by the
	// net.Listen* functions) correctly this should work, but it doesn't.
	t.Skip("No support for dual stack yet")
	backend := NewEchoServer(t, "tcp", "[::1]:0")

	defer backend.Close()
	backend.Run()
	frontendAddr := &net.TCPAddr{IP: net.IPv6loopback, Port: 0}
	proxy, err := NewProxy(frontendAddr, backend.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	ipv4ProxyAddr := &net.TCPAddr{
		IP:   net.IPv4(127, 0, 0, 1),
		Port: proxy.FrontendAddr().(*net.TCPAddr).Port,
	}
	testProxyAt(t, "tcp", proxy, ipv4ProxyAddr.String())
}

func TestNetProxyUDP4Proxy(t *testing.T) {
	displayFdGoroutines(t)
	defer displayFdGoroutines(t)

	backend := NewEchoServer(t, "udp", "127.0.0.1:0")
	defer backend.Close()

	backend.Run()
	frontendAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	proxy, err := NewProxy(frontendAddr, backend.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	testProxy(t, "udp", proxy)
}

func TestNetProxyUDP6Proxy(t *testing.T) {
	displayFdGoroutines(t)
	defer displayFdGoroutines(t)

	backend := NewEchoServer(t, "udp", "[::1]:0")
	defer backend.Close()
	backend.Run()
	frontendAddr := &net.UDPAddr{IP: net.IPv6loopback, Port: 0}
	proxy, err := NewProxy(frontendAddr, backend.LocalAddr())
	if err != nil {
		t.Fatal(err)
	}
	testProxy(t, "udp", proxy)
}

func TestNetProxyUDPWriteError(t *testing.T) {
	displayFdGoroutines(t)
	defer displayFdGoroutines(t)

	frontendAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0}
	// Hopefully, this port will be free: */
	backendAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 25587}
	proxy, err := NewProxy(frontendAddr, backendAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	go proxy.Run()
	client, err := net.Dial("udp", "127.0.0.1:25587")
	if err != nil {
		t.Fatalf("Can't connect to the proxy: %v", err)
	}
	defer client.Close()
	// Make sure the proxy doesn't stop when there is no actual backend:
	client.Write(testBuf)
	client.Write(testBuf)
	backend := NewEchoServer(t, "udp", "127.0.0.1:25587")
	defer backend.Close()
	backend.Run()
	client.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err = client.Write(testBuf); err != nil {
		t.Fatal(err)
	}
	recvBuf := make([]byte, testBufSize)
	if _, err = client.Read(recvBuf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(testBuf, recvBuf) {
		t.Fatal(fmt.Errorf("Expected [%v] but got [%v]", testBuf, recvBuf))
	}
}