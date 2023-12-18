package socket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type Server struct {
	port                 string
	Logger               Logger
	ConnMap              ConnMap
	size                 int
	pingInterval         time.Duration
	maxNoPingPongTimeout time.Duration

	fanIn chan SocketMessage
}

type SocketConn struct {
	conn net.Conn

	pingSent     time.Time
	pongReceived time.Time

	lastMsgReceived time.Time

	in   chan SocketMessage
	out  chan SocketMessage
	stop func()
}

func NewServer(port string) *Server {
	return &Server{
		fanIn:  make(chan SocketMessage, 100),
		port:   port,
		Logger: &simpleLogger{pref: "[srv] "},
		ConnMap: ConnMap{
			mu: sync.Mutex{},
			m:  map[string]*SocketConn{},
		},
		size:                 1024 * 256,
		pingInterval:         time.Second * 5,
		maxNoPingPongTimeout: time.Second * 10,
	}
}

func (r *Server) broadcastPing(ctx context.Context) {
	ticker := time.NewTicker(r.pingInterval)
	defer ticker.Stop()

	for {
		r.ConnMap.mu.Lock()
		for _, c := range r.ConnMap.m {
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
	mu sync.Mutex
	m  map[string]*SocketConn
}

func (m *ConnMap) Connections() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.m)
}

func (m *ConnMap) AddConn(c *SocketConn) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.m[c.conn.RemoteAddr().String()] = c
}

func (r *Server) Incoming() chan SocketMessage {
	return r.fanIn
}

func (r *Server) Broadcast(m *SocketMessage) {
	r.ConnMap.mu.Lock()
	defer r.ConnMap.mu.Unlock()

	for _, c := range r.ConnMap.m {
		c.out <- *m
	}
}

func (r *Server) Cleaner(ctx context.Context) {

	ticker := time.NewTicker(r.maxNoPingPongTimeout)
	defer ticker.Stop()

	for {

		r.ConnMap.mu.Lock()
		now := time.Now()
		for key, c := range r.ConnMap.m {
			// если последний понг получен более чем 10 сек назад
			if now.Unix()-c.pongReceived.Unix() > int64(r.maxNoPingPongTimeout/time.Second) {
				r.Logger.Info(fmt.Sprintf("conn no ping/pong for %d secs. reset client %s ", now.Unix()-c.pongReceived.Unix(), key))
				r.ConnMap.m[key].stop()
				delete(r.ConnMap.m, key)
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
	ln, err := net.Listen("tcp", r.port)
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
			fmt.Println(err)
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

	in := make(chan SocketMessage, 100)
	defer close(in)

	out := make(chan SocketMessage, 100)
	defer close(out)

	r.ConnMap.AddConn(&SocketConn{
		conn:            conn,
		pingSent:        time.Now(),
		pongReceived:    time.Now(),
		lastMsgReceived: time.Now(),
		in:              in,
		out:             out,
		stop:            cancel,
	})

	r.Logger.Info("new connection with : " + remote)

	defer conn.Close()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case m := <-in:
				r.fanIn <- m
			}
		}
	}()

	// send
	go func() {
		for {
			select {
			case <-ctx.Done():
				return

			case m := <-out:
				if err := send(r.Logger, conn, m); err != nil {
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
			messages, err := read(r.Logger, r.size, conn)
			if err != nil {
				if errors.Is(err, ErrReadTimeout) {
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
					v, ok := r.ConnMap.m[remote]
					if ok {
						v.pongReceived = time.Now()
						if v.pongReceived.Unix()-v.pingSent.Unix() > 5 {
							cancel()
						}
					}
					r.ConnMap.mu.Unlock()
				case "ping":
					out <- NewPongMessage()
				default:
					in <- m
				}
			}
		}
	}
}
