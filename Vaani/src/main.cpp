#include <Arduino.h>
#include <WiFi.h>
#include <WebServer.h>

// ================= Wi-Fi Configuration =================
const char* WIFI_SSID = "ThakurMayank";
const char* WIFI_PASSWORD = "2c47G4=1";

// ================= LED Configuration ===================
const uint8_t ledPins[] = {18, 19, 21, 22};
const uint8_t NUM_LEDS = 4;

// HTTP server listens on port 80
WebServer server(80);

// -------------------------------------------------------
// Handle POST /led
// Expected raw body:
// device:1
// action:ON
// -------------------------------------------------------
void handleLedCommand() {
    String body = server.arg("plain");

    int device = -1;
    String action = "";

    // Parse one key:value pair per line
    int start = 0;

    while (start < body.length()) {
        int end = body.indexOf('\n', start);

        if (end == -1) {
            end = body.length();
        }

        String line = body.substring(start, end);
        line.trim();

        int separator = line.indexOf(':');

        if (separator != -1) {
            String key = line.substring(0, separator);
            String value = line.substring(separator + 1);

            key.trim();
            value.trim();
            key.toLowerCase();
            value.toUpperCase();

            if (key == "device") {
                device = value.toInt();
            } else if (key == "action") {
                action = value;
            }
        }

        start = end + 1;
    }

    // Validate device number
    if (device < 1 || device > NUM_LEDS) {
        server.send(
            400,
            "text/plain",
            "Invalid device. Use device:1 through device:4"
        );
        return;
    }

    // Validate action
    if (action != "ON" && action != "OFF") {
        server.send(
            400,
            "text/plain",
            "Invalid action. Use action:ON or action:OFF"
        );
        return;
    }

    // Perform the requested action on exactly one LED
    uint8_t pin = ledPins[device - 1];
    bool turnOn = (action == "ON");

    digitalWrite(pin, turnOn ? HIGH : LOW);

    String response = "Success\n";
    response += "Device: " + String(device) + "\n";
    response += "Action: " + action + "\n";
    response += "GPIO: " + String(pin) + "\n";

    Serial.println(response);

    server.send(200, "text/plain", response);
}

// -------------------------------------------------------
// Setup
// -------------------------------------------------------
void setup() {
    Serial.begin(115200);
    delay(500);

    // Configure LEDs; all LEDs start OFF
    for (uint8_t i = 0; i < NUM_LEDS; i++) {
        pinMode(ledPins[i], OUTPUT);
        digitalWrite(ledPins[i], LOW);
    }

    // Connect to Wi-Fi
    WiFi.mode(WIFI_STA);
    WiFi.begin(WIFI_SSID, WIFI_PASSWORD);

    Serial.print("Connecting to Wi-Fi");

    while (WiFi.status() != WL_CONNECTED) {
        delay(500);
        Serial.print(".");
    }

    Serial.println();
    Serial.println("Wi-Fi connected!");
    Serial.print("ESP32 IP address: ");
    Serial.println(WiFi.localIP());

    // Register HTTP endpoint
    server.on("/led", HTTP_POST, handleLedCommand);

    server.on("/", HTTP_GET, []() {
        server.send(
            200,
            "text/plain",
            "ESP32 LED Controller is running. "
            "Send POST requests to /led."
        );
    });

    server.begin();

    Serial.println("HTTP server started on port 80");
    Serial.println("Endpoint: POST http://<ESP32-IP>/led");
}

// -------------------------------------------------------
// Main loop
// -------------------------------------------------------
void loop() {
    server.handleClient();
}