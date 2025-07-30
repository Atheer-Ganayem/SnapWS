# snapws

[![Go Reference](https://pkg.go.dev/badge/github.com/Atheer-Ganayem/SnapWS.svg)](https://pkg.go.dev/github.com/Atheer-Ganayem/SnapWS)
![License](https://img.shields.io/github/license/Atheer-Ganayem/SnapWS)
![Status](https://img.shields.io/badge/status-in%20development-yellow)

**SnapWS is a minimal WebSocket library for Go.**

It takes care of ping/pong, close frames, connection safety, and lifecycle management so you can just connect, read, and write — without boilerplate or extra complexity.

> 🚧 **UNDER DEVELOPMENT** 🚧  
> This library is not yet production-ready. Expect breaking changes as development continues.

> 🧠 **Why?**  
> Using [gorilla/websocket](https://github.com/gorilla/websocket) often felt like overkill. You had to write a lot of code, worry about race conditions, manually handle timeouts, and understand the WebSocket protocol more deeply than necessary.
>
> `snapws` handles the boring stuff for you — so you can **just send and receive messages**.

---

## ✨ Features

- ✅ **Minimal code**: focus on your app, not protocol details
- ✅ Automatic handling of ping/pong and close frames.
- ✅ Connection management built-in.
- ✅ Safe for concurrent use.
- ✅ Support for middlewares and connect/disconnect hooks.
- ✅ Context support for cancellation

---

## 🚀 Getting Started

### Install

```bash
go get github.com/Atheer-Ganayem/SnapWS
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
```

## API Reference

https://pkg.go.dev/github.com/Atheer-Ganayem/SnapWS
