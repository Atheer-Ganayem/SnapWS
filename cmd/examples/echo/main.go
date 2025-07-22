package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var manager *snapws.Manager[string]

func main() {
	manager = snapws.NewManager(&snapws.Options[string]{
		PingEvery: time.Second * 25,
		ReadWait:  time.Second * 30,
	})
	defer manager.Shutdown()

	http.HandleFunc("/", handler)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := manager.Connect(r.RemoteAddr, w, r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
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
