package app

import (
	"context"
	"log"
	"net/http"

	api "github.com/saiset-co/saiP2P-go/api/http"
	"github.com/saiset-co/saiP2P-go/config"
	corelib "github.com/saiset-co/saiP2P-go/core"
	"github.com/saiset-co/saiP2P-go/core/socket"
	"go.uber.org/zap"
)

// Run - main start point of the app
func Run() {

	ctx := context.Background()

	config, err := config.Get()
	if err != nil {
		log.Fatalf("GetConfig : %s", err.Error())
	}

	// test filter func
	testFilterFunc := func(interface{}) bool {
		return true
	}

	core := corelib.Init(config, testFilterFunc)

	go core.Run(testFilterFunc)

	// start http handlers
	router := api.New(core)
	router.Routes()

	go func() {
		if err := http.ListenAndServe(":"+config.Http.Port, nil); err != nil {
			log.Println("Http server error: ", err)
		}
	}()

	socketServer := socket.NewServer(config.SocketPort, false)
	go func() {
		if err := socketServer.Listen(ctx); err != nil {
			return
		}
	}()

	go func() {
		in := socketServer.Incoming()

		socket.SendWithPool(ctx, in, 10, func(m socket.SocketMessage) error {
			core.Lock()
			core.SavedMessages[string(m.Data)] = true
			core.Unlock()

			err = core.SendMsg(m.Data, []string{}, core.GetRealAddress())
			return err
		})
	}()

	// get messages
	go func() {
		for {
			msg, err := core.NextMsg(ctx)
			if err != nil {
				core.Logger.Error("main -> s.Next", zap.Error(err))
				continue
			}
			socketServer.Broadcast(&socket.SocketMessage{Method: "forward", Data: msg.Data})
		}
	}()

	// graceful shutdown
	core.GracefulShutdown()
}
