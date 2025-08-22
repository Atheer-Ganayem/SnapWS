package main

import (
	"context"
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var roomManager *snapws.RoomManager[string]

func main() {
	roomManager = snapws.NewRoomManager[string](nil)
	defer roomManager.Shutdown()
	roomManager.Upgrader.Use(validateQuery)

	http.HandleFunc("/join", handleJoin)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func handleJoin(w http.ResponseWriter, r *http.Request) {
	roomQuery := r.URL.Query().Get("room")
	name := r.URL.Query().Get("name")

	conn, room, err := roomManager.Connect(w, r, roomQuery)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		msg, err := conn.ReadString()
		if snapws.IsFatalErr(err) {
			return
		} else if err != nil {
			continue
		}

		msg = append([]byte(name+": "), msg...)
		room.BroadcastString(context.TODO(), msg)
	}
}

// This is just some dummy example.
// In real-world apps you will have authentication, which will prevent having
// duplicate usernames (or whatever you identifier is).
func validateQuery(w http.ResponseWriter, r *http.Request) error {
	room := r.URL.Query().Get("room")
	name := r.URL.Query().Get("name")

	if room == "" || name == "" {
		return snapws.NewMiddlewareErr(http.StatusBadRequest, "empty room or name")
	}

	return nil
}

func onJoin() {

}
