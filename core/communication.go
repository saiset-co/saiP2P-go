package core

import (
	"encoding/json"
	"fmt"
	"net"
)

type EventType int

const (
	DisconnectionEventType EventType = 0
	ConnectionEventType    EventType = 1
)

// Event model, used to handle different types of events (connecting, disconnecting...)
type Event struct {
	Address string    `json:"address"`
	Type    EventType `json:"event_type"`
}

// Standart request model for p2p communication
type Request struct {
	Type       string  `json:"type"`
	LocalAddr  string  `json:"local_addr"`
	RemoteAddr string  `json:"remote_addr"`
	Message    Message `json:"message"`
	Event      *Event  `json:"event,omitempty"`
}

func (r *Request) Send(conn *net.UDPConn, addr *net.UDPAddr) error {
	requestJSON, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("Marshal : %w", err)
	}
	_, err = conn.WriteToUDP(requestJSON, addr)
	if err != nil {
		return fmt.Errorf("WriteToUDP : %w", err)
	}
	return nil
}

// Standart response model for p2p communication
type Response struct {
	Type        string          `json:"type"`
	Status      string          `json:"status"`
	Connections map[string]bool `json:"list"`
	Punch       string          `json:"punch"`
	Ip          string          `json:"ip"`
	Port        string          `json:"port"`
}

func (r *Response) Send(conn *net.UDPConn, addr *net.UDPAddr) error {
	requestJSON, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("Marshal : %w", err)
	}
	_, err = conn.WriteToUDP(requestJSON, addr)
	if err != nil {
		return fmt.Errorf("WriteToUDP : %w", err)
	}
	return nil
}

// Message model for p2p communication
type Message struct {
	From string
	To   []string
	Data []byte
}
