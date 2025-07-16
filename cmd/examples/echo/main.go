package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

func main() {
	manager := snapws.NewManager(&snapws.Args[string]{
		PingEvery: time.Second * 5,
		ReadWait:  time.Second * 10,
	})
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
		// all read errors close the connection exepet ErrMessageTypeMismatch (you have the option to close it or not).
		// hoverever, defering conn.Close() is the best practice just in case it stay open.
		defer conn.Close()
		time.Sleep(time.Second * 3)

		for {
			msg, err := conn.ReadString()
			if err == snapws.ErrMessageTypeMismatch {
				fmt.Println("received wrong type of message.")
				continue
			} else if err != nil {
				fmt.Printf("Err: %s\n", err.Error())
				return
			}
			if msg != "" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
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
