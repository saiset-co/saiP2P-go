package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"

	"go.uber.org/zap"
)

// Punch response handler for client core part
func (c *Core) ClientPunchResponseHandler(message map[string]interface{}, serverIP, serverPort string) error {
	//			client.server.Logger.Debug(punchResponse, zap.Any("message", message))

	if status, ok := message["status"]; ok == true && status.(string) == statusConnectionRejected {
		c.Logger.Debug("client - connection rejected", zap.String("IP", message["ip"].(string)), zap.String("Port", message["port"].(string)))
		return errors.New("connection rejected")
	}

	if punch, ok := message["punch"]; ok != false && c.Server.Address.PunchPort == "" {
		if punch, done := punch.(string); done != false {
			c.Server.Address.PunchPort = punch
			c.Server.Address.IP = net.ParseIP(message["ip"].(string))
			c.addConnection(serverIP, serverPort, c.Server.Address.PunchPort)
		}
	}

	//client.server.Logger.Debug("p2p - server - AddPubkeyToStorage - punchResp", zap.String("address", serverAddr.String())) //@debug

	if list, ok := message["list"]; ok != false && len(c.ConnectionStorage) < c.Config.P2P.Slot {
		if list, done := list.(map[string]interface{}); done != false {
			for address := range list {
				// client.server.Logger.Debug(punchResponse, zap.Any("ip", ip))
				// client.server.Logger.Debug(punchResponse, zap.Any("port", port))
				c.Server.AddrChan <- address
			}
		}
	}
	return nil
}

// Handshake request handler for client core part
func (c *Core) ClientHandshakeRequestHandler(message map[string]interface{}) error {
	if localAddrS, ok := message["local_addr"]; ok == true {
		if localAddrS, done := localAddrS.(string); done {
			localAddr, err := net.ResolveUDPAddr("udp4", localAddrS)
			if err != nil {
				c.Logger.Error("Connect", zap.Error(err))
				return err
			}

			if len(c.ConnectionStorage) < c.Config.P2P.Slot {
				path := strings.Split(localAddrS, ":")

				if len(path) < 2 {
					return errors.New("wrong address format")
				}
				err = c.CheckAllowConn(c.Server.FilterConnections, path[0], path[1])
				if err != nil {
					return errors.New("connection not allowed")
				}
				c.addConnection(path[0], path[1], c.Server.Address.PunchPort)
				response := Response{Type: handshakeResponse,
					Status:      "OK",
					Connections: c.ConnectionStorage,
					Punch:       "",
				}

				err = response.Send(c.Server.Connections.Out, localAddr)
				if err != nil {
					return fmt.Errorf("response.Send :%w", err)
				}

			} else {
				response := Response{Type: handshakeResponse, Status: "NOK", Connections: c.ConnectionStorage}

				err = response.Send(c.Server.Connections.Out, localAddr)
				if err != nil {
					return fmt.Errorf("response.Send :%w", err)
				}
			}
		}
	}
	c.Client.Messages.System++
	return nil
}

// Handshake response handler for client core part
func (c *Core) ClientHandshakeResponseHandler(message map[string]interface{}, serverIP, serverPort string) error {
	if list, ok := message["list"]; ok != false && len(c.ConnectionStorage) < c.Config.P2P.Slot {
		if list, done := list.(map[string]bool); done != false {
			for address := range list {
				c.Server.AddrChan <- address
			}
		}
	}

	if status, ok := message["status"]; ok != false && status == "OK" {
		c.Logger.Debug("handshakeResponse")
		err := c.CheckAllowConn(c.Server.FilterConnections, serverIP, serverPort)
		if err != nil {
			return fmt.Errorf("CheckAllowConn :%w", err)
		}
		c.addConnection(serverIP, serverPort, c.Server.Address.PunchPort)
	}
	return nil
}

// Message handler for client core part
func (c *Core) ClientMessageHandler(buf []byte) error {
	msg := Request{}
	err := json.Unmarshal(buf, &msg)
	if err != nil {
		return fmt.Errorf("Unmarshal :%w", err)
	}
	c.Logger.Debug("client - messageRequest", zap.String("type", msg.Type),
		zap.String("local", msg.LocalAddr),
		zap.String("remote", msg.RemoteAddr))

	c.RLock()

	alreadyExist := c.SavedMessages[string(msg.Message.Data)]
	if alreadyExist {
		c.RUnlock()
		c.Logger.Debug("p2p -> ClientMessageHandler -> message already exists")
		return nil
	} else {
		c.SavedMessages[string(msg.Message.Data)] = true
		c.RUnlock()
	}

	c.MsgCh <- &msg

	c.SendMsg(msg.Message.Data, msg.Message.To, msg.LocalAddr)
	if err != nil {
		return fmt.Errorf("Send :%w", err)
	}
	c.Client.Messages.Messages++
	return nil
}

// Event handler for client core part
func (c *Core) ClientEventHandler(buf []byte) error {
	request := Request{}
	err := json.Unmarshal(buf, &request)
	if err != nil {
		return fmt.Errorf("Unmarshal :%w", err)
	}
	c.Logger.Debug("client - EventRequest", zap.String("type", request.Type),
		zap.String("local", request.LocalAddr),
		zap.String("remote", request.RemoteAddr))
	c.HandleEvent(&request)
	return nil
}

// Default handler for client core part
func (c *Core) ClientDefaultHandler(message map[string]interface{}) error {
	c.Logger.Debug("Default", zap.Any("message", message))
	if messageData, ok := message["message"]; ok == true {
		if mess, done := messageData.(Message); done {
			c.Logger.Debug("MessageHandler", zap.Any("message", mess))

			if _, ok := c.SavedMessages[string(mess.Data)]; ok != true {
				c.SavedMessages[string(mess.Data)] = true
				c.Client.Messages.Unique++
			} else {
				return nil
			}

			err := c.SendMsg(mess.Data, mess.To, mess.From)
			if err != nil {
				return fmt.Errorf("Send :%w", err)
			}

			c.Logger.Debug("client - Connect - for loop - default", zap.String("type", reflect.TypeOf(messageData).String()))
			err = c.Notify(mess)
			if err != nil {
				return fmt.Errorf("Notify :%w", err)
			}
			c.Client.Messages.Messages++
		}
	}
	return nil
}
