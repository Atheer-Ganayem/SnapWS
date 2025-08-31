package main

import (
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var rm *snapws.RoomManager[string]

func main() {
	rm = snapws.NewRoomManager[string](snapws.NewUpgrader(&snapws.Options{
		SkipUTF8Validation: true,
	}))
	defer rm.Shutdown()

	rm.Add("normal")
	r := rm.Add("batch")
	r.EnableJSONBatching(nil, time.Millisecond*50, nil)

	http.HandleFunc("/normal", normal)
	http.HandleFunc("/batch", batch)

	fmt.Println("server letstining port 8080")
	http.ListenAndServe(":8080", nil)
}

func normal(w http.ResponseWriter, r *http.Request) {
	conn, room, err := rm.Connect(w, r, "room")
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

		if n, err := room.BroadcastString(nil, data); err != nil {
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

		if err := room.BatchJSON(data); err != nil {
			fmt.Println(err)
		}
	}
}
