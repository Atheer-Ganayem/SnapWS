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

- ✅ Minimal and easy to use API.
- ✅ Fully passes the [autobahn-testsuite](https://github.com/crossbario/autobahn-testsuite)
- ✅ Automatic handling of ping/pong and close frames.
- ✅ Written completely in standard library, no external libraries imported.
- ✅ Connection management built-in (useful when communicating between different clients like chat apps).
- ✅ Support for middlewares and connect/disconnect hooks.

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


|    Framework     |  TPS  | Min  |   Avg   |   Max    | TP50 | TP75  |   TP90   |   TP95   |   TP99   |   Used   | Total | Success | Failed | Concurrency |
|     ---          |  ---  | ---  |   ---   |   ---    | ---  |  ---  |   ---    |   ---    |   ---    |   ---    |  ---  |   ---   |  ---   |     ---     |
|    snapws        | 30933 | 30ns | 54.50ms | 321.81ms | 60ns | 70ns  | 272.11ms | 291.46ms | 314.03ms | 323.27ms | 10000 |  10000  |   0    |    2000     |
|   fasthttp       | 30011 | 30ns | 56.97ms | 330.25ms | 60ns | 81ns  | 285.82ms | 301.44ms | 323.58ms | 333.20ms | 10000 |  10000  |   0    |    2000     |
|    gobwas        | 32198 | 30ns | 52.78ms | 309.42ms | 40ns | 60ns  | 258.08ms | 277.10ms | 303.25ms | 310.57ms | 10000 |  10000  |   0    |    2000     |
|    gorilla       | 31796 | 30ns | 53.83ms | 312.16ms | 60ns | 80ns  | 266.50ms | 287.16ms | 307.24ms | 314.50ms | 10000 |  10000  |   0    |    2000     |
|     gws          | 32703 | 30ns | 51.92ms | 304.97ms | 40ns | 60ns  | 255.50ms | 272.45ms | 298.76ms | 305.78ms | 10000 |  10000  |   0    |    2000     |
|    gws_std       | 31438 | 30ns | 53.52ms | 312.59ms | 60ns | 70ns  | 264.63ms | 282.49ms | 305.83ms | 318.08ms | 10000 |  10000  |   0    |    2000     |
|  nbio_blocking   | 33490 | 40ns | 50.11ms | 296.61ms | 60ns | 130ns | 245.71ms | 264.33ms | 287.57ms | 298.59ms | 10000 |  10000  |   0    |    2000     |
|   nbio_mixed     | 32773 | 30ns | 51.98ms | 304.01ms | 40ns | 50ns  | 255.02ms | 274.72ms | 299.08ms | 305.13ms | 10000 |  10000  |   0    |    2000     |
| nbio_nonblocking | 27100 | 30ns | 60.59ms | 366.78ms | 50ns | 70ns  | 302.82ms | 325.46ms | 352.71ms | 369.00ms | 10000 |  10000  |   0    |    2000     |
|   nbio_std       | 29485 | 40ns | 55.85ms | 337.18ms | 60ns | 110ns | 281.09ms | 298.23ms | 323.00ms | 339.15ms | 10000 |  10000  |   0    |    2000     |
|    nettyws       | 30890 | 30ns | 54.79ms | 322.48ms | 50ns | 70ns  | 271.05ms | 287.60ms | 314.48ms | 323.73ms | 10000 |  10000  |   0    |    2000     |
|    nhooyr        | 30191 | 40ns | 55.80ms | 328.71ms | 60ns | 80ns  | 276.34ms | 294.56ms | 321.50ms | 331.22ms | 10000 |  10000  |   0    |    2000     |
|    quickws       | 28825 | 30ns | 56.24ms | 345.09ms | 60ns | 80ns  | 278.86ms | 307.13ms | 334.15ms | 346.91ms | 10000 |  10000  |   0    |    2000     |
|    greatws       | 28730 | 30ns | 59.37ms | 347.15ms | 60ns | 90ns  | 292.40ms | 312.96ms | 339.64ms | 348.06ms | 10000 |  10000  |   0    |    2000     |
|  greatws_event   | 30554 | 30ns | 53.65ms | 324.72ms | 40ns | 60ns  | 262.94ms | 284.81ms | 317.90ms | 327.28ms | 10000 |  10000  |   0    |    2000     |

20250808 09:59.58.974 [BenchEcho] Report

|    Framework     |  TPS   |  EER   |   Min   |   Avg   |   Max    |  TP50   |  TP75   |  TP90   |  TP95   |   TP99   |  Used  |  Total  | Success | Failed | Conns | Concurrency | Payload | CPU Min | CPU Avg | CPU Max | MEM Min | MEM Avg | MEM Max |
|     ---          |  ---   |  ---   |   ---   |   ---   |   ---    |   ---   |   ---   |   ---   |   ---   |   ---    |  ---   |   ---   |   ---   |  ---   |  ---  |     ---     |   ---   |   ---   |   ---   |   ---   |   ---   |   ---   |   ---   |
|    snapws        | 252100 | 350.48 | 29.48us | 39.55ms | 139.07ms | 37.72ms | 41.96ms | 49.00ms | 50.71ms | 53.89ms  | 7.93s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 711.43  | 719.30  | 728.59  | 278.03M | 279.03M | 281.91M |
|   fasthttp       | 251615 | 368.23 | 29.00us | 39.63ms | 108.67ms | 37.81ms | 42.65ms | 48.93ms | 51.46ms | 56.97ms  | 7.95s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 652.78  | 683.32  | 703.61  | 257.05M | 264.06M | 273.17M |
|    gobwas        | 196049 | 206.45 | 42.85us | 50.88ms | 287.90ms | 45.22ms | 53.75ms | 66.22ms | 76.88ms | 155.61ms | 10.20s | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 944.22  | 949.63  | 956.39  | 377.75M | 378.17M | 378.38M |
|    gorilla       | 250989 | 368.14 | 30.32us | 39.72ms | 122.56ms | 37.62ms | 43.16ms | 49.38ms | 51.90ms | 59.57ms  | 7.97s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 650.70  | 681.77  | 700.74  | 254.80M | 262.11M | 270.80M |
|     gws          | 259100 | 369.36 | 32.06us | 38.48ms | 124.98ms | 36.40ms | 40.86ms | 48.03ms | 50.14ms | 58.58ms  | 7.72s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 659.61  | 701.48  | 717.80  | 177.92M | 191.65M | 204.29M |
|    gws_std       | 261237 | 396.02 | 28.95us | 38.16ms | 115.04ms | 36.03ms | 42.01ms | 47.13ms | 49.33ms | 60.22ms  | 7.66s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 536.72  | 659.66  | 695.81  | 160.58M | 185.65M | 196.71M |
|  nbio_blocking   | 260915 | 386.97 | 29.47us | 38.22ms | 107.40ms | 36.33ms | 40.90ms | 47.41ms | 49.09ms | 53.81ms  | 7.67s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 571.29  | 674.25  | 697.91  | 176.18M | 185.95M | 191.18M |
|   nbio_mixed     | 260053 | 388.00 | 30.79us | 38.33ms | 137.28ms | 36.36ms | 40.57ms | 47.72ms | 49.47ms | 54.83ms  | 7.69s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 592.82  | 670.24  | 697.73  | 389.62M | 431.06M | 448.12M |
| nbio_nonblocking | 228171 | 312.72 | 38.48us | 43.69ms | 147.14ms | 41.54ms | 48.26ms | 55.76ms | 60.64ms | 74.54ms  | 8.77s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 648.72  | 729.64  | 774.82  | 63.66M  | 66.62M  | 69.14M  |
|   nbio_std       | 259608 | 387.71 | 27.39us | 38.41ms | 135.19ms | 36.41ms | 40.94ms | 47.74ms | 49.67ms | 55.57ms  | 7.70s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 598.48  | 669.59  | 693.10  | 179.32M | 179.79M | 181.09M |
|    nettyws       | 258237 | 383.30 | 28.80us | 38.62ms | 177.14ms | 36.48ms | 41.07ms | 47.96ms | 50.00ms | 61.69ms  | 7.74s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 620.49  | 673.71  | 691.42  | 166.73M | 168.23M | 171.48M |
|    nhooyr        | 207260 | 242.40 | 41.15us | 48.09ms | 171.43ms | 46.00ms | 51.21ms | 57.77ms | 60.25ms | 69.44ms  | 9.65s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 766.39  | 855.02  | 879.71  | 363.49M | 365.70M | 368.74M |
|    quickws       | 264971 | 412.55 | 27.69us | 37.63ms | 82.07ms  | 35.85ms | 41.12ms | 46.60ms | 48.17ms | 51.42ms  | 7.55s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 514.48  | 642.28  | 675.75  | 128.24M | 140.23M | 145.24M |
|    greatws       | 237711 | 356.60 | 57.77us | 41.91ms | 130.54ms | 40.00ms | 46.40ms | 51.70ms | 54.78ms | 61.09ms  | 8.41s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 456.67  | 666.60  | 710.82  | 53.04M  | 56.48M  | 58.09M  |
|  greatws_event   | 255706 | 377.00 | 40.86us | 38.99ms | 133.43ms | 37.04ms | 42.30ms | 48.07ms | 50.10ms | 54.79ms  | 7.82s  | 2000000 | 2000000 |   0    | 10000 |    10000    |  1024   | 667.73  | 678.27  | 688.27  | 51.08M  | 54.35M  | 55.45M  |


20250808 09:59.58.987 [BenchRate] Report

|    Framework     | Duration | EchoEER | Packet Sent | Bytes Sent | Packet Recv | Bytes Recv | Conns | SendRate | Payload | CPU Min | CPU Avg | CPU Max | MEM Min | MEM Avg | MEM Max |
|     ---          |   ---    |   ---   |     ---     |    ---     |     ---     |    ---     |  ---  |   ---    |   ---   |   ---   |   ---   |   ---   |   ---   |   ---   |   ---   |
|    snapws        |  10.00s  | 717.48  |   5978230   |   5.70G    |   5647701   |   5.39G    | 10000 |   200    |  1024   | 327.83  | 787.16  | 972.40  | 278.03M | 280.84M | 282.28M |
|   fasthttp       |  10.00s  | 760.64  |   6071240   |   5.79G    |   5747045   |   5.48G    | 10000 |   200    |  1024   | 329.73  | 755.55  | 912.69  | 257.05M | 283.51M | 318.64M |
|    gobwas        |  10.00s  | 399.22  |   4241420   |   4.04G    |   3895531   |   3.72G    | 10000 |   200    |  1024   | 425.66  | 975.77  | 1080.85 | 377.75M | 391.24M | 428.14M |
|    gorilla       |  10.00s  | 760.38  |   6081960   |   5.80G    |   5739793   |   5.47G    | 10000 |   200    |  1024   | 248.92  | 754.86  | 949.73  | 254.80M | 279.21M | 301.18M |
|     gws          |  10.00s  | 716.20  |   6110240   |   5.83G    |   5759645   |   5.49G    | 10000 |   200    |  1024   | 495.86  | 804.20  | 930.27  | 177.92M | 206.19M | 219.54M |
|    gws_std       |  10.00s  | 752.95  |   5954670   |   5.68G    |   5641208   |   5.38G    | 10000 |   200    |  1024   | 505.84  | 749.22  | 884.70  | 160.58M | 194.19M | 201.96M |
|  nbio_blocking   |  10.00s  | 726.21  |   5929640   |   5.65G    |   5592390   |   5.33G    | 10000 |   200    |  1024   | 571.29  | 770.07  | 888.73  | 176.18M | 192.07M | 198.05M |
|   nbio_mixed     |  10.00s  | 739.53  |   5929760   |   5.66G    |   5613145   |   5.35G    | 10000 |   200    |  1024   | 559.80  | 759.02  | 872.70  | 389.62M | 498.75M | 558.32M |
| nbio_nonblocking |  10.00s  | 687.16  |   5662620   |   5.40G    |   5355830   |   5.11G    | 10000 |   200    |  1024   | 355.79  | 779.42  | 929.76  | 63.66M  | 103.71M | 175.78M |
|   nbio_std       |  10.00s  | 746.67  |   6024550   |   5.75G    |   5706065   |   5.44G    | 10000 |   200    |  1024   | 550.82  | 764.20  | 901.79  | 179.32M | 185.27M | 190.05M |
|    nettyws       |  10.00s  | 738.37  |   5936270   |   5.66G    |   5606010   |   5.35G    | 10000 |   200    |  1024   | 489.47  | 759.24  | 907.75  | 166.73M | 170.29M | 171.73M |
|    nhooyr        |  10.00s  | 482.98  |   4865560   |   4.64G    |   4493755   |   4.29G    | 10000 |   200    |  1024   | 594.85  | 930.43  | 1092.93 | 363.49M | 367.41M | 369.11M |
|    quickws       |  10.00s  | 739.67  |   6033120   |   5.75G    |   5710266   |   5.45G    | 10000 |   200    |  1024   | 514.48  | 772.00  | 907.72  | 114.14M | 128.02M | 145.24M |
|    greatws       |  10.00s  | 663.75  |   5516010   |   5.26G    |   5194435   |   4.95G    | 10000 |   200    |  1024   | 456.67  | 782.59  | 924.55  | 53.04M  | 64.17M  | 73.54M  |
|  greatws_event   |  10.00s  | 695.37  |   5714660   |   5.45G    |   5398832   |   5.15G    | 10000 |   200    |  1024   | 352.75  | 776.40  | 949.51  | 51.08M  | 53.60M  | 55.45M  |


## API Reference

https://pkg.go.dev/github.com/Atheer-Ganayem/SnapWS
