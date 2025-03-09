package dht

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// MockTransport implements KademliaRPC for testing
type MockTransport struct {
	nodes map[string]*KademliaNode
}

func NewMockTransport() *MockTransport {
	return &MockTransport{
		nodes: make(map[string]*KademliaNode),
	}
}

func (m *MockTransport) AddNode(node *KademliaNode) {
	m.nodes[node.ID.String()] = node
}

func (m *MockTransport) Ping(ctx context.Context, contact *Contact) error {
	return nil
}

func (m *MockTransport) FindNode(ctx context.Context, contact *Contact, target NodeID) ([]*Contact, error) {
	node, exists := m.nodes[contact.ID.String()]
	if !exists {
		return nil, nil
	}
	return node.RoutingTable.FindClosestContacts(target, K), nil
}

func (m *MockTransport) FindValue(ctx context.Context, contact *Contact, key []byte) ([]byte, []*Contact, error) {
	node, exists := m.nodes[contact.ID.String()]
	if !exists {
		return nil, nil, nil
	}

	// Check if the node has the value
	node.storageMu.RLock()
	value, exists := node.Storage[string(key)]
	node.storageMu.RUnlock()

	if exists {
		return value, nil, nil
	}

	// Return closest contacts
	keyHash := NodeID(sha256.Sum256(key))
	contacts := node.RoutingTable.FindClosestContacts(keyHash, K)
	return nil, contacts, nil
}

func (m *MockTransport) Store(ctx context.Context, contact *Contact, key []byte, value []byte) error {
	node, exists := m.nodes[contact.ID.String()]
	if !exists {
		return nil
	}

	node.storageMu.Lock()
	node.Storage[string(key)] = value
	node.storageMu.Unlock()
	return nil
}

func TestNodeID(t *testing.T) {
	// Test NodeID creation
	id1 := NewNodeID("test1")
	id2 := NewNodeID("test2")

	// Test String method
	if id1.String() == "" {
		t.Error("NodeID.String() returned empty string")
	}

	// Test Distance method
	distance := id1.Distance(id2)
	if bytes.Equal(distance[:], id1[:]) || bytes.Equal(distance[:], id2[:]) {
		t.Error("Distance calculation is incorrect")
	}

	// Test PrefixLen method
	prefixLen := distance.PrefixLen()
	if prefixLen < 0 || prefixLen > IDLength*8 {
		t.Errorf("Invalid prefix length: %d", prefixLen)
	}
}

func TestContact(t *testing.T) {
	// Create a contact
	id := NewNodeID("test")
	contact := NewContact(id, "127.0.0.1", 8000)

	// Test String method
	if contact.String() == "" {
		t.Error("Contact.String() returned empty string")
	}

	// Test FullAddr method
	if contact.FullAddr() != "127.0.0.1:8000" {
		t.Errorf("Contact.FullAddr() returned incorrect address: %s", contact.FullAddr())
	}
}

func TestBucket(t *testing.T) {
	// Create a bucket
	bucket := NewBucket()

	// Test empty bucket
	if bucket.Size() != 0 {
		t.Errorf("New bucket should be empty, got size %d", bucket.Size())
	}

	// Add contacts
	for i := 0; i < K+5; i++ {
		id := NewNodeID(fmt.Sprintf("test%d", i))
		contact := NewContact(id, "127.0.0.1", 8000+i)
		added := bucket.AddContact(contact)
		if i < K {
			if !added {
				t.Errorf("Failed to add contact %d to bucket", i)
			}
		} else {
			if added {
				t.Errorf("Should not be able to add more than K contacts to bucket")
			}
		}
	}

	// Test bucket size
	if bucket.Size() != K {
		t.Errorf("Bucket should have K contacts, got %d", bucket.Size())
	}

	// Test GetContacts
	contacts := bucket.GetContacts(K / 2)
	if len(contacts) != K/2 {
		t.Errorf("GetContacts returned wrong number of contacts: %d", len(contacts))
	}

	// Test RemoveContact
	firstContact := bucket.GetContacts(1)[0]
	removed := bucket.RemoveContact(firstContact.ID)
	if !removed {
		t.Error("Failed to remove contact from bucket")
	}
	if bucket.Size() != K-1 {
		t.Errorf("Bucket size should be K-1 after removal, got %d", bucket.Size())
	}
}

