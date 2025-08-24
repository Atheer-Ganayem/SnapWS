# snapws

[![Go Reference](https://pkg.go.dev/badge/github.com/Atheer-Ganayem/SnapWS.svg)](https://pkg.go.dev/github.com/Atheer-Ganayem/SnapWS)
![License](https://img.shields.io/github/license/Atheer-Ganayem/SnapWS)
![Status](https://img.shields.io/badge/status-in%20development-yellow)

**SnapWS is a minimal WebSocket library for Go.**

It takes care of ping/pong, close frames, connection safety, rate limiting, and lifecycle management so you can just connect, read, and write — without boilerplate or extra complexity.

> 🧠 **Why?**  
> Using [gorilla/websocket](https://github.com/gorilla/websocket) often felt like overkill. You had to write a lot of code, worry about race conditions, manually handle timeouts, and understand the WebSocket protocol more deeply than necessary.
>
> `snapws` handles the boring stuff for you — so you can **just send and receive messages**.

---

## ✨ Features

- ✅ Minimal and easy to use API.
- ✅ Fully passes the [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite) (not including PMCE)
- ✅ Automatic handling of ping/pong and close frames.
- ✅ Connection manager (useful when communicating between different clients like chat apps).
- ✅ Room manager.
- ✅ Rate limiter.
- ✅ Written completely in standard library amd Go offical libraries, no external libraries imported.
- ✅ Support for middlewares and connect/disconnect hooks.

---

## API Reference

[API Reference](https://pkg.go.dev/github.com/Atheer-Ganayem/SnapWS)

## Examples

[Examples]("./cmd/examples")

- [Simple echo](./cmd/examples/echo/main.go)
- [Room bases chat](./cmd/examples/room-chat/main.go)
- [Direct messages (1:1 chat)](./cmd/examples/direct-messages/main.go)
- [File streaming](./cmd/examples/file-streaming/main.go)

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

	http.HandleFunc("/echo", handler)

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
		data, err := conn.ReadString()
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

## Benchmark

For the benchmark i used [lesismal/go-websocket-benchmark](https://github.com/lesismal/go-websocket-benchmark)

20250809 16:38.55.590 [Connections] Report

| Framework        | TPS   | Min  | Avg     | Max      | TP50 | TP75  | TP90     | TP95     | TP99     | Used     | Total | Success | Failed | Concurrency |
| ---------------- | ----- | ---- | ------- | -------- | ---- | ----- | -------- | -------- | -------- | -------- | ----- | ------- | ------ | ----------- |
| snapws           | 28684 | 30ns | 55.99ms | 346.35ms | 50ns | 70ns  | 275.47ms | 298.87ms | 336.63ms | 348.62ms | 10000 | 10000   | 0      | 2000        |
| fasthttp         | 28642 | 40ns | 58.34ms | 347.25ms | 60ns | 101ns | 290.28ms | 306.14ms | 329.82ms | 349.14ms | 10000 | 10000   | 0      | 2000        |
| gobwas           | 29390 | 40ns | 57.45ms | 336.80ms | 60ns | 90ns  | 291.22ms | 305.66ms | 329.38ms | 340.25ms | 10000 | 10000   | 0      | 2000        |
| gorilla          | 30774 | 30ns | 54.98ms | 324.65ms | 60ns | 71ns  | 270.29ms | 289.05ms | 318.33ms | 324.94ms | 10000 | 10000   | 0      | 2000        |
| gws              | 31822 | 30ns | 53.05ms | 312.51ms | 40ns | 60ns  | 263.19ms | 280.10ms | 307.31ms | 314.25ms | 10000 | 10000   | 0      | 2000        |
| gws_std          | 29149 | 30ns | 57.51ms | 341.75ms | 50ns | 70ns  | 282.57ms | 302.46ms | 333.35ms | 343.06ms | 10000 | 10000   | 0      | 2000        |
| nbio_blocking    | 31467 | 30ns | 51.78ms | 316.16ms | 60ns | 90ns  | 256.40ms | 273.12ms | 297.19ms | 317.79ms | 10000 | 10000   | 0      | 2000        |
| nbio_mixed       | 31634 | 30ns | 52.15ms | 314.56ms | 40ns | 60ns  | 254.83ms | 276.40ms | 309.25ms | 316.11ms | 10000 | 10000   | 0      | 2000        |
| nbio_nonblocking | 31450 | 30ns | 53.50ms | 315.71ms | 50ns | 70ns  | 265.12ms | 291.28ms | 311.19ms | 317.96ms | 10000 | 10000   | 0      | 2000        |
| nbio_std         | 31475 | 30ns | 53.96ms | 315.56ms | 51ns | 70ns  | 268.46ms | 291.03ms | 309.30ms | 317.71ms | 10000 | 10000   | 0      | 2000        |
| nettyws          | 28249 | 30ns | 60.69ms | 353.68ms | 60ns | 71ns  | 298.53ms | 320.95ms | 343.66ms | 353.99ms | 10000 | 10000   | 0      | 2000        |
| nhooyr           | 27858 | 40ns | 60.12ms | 356.48ms | 61ns | 150ns | 296.83ms | 314.80ms | 342.78ms | 358.95ms | 10000 | 10000   | 0      | 2000        |
| quickws          | 30345 | 30ns | 55.77ms | 324.83ms | 50ns | 70ns  | 274.96ms | 291.11ms | 319.53ms | 329.54ms | 10000 | 10000   | 0      | 2000        |
| greatws          | 31196 | 30ns | 54.83ms | 316.69ms | 60ns | 80ns  | 271.19ms | 287.38ms | 309.14ms | 320.55ms | 10000 | 10000   | 0      | 2000        |
| greatws_event    | 31293 | 30ns | 54.57ms | 316.67ms | 60ns | 100ns | 269.73ms | 285.75ms | 308.87ms | 319.56ms | 10000 | 10000   | 0      | 2000        |

20250809 16:38.55.603 [BenchEcho] Report
| Framework | TPS | EER | Min | Avg | Max | TP50 | TP75 | TP90 | TP95 | TP99 | Used | Total | Success | Failed | Conns | Concurrency | Payload | CPU Min | CPU Avg | CPU Max | MEM Min | MEM Avg | MEM Max |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| snapws | 236363 | 343.02 | 36.54us | 42.18ms | 155.55ms | 40.15ms | 44.80ms | 51.95ms | 54.03ms | 60.17ms | 8.46s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 492.55 | 689.07 | 721.93 | 278.23M | 279.79M | 282.48M |
| fasthttp | 239613 | 377.94 | 31.85us | 41.61ms | 170.61ms | 39.14ms | 45.88ms | 51.33ms | 54.83ms | 68.28ms | 8.35s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 378.65 | 634.00 | 685.17 | 255.04M | 263.28M | 272.17M |
| gobwas | 185234 | 202.44 | 42.28us | 53.84ms | 313.39ms | 48.59ms | 56.89ms | 68.78ms | 78.44ms | 163.85ms | 10.80s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 884.37 | 914.99 | 938.72 | 373.06M | 373.92M | 374.44M |
| gorilla | 237741 | 368.23 | 31.83us | 41.94ms | 133.16ms | 39.40ms | 46.09ms | 51.82ms | 54.22ms | 68.08ms | 8.41s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 435.74 | 645.64 | 690.66 | 255.05M | 263.41M | 272.30M |
| gws | 230658 | 349.94 | 32.61us | 43.21ms | 149.39ms | 40.68ms | 48.10ms | 54.27ms | 58.61ms | 68.74ms | 8.67s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 547.45 | 659.13 | 703.63 | 180.92M | 195.75M | 201.79M |
| gws_std | 247967 | 376.79 | 30.24us | 40.19ms | 138.11ms | 37.92ms | 42.49ms | 50.13ms | 52.13ms | 63.86ms | 8.07s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 615.85 | 658.10 | 678.29 | 157.96M | 181.97M | 192.70M |
| nbio_blocking | 245887 | 363.56 | 28.45us | 40.52ms | 143.64ms | 38.32ms | 42.78ms | 50.39ms | 52.63ms | 61.22ms | 8.13s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 666.77 | 676.33 | 685.51 | 172.87M | 185.99M | 194.12M |
| nbio_mixed | 239852 | 382.37 | 27.60us | 41.55ms | 119.88ms | 39.53ms | 45.62ms | 51.43ms | 54.29ms | 61.11ms | 8.34s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 366.83 | 627.27 | 683.03 | 383.18M | 439.26M | 459.18M |
| nbio_nonblocking | 222046 | 293.09 | 42.97us | 44.92ms | 169.98ms | 42.77ms | 49.27ms | 56.72ms | 61.13ms | 71.59ms | 9.01s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 745.24 | 757.62 | 767.76 | 59.64M | 64.55M | 67.52M |
| nbio_std | 250083 | 373.14 | 31.71us | 39.82ms | 124.51ms | 37.77ms | 42.48ms | 49.47ms | 51.44ms | 57.87ms | 8.00s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 663.82 | 670.21 | 678.70 | 174.72M | 178.93M | 181.09M |
| nettyws | 246737 | 370.00 | 24.45us | 40.40ms | 115.49ms | 38.36ms | 42.78ms | 50.24ms | 52.10ms | 57.40ms | 8.11s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 655.79 | 666.85 | 677.53 | 162.61M | 163.68M | 165.86M |
| nhooyr | 194207 | 241.66 | 42.80us | 51.35ms | 173.62ms | 49.38ms | 55.01ms | 61.00ms | 63.37ms | 71.74ms | 10.30s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 471.50 | 803.63 | 848.54 | 365.11M | 367.61M | 370.11M |
| quickws | 252792 | 388.74 | 29.61us | 39.41ms | 131.88ms | 37.58ms | 42.00ms | 48.83ms | 50.58ms | 54.18ms | 7.91s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 637.78 | 650.29 | 670.81 | 127.99M | 129.62M | 132.24M |
| greatws | 232138 | 338.37 | 47.61us | 42.96ms | 120.72ms | 41.19ms | 46.60ms | 53.14ms | 55.58ms | 61.57ms | 8.62s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 593.87 | 686.06 | 705.71 | 53.09M | 55.75M | 57.70M |
| greatws_event | 240881 | 386.32 | 42.47us | 41.40ms | 115.71ms | 39.55ms | 44.38ms | 51.01ms | 53.14ms | 57.78ms | 8.30s | 2000000 | 2000000 | 0 | 10000 | 10000 | 1024 | 346.84 | 623.52 | 672.67 | 48.77M | 52.11M | 53.27M |

20250809 16:38.55.610 [BenchRate] Report
| Framework | Duration | EchoEER | Packet Sent | Bytes Sent | Packet Recv | Bytes Recv | Conns | SendRate | Payload | CPU Min | CPU Avg | CPU Max | MEM Min | MEM Avg | MEM Max |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| snapws | 10.00s | 686.77 | 5709440 | 5.44G | 5395180 | 5.15G | 10000 | 200 | 1024 | 492.55 | 785.58 | 891.36 | 278.23M | 281.49M | 282.98M |
| fasthttp | 10.00s | 755.80 | 5997330 | 5.72G | 5696392 | 5.43G | 10000 | 200 | 1024 | 378.65 | 753.69 | 871.76 | 255.04M | 282.46M | 318.98M |
| gobwas | 10.00s | 381.06 | 3899360 | 3.72G | 3547509 | 3.38G | 10000 | 200 | 1024 | 554.21 | 930.96 | 1142.24 | 373.06M | 380.17M | 416.00M |
| gorilla | 10.00s | 729.47 | 5797780 | 5.53G | 5460790 | 5.21G | 10000 | 200 | 1024 | 435.74 | 748.59 | 860.77 | 255.05M | 274.74M | 286.30M |
| gws | 10.00s | 707.68 | 5573440 | 5.32G | 5261550 | 5.02G | 10000 | 200 | 1024 | 491.85 | 743.49 | 867.73 | 180.92M | 210.12M | 233.69M |
| gws_std | 10.00s | 738.69 | 5731410 | 5.47G | 5399665 | 5.15G | 10000 | 200 | 1024 | 241.92 | 730.98 | 966.15 | 157.96M | 190.84M | 201.57M |
| nbio_blocking | 10.00s | 714.49 | 5648280 | 5.39G | 5313563 | 5.07G | 10000 | 200 | 1024 | 238.71 | 743.69 | 954.78 | 172.87M | 192.14M | 196.99M |
| nbio_mixed | 10.00s | 724.91 | 5730050 | 5.46G | 5399240 | 5.15G | 10000 | 200 | 1024 | 366.83 | 744.81 | 861.81 | 383.18M | 505.80M | 579.18M |
| nbio_nonblocking | 10.00s | 649.80 | 5471200 | 5.22G | 5148591 | 4.91G | 10000 | 200 | 1024 | 294.85 | 792.33 | 1012.94 | 59.64M | 98.42M | 198.98M |
| nbio_std | 10.00s | 710.06 | 5559300 | 5.30G | 5224786 | 4.98G | 10000 | 200 | 1024 | 288.92 | 735.82 | 961.67 | 174.72M | 187.30M | 196.22M |
| nettyws | 10.00s | 731.41 | 5795280 | 5.53G | 5485818 | 5.23G | 10000 | 200 | 1024 | 214.93 | 750.04 | 1009.48 | 162.61M | 169.25M | 174.23M |
| nhooyr | 10.00s | 486.79 | 4862030 | 4.64G | 4529391 | 4.32G | 10000 | 200 | 1024 | 471.50 | 930.46 | 1095.54 | 365.11M | 368.99M | 370.61M |
| quickws | 10.00s | 723.30 | 5760040 | 5.49G | 5446060 | 5.19G | 10000 | 200 | 1024 | 354.88 | 752.95 | 942.73 | 113.04M | 122.09M | 132.24M |
| greatws | 10.00s | 643.54 | 5365640 | 5.12G | 5004079 | 4.77G | 10000 | 200 | 1024 | 555.87 | 777.59 | 911.84 | 53.09M | 64.16M | 75.27M |
| greatws_event | 10.00s | 685.13 | 5614890 | 5.35G | 5254480 | 5.01G | 10000 | 200 | 1024 | 346.84 | 766.93 | 899.18 | 48.77M | 51.42M | 53.27M |
