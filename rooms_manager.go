package snapws

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
)

// User for the OnJoin/OnLeave room hooks.
//
// Receives a pointer to a room, a pointer to a connection, and an optional args.
type roomHook[keyType comparable] func(room *Room[keyType], conn *Conn, args ...any)

// A helper fucntion for room hooks.
//
// Receives a type as a generic value, an args slice, and an index.
// If the args slice has a value at args[index] of the given type.
// it returns the casted value and true.
// Otherwise it returns the zero value of the given generic type and false.
func GetArg[T any](args []any, index int) (T, bool) {
	if len(args) > index {
		v, ok := args[index].(T)
		return v, ok
	}

	var zero T
	return zero, false
}

// Room represents a group of WebSocket connections that can receive broadcast messages together.
// Rooms are identified by a comparable key type and provide thread-safe operations for
// managing connections and broadcasting messages.
//
// Typical usage:
//   - Chat rooms where users in the same room receive each other's messages
//   - Game lobbies where players receive game state updates
//   - Collaborative editing where document collaborators receive changes
type Room[keyType comparable] struct {
	rm  *RoomManager[keyType]
	Key keyType // key in the parent roomManager

	conns map[*Conn]bool
	mu    sync.RWMutex

	// Called when a connection is added to the room.
	//If not set it will default to "DefaultOnJoin" in the RoomManager.
	OnJoin roomHook[keyType]
	// Called when a connection is removed from the room.
	//If not set it will default to "DefaultOnLeave" in the RoomManager.
	OnLeave roomHook[keyType]
}

// RoomManager handles creation, lookup, and lifecycle of rooms.
// It provides thread-safe operations for managing multiple rooms and their connections.
// Each room is identified by a unique comparable key.
type RoomManager[keyType comparable] struct {
	rooms    map[keyType]*Room[keyType]
	mu       sync.RWMutex
	Upgrader *Upgrader

	// This is used when the "OnJoin" field of a room is unset.
	DefaultOnJoin roomHook[keyType]
	// This is used when the "OnLeave" field of a room is unset.
	DefaultOnLeave roomHook[keyType]
}

// NewRoomManager creates a new RoomManager with the given WebSocket upgrader.
// If upgrader is nil, a default upgrader with standard options will be created.
//
// The keyType must be a comparable type (string, int, custom types with comparable fields).
// This key will be used to uniquely identify and retrieve rooms.
func NewRoomManager[keyType comparable](upgrader *Upgrader) *RoomManager[keyType] {
	if upgrader == nil {
		upgrader = NewUpgrader(nil)
	}

	return &RoomManager[keyType]{
		rooms:    make(map[keyType]*Room[keyType]),
		Upgrader: upgrader,
	}
}

// newRoom creates a new Room instance with empty connection set.
// This is an internal method used by RoomManager to create rooms.
func (rm *RoomManager[keyType]) newRoom(key keyType) *Room[keyType] {
	return &Room[keyType]{
		conns:   make(map[*Conn]bool),
		rm:      rm,
		Key:     key,
		OnJoin:  rm.DefaultOnJoin,
		OnLeave: rm.DefaultOnLeave,
	}
}

// Connect upgrades an HTTP connection to WebSocket and adds it to the specified room.
// If the room doesn't exist, it will be created automatically.
//
// Receives args as an optinal value, these args will be passed to both the OnJoin AND OnLeave hooks.
//
// This is a convenience method that combines WebSocket upgrade, room creation/lookup,
// and connection addition in a single atomic operation.
//
// Returns the upgraded connection, the room it was added to, and any error from the upgrade process.
func (rm *RoomManager[keyType]) Connect(w http.ResponseWriter, r *http.Request, roomKey keyType, args ...any) (*Conn, *Room[keyType], error) {
	conn, err := rm.Upgrader.Upgrade(w, r)
	if err != nil {
		return nil, nil, err
	}

	room := rm.Add(roomKey)
	room.Add(conn, args...)

	return conn, room, nil
}

