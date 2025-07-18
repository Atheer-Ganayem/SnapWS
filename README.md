# snapws
![Go Version](https://img.shields.io/badge/go-%3E=1.18-blue)
![License](https://img.shields.io/github/license/yourusername/snapws)
![Status](https://img.shields.io/badge/status-in%20development-yellow)

A lightweight WebSocket library for Go, designed to make working with WebSockets **simple, intuitive, and minimal** — especially for developers who **just want it to work** without having to manage ping/pong, connection safety, or concurrency manually.

> 🚧 **UNDER DEVELOPMENT** 🚧  
> This library is not yet production-ready. Expect breaking changes as development continues.

> 🧠 **Why?**  
> Using [gorilla/websocket](https://github.com/gorilla/websocket) often felt like overkill. You had to write a lot of code, worry about race conditions, manually handle timeouts, and understand the WebSocket protocol more deeply than necessary.  
>  
> `snapws` handles the boring stuff for you — so you can **just send and receive messages**.

---

## ✨ Features

- ✅ Minimal setup — just plug it into your HTTP server.
- ✅ Automatic handling of ping/pong (you don't need to write it).
- ✅ Connection management built-in — no need to write your own manager.
- ✅ Safe for concurrent use — internally protected.
- ✅ Support for middlewares and connect/disconnect hooks.
- ✅ Sensible defaults (timeouts, buffer sizes).
- ✅ Supports both text and binary frames.
- ✅ Read/write APIs.

---

## 🚀 Getting Started

### Install

```bash
go get github.com/yourusername/snapws
```

### Basic echo example

```go
package main

import (
	"context"
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var manager *snapws.Manager[string]

func main() {
	manager = snapws.NewManager[string](nil)
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
		msg, err := conn.ReadString(context.TODO())
		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}

		err = conn.SendString(context.TODO(), msg)

		if snapws.IsFatalErr(err) {
			return // Connection closed
		} else if err != nil {
			fmt.Println("Non-fatal error:", err)
			continue
		}
	}
}
```
