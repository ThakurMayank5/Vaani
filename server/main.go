package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Device IDs are the stable values used by the app and the ESP32.
const (
	DeviceDeskLight    = 1
	DeviceReadingLight = 2
	DeviceAccentLight  = 3
	DeviceNightLight   = 4
)

type LEDCommand struct {
	Device int    `json:"device"`
	Action string `json:"action"`
}

type DeviceInfo struct {
	Device     int    `json:"device"`
	Name       string `json:"name"`
	CustomWord string `json:"customWord"`
	GPIO       int    `json:"gpio"`
	State      string `json:"state"`
}

type RenameDeviceRequest struct {
	Name string `json:"name"`
}

type APIResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Device  int    `json:"device,omitempty"`
	Action  string `json:"action,omitempty"`
	GPIO    int    `json:"gpio,omitempty"`
}

type DevicesResponse struct {
	Success bool         `json:"success"`
	Devices []DeviceInfo `json:"devices"`
}

type DeviceResponse struct {
	Success bool       `json:"success"`
	Device  DeviceInfo `json:"device"`
}

var (
	esp32URL   string
	httpClient *http.Client

	devicesMu sync.RWMutex
	devices   = map[int]DeviceInfo{
		DeviceDeskLight: {
			Device:     DeviceDeskLight,
			Name:       "Desk Light",
			CustomWord: "Prism",
			GPIO:       18,
			State:      "OFF",
		},
		DeviceReadingLight: {
			Device:     DeviceReadingLight,
			Name:       "Reading Light",
			CustomWord: "Halo",
			GPIO:       19,
			State:      "OFF",
		},
		DeviceAccentLight: {
			Device:     DeviceAccentLight,
			Name:       "Accent Light",
			CustomWord: "Ember",
			GPIO:       21,
			State:      "OFF",
		},
		DeviceNightLight: {
			Device:     DeviceNightLight,
			Name:       "Night Light",
			CustomWord: "Nova",
			GPIO:       22,
			State:      "OFF",
		},
	}
	deviceOrder = []int{
		DeviceDeskLight,
		DeviceReadingLight,
		DeviceAccentLight,
		DeviceNightLight,
	}
)

func writeJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func getDevice(deviceID int) (DeviceInfo, bool) {
	devicesMu.RLock()
	defer devicesMu.RUnlock()
	device, exists := devices[deviceID]
	return device, exists
}

func listDevices() []DeviceInfo {
	devicesMu.RLock()
	defer devicesMu.RUnlock()

	result := make([]DeviceInfo, 0, len(deviceOrder))
	for _, deviceID := range deviceOrder {
		result = append(result, devices[deviceID])
	}
	return result
}

func setDeviceState(deviceID int, state string) {
	devicesMu.Lock()
	defer devicesMu.Unlock()
	device := devices[deviceID]
	device.State = state
	devices[deviceID] = device
}

// CORS support is useful during Expo Web development.
// Native Expo apps generally do not enforce browser CORS rules.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use GET for this endpoint"})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{Success: true, Message: "Go device controller is running"})
}

func devicesHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/devices")
	if path == "" || path == "/" {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, OPTIONS")
			writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use GET to list devices"})
			return
		}
		writeJSON(w, http.StatusOK, DevicesResponse{Success: true, Devices: listDevices()})
		return
	}

	deviceID, err := strconv.Atoi(strings.TrimPrefix(path, "/"))
	if err != nil || deviceID < 1 {
		writeJSON(w, http.StatusNotFound, APIResponse{Message: "Device not found"})
		return
	}

	device, exists := getDevice(deviceID)
	if !exists {
		writeJSON(w, http.StatusNotFound, APIResponse{Message: "Device not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, DeviceResponse{Success: true, Device: device})
	case http.MethodPatch:
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		defer r.Body.Close()

		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var update RenameDeviceRequest
		if err := decoder.Decode(&update); err != nil {
			writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid JSON. Expected a name."})
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Request must contain exactly one JSON object"})
			return
		}

		name := strings.TrimSpace(update.Name)
		if name == "" || len(name) > 60 {
			writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Device name must be between 1 and 60 characters"})
			return
		}

		devicesMu.Lock()
		device.Name = name
		devices[deviceID] = device
		devicesMu.Unlock()
		writeJSON(w, http.StatusOK, DeviceResponse{Success: true, Device: device})
	default:
		w.Header().Set("Allow", "GET, PATCH, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use GET or PATCH for this endpoint"})
	}
}

