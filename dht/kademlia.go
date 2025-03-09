package dht

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"net"
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// IDLength is the length of node IDs in bytes
	IDLength = 32 // SHA-256 hash length
	// K is the size of k-buckets
	K = 20
	// Alpha is the concurrency parameter for node lookups
	Alpha = 3
	// RefreshInterval is the interval for refreshing buckets
	RefreshInterval = 1 * time.Hour
	// ReplicationInterval is the interval for replicating keys
	ReplicationInterval = 1 * time.Hour
	// RepublishInterval is the interval for republishing keys
	RepublishInterval = 24 * time.Hour
	// ExpireTime is the time after which a key-value pair expires
	ExpireTime = 24 * time.Hour
	// MaxPingRetries is the maximum number of ping retries
	MaxPingRetries = 3
)

// NodeID represents a node ID in the DHT
type NodeID [IDLength]byte

// String returns a hex string representation of the node ID
func (id NodeID) String() string {
	return hex.EncodeToString(id[:])
}

// Distance calculates the XOR distance between two node IDs
func (id NodeID) Distance(other NodeID) NodeID {
	var distance NodeID
	for i := 0; i < IDLength; i++ {
		distance[i] = id[i] ^ other[i]
	}
	return distance
}

// PrefixLen returns the number of leading zeros in the node ID
func (id NodeID) PrefixLen() int {
	for i, b := range id {
		if b != 0 {
			return i*8 + bits.LeadingZeros8(b)
		}
	}
	return IDLength * 8
}

// NewNodeID creates a new node ID from a string
func NewNodeID(s string) NodeID {
	hash := sha256.Sum256([]byte(s))
	return hash
}

// Contact represents a node in the DHT
type Contact struct {
	ID       NodeID
	Address  string
	Port     int
	LastSeen time.Time
}

// NewContact creates a new contact
func NewContact(id NodeID, address string, port int) *Contact {
	return &Contact{
		ID:       id,
		Address:  address,
		Port:     port,
		LastSeen: time.Now(),
	}
}

// String returns a string representation of the contact
func (c *Contact) String() string {
	return fmt.Sprintf("%s@%s:%d", c.ID.String()[:8], c.Address, c.Port)
}

// FullAddr returns the full address of the contact
func (c *Contact) FullAddr() string {
	return fmt.Sprintf("%s:%d", c.Address, c.Port)
}

// Bucket represents a k-bucket in the routing table
type Bucket struct {
	contacts    []*Contact
	mu          sync.RWMutex
	lastUpdated time.Time
}

// NewBucket creates a new bucket
func NewBucket() *Bucket {
	return &Bucket{
		contacts:    make([]*Contact, 0, K),
		lastUpdated: time.Now(),
	}
}

// AddContact adds a contact to the bucket
func (b *Bucket) AddContact(contact *Contact) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if the contact already exists
	for i, c := range b.contacts {
		if bytes.Equal(c.ID[:], contact.ID[:]) {
			// Move the contact to the end (most recently seen)
			b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
			b.contacts = append(b.contacts, contact)
			b.lastUpdated = time.Now()
			return true
		}
	}

	// If the bucket is not full, add the contact
	if len(b.contacts) < K {
		b.contacts = append(b.contacts, contact)
		b.lastUpdated = time.Now()
		return true
	}

	// Bucket is full, return false
	return false
}

// RemoveContact removes a contact from the bucket
func (b *Bucket) RemoveContact(id NodeID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	for i, c := range b.contacts {
		if bytes.Equal(c.ID[:], id[:]) {
			b.contacts = append(b.contacts[:i], b.contacts[i+1:]...)
			b.lastUpdated = time.Now()
			return true
		}
	}

	return false
}

// GetContacts returns up to count contacts from the bucket
func (b *Bucket) GetContacts(count int) []*Contact {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if count >= len(b.contacts) {
		result := make([]*Contact, len(b.contacts))
		copy(result, b.contacts)
		return result
	}

	result := make([]*Contact, count)
	copy(result, b.contacts[len(b.contacts)-count:])
	return result
}

// Size returns the number of contacts in the bucket
func (b *Bucket) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.contacts)
}

