package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/saiset-co/saiP2P-go/config"
	"github.com/saiset-co/saiP2P-go/utils"
	"go.uber.org/zap"
)

const (
	handshakeRequest  = "handshakeRequest"
	handshakeResponse = "handshakeResponse"
	punchRequest      = "punchRequest"
	punchResponse     = "punchResponse"
	MessageRequest    = "message"
	EventRequest      = "event"

	statusConnectionRejected = "CONNECTION_REJECTED"
)

// Main p2p struct
type Core struct {
	Server *Server       // servert part
	Client *Client       // client part
	Logger *zap.Logger   // logger
	Config config.Config // configuration
	sync.RWMutex
	ConnectionStorage map[string]bool // p2p connections listed here
	SavedMessages     map[string]bool // saved messages, to prevent double messages sending
	MsgCh             chan *Request   // to handle messages inside core
}

// filter connections func type
type filterConnections func(interface{}) bool

// Init core
func Init(config config.Config, f filterConnections) *Core {
	core := &Core{
		ConnectionStorage: make(map[string]bool),
		SavedMessages:     make(map[string]bool),
		MsgCh:             make(chan *Request),
		Config:            config,
	}

	logger, err := utils.BuildLogger(true)
	if err != nil {
		log.Fatalf("p2p -> Server -> BuildLogger : %s", err.Error())
	}

	core.Logger = logger

	server := &Server{
		AddrChan:          make(chan string),
		FilterConnections: f,
	}

	core.Server = server

	return core
}

// Init client
func (c *Core) CreateClient(peer string) *Core {
	client := Client{}
	client.Address.Local = c.Server.Address.IP.String() + ":" + c.Config.P2P.Port
	client.Address.Remote = peer

	c.Client = &client

	return c
}

// Run main p2p struct
func (c *Core) Run(f filterConnections) {
	c.Logger.Debug("Start", zap.Any("peers", c.Config.Peers))

	go c.ProcessHandshakes()

	if len(c.Config.Peers) > 0 {
		go c.RunClient()
	}

	if err := c.Serve(f); err != nil {
		c.Logger.Error("Start", zap.Error(err))
	}
}

// Broadcasting messages logic
func (c *Core) SendMsg(mes []byte, to []string, senderAddr string) error {

	var message = Message{
		From: senderAddr,
		To:   to,
		Data: mes,
	}

	// send msg only to recepients if recepients exists
	if len(to) != 0 {
		for _, address := range to {
			// check if recepient is from connected list
			if c.ConnectionStorage[address] {
				err := c.sendMsg(message, address)
				if err != nil {
					continue
				}
			} else {
				c.Logger.Debug("p2p -> server -> Send -> recepient is not from connected list", zap.String("recepient", address))
				continue
			}

		}
		//send msg to all connections if recepients is empty
	} else {
		for address := range c.ConnectionStorage {
			err := c.sendMsg(message, address)
			if err != nil {
				continue
			}
		}
	}

	return nil
}

func (c *Core) sendMsg(message Message, address string) error {

	if address == net.JoinHostPort(c.Server.Address.IP.String(), c.Config.P2P.Port) || address == net.JoinHostPort(c.Server.Address.IP.String(), c.Server.Address.PunchPort) {
		c.Logger.Error("p2p -> server -> Send : skip send to myself", zap.String("target", address))
		return errors.New("skip sending to myself")
	}
	clientAddr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		c.Logger.Error("p2p -> server -> ResolveUDPAddr", zap.Error(err), zap.String("addr", address))
		return err
	}

	var localAddr string
	if c.Server.Address.PunchPort == "" {
		localAddr = net.JoinHostPort(c.Server.Address.IP.String(), c.Config.P2P.Port)
	} else {
		localAddr = net.JoinHostPort(c.Server.Address.IP.String(), c.Server.Address.PunchPort)
	}

	request := Request{Type: "message", LocalAddr: localAddr, RemoteAddr: clientAddr.String(), Message: message}

	if c.Server.Connections.Out != nil {
		err = request.Send(c.Server.Connections.Out, clientAddr)
		if err != nil {
			c.Logger.Error("p2p -> server -> sendMsg -> request.Send(out)", zap.Error(err), zap.String("addr", clientAddr.String()))
		}
	} else {
		err = request.Send(c.Server.Connections.In, clientAddr)
		if err != nil {
			c.Logger.Error("p2p -> server -> sendMsg -> request.Send(In)", zap.Error(err), zap.String("addr", clientAddr.String()))
		}
	}

	c.Logger.Debug("p2p -> server -> sendMsg", zap.String("target", address))
	return nil
}