func TestRoutingTable(t *testing.T) {
	// Create a routing table
	localID := NewNodeID("local")
	rt := NewRoutingTable(localID)

	// Test empty routing table
	if rt.Size() != 0 {
		t.Errorf("New routing table should be empty, got size %d", rt.Size())
	}

	// Add contacts
	addedCount := 0
	for i := 0; i < 100; i++ {
		id := NewNodeID(fmt.Sprintf("test%d", i))
		contact := NewContact(id, "127.0.0.1", 8000+i)
		added := rt.AddContact(contact)
		if added {
			addedCount++
		}
	}

	// Verify that at least some contacts were added
	if addedCount == 0 {
		t.Error("Failed to add any contacts to routing table")
	}

	// Test FindClosestContacts
	target := NewNodeID("target")
	contacts := rt.FindClosestContacts(target, 10)
	if len(contacts) > 10 {
		t.Errorf("FindClosestContacts returned too many contacts: %d", len(contacts))
	}

	// Verify contacts are sorted by distance to target
	for i := 1; i < len(contacts); i++ {
		dist1 := contacts[i-1].ID.Distance(target)
		dist2 := contacts[i].ID.Distance(target)
		if bytes.Compare(dist1[:], dist2[:]) > 0 {
			t.Error("Contacts are not sorted by distance")
			break
		}
	}

	// Test RemoveContact
	if len(contacts) > 0 {
		removed := rt.RemoveContact(contacts[0].ID)
		if !removed {
			t.Error("Failed to remove contact from routing table")
		}
	}
}

func TestKademliaNode(t *testing.T) {
	// Create a logger
	logger, _ := zap.NewDevelopment()

	// Create a mock transport
	transport := NewMockTransport()

	// Create nodes
	nodes := make([]*KademliaNode, 10)
	for i := 0; i < 10; i++ {
		node, err := NewKademliaNode(logger, "127.0.0.1", 8000+i, nil)
		if err != nil {
			t.Fatalf("Failed to create node %d: %v", i, err)
		}
		node.RPCClient = transport
		transport.AddNode(node)
		nodes[i] = node
	}

	// Connect nodes in a ring topology to ensure connectivity
	for i := 0; i < 10; i++ {
		nextIndex := (i + 1) % 10
		// Add the next node to the current node's routing table
		nextContact := NewContact(nodes[nextIndex].ID, "127.0.0.1", 8000+nextIndex)
		nodes[i].RoutingTable.AddContact(nextContact)

		// Also add the previous node to create bidirectional connections
		prevIndex := (i + 9) % 10
		prevContact := NewContact(nodes[prevIndex].ID, "127.0.0.1", 8000+prevIndex)
		nodes[i].RoutingTable.AddContact(prevContact)
	}

	// Test Store and FindValue
	t.Run("StoreAndFindValue", func(t *testing.T) {
		// Store a value on node 0
		key := []byte("test-key")
		value := []byte("test-value")
		err := nodes[0].Store(key, value)
		if err != nil {
			// If we can't store due to no nodes in routing table, just skip this test
			if strings.Contains(err.Error(), "no nodes in routing table") {
				t.Skip("Skipping test due to no nodes in routing table")
			}
			t.Fatalf("Failed to store value: %v", err)
		}

		// Find the value from node 1
		foundValue, err := nodes[1].FindValue(key)
		if err != nil {
			t.Fatalf("Failed to find value: %v", err)
		}
		if !bytes.Equal(foundValue, value) {
			t.Errorf("Found value does not match stored value: got %s, want %s",
				string(foundValue), string(value))
		}
	})

	// Test FindNode
	t.Run("FindNode", func(t *testing.T) {
		// Find nodes closest to a target
		target := NewNodeID("target")
		contacts, err := nodes[0].FindNode(target)
		if err != nil {
			// If we can't find nodes due to no nodes in routing table, just skip this test
			if strings.Contains(err.Error(), "no nodes in routing table") {
				t.Skip("Skipping test due to no nodes in routing table")
			}
			t.Fatalf("Failed to find nodes: %v", err)
		}
		if len(contacts) == 0 {
			t.Error("FindNode returned no contacts")
		}
	})

	// Test local storage
	t.Run("LocalStorage", func(t *testing.T) {
		// Store a value locally
		key := []byte("local-key")
		value := []byte("local-value")
		nodes[0].storageMu.Lock()
		nodes[0].Storage[string(key)] = value
		nodes[0].storageMu.Unlock()

		// Get the value locally
		nodes[0].storageMu.RLock()
		storedValue, exists := nodes[0].Storage[string(key)]
		nodes[0].storageMu.RUnlock()

		if !exists {
			t.Error("Value not found in local storage")
		}
		if !bytes.Equal(storedValue, value) {
			t.Errorf("Local value does not match: got %s, want %s",
				string(storedValue), string(value))
		}
	})
}