// RoutingTable represents the routing table of a node
type RoutingTable struct {
	buckets []*Bucket
	localID NodeID
	mu      sync.RWMutex
}

// NewRoutingTable creates a new routing table
func NewRoutingTable(localID NodeID) *RoutingTable {
	rt := &RoutingTable{
		buckets: make([]*Bucket, IDLength*8),
		localID: localID,
	}

	for i := 0; i < IDLength*8; i++ {
		rt.buckets[i] = NewBucket()
	}

	return rt
}

// AddContact adds a contact to the routing table
func (rt *RoutingTable) AddContact(contact *Contact) bool {
	if bytes.Equal(contact.ID[:], rt.localID[:]) {
		return false // Don't add ourselves
	}

	bucketIndex := rt.getBucketIndex(contact.ID)

	rt.mu.RLock()
	bucket := rt.buckets[bucketIndex]
	rt.mu.RUnlock()

	return bucket.AddContact(contact)
}

// RemoveContact removes a contact from the routing table
func (rt *RoutingTable) RemoveContact(id NodeID) bool {
	bucketIndex := rt.getBucketIndex(id)

	rt.mu.RLock()
	bucket := rt.buckets[bucketIndex]
	rt.mu.RUnlock()

	return bucket.RemoveContact(id)
}

// FindClosestContacts finds the count closest contacts to the target ID
func (rt *RoutingTable) FindClosestContacts(target NodeID, count int) []*Contact {
	bucketIndex := rt.getBucketIndex(target)

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var contacts []*Contact

	// Add contacts from the target bucket
	contacts = append(contacts, rt.buckets[bucketIndex].GetContacts(K)...)

	// If we need more contacts, look in other buckets
	for i := 1; len(contacts) < count && (bucketIndex-i >= 0 || bucketIndex+i < len(rt.buckets)); i++ {
		if bucketIndex-i >= 0 {
			contacts = append(contacts, rt.buckets[bucketIndex-i].GetContacts(K)...)
		}
		if bucketIndex+i < len(rt.buckets) {
			contacts = append(contacts, rt.buckets[bucketIndex+i].GetContacts(K)...)
		}
	}

	// Sort contacts by distance to target
	sort.Slice(contacts, func(i, j int) bool {
		distI := contacts[i].ID.Distance(target)
		distJ := contacts[j].ID.Distance(target)
		return bytes.Compare(distI[:], distJ[:]) < 0
	})

	// Return up to count contacts
	if len(contacts) > count {
		return contacts[:count]
	}
	return contacts
}

// getBucketIndex returns the index of the bucket for the given ID
func (rt *RoutingTable) getBucketIndex(id NodeID) int {
	distance := rt.localID.Distance(id)
	prefixLen := distance.PrefixLen()

	// Ensure the bucket index is within bounds
	if prefixLen >= IDLength*8 {
		return IDLength*8 - 1
	}
	return prefixLen
}

// Size returns the total number of contacts in the routing table
func (rt *RoutingTable) Size() int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()

	size := 0
	for _, bucket := range rt.buckets {
		size += bucket.Size()
	}
	return size
}

// KademliaRPC defines the RPC methods for Kademlia
type KademliaRPC interface {
	Ping(ctx context.Context, contact *Contact) error
	FindNode(ctx context.Context, contact *Contact, target NodeID) ([]*Contact, error)
	FindValue(ctx context.Context, contact *Contact, key []byte) ([]byte, []*Contact, error)
	Store(ctx context.Context, contact *Contact, key []byte, value []byte) error
}

