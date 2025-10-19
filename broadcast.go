package snapws

import (
	"context"
	"fmt"
	"time"
)

type message interface {
	writeToConn(conn *Conn) error
}

// normal message (not a batch)
// This is used for (u *Upgrader) broadcast(), to broadcast a single normal websocket message in one frame.
// Implements the message interface
type frame struct {
	ctx context.Context
	buf []byte
}

// Here we will send the frame directly (in one syscall) without copying to user buffer (without facing the possibility of fragmenting it).
// This will save us syscalls and copies.
func (m *frame) writeToConn(conn *Conn) error {
	// Acquire write lock
	if err := conn.writeLock.lockCtx(m.ctx); err != nil {
		return err
	}
	defer conn.writeLock.unLock()

	// set deadline
	if err := conn.raw.SetWriteDeadline(time.Now().Add(conn.upgrader.WriteWait)); err != nil {
		conn.CloseWithCode(CloseInternalServerErr, ErrInternalServer.Error())
		return fatal(err)
	}

	// send frame
	if err := conn.sendFrame(m.buf); err != nil {
		return fatal(err)
	}

	return nil
}

// Implements the message interface.
// It represents a single message (only payload) to-be batched by a Conn.
type batchMessage struct {
	buf []byte
}

func (m *batchMessage) writeToConn(conn *Conn) error {
	return conn.Batch(m.buf)
}

// Listens for the connection's broadcastQueue channel and executes the writeToConn method of the inqueued message.
func (conn *Conn) broadcastListener() {
	for {
		select {
		case <-conn.done:
			// drain the channel (this will make the GC clean the channel faster)
			for len(conn.broadcastQueue) > 0 {
				<-conn.broadcastQueue
			}
			return
		case m, ok := <-conn.broadcastQueue:
			if !ok {
				return
			}
			if err := m.writeToConn(conn); IsFatalErr(err) {
				fmt.Println(err)
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

	m := &frame{ctx: ctx, buf: make([]byte, len(data)+MaxHeaderSize)}
	start, err := writeHeaders(m.buf, 14, len(m.buf), true, opcode, false)
	if err != nil {
		return n, err
	}
	copy(m.buf[14:], data)
	m.buf = m.buf[start:]

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

// broadcast creates a message queues it to the given conns as a batch message.
// If a connection queue if full, this method will handle it according to the upgrader's
// "BroadcastBackpressure" option. Look at "Options" struct for more info.
//
// The broadcasting respects context cancellation and will terminate early if the context
// is cancelled during the operation.
//
// Returns:
//   - int: number of connections that successfully received the message in thier
//     queue (doesnt necessarily mean they sent/batched it successfully)
//   - error: context cancellation error, or nil if completed normally
func (u *Upgrader) batchBroadcast(ctx context.Context, conns []*Conn, data []byte) (n int, err error) {
	if u.Flusher == nil {
		return 0, ErrBatchingUninitialized
	}

	m := &batchMessage{buf: data}

	for _, conn := range conns {
		select {
		case <-u.Flusher.closed:
			return n, ErrFlusherClosed
		case <-u.Flusher.ctx.Done():
			return n, u.Flusher.ctx.Err()
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
				// wait till sent
				select {
				case <-u.Flusher.closed:
					return n, ErrFlusherClosed
				case <-u.Flusher.ctx.Done():
					return n, u.Flusher.ctx.Err()
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
