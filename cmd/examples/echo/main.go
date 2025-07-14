package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

func main() {
	manager := snapws.NewManager[string](nil)
	manager.OnConnect = func(id string, conn *snapws.Conn[string]) {
		fmt.Printf("User %s has been connected\n", id)
	}
	manager.OnDisconnect = func(id string, conn *snapws.Conn[string]) {
		fmt.Printf("User %s has been disconnected\n", id)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := manager.Connect(r.RemoteAddr, w, r)
		if err != nil {
			w.Write([]byte(err.Error()))
			w.WriteHeader(http.StatusBadRequest)
		}
		defer conn.Close()

		for {
			msg, err := conn.ReadString()
			if err != nil {
				fmt.Printf("Err: %s\n", err.Error())
				return
			}

			if msg != "" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()

				err = conn.SendString(ctx, fmt.Sprintf("Received: %s", msg))
				if err != nil {
					fmt.Println(err)
				}
			}
		}
	})
	fmt.Println("Server listenning")
	http.ListenAndServe(":8080", nil)
}
