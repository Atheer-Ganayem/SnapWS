package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

type FileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var manager *snapws.Manager[string]

func main() {
	manager = snapws.NewManager(&snapws.Options[string]{
		PingEvery:      time.Second * 25,
		ReadWait:       time.Second * 30,
		MaxMessageSize: snapws.DefaultReadBufferSize * 2,
	})
	defer manager.Shutdown()

	http.HandleFunc("/download", donwloadHandler)
	http.HandleFunc("/upload", uploadHandler)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func donwloadHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := manager.Connect(r.RemoteAddr, w, r)
	if err != nil {
		return
	}
	defer conn.Close()

	file, err := os.Open("cmd/examples/file-streaming/lorem.txt")
	if err != nil {
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return
	}

	err = conn.SendJSON(context.TODO(), &FileInfo{Name: file.Name(), Size: info.Size()})
	if err != nil {
		return
	}

	writer, err := conn.NextWriter(context.TODO(), snapws.OpcodeBinary)
	if err != nil {
		return
	}
	defer writer.Close()

	_, err = file.WriteTo(writer)
	if err != nil {
		return
	}
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := manager.Connect(r.RemoteAddr, w, r)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(err.Error()))
		return
	}
	defer conn.Close()

	var info FileInfo
	if err := conn.ReadJSON(context.TODO(), &info); err != nil || info.Name == "" || info.Size == 0 {
		return
	}

	file, err := os.Create("cmd/examples/file-streaming/" + info.Name)
	if err != nil {
		return
	}
	defer file.Close()

	reader, msgType, err := conn.NextReader(context.TODO())
	if err != nil || msgType != snapws.OpcodeBinary {
		return
	}

	written := 0
	buf := make([]byte, manager.ReadBufferSize)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if written+n > int(info.Size) {
				return
			}
			if _, wErr := file.Write(buf[:n]); wErr != nil {
				return
			}
			written += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
	}
}