func executeESP32Command(ctx context.Context, deviceID int, action string) (int, error) {
	device, exists := getDevice(deviceID)
	if !exists {
		return 0, fmt.Errorf("device %d not found", deviceID)
	}

	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "ON" && action != "OFF" {
		return 0, fmt.Errorf("invalid action: %s", action)
	}

	espBody := fmt.Sprintf("device:%d\naction:%s\n", deviceID, action)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, esp32URL, bytes.NewBufferString(espBody))
	if err != nil {
		return 0, fmt.Errorf("could not create ESP32 request: %w", err)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("could not reach ESP32: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		return 0, fmt.Errorf("could not read a valid ESP32 response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("ESP32 rejected the command with HTTP %d: %s", resp.StatusCode, string(body))
	}

	setDeviceState(deviceID, action)
	log.Printf("Device command successful: device=%d action=%s GPIO=%d", deviceID, action, device.GPIO)
	return device.GPIO, nil
}

func ledHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use POST for LED commands"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var command LEDCommand
	if err := decoder.Decode(&command); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid JSON. Expected device and action."})
		return
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Request must contain exactly one JSON object"})
		return
	}

	command.Action = strings.ToUpper(strings.TrimSpace(command.Action))
	if command.Action != "ON" && command.Action != "OFF" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid action. Use ON or OFF"})
		return
	}

	gpio, err := executeESP32Command(r.Context(), command.Device, command.Action)
	if err != nil {
		log.Printf("LED command failed: %v", err)
		writeJSON(w, http.StatusBadGateway, APIResponse{Message: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, APIResponse{
		Success: true,
		Message: "Device command executed successfully",
		Device:  command.Device,
		Action:  command.Action,
		GPIO:    gpio,
	})
}

func intentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use POST for intent parsing"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	defer r.Body.Close()

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req IntentRequest
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid JSON. Expected 'text' field."})
		return
	}

	rawText := strings.TrimSpace(req.Text)
	if rawText == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Text cannot be empty."})
		return
	}

	parsed := ParseIntent(rawText, listDevices())

	shouldExecute := true
	if req.Execute != nil {
		shouldExecute = *req.Execute
	}

	resp := IntentResponse{
		Success:  parsed.Intent != IntentUnknown,
		Intent:   parsed,
		Executed: false,
	}

	if parsed.Intent == IntentUnknown {
		if parsed.Action != "" {
			resp.Message = fmt.Sprintf("Action '%s' detected, but no matching device found. Try 'Prism', 'Halo', 'Ember', 'Nova', or 'all lights'.", parsed.Action)
		} else {
			resp.Message = "Could not recognize device command from speech"
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if parsed.Confidence < 0.70 {
		resp.Success = false
		resp.Message = fmt.Sprintf("Confidence too low (%.2f). Action ignored to prevent false triggers in noisy environment.", parsed.Confidence)
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Message = fmt.Sprintf("Parsed intent: %s (confidence: %.2f)", parsed.Intent, parsed.Confidence)

	if !shouldExecute || parsed.Intent == IntentQueryStatus {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	// Dispatch commands across matched devices
	resp.Executed = true
	resp.Results = make([]DeviceExecutionResult, 0, len(parsed.DeviceIDs))

	for _, deviceID := range parsed.DeviceIDs {
		dev, exists := getDevice(deviceID)
		if !exists {
			continue
		}

		targetAction := parsed.Action
		if parsed.Intent == IntentToggle {
			if dev.State == "ON" {
				targetAction = "OFF"
			} else {
				targetAction = "ON"
			}
		}

		gpio, err := executeESP32Command(r.Context(), deviceID, targetAction)
		result := DeviceExecutionResult{
			Device:  deviceID,
			Name:    dev.Name,
			GPIO:    dev.GPIO,
			State:   targetAction,
			Success: err == nil,
		}
		if err != nil {
			result.Error = err.Error()
			result.State = dev.State
		} else {
			result.GPIO = gpio
		}
		resp.Results = append(resp.Results, result)
	}

	writeJSON(w, http.StatusOK, resp)
}

func main() {
	// Set this to the URL printed or configured for your ESP32,
	// e.g. http://192.168.1.105/led.
	esp32URL = "http://192.168.137.64/led"
	if esp32URL == "" {
		log.Fatal("ESP32_URL is missing. Set it to http://<ESP32-IP>/led")
	}
	if !strings.HasPrefix(esp32URL, "http://") && !strings.HasPrefix(esp32URL, "https://") {
		log.Fatal("ESP32_URL must start with http:// or https://")
	}

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/devices", devicesHandler)
	mux.HandleFunc("/api/devices/", devicesHandler)
	mux.HandleFunc("/api/led", ledHandler)
	mux.HandleFunc("/api/intent", intentHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "42069"
	}
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatal("PORT must be a valid port number")
	}

	server := &http.Server{
		Addr:              net.JoinHostPort("0.0.0.0", port),
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("Go device server starting on port %s", port)
	log.Printf("ESP32 endpoint: %s", esp32URL)
	log.Printf("Device endpoint: GET http://localhost:%s/api/devices", port)
	log.Printf("LED endpoint: POST http://localhost:%s/api/led", port)
	log.Printf("Intent endpoint: POST http://localhost:%s/api/intent", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
