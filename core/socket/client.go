package socket

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"time"
)

type Client struct {
	host         string
	connecting   *atomic.Bool
	Logger       Logger
	size         int
	pingInterval time.Duration
	SocketConn
}

func NewClient(host string) *Client {
	return &Client{
		SocketConn: SocketConn{
			conn:            nil,
			pingSent:        time.Time{},
			pongReceived:    time.Time{},
			lastMsgReceived: time.Time{},
			in:              make(chan SocketMessage, 100),
			out:             make(chan SocketMessage, 100),
		},
		host:         host,
		connecting:   &atomic.Bool{},
		Logger:       &simpleLogger{pref: "[cli] "},
		size:         1024 * 256,
		pingInterval: time.Second * 5,
	}
}

func (r *Client) Incoming() chan SocketMessage {
	return r.in
}

func (r *Client) Disconnect() {
	r.stop()
}

func (r *Client) Run(pctx context.Context) {

	ctx, cancel := context.WithCancel(pctx)
	r.stop = cancel

	// receive
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				messages, err := r.read()
				if err != nil {
					if errors.Is(err, ErrReadTimeout) {
						continue
					}

					r.Logger.Error(err, "client.run.read")
					continue
				}

				for _, m := range messages {
					r.Logger.Info(m.Method + " from: " + r.conn.RemoteAddr().String())

					switch m.Method {
					case "ping":
						r.out <- NewPongMessage()
					case "pong":
						r.pongReceived = time.Now()
					default:
						r.lastMsgReceived = time.Now()
						r.in <- m
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
			case m := <-r.out:
				if err := r.send(m); err != nil {
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
				r.pingSent = time.Now()
				r.out <- NewPingMessage()
			}
		}
	}()

}

func (r *Client) read() ([]SocketMessage, error) {
	r.resolveConn()
	return read(r.Logger, r.size, r.conn)
}

func (r *Client) send(m SocketMessage) error {
	r.resolveConn()
	return send(r.Logger, r.conn, m)
}

func (r *Client) resolveConn() {

	for r.connecting.Load() {
		r.Logger.Info("wait for connection")
		time.Sleep(time.Second)
	}

	if r.conn != nil {
		return
	}

	r.connecting.Store(true)
	defer r.connecting.Store(false)

	for r.conn == nil {
		if err := r.connect(); err != nil {
			r.Logger.Error(err, "not connected to: "+r.host)
			time.Sleep(time.Second)
			continue
		}
		r.Logger.Info("connected to: " + r.host)
		return
	}
}

func (r *Client) Send(m SocketMessage) {
	r.out <- m
}

func (r *Client) connect() error {

	conn, err := net.DialTimeout("tcp", r.host, time.Second*5)
	if err != nil {
		return err
	}
	r.conn = conn
	r.pingSent = time.Now()
	r.pongReceived = time.Now()
	return nil
}
