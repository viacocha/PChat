package main

import (
	"net"
	"testing"
	"time"
)

// TestClientInfo_Fields 测试ClientInfo字段
func TestClientInfo_Fields(t *testing.T) {
	client := &ClientInfo{
		PeerID:    "QmTest123",
		Username:  "Alice",
		Addresses: []string{"/ip4/127.0.0.1/tcp/9001"},
		LastSeen:  time.Now(),
	}

	if client.PeerID != "QmTest123" {
		t.Errorf("PeerID = %q, want %q", client.PeerID, "QmTest123")
	}
	if client.Username != "Alice" {
		t.Errorf("Username = %q, want %q", client.Username, "Alice")
	}
	if len(client.Addresses) != 1 {
		t.Errorf("Addresses length = %d, want 1", len(client.Addresses))
	}
	if client.LastSeen.IsZero() {
		t.Error("LastSeen should not be zero")
	}
}

// TestClientInfo_EmptyFields 测试空字段
func TestClientInfo_EmptyFields(t *testing.T) {
	client := &ClientInfo{}

	if client.PeerID != "" {
		t.Errorf("PeerID should be empty, got %q", client.PeerID)
	}
	if client.Username != "" {
		t.Errorf("Username should be empty, got %q", client.Username)
	}
	if client.Addresses != nil && len(client.Addresses) != 0 {
		t.Errorf("Addresses should be empty, got %v", client.Addresses)
	}
	if !client.LastSeen.IsZero() {
		t.Error("LastSeen should be zero")
	}
}

func setupRegistryClientTest(t *testing.T) (*RegistryServer, func()) {
	t.Helper()

	server := NewRegistryServer()
	prevDial := dialRegistryFunc
	prevDialTimeout := dialRegistryWithTimeoutFunc

	dialRegistryFunc = func(addr string) (net.Conn, error) {
		clientConn, serverConn := net.Pipe()
		go server.handleRequest(serverConn)
		return clientConn, nil
	}

	dialRegistryWithTimeoutFunc = func(addr string, timeout time.Duration) (net.Conn, error) {
		return dialRegistryFunc(addr)
	}

	return server, func() {
		dialRegistryFunc = prevDial
		dialRegistryWithTimeoutFunc = prevDialTimeout
	}
}

func newTestRegistryClient() *RegistryClient {
	return &RegistryClient{
		serverAddr: "mock",
		peerID:     "peer1",
		addresses:  []string{"/ip4/127.0.0.1/tcp/9001/p2p/peer1"},
		username:   "Alice",
	}
}

func TestRegistryClient_RegisterListLookup(t *testing.T) {
	server, cleanup := setupRegistryClientTest(t)
	defer cleanup()

	client := newTestRegistryClient()
	if err := client.Register(); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	clients, err := client.ListClients()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(clients) != 1 {
		t.Fatalf("expected 1 client, got %d", len(clients))
	}

	info, err := client.LookupClient("peer1")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if info.Username != "Alice" {
		t.Fatalf("expected Alice, got %s", info.Username)
	}

	if err := client.SendHeartbeat(); err != nil {
		t.Fatalf("heartbeat failed: %v", err)
	}

	server.mutex.RLock()
	lastSeen := server.clients["peer1"].LastSeen
	server.mutex.RUnlock()
	if lastSeen.IsZero() {
		t.Fatal("LastSeen should be updated after heartbeat")
	}
}

func TestRegistryClient_Unregister(t *testing.T) {
	server, cleanup := setupRegistryClientTest(t)
	defer cleanup()

	client := newTestRegistryClient()
	if err := client.Register(); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	if err := client.Unregister(); err != nil {
		t.Fatalf("unregister failed: %v", err)
	}

	server.mutex.RLock()
	_, exists := server.clients["peer1"]
	server.mutex.RUnlock()
	if exists {
		t.Fatal("client should be removed after unregister")
	}
}
