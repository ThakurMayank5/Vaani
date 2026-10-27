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
	"time"
)

// Device IDs are the stable values shared by the app, Go server, and ESP32.
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
	Device          int    `json:"device"`
	Name            string `json:"name"`
	CustomWord      string `json:"customWord"`
	GPIO            int    `json:"gpio"`
	State           string `json:"state"`
	UpdatedAtUTC    string `json:"updatedAtUtc"`
	UpdatedAtUnixMS int64  `json:"updatedAtUnixMs"`
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

var defaultDevices = []DeviceInfo{
	{Device: DeviceDeskLight, Name: "Desk Light", CustomWord: "Prism", GPIO: 18, State: "OFF"},
	{Device: DeviceReadingLight, Name: "Reading Light", CustomWord: "Halo", GPIO: 19, State: "OFF"},
	{Device: DeviceAccentLight, Name: "Accent Light", CustomWord: "Ember", GPIO: 21, State: "OFF"},
	{Device: DeviceNightLight, Name: "Night Light", CustomWord: "Nova", GPIO: 22, State: "OFF"},
}

var (
	esp32URL  string
	httpClient *http.Client
	store      *Store
	deviceEvents *DeviceEventHub
)

func writeJSON(w http.ResponseWriter, status int, response any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response)
}

func requestSource(r *http.Request) string {
	source := strings.TrimSpace(r.Header.Get("X-Control-Source"))
	if source == "" {
		return "api"
	}
	if len(source) > 100 {
		return source[:100]
	}
	return source
}

func recordAction(record ControlActionRecord) {
	if err := store.RecordControlAction(context.Background(), record); err != nil {
		log.Printf("Could not save control action history: %v", err)
	}
}

func recordVoiceCommand(source, transcript string, parsed ParsedIntent, executeRequested, executed bool, outcome string) {
	if err := store.RecordVoiceCommand(context.Background(), VoiceCommandRecord{
		Source: source, Transcript: transcript, Intent: string(parsed.Intent), Confidence: parsed.Confidence,
		ExecuteRequested: executeRequested, Executed: executed, Outcome: outcome, CreatedAt: time.Now().UTC(),
	}); err != nil {
		log.Printf("Could not save voice command history: %v", err)
	}
}

func listDevices() []DeviceInfo {
	devices, err := store.ListDevices(context.Background())
	if err != nil {
		log.Printf("Could not list devices for intent parsing: %v", err)
		return nil
	}
	return devices
}

