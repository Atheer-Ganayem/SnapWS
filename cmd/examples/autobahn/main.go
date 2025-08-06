package main

import (
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var upgrader *snapws.Upgrader

func main() {
	upgrader = snapws.NewUpgrader(&snapws.Options{
		MaxMessageSize: snapws.DefaultMaxMessageSize * 16, // autobahn tests messages up to 16MB
	})

	http.HandleFunc("/", handler)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		t, data, err := conn.ReadMessage()
		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}

		err = conn.SendMessage(nil, t, data)
		if snapws.IsFatalErr(err) {
			fmt.Println("fatal", err)
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}
	}
}
