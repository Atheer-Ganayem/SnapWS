package snapws

import (
	"context"
	"sync"
	"sync/atomic"
)

type broadcastTask struct {
	ctx    context.Context
	opcode uint8
	data   []byte
}

type batchTask struct {
	ctx      context.Context
	messages [][]byte
	batcher  messageBatcher
}

func (u *Upgrader) broadcast(ctx context.Context, conns []*Conn, opcode uint8, data []byte) (n int, err error) {
	if u.PersistentBroadcastWorkers {
		// for loop on channels
		// gotta have sone different stratigies here
		return len(conns), nil
	}

	return workersBroadcast(ctx, conns, opcode, data, u.BroadcastWorkers(len(conns)))
}

func (conn *Conn) broadcastListener() {
	for {
		select {
		case <-conn.done:
			return
		case bt, ok := <-conn.broadcast:
			if !ok {
				return
			}
			if err := conn.SendMessage(bt.ctx, bt.opcode, bt.data); IsFatalErr(err) {
				return
			}
		case bt, ok := <-conn.batch:
			if !ok {
				return
			}

			if err := bt.batcher.send(conn, bt.messages); IsFatalErr(err) {
				return
			}
		}
	}
}

func workersBroadcast(ctx context.Context, conns []*Conn, opcode uint8, data []byte, workers int) (int, error) {
	var wg sync.WaitGroup
	ch := make(chan *Conn, workers)
	done := make(chan struct{})
	var n int64

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for conn := range ch {
				if ctx != nil && ctx.Err() != nil {
					return
				}

				if err := conn.SendMessage(ctx, opcode, data); err == nil {
					atomic.AddInt64(&n, 1)
				}
			}
		}()
	}

	go func() {
		for _, conn := range conns {
			if ctx != nil && ctx.Err() != nil {
				break
			}
			ch <- conn
		}
		close(ch)
		wg.Wait()
		close(done)
	}()

	if ctx == nil {
		<-done
		return int(n), nil
	}

	select {
	case <-done:
		return int(n), nil
	case <-ctx.Done():
		return int(n), ctx.Err()
	}
}
