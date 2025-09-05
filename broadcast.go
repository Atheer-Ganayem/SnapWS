package snapws

import (
	"context"
	"fmt"
)

type message interface {
	writeToConn(conn *Conn) error
}

// normal message (not a batch)
type singleMessage struct {
	ctx    context.Context
	opcode uint8
	data   []byte
}

func (m *singleMessage) writeToConn(conn *Conn) error {
	return conn.SendMessage(m.ctx, m.opcode, m.data)
}

type batchMessage struct {
	data []byte
}

func (m *batchMessage) writeToConn(conn *Conn) error {
	return conn.Batch(m.data)
}

// Listens for the connection's broadcastQueue channel and executes the writeToConn method of the inqueued message.
func (conn *Conn) broadcastListener() {
	for {
		select {
		case <-conn.done:
			return
		case m, ok := <-conn.broadcastQueue:
			if !ok {
				return
			}
			if err := m.writeToConn(conn); IsFatalErr(err) {
				return
			}
		}
	}
}

// broadcast creates a message of the given opcode and data and queues it to the given conns.
// If a connection queue if full, this method will handle it according to the upgrader's
// "BroadcastBackpressure" option. Look at "Options" struct for more info.
//
// The broadcasting respects context cancellation and will terminate early if the context
// is cancelled during the operation.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent it successfully)
//   - error: context cancellation error, or nil if completed normally
func (u *Upgrader) broadcast(ctx context.Context, conns []*Conn, opcode uint8, data []byte) (n int, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	m := &singleMessage{ctx: ctx, opcode: opcode, data: data}

	for _, conn := range conns {
		select {
		case <-ctx.Done():
			return n, ctx.Err()
		case conn.broadcastQueue <- m:
			n++

		default:
			switch u.BroadcastBackpressure {
			case BackpressureDrop: // drop
			case BackpressureClose:
				go conn.CloseWithCode(ClosePolicyViolation, "client too slow")
			case BackpressureWait:
				// inqueue again
				select {
				case <-ctx.Done():
					return n, ctx.Err()
				case conn.broadcastQueue <- m:
					n++
				}
			default:
				panic(fmt.Errorf("unexpected backpressure strategy: %v", u.BroadcastBackpressure))
			}
		}
	}

	return n, nil
}
