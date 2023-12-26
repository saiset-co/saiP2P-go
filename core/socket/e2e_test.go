package socket

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestP2P(t *testing.T) {
	t.Run("normalin", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3331", true)
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3331", "c1", 1, true)
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
		server := NewServer(":3332", false)
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3332", "c1", 1, false)
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
		server := NewServer(":3333", true)
		server.pingInterval = time.Second
		server.maxNoPingPongTimeout = time.Second * 3
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		cli := NewClient("localhost:3333", "c1", 1, true)
		cli.Run(ctx)

		time.Sleep(time.Second)
		cli.Disconnect()
		for server.ConnMap.Connections() != 0 {

		}
	})

	t.Run("server slow", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3334", false)
		go func() {
			time.Sleep(time.Second * 3)
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		server.pingInterval = time.Second
		cli := NewClient("localhost:3334", "c1", 1, true)
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

	t.Run("spam", func(t *testing.T) {

		ctx, cancel := context.WithTimeout(context.TODO(), time.Second*5)
		defer cancel()

		server := NewServer(":3335", false)
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()
		server.pingInterval = time.Second

		cli := NewClient("localhost:3335", "c1", 3, false)
		cli.Run(ctx)

		messagesCount := 300_000
		go func() {
			needSend := messagesCount
			for needSend > 0 {
				cli.Send(SocketMessage{Method: "test", Data: []byte("xuy")})
				needSend--
			}
		}()

		serverIn := server.Incoming()

		received := 0
		for {
			select {
			case <-ctx.Done():
				t.Log(fmt.Sprintf("received %d/%d", received, messagesCount))
				t.FailNow()
			case m := <-serverIn:
				if m.Method == "test" {
					received++
				}
				if received == messagesCount {
					t.Log(fmt.Sprintf("received %d/%d", received, messagesCount))
					return
				}
			}
		}
	})

	t.Run("server stopped", func(t *testing.T) {
		ctx := context.TODO()
		server := NewServer(":3334", true)
		go func() {
			err := server.Listen(ctx)
			assert.NoError(t, err)
		}()

		server.pingInterval = time.Second
		cli := NewClient("localhost:3334", "c1", 3, true)
		cli.Run(ctx)

		count := 100
		go func() {
			boba := count
			for boba > 0 {
				cli.Send(SocketMessage{Method: "test", Data: []byte("a\nf\nhjh")})
				time.Sleep(time.Millisecond * 10)
				boba--
			}
		}()

		serverIn := server.Incoming()

		for {
			select {
			case _ = <-serverIn:
				if count == 100 {
					server.DropConnections()
				}
				count--
				if count <= 0 {
					return
				}
			}
		}

	})
}
