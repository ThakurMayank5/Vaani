package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"nhooyr.io/websocket"
)

type DeviceEvent struct {
	Type   string       `json:"type"`
	Device *DeviceInfo  `json:"device,omitempty"`
	Devices []DeviceInfo `json:"devices,omitempty"`
}

type deviceEventClient struct {
	connection *websocket.Conn
	writeMu    sync.Mutex
}

type DeviceEventHub struct {
	mu      sync.RWMutex
	clients map[*deviceEventClient]struct{}
}

func newDeviceEventHub() *DeviceEventHub {
	return &DeviceEventHub{clients: make(map[*deviceEventClient]struct{})}
}

func (h *DeviceEventHub) add(client *deviceEventClient) {
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
}

func (h *DeviceEventHub) remove(client *deviceEventClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
	_ = client.connection.Close(websocket.StatusNormalClosure, "connection closed")
}

func (h *DeviceEventHub) broadcast(event DeviceEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		log.Printf("Could not encode device event: %v", err)
		return
	}

	h.mu.RLock()
	clients := make([]*deviceEventClient, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	for _, client := range clients {
		client.writeMu.Lock()
		writeContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := client.connection.Write(writeContext, websocket.MessageText, payload)
		cancel()
		client.writeMu.Unlock()
		if err != nil {
			h.remove(client)
		}
	}
}

func (h *DeviceEventHub) serveHTTP(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The Expo web client can be served from a different local origin.
		InsecureSkipVerify: true,
	})
	if err != nil {
		log.Printf("Could not accept device event WebSocket: %v", err)
		return
	}
	client := &deviceEventClient{connection: connection}
	h.add(client)
	defer h.remove(client)

	devices, err := store.ListDevices(r.Context())
	if err != nil {
		log.Printf("Could not send initial device snapshot: %v", err)
		return
	}
	client.writeMu.Lock()
	writeContext, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	err = connection.Write(writeContext, websocket.MessageText, mustMarshalDeviceEvent(DeviceEvent{
		Type: "devices.snapshot", Devices: devices,
	}))
	cancel()
	client.writeMu.Unlock()
	if err != nil {
		return
	}

	for {
		if _, _, err := connection.Read(r.Context()); err != nil {
			return
		}
	}
}

func mustMarshalDeviceEvent(event DeviceEvent) []byte {
	payload, err := json.Marshal(event)
	if err != nil {
		// All event fields are local primitives; this would indicate a programming error.
		panic(err)
	}
	return payload
}
