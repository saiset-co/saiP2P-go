package socket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
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

type Status string

const StatusOK Status = "ok"
const StatusConnecting Status = "connecting"
const StatusClosing Status = "closing"
const StatusIdle Status = "idle"

type SocketConn struct {
	name string

	conn net.Conn

	pingSent     time.Time
	pongReceived time.Time

	lastMsgReceived time.Time

	out  chan SocketMessage
	stop func()

	SentCount int

	readBuffer *bytes.Buffer

	readingErrorCount int

	status Status
}

func NewServer(port string, v bool) *Server {
	return &Server{
		fanIn:  make(chan SocketMessage, 100_000),
		port:   port,
		Logger: &simpleLogger{pref: "[srv] ", verbose: v},
		ConnMap: ConnMap{
			mu:             sync.Mutex{},
			connections:    map[int64]*SocketConn{},
			nextConnNumber: &atomic.Int64{},
			nums:           []int64{},
		},
		pingInterval:         time.Second * 5,
		maxNoPingPongTimeout: time.Second * 10,
	}
}

func (r *Server) DropConnections() {
	r.ConnMap.mu.Lock()
	for key, _ := range r.ConnMap.connections {
		r.Logger.Info(fmt.Sprintf("server stoped. reset conn %d ", key))
		r.ConnMap.connections[key].stop()
	}
	r.ConnMap.mu.Unlock()
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

func (r *Server) Direct(conn int64, m *SocketMessage) {
	r.ConnMap.mu.Lock()
	defer r.ConnMap.mu.Unlock()

	c, exist := r.ConnMap.connections[conn]
	if exist {
		r.Logger.Error(errors.New(".Direct conn not exist!"), "want conn: "+strconv.Itoa(int(conn)))
		return
	}

	c.out <- *m
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

	socket := &SocketConn{
		name:            "", // set later with greeting mesage
		conn:            conn,
		pingSent:        time.Now(),
		pongReceived:    time.Now(),
		lastMsgReceived: time.Now(),
		out:             out,
		stop:            cancel,
		SentCount:       0,
		readBuffer:      readBuffer,
		status:          StatusOK,
	}

	num := r.ConnMap.AddConn(socket)

	defer func() {
		r.ConnMap.DropConn(num)
	}()

	r.Logger.Info("new connection with : " + remote)
	defer r.Logger.Info("connection dropped : " + remote)

	sendStopped := make(chan struct{})
	defer close(sendStopped)
	// send
	go func() {
		defer func() {
			sendStopped <- struct{}{}
		}()
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

	readStopped := make(chan struct{})
	defer close(readStopped)

	// receive
	go func() {
		defer func() {
			readStopped <- struct{}{}
		}()
		for {
			select {
			case <-ctx.Done():
				return
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
						r.Logger.Info(m.Method + " from " + remote)
						r.ConnMap.mu.Lock()
						r.ConnMap.connections[num].name = string(m.Data)
						r.ConnMap.mu.Unlock()
					default:
						r.Logger.Info(m.Method + " from " + remote)
						m.Conn = num
						r.fanIn <- m
					}
				}
			}
		}
	}()

	pingStopped := make(chan struct{})
	defer close(pingStopped)
	// ping
	go func() {
		defer func() {
			pingStopped <- struct{}{}
		}()
		t := time.NewTicker(r.pingInterval)
		defer t.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case _ = <-t.C:
				r.ConnMap.Update(num, func(c *SocketConn) {
					c.pingSent = time.Now()
				})

				out <- NewPingMessage()
			}
		}
	}()

	<-readStopped
	<-sendStopped
	<-pingStopped
}
