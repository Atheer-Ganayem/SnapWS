package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var manager *snapws.Manager[string]

func main() {
	upgrader := snapws.NewUpgrader(&snapws.Options{
		Middlwares: []snapws.Middlware{rejectDuplicateNames},
	})
	manager = snapws.NewManager[string](upgrader)
	defer manager.Shutdown()
	manager.OnRegister = onRigester
	manager.OnUnregister = onUnrigester

	http.HandleFunc("/", handler)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func handler(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	conn, err := manager.Connect(name, w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		data, err := conn.ReadString()
		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}

		// Broadcast message to all except sender
		msg := fmt.Sprintf("%s: %s", name, data)
		_, err = manager.BroadcastString(context.TODO(), conn.Key, []byte(msg))
		if err != nil {
			fmt.Println(err)
		}
	}
}

func rejectDuplicateNames(w http.ResponseWriter, r *http.Request) error {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		return snapws.NewMiddlewareErr(http.StatusBadRequest, "username cannot be empty.")
	}
	_, exists := manager.GetConn(name)
	if exists {
		return snapws.NewMiddlewareErr(http.StatusBadRequest, "username already exists, choose another one.")
	}

	return nil
}

func onRigester(id string, conn *snapws.ManagedConn[string]) {
	manager.BroadcastString(context.TODO(), id, []byte(id+" connected"))
}
func onUnrigester(id string, conn *snapws.ManagedConn[string]) {
	conn.Manager.BroadcastString(context.TODO(), id, []byte(id+" disconnected"))
}
