package HTTPServer

import (
	"bytes"
	"github.com/gorilla/websocket"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/Djoulzy/Polycom/CLog"
	"github.com/Djoulzy/Polycom/Hub"
	"github.com/Djoulzy/Polycom/monitoring"
)

const (
	writeWait      = 5 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var (
	Newline = []byte{'\r', '\n'}
	Space   = []byte{' '}
)

type Manager struct {
	Httpaddr         string
	ServerName       string
	Upgrader         websocket.Upgrader
	Hub              *Hub.Hub
	ReadBufferSize   int
	WriteBufferSize  int
	HandshakeTimeout int
}

func (m *Manager) Connect() *websocket.Conn {
	u := url.URL{Scheme: "ws", Host: m.Httpaddr, Path: "/ws"}
	clog.Info("HTTPServer", "Connect", "Connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		clog.Error("HTTPServer", "Connect", "%s", err)
		return nil
	}

	return conn
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (m *Manager) Reader(c *Hub.Client) {
	defer func() {
		c.Conn.(*websocket.Conn).Close()
	}()

	conn := c.Conn.(*websocket.Conn)
	conn.SetReadLimit(maxMessageSize)
	conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		// c.ReadProtect.Lock()
		conn.SetReadDeadline(time.Now().Add(pongWait))
		// c.ReadProtect.Unlock()
		clog.Debug("HTTPServer", "Reader", "PONG! from %s", c.Name)
		return nil
	})
	for {
		// c.ReadProtect.Lock()
		// messType, message, err := conn.ReadMessage()
		// c.ReadProtect.Unlock()
		_, message, err := conn.ReadMessage()
		// clog.Debug("HTTPServer", "Writer", "Read from Client %s [%s]: %s", c.Name, c.ID, message)
		if err != nil {
			// clog.Error("HTTPServer", "Writer", "Type: %d, error: %v", messType, err)
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway) {
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, Newline, Space, -1))
		// mess := Hub.NewMessage(c.CType, c, message)
		// c.Hub.Action <- mess
		go c.CallToAction(c, message)
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
func (m *Manager) _write(ws *websocket.Conn, mt int, message []byte) error {
	ws.SetWriteDeadline(time.Now().Add(writeWait))
	return ws.WriteMessage(mt, message)
}

func (m *Manager) Writer(c *Hub.Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.Conn.(*websocket.Conn).Close()
	}()

	conn := c.Conn.(*websocket.Conn)
	for {
		select {
		case message, ok := <-c.Send:
			if !ok {
				clog.Warn("HTTPServer", "Writer", "Error: %s", ok)
				cm := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Disconnected")
				if err := m._write(conn, websocket.CloseMessage, cm); err != nil {
					clog.Error("HTTPServer", "_close", "Cannot write CloseMessage to %s", c.Name)
				}
				return
			}
			// clog.Debug("HTTPServer", "Writer", "Sending: %s", message)
			if err := m._write(conn, websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			clog.Debug("HTTPServer", "Writer", "Client %s Ping!", c.Name)
			if err := m._write(conn, websocket.PingMessage, []byte{}); err != nil {
				return
			}
		case <-c.Quit:
			return
		}
	}
}

func (m *Manager) statusPage(w http.ResponseWriter, r *http.Request) {
	var data = struct {
		Host  string
		Nb    int
		Users map[string]*Hub.Client
		Stats string
	}{
		m.Httpaddr,
		len(m.Hub.Users),
		m.Hub.Users,
		monitoring.MachineLoad.String(),
	}

	homeTempl, err := template.ParseFiles("status.html")
	if err != nil {
		clog.Error("HTTPServer", "statusPage", "%s", err)
		return
	}
	homeTempl.Execute(w, &data)
}

func (m *Manager) testPage(w http.ResponseWriter, r *http.Request) {
	var data = struct {
		Host string
	}{
		m.Httpaddr,
	}

	homeTempl, err := template.ParseFiles("client.html")
	if err != nil {
		clog.Error("HTTPServer", "testPage", "%s", err)
		return
	}
	homeTempl.Execute(w, &data)
}

// serveWs handles websocket requests from the peer.
func (m *Manager) wsConnect(w http.ResponseWriter, r *http.Request, cta Hub.CallToAction) {
	httpconn, err := m.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		clog.Error("HTTPServer", "wsConnect", "%s", err)
		return
	}
	name := r.Header["Sec-Websocket-Key"][0]

	var ua string
	if len(r.Header["User-Agent"]) > 0 {
		ua = r.Header["User-Agent"][0]
	} else {
		ua = "n/a"
	}

	if m.Hub.UserExists(name, Hub.ClientUser) {
		clog.Warn("HTTPServer", "wsConnect", "Client %s already exists ... Refusing connection", name)
		return
	}

	client := &Hub.Client{Hub: m.Hub, Conn: httpconn, Quit: make(chan bool),
		CType: Hub.ClientUndefined, Send: make(chan []byte, 256), CallToAction: cta, Addr: httpconn.RemoteAddr().String(),
		Identified: false, Name: name, Content_id: 0, Front_id: "", App_id: "", Country: "", User_agent: ua, Mode: Hub.ReadWrite}
	m.Hub.Register <- client
	go m.Writer(client)
	m.Reader(client)
	m.Hub.Unregister <- client
}

func (m *Manager) Start(conf *Manager, cta Hub.CallToAction) {
	m = conf
	m.Upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
		ReadBufferSize:   m.ReadBufferSize,
		WriteBufferSize:  m.WriteBufferSize,
		HandshakeTimeout: time.Duration(m.HandshakeTimeout) * time.Second,
	} // use default options

	http.HandleFunc("/test", m.testPage)
	http.HandleFunc("/status", m.statusPage)
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { m.wsConnect(w, r, cta) })

	err := http.ListenAndServe(m.Httpaddr, nil)
	if err != nil {
		log.Fatal("HTTPServer: ", err)
	}
}