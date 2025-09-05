package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var rm *snapws.RoomManager[string]

func main() {
	rm = snapws.NewRoomManager[string](snapws.NewUpgrader(&snapws.Options{
		SkipUTF8Validation:    true,
		BroadcastBackpressure: snapws.BackpressureWait,
		MaxBatchSize:          11,
	}))
	rm.Upgrader.EnableJSONBatching(context.TODO(), time.Second*5)
	defer rm.Shutdown()
	// rm.Upgrader.EnableBatching(nil, time.Second*5, func(ctx context.Context, conn *snapws.Conn, messages [][]byte) error {
	// 	w, err := conn.NextWriter(ctx, snapws.OpcodeText)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	defer w.Close()

	// 	if _, err := w.Write([]byte("this is the message: ")); err != nil {
	// 		return err
	// 	}

	// 	for i, msg := range messages {
	// 		if _, err := w.Write([]byte(fmt.Sprintf("%d: ", i))); err != nil {
	// 			return err
	// 		}
	// 		if _, err := w.Write([]byte(string(msg))); err != nil {
	// 			return err
	// 		}
	// 	}

	// 	if _, err := w.Write([]byte("::end of message.")); err != nil {
	// 		return err
	// 	}

	// 	return nil
	// })

	http.HandleFunc("/normal", normal)
	http.HandleFunc("/batch", batch)

	fmt.Println("server letstining port 8080")
	http.ListenAndServe(":8080", nil)
}

func normal(w http.ResponseWriter, r *http.Request) {
	conn, room, err := rm.Connect(w, r, "normal")
	if err != nil {
		return
	}
	defer conn.Close()

	if err := conn.SendString(nil, []byte("ack")); err != nil {
		return
	}

	for {
		data, err := conn.ReadString()
		if err != nil {
			return
		}

		n, err := room.BroadcastString(nil, data)
		if err != nil {
			fmt.Println("boradcast faild:", n, err)
		}
	}
}

func batch(w http.ResponseWriter, r *http.Request) {
	conn, room, err := rm.Connect(w, r, "batch")
	if err != nil {
		return
	}
	defer conn.Close()

	if err := conn.SendString(nil, []byte("ack")); err != nil {
		return
	}

	for {
		data, err := conn.ReadString()
		if err != nil {
			return
		}

		if err := room.BatchBroadcastJSON(string(data)); err != nil {
			fmt.Println(err)
		}
	}
}
