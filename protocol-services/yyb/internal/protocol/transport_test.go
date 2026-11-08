package protocol

import (
	"bufio"
	"net"
	"strings"
	"testing"
)

func TestParseTCPProxyHTTPAuth(t *testing.T) {
	proxy, err := parseTCPProxy("http://user:p%40ss@127.0.0.1:2022")
	if err != nil {
		t.Fatalf("parse proxy: %v", err)
	}
	if proxy.Scheme != "http" || proxy.Host != "127.0.0.1" || proxy.Port != "2022" {
		t.Fatalf("unexpected proxy address: %+v", proxy)
	}
	if proxy.Username != "user" || proxy.Password != "p@ss" {
		t.Fatalf("unexpected proxy credentials: %+v", proxy)
	}
}

func TestHTTPConnectSendsProxyAuthorization(t *testing.T) {
	client, server := net.Pipe()
	done := make(chan error, 1)
	go func() {
		defer server.Close()
		reader := bufio.NewReader(server)
		var lines []string
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				done <- err
				return
			}
			lines = append(lines, line)
			if line == "\r\n" {
				break
			}
		}
		if !strings.Contains(strings.Join(lines, ""), "Proxy-Authorization: Basic dXNlcjpwYXNz\r\n") {
			done <- errMissingProxyAuth
			return
		}
		_, _ = server.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		done <- nil
	}()

	if err := httpConnect(client, "example.com", 443, "user", "pass"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	client.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

var errMissingProxyAuth = &proxyAuthError{}

type proxyAuthError struct{}

func (*proxyAuthError) Error() string { return "proxy authorization header missing" }
