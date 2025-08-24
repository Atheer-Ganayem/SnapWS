package main

import (
	"context"
	"fmt"
	"net/http"

	snapws "github.com/Atheer-Ganayem/SnapWS"
)

var roomManager *snapws.RoomManager[string]

func main() {
	roomManager = snapws.NewRoomManager[string](nil)
	defer roomManager.Shutdown()
	roomManager.Upgrader.Use(validateQuery)

	http.HandleFunc("/ws", handleJoin)
	http.HandleFunc("/", serveHome)

	fmt.Println("Server listening on port 8080")
	http.ListenAndServe(":8080", nil)
}

func handleJoin(w http.ResponseWriter, r *http.Request) {
	roomQuery := r.URL.Query().Get("room")
	name := r.URL.Query().Get("name")

	conn, room, err := roomManager.Connect(w, r, roomQuery)
	if err != nil {
		return
	}
	defer conn.Close()

	room.BroadcastString(context.TODO(), []byte(name+" has joined the room"), conn)
	defer room.BroadcastString(context.TODO(), []byte(name+" has left the room"), conn)

	for {
		msg, err := conn.ReadString()
		if snapws.IsFatalErr(err) {
			return
		} else if err != nil {
			continue
		}

		msg = append([]byte(name+": "), msg...)
		room.BroadcastString(context.TODO(), msg)
	}
}

// This is just some dummy example.
// In real-world apps you will have authentication, which will prevent having
// duplicate usernames (or whatever your identifier is).
func validateQuery(w http.ResponseWriter, r *http.Request) error {
	room := r.URL.Query().Get("room")
	name := r.URL.Query().Get("name")

	if room == "" || name == "" {
		return snapws.NewMiddlewareErr(http.StatusBadRequest, "empty room or name")
	}

	return nil
}

func serveHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
<!DOCTYPE html>
<html>
<head><title>SnapWS Rooms</title></head>
<body>
	<h1>Room Chat</h1>
	<div>
		<input type="text" id="userInput" placeholder="Username">
		<input type="text" id="roomInput" placeholder="Room name">
		<button onclick="joinRoom()">Join Room</button>
	</div>
	<div id="chat" style="display:none">
		<div id="messages" style="height:300px;overflow-y:scroll;border:1px solid #ccc"></div>
		<input type="text" id="messageInput" placeholder="Type message...">
		<button onclick="sendMessage()">Send</button>
		<button onclick="leaveRoom()">Leave Room</button>
	</div>
	
	<script>
		let ws, currentRoom, currentUser;
		
		function joinRoom() {
			currentUser = document.getElementById('userInput').value;
			currentRoom = document.getElementById('roomInput').value;
			
			ws = new WebSocket('ws://localhost:8080/ws?name=' + currentUser + '&room=' + currentRoom);
			
			ws.onmessage = function(event) {
				const p = document.createElement('p');
				p.innerHTML = event.data
				document.getElementById('messages').appendChild(p);
			};
			
			document.getElementById('chat').style.display = 'block';
		}
		
		function sendMessage() {
			const input = document.getElementById('messageInput');
			ws.send(input.value);
			input.value = '';
		}
		
		function leaveRoom() {
			ws.close();
			document.getElementById('chat').style.display = 'none';
		}
	</script>
</body>
</html>`))
}
