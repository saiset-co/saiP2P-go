package socket

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	host         string
	Logger       Logger
	pingInterval time.Duration
	name         string
	ConnMap      ConnMap
	connections  int
	fanIn        chan SocketMessage
}

func NewClient(host, name string, connections int) *Client {
	return &Client{
		ConnMap: ConnMap{
			mu:             sync.Mutex{},
			connections:    map[int64]*SocketConn{},
			nextConnNumber: &atomic.Int64{},
			lastUsed:       0,
			nums:           []int64{},
		},
		host:         host,
		Logger:       &simpleLogger{pref: "[cli] "},
		pingInterval: time.Second * 5,
		name:         name,
		connections:  connections,
		fanIn:        make(chan SocketMessage, 100),
	}
}

func (r *Client) Incoming() chan SocketMessage {
	return r.fanIn
}

func (r *Client) Disconnect() {

	r.ConnMap.mu.Lock()
	defer r.ConnMap.mu.Unlock()

	for num, conn := range r.ConnMap.connections {
		conn.stop() // stops routines
		go func(num int64) {
			time.Sleep(time.Second * 3)
			r.ConnMap.DropConn(num) // closes connection + clears socket
		}(num)
	}
}

func (r *Client) Run(ctx context.Context) {
	wg := sync.WaitGroup{}
	wg.Add(r.connections)

	for i := 0; i < r.connections; i++ {
		go func() {
			r.run(ctx)
			wg.Done()
		}()
	}
	wg.Wait()
}

func (r *Client) run(pctx context.Context) {

	ctx, cancel := context.WithCancel(pctx)

	out := make(chan SocketMessage, 100)
	num := r.ConnMap.AddConn(&SocketConn{
		name:            r.name,
		conn:            nil,
		pingSent:        time.Time{},
		pongReceived:    time.Time{},
		lastMsgReceived: time.Time{},
		out:             out,
		stop:            cancel,
		connecting:      &atomic.Bool{},
		readBuffer:      bytes.NewBuffer([]byte{}),
	})

	r.resolveConn(num)

	// receive
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				messages, err := r.read(num)
				if err != nil {
					r.Logger.Error(err, "client.run.read")
					continue
				}

				for _, m := range messages {
					r.Logger.Info(m.Method + " from: " + strconv.Itoa(int(num)))

					switch m.Method {
					case "ping":
						r.Send(NewPongMessage())
					case "pong":
						r.ConnMap.Update(num, func(c *SocketConn) {
							c.pongReceived = time.Now()
						})
					default:
						r.ConnMap.Update(num, func(c *SocketConn) {
							c.lastMsgReceived = time.Now()
						})
						r.fanIn <- m
					}
				}
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
				if err := r.send(num, m); err != nil {
					r.Logger.Error(err, "tcpRepeater.send.failed")
				}
			}
		}
	}()

	// ping
	go func() {
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

				r.Send(NewPingMessage())
			}
		}
	}()

}

func (r *Client) read(num int64) ([]SocketMessage, error) {
	r.resolveConn(num)
	conn := r.ConnMap.Conn(num)
	readBuffer := r.ConnMap.ReadBuffer(num)
	return read(readBuffer, conn)
}

func (r *Client) send(num int64, m SocketMessage) error {
	r.resolveConn(num)
	r.ConnMap.Update(num, func(c *SocketConn) {
		c.SentCount++
	})
	return send(r.Logger, r.ConnMap.Conn(num), m)
}

func (r *Client) resolveConn(num int64) {

	for r.ConnMap.Connecting(num) {
		r.Logger.Info("wait for connection")
		time.Sleep(time.Millisecond * 100)
	}

	if r.ConnMap.Conn(num) != nil {
		return
	}

	r.ConnMap.Update(num, func(c *SocketConn) {
		c.connecting.Store(true)
	})
	defer r.ConnMap.Update(num, func(c *SocketConn) {
		c.connecting.Store(false)
	})

	for r.ConnMap.Conn(num) == nil {
		if err := r.connect(num); err != nil {
			r.Logger.Error(err, "not connected to: "+r.host)
			time.Sleep(time.Second)
			continue
		}
		r.Logger.Info("connected to: " + r.host)
		return
	}
}

func (r *Client) Send(m SocketMessage) {
	num := r.ConnMap.Next()
	r.resolveConn(num)
	r.ConnMap.Send(num, m)
}

func (r *Client) connect(num int64) error {

	conn, err := net.DialTimeout(SocketType, r.host, time.Second*5)
	if err != nil {
		return err
	}

	r.ConnMap.Update(num, func(c *SocketConn) {
		c.conn = conn
		c.pingSent = time.Now()
		c.pongReceived = time.Now()
	})

	if err := send(r.Logger, conn, NewGreetingMessage(r.name)); err != nil {
		return err
	}

	return nil
}
