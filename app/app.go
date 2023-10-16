package app

import (
	"context"
	"log"
	"net/http"

	api "github.com/saiset-co/saiP2P-go/api/http"
	"github.com/saiset-co/saiP2P-go/config"
	corelib "github.com/saiset-co/saiP2P-go/core"
	"go.uber.org/zap"
)

// Run - main start point of the app
func Run() {
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

	// get messages
	go func() {
		for {
			msg, err := core.NextMsg(context.Background())
			if err != nil {
				core.Logger.Error("main -> s.Next", zap.Error(err))
				continue
			}
			core.Logger.Info("MESSAGE", zap.String("FROM", msg.From), zap.Strings("TO", msg.To), zap.Any("DATA", msg.Data))
		}
	}()

	// graceful shutdown
	core.GracefulShutdown()
}
