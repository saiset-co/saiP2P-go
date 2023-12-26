package socket

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"strings"
	"time"

	"github.com/pkg/errors"
)

const (
	ReadTimeout  = time.Minute
	WriteTimeout = time.Minute

	SocketType = "tcp"
	dimmer     = "\n"
	readSize   = 1024 * 100
)

type Logger interface {
	Info(string)
	Error(err error, s string)
}

type simpleLogger struct {
	pref    string
	verbose bool
}

func (l *simpleLogger) Info(s string) {
	if !l.verbose {
		return
	}
	println(l.pref + s)
}

func (l *simpleLogger) Error(err error, s string) {
	println(l.pref, s+": "+err.Error())
}

func NewPingMessage() SocketMessage {
	return SocketMessage{
		Method: "ping",
		Data:   nil,
	}
}

func NewGreetingMessage(name string) SocketMessage {
	return SocketMessage{
		Method: "greeting",
		Data:   []byte(name),
	}
}

func NewPongMessage() SocketMessage {
	return SocketMessage{
		Method: "pong",
		Data:   nil,
	}
}

type SocketMessage struct {
	Method string `json:"method"`
	Data   []byte `json:"data"`
}

func read(buf *bytes.Buffer, conn net.Conn) ([]SocketMessage, error) {

	data := make([]byte, readSize)
	if err := conn.SetReadDeadline(time.Now().Add(ReadTimeout)); err != nil {
		return nil, err
	}
	n, err := conn.Read(data)
	if err != nil {
		if strings.Contains(err.Error(), "i/o timeout") {
			return nil, ErrReadTimeout
		}

		return nil, errors.Wrap(err, "conn.Read")
	}
	data = data[:n]

	if buf.Len() > 0 {
		data = append(buf.Bytes(), data...)
		buf.Reset()
	}

	items := strings.Split(string(data), dimmer)

	m := make([]SocketMessage, 0, len(items))

	for _, item := range items {
		if item == "" {
			continue
		}
		var itemMessage SocketMessage
		if err := json.Unmarshal([]byte(item), &itemMessage); err != nil {
			buf.WriteString(item)
			continue
		}
		m = append(m, itemMessage)
	}

	return m, nil
}
func send(l Logger, conn net.Conn, m SocketMessage) error {

	l.Info(m.Method + " to: " + conn.RemoteAddr().String())

	if err := conn.SetWriteDeadline(time.Now().Add(WriteTimeout)); err != nil {
		return errors.Wrap(err, "send.conn.SetWriteDeadline")
	}

	b, err := json.Marshal(&m)
	if err != nil {
		return errors.Wrap(err, "send.json.Marshal")
	}

	b = append(b, []byte(dimmer)...)
	_, err = conn.Write(b)
	if err != nil {
		return errors.Wrap(err, "send.conn.Write")
	}

	return nil
}

func SendWithPool[Message any](ctx context.Context, in chan Message, count int, fn func(Message) error) {

	repeaters := make([]chan Message, count)
	for i := range repeaters {
		repeaters[i] = make(chan Message, 100)
		defer close(repeaters[i])

		go func(i int) {
			for {
				select {
				case <-ctx.Done():
					return
				case m := <-repeaters[i]:
					fn(m)
				}
			}
		}(i)
	}

	next := 0
	for {
		select {
		case <-ctx.Done():
			return
		case m := <-in:
			repeaters[next] <- m
			if next > count-1 {
				next = 0
			}
		}
	}
}
