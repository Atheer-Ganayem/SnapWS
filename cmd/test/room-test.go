package main

import (
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var roomManager *snapws.RoomManager[string]

// func main() {
// 	roomManager = snapws.NewRoomManager[string](nil)
// 	defer roomManager.Shutdown()

// 	roomManager.Upgrader.Use(mw)

// 	// roomManager.DefaultOnJoin = func(room *snapws.Room[string], conn *snapws.Conn, args ...any) {
// 	// 	id, _ := snapws.GetArg[string](args, 0)
// 	// 	room.BatchJSON(id + " joined")
// 	// }
// 	// roomManager.DefaultOnLeave = func(room *snapws.Room[string], conn *snapws.Conn, args ...any) {
// 	// 	id, _ := snapws.GetArg[string](args, 0)
// 	// 	room.BatchJSON(id + " left")
// 	// }

// 	http.HandleFunc("/join", handler)
// 	fmt.Println("server listening port 8080")
// 	http.ListenAndServe(":8080", nil)
// }

type msg struct {
	Sender string   `json:"sender"`
	Text   string   `json:"text"`
	Vals   []string `json:"vals"`
}

func handler(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	roomQuery := r.URL.Query().Get("room")
	conn, room, err := roomManager.Connect(w, r, roomQuery, id)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return // fatal
		}

		msg := fmt.Sprintf("%s: %s", id, data)
		room.BroadcastString(nil, []byte(msg))
		// room.BatchJSON(msg)

		// err = room.BatchJSON(&msg{Sender: id, Text: string(data), Vals: []string{"hello", "world", "!"}})
		// if err != nil {
		// 	fmt.Println(err)
		// }
		// room.BatchJSON(&[]string{"part", "2"})
		// room.BatchJSON(&[]int{1, 2, 3, 4, 5, 100, 123})
		// room.BatchJSON("4th")

		// b := true
		// n := 69
		// room.BatchJSON(b)
		// room.BatchJSON(n)
		// room.BatchJSON([]int8{1, 2, 3, 4, 5, 127})
		// room.BatchJSON([]int{1, 2, 3, 4, 5})

		// room.BatchJSON([]byte{1, 1, 1})
	}
}

func mw(w http.ResponseWriter, r *http.Request) error {
	id := r.URL.Query().Get("id")
	roomQuery := r.URL.Query().Get("room")
	if id == "" || roomQuery == "" {
		return snapws.NewMiddlewareErr(http.StatusBadRequest, "invalid query")
	}

	room := roomManager.Get(roomQuery)
	if room == nil {
		room = roomManager.Add(roomQuery)
		// room.EnableJSONBatching(context.TODO(), time.Millisecond*50, nil)
	}

	return nil
}
