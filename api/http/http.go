package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/saiset-co/saiP2P-go/core"
	"go.uber.org/zap"
)

type Router struct {
	Core *core.Core
}

func New(core *core.Core) *Router {
	return &Router{
		Core: core,
	}
}

// Routes - http routes for the app
func (r *Router) Routes() {
	http.HandleFunc("/send", r.SendApi)   // send msgs by http
	http.HandleFunc("/stats", r.GetStats) //get stats
}

// GetStats - return app stats for a debug purposes
func (r *Router) GetStats(resp http.ResponseWriter, _ *http.Request) {
	stats := r.Core.Stats()
	data, err := json.Marshal(stats)
	if err != nil {
		err := map[string]interface{}{"Status": "NOK", "Error": err.Error()}
		errBody, _ := json.Marshal(err)
		log.Println(err)
		resp.Write(errBody)
		return
	}
	resp.Write(data)
}

// SendApi - api for accept messages from the http
func (r *Router) SendApi(resp http.ResponseWriter, req *http.Request) {
	message, err := io.ReadAll(req.Body)
	if err != nil {
		err := map[string]interface{}{"Status": "NOK", "Error": err.Error()}
		errBody, _ := json.Marshal(err)
		log.Println(err)
		resp.Write(errBody)
		return
	}

	to := req.URL.Query().Get("to")

	recipients := []string{}

	if len(to) > 1 {
		recipients = strings.Split(to, ",")
	} else if len(to) == 1 {
		recipients = append(recipients, to)
	}

	if err != nil {
		err := map[string]interface{}{"Status": "NOK", "Error": err.Error()}
		errBody, _ := json.Marshal(err)
		log.Println(err)
		resp.Write(errBody)
		return
	}

	address := r.Core.GetRealAddress()

	r.Core.Lock()
	r.Core.SavedMessages[string(message)] = true
	r.Core.Logger.Debug("Messages in cache", zap.Any("cache", r.Core.SavedMessages))
	r.Core.Unlock()

	err = r.Core.SendMsg(message, recipients, address)
	if err != nil {
		err := map[string]interface{}{"Status": "NOK", "Error": err.Error()}
		errBody, _ := json.Marshal(err)
		log.Println(err)
		resp.Write(errBody)
		return
	}
	r.Core.Logger.Debug("Send", zap.Strings("recepients", recipients))

	response := map[string]interface{}{"Status": "OK"}
	responseBody, _ := json.Marshal(response)
	resp.Write(responseBody)
}
