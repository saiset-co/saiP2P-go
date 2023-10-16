package core

import (
	"net"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// Handle punch request from server part of core
func (c *Core) HandlePunch(request Request, newClientAddr *net.UDPAddr) {
	c.Logger.Debug("p2p -> server -> punchRequest", zap.String("request", request.RemoteAddr))

	c.Server.Address.IP = net.ParseIP(strings.Split(request.RemoteAddr, ":")[0])

	newClientIP := newClientAddr.IP.String()
	newClientPort := strconv.Itoa(newClientAddr.Port)

	c.Logger.Debug("StartServer -> newClientAddr",
		zap.String("addr", newClientAddr.String()),
	)

	_, err := c.GetConnection(newClientIP, newClientPort)
	if err != nil && len(c.ConnectionStorage) < c.Config.P2P.Slot {
		c.addConnection(newClientIP, newClientPort, "")
		c.Logger.Info("clients connected", zap.Any("clients", c.ConnectionStorage))
	} else {
		c.Logger.Error("PunchHandler", zap.Error(err))
		c.Logger.Error("PunchHandler", zap.Any("len", len(c.ConnectionStorage)))
		c.Logger.Error("PunchHandler", zap.Any("slot", c.Config.P2P.Slot))
	}

	//c.Logger.Debug("p2p - server - AddPubkeyToStorage - PunchHandler", zap.String("address", newClientAddr.String())) //@debug

	// respone + send own pubkey to connected p2p node
	response := Response{Type: punchResponse, Status: "OK", Connections: c.ConnectionStorage, Punch: newClientPort, Ip: newClientIP, Port: c.Config.P2P.Port}
	err = response.Send(c.Server.Connections.In, newClientAddr)
	if err != nil {
		c.Logger.Error("p2p -> server -> HandlePunch -> response.Send", zap.Error(err))
		return
	}
}

// Handle handshake request from server part of core
func (c *Core) HandleHandshake(request Request, newClientAddr *net.UDPAddr) {

	if len(c.ConnectionStorage) >= c.Config.P2P.Slot {
		response := Response{Type: handshakeResponse, Status: "NOK", Connections: c.ConnectionStorage, Punch: ""}
		err := response.Send(c.Server.Connections.In, newClientAddr)
		if err != nil {
			c.Logger.Error("p2p -> server -> HandleHandshake -> response.Send", zap.Error(err))
			return
		}

		return
	}

	c.HandlePunch(request, newClientAddr)
}

// Handle message from server part of core
func (c *Core) HandleMessage(request Request, newClientAddr *net.UDPAddr) {
	c.RLock()
	alreadyExist := c.SavedMessages[string(request.Message.Data)]
	c.RUnlock()
	if alreadyExist {
		c.Logger.Debug("p2p -> server -> MessageHandler : msg exists, skip sending ", zap.Any("from", request.Message.From))
		return
	} else {
		c.Lock()
		c.Logger.Debug("p2p -> server -> h", zap.Any("h", c.SavedMessages))
		c.SavedMessages[string(request.Message.Data)] = true
		c.Unlock()
	}

	c.MsgCh <- &request

	err := c.SendMsg(request.Message.Data, request.Message.To, request.LocalAddr)
	if err != nil {
		c.Logger.Error("Error", zap.Error(err))
		return
	}
}

// Event handling. For example,connecting/disconnecting to node
func (c *Core) HandleEvent(r *Request) {
	switch r.Event.Type {
	case DisconnectionEventType:
		c.RemoveConnection(r.Event.Address)
		c.Logger.Debug("handlers -> EventHandler -> node disconnected", zap.String("address", r.Event.Address))

	case ConnectionEventType:
		addr := strings.Split(r.Event.Address, ":")
		if len(addr) != 2 {
			c.Logger.Error("handlers -> EventHandler -> ConnectionEventType : wrong address provided", zap.String("address", r.Event.Address))
			return
		}
		c.addConnection(addr[0], addr[1], "")
	}
}
