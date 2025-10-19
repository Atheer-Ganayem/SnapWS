package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var rm *snapws.RoomManager[string]

type message struct {
	Sender string `json:"sender"`
	Text   string `json:"text"`
}

func main() {
	rm = snapws.NewRoomManager[string](nil)
	defer rm.Shutdown()

	// Batch messages as a JSON array and flush them every 100MS.
	// The batch IS per connection NOT per room.
	rm.Upgrader.EnableJSONBatching(context.TODO(), time.Millisecond*1000)

	http.HandleFunc("/ws", handleWS)
	http.ListenAndServe(":8080", nil)
}

func handleWS(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("username")
	roomQuery := r.URL.Query().Get("room")

	conn, room, err := rm.Connect(w, r, roomQuery)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if snapws.IsFatalErr(err) {
			return
		} else if err != nil {
			fmt.Printf("non-fatal: %s\n", err)
		}

		msg := message{Sender: name, Text: string(data)}
		_, err = room.BatchBroadcastJSON(context.TODO(), &msg)
		if err != nil {
			fmt.Println(err)
			// continue
		}
	}
}
