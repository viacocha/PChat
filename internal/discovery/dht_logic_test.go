package discovery

import (
	"testing"
	"time"
)

func newTestDiscovery() *DHTDiscovery {
	return &DHTDiscovery{
		localUsers:   make(map[string]*UserInfo),
		peerIDToUser: make(map[string]*UserInfo),
	}
}

func TestRecordUserFromConnection(t *testing.T) {
	d := newTestDiscovery()
	d.RecordUserFromConnection("Alice", "peerAlice", []string{"/ip4/127.0.0.1/tcp/9001"})

	d.mutex.RLock()
	defer d.mutex.RUnlock()

	if len(d.localUsers) != 1 {
		t.Fatalf("expected 1 user, got %d", len(d.localUsers))
	}
	if d.localUsers["Alice"].PeerID != "peerAlice" {
		t.Errorf("expected peerAlice, got %s", d.localUsers["Alice"].PeerID)
	}
	if d.peerIDToUser["peerAlice"].Username != "Alice" {
		t.Errorf("expected Alice, got %s", d.peerIDToUser["peerAlice"].Username)
	}
}

func TestListUsersFiltersExpiredEntries(t *testing.T) {
	d := newTestDiscovery()
	now := time.Now().Unix()
	d.localUsers["fresh"] = &UserInfo{Username: "fresh", PeerID: "peerFresh", Timestamp: now}
	d.localUsers["old"] = &UserInfo{Username: "old", PeerID: "peerOld", Timestamp: now - int64(userInfoTTL.Seconds()) - 1}

	users := d.ListUsers()
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}
	if users[0].Username != "fresh" {
		t.Fatalf("expected fresh user, got %s", users[0].Username)
	}
}

func TestGetUserByPeerIDRespectsTTL(t *testing.T) {
	d := newTestDiscovery()
	d.peerIDToUser["peerFresh"] = &UserInfo{Username: "fresh", PeerID: "peerFresh", Timestamp: time.Now().Unix()}
	d.peerIDToUser["peerOld"] = &UserInfo{Username: "old", PeerID: "peerOld", Timestamp: time.Now().Unix() - int64(userInfoTTL.Seconds()) - 1}

	if user := d.GetUserByPeerID("peerFresh"); user == nil {
		t.Fatal("expected fresh user")
	}
	if user := d.GetUserByPeerID("peerOld"); user != nil {
		t.Fatal("expected expired user to be nil")
	}
}

func TestCleanupExpiredUsersRemovesEntries(t *testing.T) {
	d := newTestDiscovery()
	d.localUsers["old"] = &UserInfo{Username: "old", PeerID: "peerOld", Timestamp: time.Now().Unix() - int64(userInfoTTL.Seconds()) - 1}
	d.peerIDToUser["peerOld"] = d.localUsers["old"]

	d.cleanupExpiredUsers()

	d.mutex.RLock()
	defer d.mutex.RUnlock()
	if len(d.localUsers) != 0 {
		t.Fatalf("expected localUsers empty, got %d", len(d.localUsers))
	}
	if len(d.peerIDToUser) != 0 {
		t.Fatalf("expected peerIDToUser empty, got %d", len(d.peerIDToUser))
	}
}

func TestRecordUserFromConnection_EdgeCases(t *testing.T) {
	d := newTestDiscovery()

	// 测试空用户名
	d.RecordUserFromConnection("", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})
	if len(d.localUsers) != 0 {
		t.Errorf("expected no users with empty username, got %d", len(d.localUsers))
	}

	// 测试空peerID
	d.RecordUserFromConnection("Alice", "", []string{"/ip4/127.0.0.1/tcp/9001"})
	if len(d.localUsers) != 0 {
		t.Errorf("expected no users with empty peerID, got %d", len(d.localUsers))
	}

	// 测试nil discovery
	var nilD *DHTDiscovery
	nilD.RecordUserFromConnection("Alice", "peer1", []string{"/ip4/127.0.0.1/tcp/9001"})
	// 应该不会panic
}

func TestRecordUserFromConnection_UpdatesExisting(t *testing.T) {
	d := newTestDiscovery()
	d.RecordUserFromConnection("Alice", "peerAlice", []string{"/ip4/127.0.0.1/tcp/9001"})
	time.Sleep(10 * time.Millisecond) // 确保时间戳不同
	d.RecordUserFromConnection("Alice", "peerAlice", []string{"/ip4/127.0.0.1/tcp/9002"})

	d.mutex.RLock()
	defer d.mutex.RUnlock()
	if len(d.localUsers) != 1 {
		t.Fatalf("expected 1 user, got %d", len(d.localUsers))
	}
	if len(d.localUsers["Alice"].Addresses) != 1 {
		t.Errorf("expected 1 address, got %d", len(d.localUsers["Alice"].Addresses))
	}
}

func TestListUsers_Empty(t *testing.T) {
	d := newTestDiscovery()
	users := d.ListUsers()
	if len(users) != 0 {
		t.Errorf("expected empty list, got %d users", len(users))
	}
}

func TestGetUserByPeerID_NotFound(t *testing.T) {
	d := newTestDiscovery()
	user := d.GetUserByPeerID("nonexistent")
	if user != nil {
		t.Errorf("expected nil for nonexistent peer, got %v", user)
	}
}
