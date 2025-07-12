package main

import (
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

func main() {
	manager := snapws.NewManager[string](nil)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := manager.Connect(r.RemoteAddr, w, r)
		if err != nil {
			w.Write([]byte(err.Error()))
			w.WriteHeader(http.StatusBadRequest)
		}
		defer manager.Unregister(r.RemoteAddr)

		for {
			msg, err := conn.ReadString()
			if err != nil {
				fmt.Printf("Err: %s\n", err.Error())
				return
			}

			fmt.Println(msg)
		}
	})

	fmt.Println("Server listenning")
	http.ListenAndServe(":8080", nil)
}