// executeESP32Command is used by the voice-intent path. It shares the same
// database updates and action history format as the public LED endpoint.
func executeESP32Command(ctx context.Context, deviceID int, action, source string) (int, error) {
	device, exists, err := store.GetDevice(ctx, deviceID)
	if err != nil {
		return 0, fmt.Errorf("load device: %w", err)
	}
	if !exists {
		return 0, fmt.Errorf("device %d not found", deviceID)
	}
	action = strings.ToUpper(strings.TrimSpace(action))
	if action != "ON" && action != "OFF" {
		return 0, fmt.Errorf("invalid action: %s", action)
	}

	startedAt := time.Now().UTC()
	record := ControlActionRecord{
		ActionType: "DEVICE_COMMAND", Source: source, DeviceID: &deviceID,
		RequestedAction: action, PreviousState: device.State, ResultingState: device.State,
		Success: false, HTTPStatus: http.StatusBadGateway, Message: "Command did not complete", StartedAt: startedAt,
	}
	defer func() {
		record.CompletedAt = time.Now().UTC()
		recordAction(record)
	}()

	espBody := fmt.Sprintf("device:%d\naction:%s\n", deviceID, action)
	requestContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestContext, http.MethodPost, esp32URL, bytes.NewBufferString(espBody))
	if err != nil {
		record.HTTPStatus = http.StatusInternalServerError
		record.Message = "Could not create ESP32 request"
		return 0, fmt.Errorf("%s: %w", record.Message, err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := httpClient.Do(req)
	if err != nil {
		record.Message = "Could not reach ESP32"
		return 0, fmt.Errorf("%s: %w", record.Message, err)
	}
	defer resp.Body.Close()
	record.ESPHTTPStatus = &resp.StatusCode
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		record.Message = "Could not read a valid ESP32 response"
		return 0, fmt.Errorf("%s", record.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		record.Message = "ESP32 rejected the command"
		return 0, fmt.Errorf("%s: HTTP %d", record.Message, resp.StatusCode)
	}
	updatedDevice, err := store.SetDeviceState(ctx, deviceID, action)
	if err != nil {
		record.HTTPStatus = http.StatusInternalServerError
		record.Message = "ESP32 accepted the command but the device state could not be saved"
		return 0, fmt.Errorf("%s: %w", record.Message, err)
	}
	record.Success = true
	record.HTTPStatus = http.StatusOK
	record.ResultingState = updatedDevice.State
	record.Message = "Device command executed successfully"
	deviceEvents.broadcast(DeviceEvent{Type: "device.updated", Device: &updatedDevice})
	return device.GPIO, nil
}

// CORS support is useful during Expo Web development.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Control-Source")
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
		devices, err := store.ListDevices(r.Context())
		if err != nil {
			log.Printf("Could not list devices: %v", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Message: "Could not load devices"})
			return
		}
		writeJSON(w, http.StatusOK, DevicesResponse{Success: true, Devices: devices})
		return
	}

	deviceID, err := strconv.Atoi(strings.TrimPrefix(path, "/"))
	if err != nil || deviceID < 1 {
		writeJSON(w, http.StatusNotFound, APIResponse{Message: "Device not found"})
		return
	}

	device, exists, err := store.GetDevice(r.Context(), deviceID)
	if err != nil {
		log.Printf("Could not load device %d: %v", deviceID, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Message: "Could not load device"})
		return
	}
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

		startedAt := time.Now().UTC()
		updatedDevice, _, err := store.RenameDevice(r.Context(), deviceID, name)
		completedAt := time.Now().UTC()
		record := ControlActionRecord{
			ActionType: "DEVICE_RENAMED", Source: requestSource(r), DeviceID: &deviceID,
			RequestedAction: name, PreviousState: device.State, ResultingState: device.State,
			Success: err == nil, HTTPStatus: http.StatusOK, Message: "Device name updated",
			StartedAt: startedAt, CompletedAt: completedAt,
		}
		if err != nil {
			log.Printf("Could not rename device %d: %v", deviceID, err)
			record.HTTPStatus = http.StatusInternalServerError
			record.Message = "Could not save device name"
			record.Success = false
			recordAction(record)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Message: record.Message})
			return
		}
		recordAction(record)
		deviceEvents.broadcast(DeviceEvent{Type: "device.updated", Device: &updatedDevice})
		writeJSON(w, http.StatusOK, DeviceResponse{Success: true, Device: updatedDevice})
	default:
		w.Header().Set("Allow", "GET, PATCH, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use GET or PATCH for this endpoint"})
	}
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

	device, exists, err := store.GetDevice(r.Context(), command.Device)
	if err != nil {
		log.Printf("Could not load device %d: %v", command.Device, err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Message: "Could not load device"})
		return
	}
	if !exists {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid device. Choose a configured device."})
		return
	}

	command.Action = strings.ToUpper(strings.TrimSpace(command.Action))
	if command.Action != "ON" && command.Action != "OFF" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid action. Use ON or OFF"})
		return
	}

	startedAt := time.Now().UTC()
	record := ControlActionRecord{
		ActionType: "DEVICE_COMMAND", Source: requestSource(r), DeviceID: &command.Device,
		RequestedAction: command.Action, PreviousState: device.State, ResultingState: device.State,
		Success: false, HTTPStatus: http.StatusBadGateway, Message: "Command did not complete",
		StartedAt: startedAt,
	}
	defer func() {
		record.CompletedAt = time.Now().UTC()
		recordAction(record)
	}()

	espBody := fmt.Sprintf("device:%d\naction:%s\n", command.Device, command.Action)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, esp32URL, bytes.NewBufferString(espBody))
	if err != nil {
		log.Printf("Could not create ESP32 request: %v", err)
		record.HTTPStatus = http.StatusInternalServerError
		record.Message = "Could not create ESP32 request"
		writeJSON(w, record.HTTPStatus, APIResponse{Message: record.Message})
		return
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("ESP32 request failed: %v", err)
		record.Message = "Could not reach ESP32. Check its IP and Wi-Fi."
		writeJSON(w, record.HTTPStatus, APIResponse{Message: record.Message})
		return
	}
	defer resp.Body.Close()
	record.ESPHTTPStatus = &resp.StatusCode

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if err != nil || len(body) > 4096 {
		record.Message = "Could not read a valid ESP32 response"
		writeJSON(w, record.HTTPStatus, APIResponse{Message: record.Message})
		return
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("ESP32 returned HTTP %d: %s", resp.StatusCode, body)
		record.Message = "ESP32 rejected the command"
		writeJSON(w, record.HTTPStatus, APIResponse{Message: record.Message})
		return
	}

	updatedDevice, err := store.SetDeviceState(r.Context(), command.Device, command.Action)
	if err != nil {
		log.Printf("Could not save state for device %d: %v", command.Device, err)
		record.HTTPStatus = http.StatusInternalServerError
		record.Message = "ESP32 accepted the command but the device state could not be saved"
		writeJSON(w, record.HTTPStatus, APIResponse{Message: record.Message})
		return
	}

	record.Success = true
	record.HTTPStatus = http.StatusOK
	record.ResultingState = updatedDevice.State
	record.Message = "Device command executed successfully"
	log.Printf("Device command successful: device=%d action=%s GPIO=%d", command.Device, command.Action, device.GPIO)
	deviceEvents.broadcast(DeviceEvent{Type: "device.updated", Device: &updatedDevice})
	writeJSON(w, http.StatusOK, APIResponse{
		Success: true, Message: record.Message, Device: command.Device, Action: command.Action, GPIO: device.GPIO,
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
	var intentRequest IntentRequest
	if err := decoder.Decode(&intentRequest); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid JSON. Expected 'text' field."})
		return
	}
	text := strings.TrimSpace(intentRequest.Text)
	if text == "" {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Text cannot be empty."})
		return
	}

	parsed := ParseIntent(text, listDevices())
	response := IntentResponse{Success: parsed.Intent != IntentUnknown, Intent: parsed}
	shouldExecute := intentRequest.Execute == nil || *intentRequest.Execute
	source := requestSource(r)
	defer func() {
		recordVoiceCommand(source, text, parsed, shouldExecute, response.Executed, response.Message)
	}()
	if parsed.Intent == IntentUnknown {
		response.Message = "Could not recognize device command from speech"
		writeJSON(w, http.StatusOK, response)
		return
	}
	if parsed.Confidence < 0.70 {
		response.Success = false
		response.Message = fmt.Sprintf("Confidence too low (%.2f). Action ignored.", parsed.Confidence)
		writeJSON(w, http.StatusOK, response)
		return
	}
	response.Message = fmt.Sprintf("Parsed intent: %s (confidence: %.2f)", parsed.Intent, parsed.Confidence)
	if !shouldExecute || parsed.Intent == IntentQueryStatus {
		writeJSON(w, http.StatusOK, response)
		return
	}

	response.Executed = true
	response.Results = make([]DeviceExecutionResult, 0, len(parsed.DeviceIDs))
	for _, deviceID := range parsed.DeviceIDs {
		device, exists, err := store.GetDevice(r.Context(), deviceID)
		if err != nil || !exists {
			continue
		}
		targetAction := parsed.Action
		if parsed.Intent == IntentToggle {
			if device.State == "ON" {
				targetAction = "OFF"
			} else {
				targetAction = "ON"
			}
		}
		gpio, commandErr := executeESP32Command(r.Context(), deviceID, targetAction, source)
		result := DeviceExecutionResult{
			Device: deviceID, Name: device.Name, GPIO: gpio, State: targetAction, Success: commandErr == nil,
		}
		if commandErr != nil {
			result.Error = commandErr.Error()
			result.State = device.State
		}
		response.Results = append(response.Results, result)
	}

	var successCount int
	for _, res := range response.Results {
		if res.Success {
			successCount++
		}
	}
	if len(response.Results) > 0 {
		if successCount == len(response.Results) {
			response.Success = true
			if len(response.Results) == 1 {
				response.Message = fmt.Sprintf("Turned %s %s (confidence: %.0f%%)", response.Results[0].State, response.Results[0].Name, parsed.Confidence*100)
			} else {
				response.Message = fmt.Sprintf("Executed %s on %d devices (confidence: %.0f%%)", parsed.Intent, successCount, parsed.Confidence*100)
			}
		} else if successCount > 0 {
			response.Success = true
			response.Message = fmt.Sprintf("Partially executed: %d/%d devices responded", successCount, len(response.Results))
		} else {
			response.Success = false
			response.Message = fmt.Sprintf("Hardware unreachable: %s", response.Results[0].Error)
		}
	}
	writeJSON(w, http.StatusOK, response)
}

