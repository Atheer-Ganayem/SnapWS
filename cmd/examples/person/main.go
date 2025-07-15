package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

type Person struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

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

		for {
			var person Person
			data, err := conn.ReadString() // json is text
			if err == snapws.ErrMessageTypeMismatch {
				fmt.Println("received wrong type of message.")
				continue
			} else if err != nil {
				fmt.Printf("Err: %s\n", err.Error())
				return
			}

			err = json.Unmarshal([]byte(data), &person)
			if err != nil {
				fmt.Println("couldnt umarshal json.")
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
			defer cancel()

			msg := fmt.Sprintf("Hello %s, you were born in %d", person.Name, time.Now().Year()-person.Age)
			err = conn.SendString(ctx, msg)
			if err != nil {
				fmt.Printf("Err sending: %s\n", err.Error())
			}
		}
	})
	fmt.Println("Server listenning")
	http.ListenAndServe(":8080", nil)
}
