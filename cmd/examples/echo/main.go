package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

func main() {
	manager := snapws.NewManager(&snapws.Args[string]{
		PingEvery:       time.Second * 5,
		ReadWait:        time.Second * 10,
		WriteBufferSize: 1024,
	})
	manager.Use(func(w http.ResponseWriter, r *http.Request) error {
		if r.URL.Query().Get("auth") != "123" {
			return errors.New("not auth")
		}
		fmt.Println("middleware1")
		return nil
	})
	manager.Use(func(w http.ResponseWriter, r *http.Request) error {
		fmt.Println("middleware2")
		return nil
	})
	manager.OnConnect = func(id string, conn *snapws.Conn[string]) {
		fmt.Printf("User %s has been connected\n", id)
	}
	manager.OnDisconnect = func(id string, conn *snapws.Conn[string]) {
		fmt.Printf("User %s has been disconnected\n", id)
	}
	defer manager.Shutdown()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := manager.Connect(r.RemoteAddr, w, r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(err.Error()))
			return
		}
		// all read errors close the connection exepet ErrMessageTypeMismatch (you have the option to close it or not).
		// hoverever, defering conn.Close() is the best practice just in case it stay open.
		defer conn.Close()

		for {
			msg, err := conn.ReadString(context.TODO())
			fmt.Println(msg)
			if snapws.IsFatalErr(err) {
				fmt.Println(err)
				return
			} else if err != nil {
				fmt.Println("received wrong type of message.")
				continue
			}

			if msg != "" {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				//
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
