package main

import (
	"context"
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var upgrader *snapws.Upgrader

func main() {
	upgrader = snapws.NewUpgrader(nil)

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
		data, err := conn.ReadString(context.TODO())
		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}

		err = conn.SendString(context.TODO(), data)
		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}
	}
}