// Run client core part, if peers provided
func (c *Core) RunClient() {
	for _, peer := range c.Config.Peers {
		//c.Logger.Debug("StartClient", zap.String("peer", peer))
		err := c.CreateClient(peer).ConnectToPeer()
		if err != nil {
			c.Logger.Error("StartClient", zap.Error(err))
		}
	}
}

// Process incoming connections
func (c *Core) ProcessHandshakes() {
	for {
		peer := <-c.Server.AddrChan
		//		c.Logger.Debug("HandleList", zap.Any("peer", peer))

		if peer == net.JoinHostPort(c.Server.Address.IP.String(), c.Config.P2P.Port) || peer == net.JoinHostPort(c.Server.Address.IP.String(), c.Server.Address.PunchPort) {
			continue
		}

		if existing := c.ConnectionStorage[peer]; existing != true || existing == true {
			serverUDPAddr, err := net.ResolveUDPAddr("udp4", peer)
			if err != nil {
				c.Logger.Error("HandleList", zap.Error(err))
				continue
			}

			request := Request{Type: handshakeRequest, LocalAddr: c.Server.Address.IP.String() + ":" + c.Server.Address.PunchPort, RemoteAddr: peer}

			err = request.Send(c.Server.Connections.Out, serverUDPAddr)
			if err != nil {
				c.Logger.Error("p2p -> core -> request.Send", zap.Error(err))
				continue
			}
		}
	}
}

// Return address ip+port if it is peernode, ip+punchport if not
func (c *Core) GetRealAddress() string {
	var address string
	if c.Server.Address.PunchPort == "" {
		address = net.JoinHostPort(c.Server.Address.IP.String(), c.Config.P2P.Port)
	} else {
		address = net.JoinHostPort(c.Server.Address.IP.String(), c.Server.Address.PunchPort)
	}
	return address
}

// Send disconnect event type to all connected nodes
func (c *Core) ProvideDisconnection() {
	c.RLock()
	defer c.RUnlock()

	address := c.GetRealAddress()

	disconnectionReq := &Request{
		Type: EventRequest,
		Event: &Event{
			Address: address,
			Type:    DisconnectionEventType,
		},
		LocalAddr: address,
	}

	for node := range c.ConnectionStorage {
		disconnectionReq.RemoteAddr = node

		addr, err := net.ResolveUDPAddr("udp4", node)
		if err != nil {
			c.Logger.Error("p2p - server - Disconnect - ResolveUDPAddr", zap.Error(err))
			continue
		}

		err = disconnectionReq.Send(c.Server.Connections.In, addr)
		if err != nil {
			c.Logger.Error("p2p -> core -> ProvideDisconnection -> request.Send", zap.Error(err))
			continue
		}
	}
	c.Logger.Debug("p2p -> server -> disconnected to all peers")
}

func (c *Core) NextMsg(ctx context.Context) (*Message, error) {
	select {
	case msg, ok := <-c.MsgCh:
		if !ok {
			return &msg.Message, errors.New("error")
		}
		return &msg.Message, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Check connection ability
func (c *Core) CheckAllowConn(filter filterConnections, ip, port string) error {
	// check if connection is allowed
	// s.IP.String() as example
	if !filter(c.Server.Address.IP.String()) {
		addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(ip, port))
		if err != nil {
			c.Logger.Error("p2p -> server -> CheckAllowConn -> ResolveUDPAddr", zap.Error(err))
			return err
		}
		c.Logger.Debug("p2p - server - StartServer - AllowConnection - reject connection", zap.String("host", addr.IP.String()), zap.String("port", port))
		var usedPort string
		if c.Server.Address.PunchPort == "" {
			usedPort = c.Config.P2P.Port
		} else {
			usedPort = c.Server.Address.PunchPort
		}
		response := Response{Type: punchResponse, Status: statusConnectionRejected, Ip: c.Server.Address.IP.String(), Port: usedPort}
		err = response.Send(c.Server.Connections.In, addr)
		if err != nil {
			c.Logger.Error("p2p -> server -> CheckAllowConn -> response.Send", zap.Error(err))
			return fmt.Errorf("response.Send :%w", err)
		}
		return errors.New("p2p -> server -> CheckAllowConn -> reject connection")
	}
	return nil
}

// Graceful shutdown
func (c *Core) GracefulShutdown() {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	select {
	case s := <-interrupt:
		c.Logger.Info("main -> got interrupt", zap.String("signal", s.String()))
		c.ProvideDisconnection()
		c.Logger.Info("main -> interrupt - success", zap.String("signal", s.String()))
	}
}

// Return stats of p2p server for debbuging purposes
func (c *Core) Stats() AppStats {
	p2pStats := AppStats{
		ConnectionStorage: c.ConnectionStorage,
		PunchPort:         c.Server.Address.PunchPort,
		SavedMessages:     c.SavedMessages,
		IP:                c.Server.Address.IP.String(),
	}

	return p2pStats

}
