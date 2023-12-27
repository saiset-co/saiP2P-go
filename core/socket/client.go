package socket

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	host                    string
	Logger                  Logger
	pingInterval            time.Duration
	maxNoPingPongTimeout    time.Duration
	connectionMakerInterval time.Duration
	name                    string
	ConnMap                 ConnMap
	connections             int
	fanIn                   chan SocketMessage
}

func NewClient(host, name string, connections int, v bool) *Client {
	return &Client{
		ConnMap: ConnMap{
			mu:             sync.Mutex{},
			connections:    map[int64]*SocketConn{},
			nextConnNumber: &atomic.Int64{},
			lastUsed:       0,
			nums:           []int64{},
		},
		host:                    host,
		Logger:                  &simpleLogger{pref: "[cli] ", verbose: v},
		pingInterval:            time.Second * 5,
		maxNoPingPongTimeout:    time.Second * 10,
		connectionMakerInterval: time.Second * 10,
		name:                    name,
		connections:             connections,
		fanIn:                   make(chan SocketMessage, 100),
	}
}

func (r *Client) Incoming() chan SocketMessage {
	return r.fanIn
}

func (r *Client) Disconnect() {

	r.ConnMap.mu.Lock()
	defer r.ConnMap.mu.Unlock()

	for _, conn := range r.ConnMap.connections {
		conn.stop()
	}
}

func (r *Client) Run(ctx context.Context) {
	go r.Cleaner(ctx)
	go r.Maker(ctx)
}

func (r *Client) run(pctx context.Context) {

	ctx, cancel := context.WithCancel(pctx)

	out := make(chan SocketMessage, 100)
	num := r.ConnMap.AddConn(&SocketConn{
		name:              r.name,
		conn:              nil,
		pingSent:          time.Time{},
		pongReceived:      time.Time{},
		lastMsgReceived:   time.Time{},
		out:               out,
		stop:              cancel,
		readBuffer:        bytes.NewBuffer([]byte{}),
		readingErrorCount: 0,
		status:            StatusIdle,
	})

	r.Logger.Info("socket open " + strconv.Itoa(int(num)))
	defer func() {
		r.Logger.Info("disconnected from server " + strconv.Itoa(int(num)))
	}()

	defer r.ConnMap.DropConn(num)

	r.resolveConn(num)

	receiveStopped := make(chan struct{})
	defer close(receiveStopped)
	// receive
	go func() {
		defer func() {
			receiveStopped <- struct{}{}
		}()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				messages, err := r.read(num)
				if err != nil {
					r.ConnMap.Update(num, func(c *SocketConn) {
						c.readingErrorCount++
					})

					if r.ConnMap.ReadErrorCount(num) > 5 {
						r.ConnMap.Update(num, func(c *SocketConn) {
							c.status = StatusClosing
						})
						cancel()

						return
					}

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
				if err := r.send(num, m); err != nil {
					r.Logger.Error(err, "tcpRepeater.send.failed")
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

				r.Send(NewPingMessage())
			}
		}
	}()

	<-sendStopped
	<-receiveStopped
	<-pingStopped
}

func (r *Client) read(num int64) ([]SocketMessage, error) {
	if !r.ConnMap.ConnExist(num) {
		return nil, errors.New("conn not exit")
	}
	conn := r.ConnMap.Conn(num)
	readBuffer := r.ConnMap.ReadBuffer(num)
	return read(readBuffer, conn)
}

func (r *Client) send(num int64, m SocketMessage) error {

	if !r.ConnMap.ConnExist(num) {
		return errors.New("conn not exit")
	}

	r.ConnMap.Update(num, func(c *SocketConn) {
		c.SentCount++
	})
	return send(r.Logger, r.ConnMap.Conn(num), m)
}

func (r *Client) resolveConn(num int64) {

	switch r.ConnMap.Status(num) {
	case StatusOK:
		return
	case StatusClosing:
		return
	case StatusConnecting:
		for r.ConnMap.Status(num) == StatusConnecting {
			r.Logger.Info("wait for connection")
			time.Sleep(time.Millisecond * 100)
		}
		return
	case StatusIdle:
		r.ConnMap.Update(num, func(c *SocketConn) {
			c.status = StatusConnecting
		})
	}

	for {

		status := r.ConnMap.Status(num)
		if status == StatusOK {
			return
		}

		if err := r.connect(num); err != nil {

			status := r.ConnMap.Status(num)
			if status == StatusOK {
				return
			}

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
		c.status = StatusOK
	})

	if err := send(r.Logger, conn, NewGreetingMessage(r.name)); err != nil {
		return err
	}

	return nil
}

func (r *Client) Maker(ctx context.Context) {
	ticker := time.NewTicker(r.connectionMakerInterval)
	defer ticker.Stop()

	for {

		needMoreConnections := r.connections - r.ConnMap.Connections()

		for i := 0; i < needMoreConnections; i++ {
			go r.run(ctx)
		}

		select {
		case <-ticker.C:
			continue
		}
	}
}
func (r *Client) Cleaner(ctx context.Context) {

	ticker := time.NewTicker(r.maxNoPingPongTimeout)
	defer ticker.Stop()

	for {

		r.ConnMap.mu.Lock()
		now := time.Now()
		for key, c := range r.ConnMap.connections {
			if c.conn == nil {
				continue
			}
			// если последний понг получен более чем 10 сек назад
			if now.Unix()-c.pongReceived.Unix() > int64(r.maxNoPingPongTimeout/time.Second) {
				r.Logger.Info(fmt.Sprintf("conn no ping/pong for %d secs. reset client %d ", now.Unix()-c.pongReceived.Unix(), key))
				c.stop() // stops routines
				go func(num int64) {
					time.Sleep(time.Second * 3)
					r.ConnMap.DropConn(num) // closes connection + clears socket
				}(key)
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