// Add creates a new room with the given key, or returns the existing room if it already exists.
// This operation is idempotent - calling it multiple times with the same key is safe
// and will always return the same room instance.
//
// Thread-safe: Multiple goroutines can call this concurrently.
func (rm *RoomManager[keyType]) Add(key keyType) *Room[keyType] {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if r, exists := rm.rooms[key]; exists {
		return r
	}

	r := rm.newRoom(key)
	rm.rooms[key] = r

	return r
}

// Get retrieves a room by its key.
// Returns nil if the room doesn't exist.
//
// The returned room pointer must be checked for nil before use:
//
//	if room := manager.Get("lobby"); room != nil {
//	    room.BroadcastString(ctx, message)
//	}
//
// Thread-safe: Multiple goroutines can call this concurrently.
func (rm *RoomManager[keyType]) Get(key keyType) *Room[keyType] {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	return rm.rooms[key]
}

// GetRoomKeys returns a slice of all rooms keys associated with the room manager.
func (rm *RoomManager[keyType]) GetRoomKeys() []keyType {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	keys := make([]keyType, 0, len(rm.rooms))
	for k := range rm.rooms {
		keys = append(keys, k)
	}

	return keys
}

// Shutdown closes all rooms and their connections.
// This should be called when shutting down the application to ensure
// all WebSocket connections are properly closed.
//
// After calling Shutdown, the RoomManager should not be used.
func (rm *RoomManager[keyType]) Shutdown() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	connsCount := 0
	for _, r := range rm.rooms {
		r.mu.Lock()
		connsCount += len(r.conns)
	}

	var wg sync.WaitGroup
	workers := (connsCount / 5) + 2
	ch := make(chan *Conn, connsCount)

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range ch {
				c.CloseWithCode(CloseGoingAway, "server is shutting down")
			}
		}()
	}

	for _, r := range rm.rooms {
		for c := range r.conns {
			ch <- c
		}
		r.mu.Unlock()
	}

	close(ch)
	rm.rooms = make(map[keyType]*Room[keyType])

	wg.Wait()
}

// Close removes all connections from the room and deletes the room from its manager.
// All connections in the room will be removed but not closed - they remain active
// and can be added to other rooms.
//
// After calling Close, this room instance should not be used.
func (r *Room[keyType]) Close() {
	r.RemoveAll()

	r.rm.mu.Lock()
	defer r.rm.mu.Unlock()
	delete(r.rm.rooms, r.Key)
}

// Add adds a connection to the room.
// The connection will be automatically removed from the room when it closes.
//
// Receives args as an optional value, these args will be passed to the OnJoin hook.
//
// Thread-safe: Multiple goroutines can call this concurrently.
// If the connection is already in the room, this is a no-op.
func (r *Room[keyType]) Add(conn *Conn, args ...any) {
	r.mu.Lock()
	r.conns[conn] = true
	r.mu.Unlock()

	// Set up automatic cleanup when connection closes
	conn.onCloseMu.Lock()
	conn.onClose = func() {
		r.Remove(conn, args...)
	}
	conn.onCloseMu.Unlock()

	conn.enableBroadcasting()
	// calling onJoin hook
	if r.OnJoin != nil {
		r.OnJoin(r, conn, args...)
	}
}

// Remove removes a connection from the room.
// If the connection is not in the room, this is a no-op.
//
// // Receives args as an optional value, these args will be passed to the OnLeave hook.
//
// Thread-safe: Multiple goroutines can call this concurrently.
func (r *Room[keyType]) Remove(conn *Conn, args ...any) {
	r.mu.Lock()
	delete(r.conns, conn)
	r.mu.Unlock()

	// calling onLeave hook
	if r.OnLeave != nil {
		r.OnLeave(r, conn, args...)
	}
}

