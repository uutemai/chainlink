package cltest

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

type EventWebsocketServer struct {
	*httptest.Server
	mutex       *sync.RWMutex // shared mutex for safe access to arrays/maps.
	t           *testing.T
	connections []*websocket.Conn
	Connected   chan struct{}
	Received    chan string
	URL         *url.URL
}

func NewEventWebsocketServer(t *testing.T) (*EventWebsocketServer, func()) {
	server := &EventWebsocketServer{
		mutex:     &sync.RWMutex{},
		t:         t,
		Connected: make(chan struct{}, 1), // have buffer of one for easier assertions after the event
		Received:  make(chan string, 100),
	}

	server.Server = httptest.NewServer(http.HandlerFunc(server.handler))
	u, err := url.Parse(server.Server.URL)
	if err != nil {
		t.Fatal("EventWebsocketServer: ", err)
	}
	u.Scheme = "ws"
	server.URL = u
	return server, func() {
		server.Close()
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (wss *EventWebsocketServer) handler(w http.ResponseWriter, r *http.Request) {
	var err error
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		wss.t.Fatal("EventWebsocketServer Upgrade: ", err)
	}

	wss.addConnection(conn)
	closeCodes := []int{websocket.CloseNormalClosure, websocket.CloseAbnormalClosure}
	for {
		_, payload, err := conn.ReadMessage() // we only read
		if websocket.IsCloseError(err, closeCodes...) {
			wss.removeConnection(conn)
			return
		}
		if err != nil {
			wss.t.Fatal("EventWebsocketServer ReadMessage: ", err)
		}

		select {
		case wss.Received <- string(payload):
		default:
		}
	}
}

func (wss *EventWebsocketServer) addConnection(conn *websocket.Conn) {
	wss.mutex.Lock()
	wss.connections = append(wss.connections, conn)
	wss.mutex.Unlock()
	select { // broadcast connected event
	case wss.Connected <- struct{}{}:
	default:
	}
}

func (wss *EventWebsocketServer) removeConnection(conn *websocket.Conn) {
	newc := []*websocket.Conn{}
	wss.mutex.Lock()
	for _, connection := range wss.connections {
		if connection != conn {
			newc = append(newc, connection)
		}
	}
	wss.connections = newc
	wss.mutex.Unlock()
}

// WriteCloseMessage tells connected clients to disconnect.
// Useful to emulate that the websocket server is shutting down without
// actually shutting down.
// This overcomes httptest.Server's inability to restart on the same URL:port.
func (wss *EventWebsocketServer) WriteCloseMessage() {
	wss.mutex.RLock()
	for _, connection := range wss.connections {
		err := connection.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			wss.t.Error(err)
		}
	}
	wss.mutex.RUnlock()
}