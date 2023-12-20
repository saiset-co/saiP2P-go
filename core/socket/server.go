package socket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
)

type Server struct {
	port                 string
	Logger               Logger
	ConnMap              ConnMap
	pingInterval         time.Duration
	maxNoPingPongTimeout time.Duration

	fanIn chan SocketMessage
}

type SocketConn struct {
	name string

	conn net.Conn

	pingSent     time.Time
	pongReceived time.Time

	lastMsgReceived time.Time

	out  chan SocketMessage
	stop func()

	connecting *atomic.Bool

	SentCount int

	readBuffer *bytes.Buffer
}

func NewServer(port string) *Server {
	return &Server{
		fanIn:  make(chan SocketMessage, 100),
		port:   port,
		Logger: &simpleLogger{pref: "[srv] "},
		ConnMap: ConnMap{
			mu:             sync.Mutex{},
			connections:    map[int64]*SocketConn{},
			nextConnNumber: &atomic.Int64{},
		},
		pingInterval:         time.Second * 5,
		maxNoPingPongTimeout: time.Second * 10,
	}
}

func (r *Server) broadcastPing(ctx context.Context) {
	ticker := time.NewTicker(r.pingInterval)
	defer ticker.Stop()

	for {
		r.ConnMap.mu.Lock()
		for _, c := range r.ConnMap.connections {
			c.pingSent = time.Now()
			c.out <- NewPingMessage()
		}
		r.ConnMap.mu.Unlock()

		select {
		case <-ticker.C:

		}
	}
}

type ConnMap struct {
	mu             sync.Mutex
	connections    map[int64]*SocketConn
	nextConnNumber *atomic.Int64

	lastUsed int
	nums     []int64
}

func (m *ConnMap) Update(num int64, fn func(c *SocketConn)) {
	m.mu.Lock()
	fn(m.connections[num])
	m.mu.Unlock()
}

func (m *ConnMap) Conn(num int64) net.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.connections[num].conn
}

func (m *ConnMap) ReadBuffer(num int64) *bytes.Buffer {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.connections[num].readBuffer
}

func (m *ConnMap) Connecting(num int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.connections[num].connecting.Load()
}

func (m *ConnMap) Connections() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.connections)
}

func (m *ConnMap) Next() int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.nums) == 0 {
		return 0
	}
	m.lastUsed++
	if m.lastUsed >= len(m.nums) {
		m.lastUsed = 0
	}
	return m.nums[m.lastUsed]
}

func (m *ConnMap) Send(num int64, msg SocketMessage) {
	m.mu.Lock()
	out := m.connections[num].out
	m.mu.Unlock()

	out <- msg
}

func (m *ConnMap) AddConn(c *SocketConn) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	num := m.nextConnNumber.Load()
	m.connections[num] = c
	m.nextConnNumber.Add(1)
	m.nums = append(m.nums, num)
	return num
}

func (m *ConnMap) DropConn(num int64) int64 {
	m.mu.Lock()
	defer m.mu.Unlock()

	_ = m.connections[num].conn.Close()
	m.nextConnNumber.Add(-1)

	resolvedNums := []int64{}
	for _, n := range m.nums {
		if n == num {
			continue
		}
		resolvedNums = append(resolvedNums, n)
	}

	m.connections[num].readBuffer.Reset()
	close(m.connections[num].out)
	m.nums = resolvedNums
	m.lastUsed = 0

	return num
}

func (r *Server) Incoming() chan SocketMessage {
	return r.fanIn
}

func (r *Server) Broadcast(m *SocketMessage) {
	r.ConnMap.mu.Lock()
	defer r.ConnMap.mu.Unlock()

	clients := map[string]struct{}{}
	for _, c := range r.ConnMap.connections {

		_, exist := clients[c.name]
		if exist {
			continue
		}
		clients[c.name] = struct{}{}

		c.out <- *m
	}
}

func (r *Server) Cleaner(ctx context.Context) {

	ticker := time.NewTicker(r.maxNoPingPongTimeout)
	defer ticker.Stop()

	for {

		r.ConnMap.mu.Lock()
		now := time.Now()
		for key, c := range r.ConnMap.connections {
			// если последний понг получен более чем 10 сек назад
			if now.Unix()-c.pongReceived.Unix() > int64(r.maxNoPingPongTimeout/time.Second) {
				r.Logger.Info(fmt.Sprintf("conn no ping/pong for %d secs. reset client %d ", now.Unix()-c.pongReceived.Unix(), key))
				r.ConnMap.connections[key].stop()
				delete(r.ConnMap.connections, key)
			}
		}
		r.ConnMap.mu.Unlock()

		select {
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
		}
	}
}

func (r *Server) Listen(ctx context.Context) error {
	ln, err := net.Listen(SocketType, r.port)
	if err != nil {
		return err
	}

	r.Logger.Info("listen in tcp port: " + r.port)
	go r.broadcastPing(ctx)
	go r.Cleaner(ctx)

	// Accept incoming connections and handle them
	for {
		conn, err := ln.Accept()
		if err != nil {
			r.Logger.Error(err, "failed to create new connection")
			continue
		}

		// Handle the connection in a new goroutine
		go r.handleConnection(conn)
	}
}

func (r *Server) handleConnection(conn net.Conn) {

	ctx, cancel := context.WithCancel(context.Background())

	remote := conn.RemoteAddr().String()
	r.Logger.Info("new client: " + remote)

	out := make(chan SocketMessage, 100) // closes by drop conn

	readBuffer := bytes.NewBuffer([]byte{})

	num := r.ConnMap.AddConn(&SocketConn{
		name:            "", // set later with greeting mesage
		conn:            conn,
		pingSent:        time.Now(),
		pongReceived:    time.Now(),
		lastMsgReceived: time.Now(),
		out:             out,
		stop:            cancel,
		connecting:      nil, // server does not need that flag
		SentCount:       0,
		readBuffer:      readBuffer,
	})

	defer r.ConnMap.DropConn(num)

	r.Logger.Info("new connection with : " + remote)

	defer conn.Close()

	// send
	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case m := <-out:
				if err := send(r.Logger, conn, m); err != nil {

					if strings.Contains(err.Error(), "broken pipe") {
						r.Logger.Error(err, "probably client disconnected")
						time.Sleep(time.Millisecond * 200)
						continue
					}

					r.Logger.Error(err, "handleConnection.send")
					continue
				}
			}
		}
	}()

	// receive
	for {
		select {
		case <-ctx.Done():
			break
		default:
			messages, err := read(readBuffer, conn)
			if err != nil {
				if errors.Is(err, io.EOF) {
					r.Logger.Error(err, "probably client disconnected")
					time.Sleep(time.Millisecond * 200)
					continue
				}
				r.Logger.Error(err, "connection.read")
				continue
			}

			for _, m := range messages {
				r.Logger.Info(m.Method + " from " + remote)

				switch m.Method {
				case "pong":
					r.ConnMap.mu.Lock()
					v, ok := r.ConnMap.connections[num]
					if ok {
						v.pongReceived = time.Now()
						if v.pongReceived.Unix()-v.pingSent.Unix() > 5 {
							cancel()
						}
					}
					r.ConnMap.mu.Unlock()
				case "ping":
					out <- NewPongMessage()
				case "greeting":
					r.ConnMap.mu.Lock()
					r.ConnMap.connections[num].name = string(m.Data)
					r.ConnMap.mu.Unlock()
				default:
					r.fanIn <- m
				}
			}
		}
	}
}