// RemoveAll removes all connections from the room.
// The connections are not closed, just removed from the room.
//
// Thread-safe: Can be called concurrently with other room operations.
func (r *Room[keyType]) RemoveAll() {
	r.mu.Lock()
	conns := r.conns               // copy
	r.conns = make(map[*Conn]bool) // empty
	r.mu.Unlock()

	// calling hook
	for c := range conns {
		r.OnLeave(r, c)
	}
}

// Move removes a connection from this room and adds it to another room.
// If the target room doesn't exist, the connection is only removed from the current room.
//
// The target room must exist in the same RoomManager as the current room.
//
// Thread-safe: The move operation is atomic from the perspective of the connection.
func (r *Room[keyType]) Move(conn *Conn, newRoom keyType) {
	r.Remove(conn)
	if nr := r.rm.Get(newRoom); nr != nil {
		nr.Add(conn)
	}
}

// Receives optional values "exclude".
// Returns a slice of pointers to connections excluding the "excludes".
func (r *Room[keyType]) GetAllConns(exclude ...*Conn) []*Conn {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.conns) == 0 {
		return nil
	}

	conns := make([]*Conn, 0, len(r.conns))
	for c := range r.conns {
		if !slices.Contains(exclude, c) {
			conns = append(conns, c)
		}
	}

	return conns
}

// broadcast sends a message to all connections in the room using worker goroutines.
// This is an internal method used by BroadcastString and BroadcastBytes.
//
// Returns the number of successful sends and any error encountered.
// Uses the BroadcastWorkers configuration from the upgrader to determine concurrency.
func (r *Room[keyType]) broadcast(ctx context.Context, opcode uint8, data []byte, exclude ...*Conn) (int, error) {
	if !isData(opcode) {
		return 0, fmt.Errorf("%w: must be text or binary", ErrInvalidOPCODE)
	}

	conns := r.GetAllConns(exclude...)

	return r.rm.Upgrader.broadcast(ctx, conns, opcode, data)
}

// BroadcastString sends a text message to all connections in the room.
//
// Returns the number of connections that successfully received the message
// and any error encountered during broadcasting.
//
// Thread-safe: Can be called concurrently from multiple goroutines.
func (r *Room[keyType]) BroadcastString(ctx context.Context, data []byte, exclude ...*Conn) (int, error) {
	return r.broadcast(ctx, OpcodeText, data, exclude...)
}

// BroadcastBytes sends a binary message to all connections in the room.
//
// Returns the number of connections that successfully received the message
// and any error encountered during broadcasting.
//
// Thread-safe: Can be called concurrently from multiple goroutines.
func (r *Room[keyType]) BroadcastBytes(ctx context.Context, data []byte, exclude ...*Conn) (int, error) {
	return r.broadcast(ctx, OpcodeBinary, data, exclude...)
}

// This is a convenience method that marshals the given value into json and calls BroadcastString.
func (r *Room[keyType]) BroadcastJSON(ctx context.Context, v interface{}, exclude ...*Conn) (int, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}

	return r.BroadcastString(ctx, data, exclude...)
}

// BatchBroadcast loops over the room's connections and adds the given data into their batch.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent/batched it successfully)
//   - error: context cancellation error, flusher errors, or nil if completed normally
func (r *Room[keyType]) BatchBroadcast(ctx context.Context, data []byte, exclude ...*Conn) (int, error) {
	conns := r.GetAllConns(exclude...)

	return r.rm.Upgrader.batchBroadcast(ctx, conns, data)
}

// BatchBroadcastJSON is just a helper method that marshals the given v into json and calls BatchBraodcast.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent/batched it successfully)
//   - error: context cancellation error, mrashal error, flusher errors, or nil if completed normally
func (r *Room[keyType]) BatchBroadcastJSON(ctx context.Context, v interface{}, exclude ...*Conn) (int, error) {
	jData, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}

	return r.BatchBroadcast(ctx, jData, exclude...)
}