// KademliaNode represents a node in the Kademlia DHT
type KademliaNode struct {
	ID           NodeID
	RoutingTable *RoutingTable
	Storage      map[string][]byte
	RPCClient    KademliaRPC
	Address      string
	Port         int
	logger       *zap.Logger
	storageMu    sync.RWMutex
	refreshTimer *time.Timer
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewKademliaNode creates a new Kademlia node
func NewKademliaNode(logger *zap.Logger, address string, port int, bootstrapNodes []*Contact) (*KademliaNode, error) {
	// Generate a node ID based on the address and port
	idStr := fmt.Sprintf("%s:%d", address, port)
	id := NewNodeID(idStr)

	ctx, cancel := context.WithCancel(context.Background())

	node := &KademliaNode{
		ID:           id,
		RoutingTable: NewRoutingTable(id),
		Storage:      make(map[string][]byte),
		Address:      address,
		Port:         port,
		logger:       logger,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Bootstrap the node
	if len(bootstrapNodes) > 0 {
		for _, contact := range bootstrapNodes {
			node.RoutingTable.AddContact(contact)
		}

		// Perform a node lookup for our own ID to populate the routing table
		go func() {
			if err := node.Bootstrap(); err != nil {
				logger.Error("Failed to bootstrap node", zap.Error(err))
			}
		}()
	}

	// Start the refresh timer
	node.refreshTimer = time.NewTimer(RefreshInterval)
	go node.refreshLoop()

	return node, nil
}

// Bootstrap bootstraps the node by performing a node lookup for its own ID
func (node *KademliaNode) Bootstrap() error {
	_, err := node.FindNode(node.ID)
	return err
}

// refreshLoop periodically refreshes the routing table
func (node *KademliaNode) refreshLoop() {
	for {
		select {
		case <-node.ctx.Done():
			return
		case <-node.refreshTimer.C:
			// Refresh each bucket
			for i := 0; i < IDLength*8; i++ {
				// Generate a random ID in the bucket
				var randomID NodeID
				copy(randomID[:], node.ID[:])

				// Flip the bit at position i
				byteIndex := i / 8
				bitIndex := i % 8
				randomID[byteIndex] ^= 1 << uint(7-bitIndex)

				// Perform a node lookup
				go func(id NodeID) {
					if _, err := node.FindNode(id); err != nil {
						node.logger.Error("Failed to refresh bucket", zap.Error(err))
					}
				}(randomID)
			}

			// Reset the timer
			node.refreshTimer.Reset(RefreshInterval)
		}
	}
}

// FindNode finds the k closest nodes to the target ID
func (node *KademliaNode) FindNode(target NodeID) ([]*Contact, error) {
	if node.RPCClient == nil {
		return nil, errors.New("RPC client not initialized")
	}

	// Get the alpha closest nodes from our routing table
	closestNodes := node.RoutingTable.FindClosestContacts(target, Alpha)
	if len(closestNodes) == 0 {
		return nil, errors.New("no nodes in routing table")
	}

	// Create a shortlist of nodes to query
	shortlist := make(map[string]*Contact)
	for _, contact := range closestNodes {
		shortlist[contact.ID.String()] = contact
	}

	// Create a set of queried nodes
	queried := make(map[string]bool)

	// Create a channel for results
	resultChan := make(chan []*Contact, Alpha)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(node.ctx, 10*time.Second)
	defer cancel()

	// Start the node lookup
	for i := 0; i < Alpha && i < len(closestNodes); i++ {
		contact := closestNodes[i]
		queried[contact.ID.String()] = true

		go func(c *Contact) {
			contacts, err := node.RPCClient.FindNode(ctx, c, target)
			if err != nil {
				node.logger.Error("Failed to query node",
					zap.String("node", c.String()),
					zap.Error(err))
				resultChan <- nil
				return
			}

			resultChan <- contacts
		}(contact)
	}

	// Process results
	for i := 0; i < len(closestNodes); i++ {
		select {
		case contacts := <-resultChan:
			if contacts == nil {
				continue
			}

			// Add new contacts to the shortlist
			for _, contact := range contacts {
				if _, exists := shortlist[contact.ID.String()]; !exists {
					shortlist[contact.ID.String()] = contact
					node.RoutingTable.AddContact(contact)
				}
			}

			// Find the closest nodes in the shortlist that haven't been queried
			var closest []*Contact
			for _, contact := range shortlist {
				if !queried[contact.ID.String()] {
					closest = append(closest, contact)
				}
			}

			// Sort by distance to target
			sort.Slice(closest, func(i, j int) bool {
				distI := closest[i].ID.Distance(target)
				distJ := closest[j].ID.Distance(target)
				return bytes.Compare(distI[:], distJ[:]) < 0
			})

			// Query the next alpha closest nodes
			for j := 0; j < Alpha && j < len(closest); j++ {
				contact := closest[j]
				queried[contact.ID.String()] = true

				go func(c *Contact) {
					contacts, err := node.RPCClient.FindNode(ctx, c, target)
					if err != nil {
						node.logger.Error("Failed to query node",
							zap.String("node", c.String()),
							zap.Error(err))
						resultChan <- nil
						return
					}

					resultChan <- contacts
				}(contact)

				// Increment the counter to wait for more results
				i++
			}
		case <-ctx.Done():
			node.logger.Warn("Node lookup timed out")
			break
		}
	}

	// Convert the shortlist to a slice and sort by distance
	var result []*Contact
	for _, contact := range shortlist {
		result = append(result, contact)
	}

	sort.Slice(result, func(i, j int) bool {
		distI := result[i].ID.Distance(target)
		distJ := result[j].ID.Distance(target)
		return bytes.Compare(distI[:], distJ[:]) < 0
	})

	// Return up to K contacts
	if len(result) > K {
		return result[:K], nil
	}
	return result, nil
}

// FindValue finds a value in the DHT
func (node *KademliaNode) FindValue(key []byte) ([]byte, error) {
	if node.RPCClient == nil {
		return nil, errors.New("RPC client not initialized")
	}

	// Check if we have the value locally
	node.storageMu.RLock()
	value, exists := node.Storage[string(key)]
	node.storageMu.RUnlock()

	if exists {
		return value, nil
	}

	// Hash the key to get the target ID
	keyHash := sha256.Sum256(key)
	target := keyHash

	// Get the alpha closest nodes from our routing table
	closestNodes := node.RoutingTable.FindClosestContacts(target, Alpha)
	if len(closestNodes) == 0 {
		return nil, errors.New("no nodes in routing table")
	}

	// Create a shortlist of nodes to query
	shortlist := make(map[string]*Contact)
	for _, contact := range closestNodes {
		shortlist[contact.ID.String()] = contact
	}

	// Create a set of queried nodes
	queried := make(map[string]bool)

	// Create a channel for results
	type findValueResult struct {
		value    []byte
		contacts []*Contact
	}
	resultChan := make(chan findValueResult, Alpha)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(node.ctx, 10*time.Second)
	defer cancel()

	// Start the value lookup
	for i := 0; i < Alpha && i < len(closestNodes); i++ {
		contact := closestNodes[i]
		queried[contact.ID.String()] = true

		go func(c *Contact) {
			value, contacts, err := node.RPCClient.FindValue(ctx, c, key)
			if err != nil {
				node.logger.Error("Failed to query node",
					zap.String("node", c.String()),
					zap.Error(err))
				resultChan <- findValueResult{nil, nil}
				return
			}

			resultChan <- findValueResult{value, contacts}
		}(contact)
	}

	// Process results
	for i := 0; i < len(closestNodes); i++ {
		select {
		case result := <-resultChan:
			// If we found the value, return it
			if result.value != nil {
				// Store the value locally
				node.storageMu.Lock()
				node.Storage[string(key)] = result.value
				node.storageMu.Unlock()

				return result.value, nil
			}

			if result.contacts == nil {
				continue
			}

			// Add new contacts to the shortlist
			for _, contact := range result.contacts {
				if _, exists := shortlist[contact.ID.String()]; !exists {
					shortlist[contact.ID.String()] = contact
					node.RoutingTable.AddContact(contact)
				}
			}

			// Find the closest nodes in the shortlist that haven't been queried
			var closest []*Contact
			for _, contact := range shortlist {
				if !queried[contact.ID.String()] {
					closest = append(closest, contact)
				}
			}

			// Sort by distance to target
			sort.Slice(closest, func(i, j int) bool {
				distI := closest[i].ID.Distance(target)
				distJ := closest[j].ID.Distance(target)
				return bytes.Compare(distI[:], distJ[:]) < 0
			})

			// Query the next alpha closest nodes
			for j := 0; j < Alpha && j < len(closest); j++ {
				contact := closest[j]
				queried[contact.ID.String()] = true

				go func(c *Contact) {
					value, contacts, err := node.RPCClient.FindValue(ctx, c, key)
					if err != nil {
						node.logger.Error("Failed to query node",
							zap.String("node", c.String()),
							zap.Error(err))
						resultChan <- findValueResult{nil, nil}
						return
					}

					resultChan <- findValueResult{value, contacts}
				}(contact)

				// Increment the counter to wait for more results
				i++
			}
		case <-ctx.Done():
			node.logger.Warn("Value lookup timed out")
			return nil, errors.New("value lookup timed out")
		}
	}

	return nil, errors.New("value not found")
}

// Store stores a value in the DHT
func (node *KademliaNode) Store(key, value []byte) error {
	if node.RPCClient == nil {
		return errors.New("RPC client not initialized")
	}

	// Hash the key to get the target ID
	keyHash := sha256.Sum256(key)
	target := keyHash

	// Find the K closest nodes to the key
	closestNodes, err := node.FindNode(target)
	if err != nil {
		return fmt.Errorf("failed to find nodes: %w", err)
	}

	// Store the value on the closest nodes
	var wg sync.WaitGroup
	var storeErr error
	var storeErrMu sync.Mutex

	for _, contact := range closestNodes {
		wg.Add(1)
		go func(c *Contact) {
			defer wg.Done()

			ctx, cancel := context.WithTimeout(node.ctx, 5*time.Second)
			defer cancel()

			if err := node.RPCClient.Store(ctx, c, key, value); err != nil {
				node.logger.Error("Failed to store value on node",
					zap.String("node", c.String()),
					zap.Error(err))

				storeErrMu.Lock()
				if storeErr == nil {
					storeErr = err
				}
				storeErrMu.Unlock()
			}
		}(contact)
	}

	wg.Wait()

	// Store the value locally as well
	node.storageMu.Lock()
	node.Storage[string(key)] = value
	node.storageMu.Unlock()

	return storeErr
}

// Get retrieves a value from the local storage
func (node *KademliaNode) Get(key []byte) ([]byte, bool) {
	node.storageMu.RLock()
	defer node.storageMu.RUnlock()

	value, exists := node.Storage[string(key)]
	return value, exists
}

// Delete removes a value from the local storage
func (node *KademliaNode) Delete(key []byte) {
	node.storageMu.Lock()
	defer node.storageMu.Unlock()

	delete(node.Storage, string(key))
}

// Stop stops the node
func (node *KademliaNode) Stop() {
	node.cancel()
	if node.refreshTimer != nil {
		node.refreshTimer.Stop()
	}
}

// TCPTransport implements KademliaRPC using TCP
type TCPTransport struct {
	node   *KademliaNode
	logger *zap.Logger
}

// NewTCPTransport creates a new TCP transport
func NewTCPTransport(node *KademliaNode, logger *zap.Logger) *TCPTransport {
	return &TCPTransport{
		node:   node,
		logger: logger,
	}
}

// Ping pings a contact
func (t *TCPTransport) Ping(ctx context.Context, contact *Contact) error {
	// Connect to the contact
	conn, err := net.DialTimeout("tcp", contact.FullAddr(), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", contact.FullAddr(), err)
	}
	defer conn.Close()

	// TODO: Implement the ping protocol
	return nil
}

// FindNode finds the k closest nodes to the target ID
func (t *TCPTransport) FindNode(ctx context.Context, contact *Contact, target NodeID) ([]*Contact, error) {
	// Connect to the contact
	conn, err := net.DialTimeout("tcp", contact.FullAddr(), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", contact.FullAddr(), err)
	}
	defer conn.Close()

	// TODO: Implement the find_node protocol
	return nil, nil
}

// FindValue finds a value in the DHT
func (t *TCPTransport) FindValue(ctx context.Context, contact *Contact, key []byte) ([]byte, []*Contact, error) {
	// Connect to the contact
	conn, err := net.DialTimeout("tcp", contact.FullAddr(), 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to connect to %s: %w", contact.FullAddr(), err)
	}
	defer conn.Close()

	// TODO: Implement the find_value protocol
	return nil, nil, nil
}

// Store stores a value in the DHT
func (t *TCPTransport) Store(ctx context.Context, contact *Contact, key []byte, value []byte) error {
	// Connect to the contact
	conn, err := net.DialTimeout("tcp", contact.FullAddr(), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", contact.FullAddr(), err)
	}
	defer conn.Close()

	// TODO: Implement the store protocol
	return nil
}