func main() {
	// Set this to the URL printed or configured for your ESP32.
	esp32URL = "http://192.168.137.174/led"
	if esp32URL == "" {
		log.Fatal("ESP32_URL is missing. Set it to http://<ESP32-IP>/led")
	}
	if !strings.HasPrefix(esp32URL, "http://") && !strings.HasPrefix(esp32URL, "https://") {
		log.Fatal("ESP32_URL must start with http:// or https://")
	}

	var err error
	store, err = openStore(context.Background())
	if err != nil {
		log.Fatalf("Could not initialize SQLite storage: %v", err)
	}
	defer store.Close()
	deviceEvents = newDeviceEventHub()

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/api/devices", devicesHandler)
	mux.HandleFunc("/api/devices/", devicesHandler)
	mux.HandleFunc("/api/led", ledHandler)
	mux.HandleFunc("/api/intent", intentHandler)
	mux.HandleFunc("/api/schedules", schedulesHandler)
	mux.HandleFunc("/api/events", deviceEvents.serveHTTP)

	port := os.Getenv("PORT")
	if port == "" {
		port = "42069"
	}
	if _, err := strconv.Atoi(port); err != nil {
		log.Fatal("PORT must be a valid port number")
	}

	server := &http.Server{
		Addr: net.JoinHostPort("0.0.0.0", port), Handler: corsMiddleware(mux),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	log.Printf("Go device server starting on port %s", port)
	log.Printf("SQLite database: %s", databaseFile)
	log.Printf("ESP32 endpoint: %s", esp32URL)
	log.Printf("Device endpoint: GET http://localhost:%s/api/devices", port)
	log.Printf("LED endpoint: POST http://localhost:%s/api/led", port)
	log.Printf("Intent endpoint: POST http://localhost:%s/api/intent", port)
	log.Printf("Schedules endpoint: GET, POST http://localhost:%s/api/schedules", port)
	log.Printf("Device events WebSocket: ws://localhost:%s/api/events", port)
	schedulerStop := make(chan struct{})
	defer close(schedulerStop)
	go runScheduler(schedulerStop)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
