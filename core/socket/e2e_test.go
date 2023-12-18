package socket

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestP2P(t *testing.T) {
	t.Run("normalin", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3337")
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3337")
		cli.Run(ctx)

		serverIn := server.Incoming()

		count := 3

		go func() {
			boba := count
			for boba > 0 {
				cli.Send(SocketMessage{Method: "test", Data: []byte("a\nf\nhjh")})
				time.Sleep(time.Millisecond * 100)
				boba--
			}
		}()

		for {
			select {
			case m := <-serverIn:
				if m.Method == "test" {
					count--
				}
				if count == 0 {
					return
				}
			}
		}
	})
	t.Run("srv broadcast", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3334")
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3334")
		cli.Run(ctx)

		time.Sleep(time.Second)
		mCount := 10
		for mCount > 0 {
			server.Broadcast(&SocketMessage{Method: "boba"})
			mCount--
		}

		clin := cli.Incoming()

		received := 10
		for received > 0 {
			select {
			case _ = <-clin:
				received--
			}
		}
	})

	t.Run("client disconnected", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3334")
		server.pingInterval = time.Second
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3334")
		cli.Run(ctx)

		time.Sleep(time.Second)
		cli.Disconnect()
		for server.ConnMap.Connections() != 0 {

		}
	})

	t.Run("server slow", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3334")
		server.pingInterval = time.Second

		cli := NewClient("localhost:3334")
		cli.Run(ctx)

		count := 10
		go func() {
			boba := count
			for boba > 0 {
				cli.Send(SocketMessage{Method: "test", Data: []byte("a\nf\nhjh")})
				time.Sleep(time.Millisecond * 100)
				boba--
			}
		}()

		time.Sleep(time.Second * 4)

		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()

		serverIn := server.Incoming()

		for {
			select {
			case m := <-serverIn:
				if m.Method == "test" {
					count--
				}
				if count == 0 {
					return
				}
			}
		}
	})
}
