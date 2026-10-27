package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

const databaseFile = "vaani.db"

type ControlActionRecord struct {
	ActionType      string
	Source          string
	DeviceID        *int
	RequestedAction string
	PreviousState   string
	ResultingState  string
	Success         bool
	HTTPStatus      int
	ESPHTTPStatus   *int
	Message         string
	StartedAt       time.Time
	CompletedAt     time.Time
}

type VoiceCommandRecord struct {
	Source          string
	Transcript      string
	Intent          string
	Confidence      float64
	ExecuteRequested bool
	Executed        bool
	Outcome         string
	CreatedAt       time.Time
}

type ScheduleInfo struct {
	ID                int      `json:"id"`
	DeviceID          int      `json:"deviceId"`
	DeviceName        string   `json:"deviceName"`
	Action            string   `json:"action"`
	Frequency         string   `json:"frequency"`
	Weekdays          []int    `json:"weekdays"`
	TimeOfDay         string   `json:"timeOfDay,omitempty"`
	ScheduledForUTC   string   `json:"scheduledForUtc,omitempty"`
	TimeZone          string   `json:"timeZone"`
	Active            bool     `json:"active"`
	CreatedAtUTC      string   `json:"createdAtUtc"`
	LastRunAtUTC      string   `json:"lastRunAtUtc,omitempty"`
	LastRunSuccessful *bool    `json:"lastRunSuccessful,omitempty"`
	lastOccurrenceKey string
}

type Store struct {
	db *sql.DB
}

func openStore(ctx context.Context) (*Store, error) {
	db, err := sql.Open("sqlite", databaseFile)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	db.SetMaxOpenConns(1)

	store := &Store{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.seedDevices(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		PRAGMA foreign_keys = ON;
		PRAGMA journal_mode = WAL;

		CREATE TABLE IF NOT EXISTS devices (
			device_id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			custom_word TEXT NOT NULL,
			gpio INTEGER NOT NULL,
			state TEXT NOT NULL CHECK (state IN ('ON', 'OFF')),
			created_at_utc TEXT NOT NULL,
			updated_at_utc TEXT NOT NULL,
			updated_at_unix_ms INTEGER NOT NULL
		);

		CREATE TABLE IF NOT EXISTS control_actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			action_type TEXT NOT NULL,
			source TEXT NOT NULL,
			device_id INTEGER,
			requested_action TEXT NOT NULL,
			previous_state TEXT NOT NULL,
			resulting_state TEXT NOT NULL,
			success INTEGER NOT NULL CHECK (success IN (0, 1)),
			http_status INTEGER NOT NULL,
			esp_http_status INTEGER,
			message TEXT NOT NULL,
			started_at_utc TEXT NOT NULL,
			started_at_unix_ms INTEGER NOT NULL,
			completed_at_utc TEXT NOT NULL,
			completed_at_unix_ms INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			FOREIGN KEY (device_id) REFERENCES devices(device_id)
		);

		CREATE INDEX IF NOT EXISTS idx_control_actions_started_at
			ON control_actions(started_at_unix_ms);
		CREATE INDEX IF NOT EXISTS idx_control_actions_device_id
			ON control_actions(device_id);

		CREATE TABLE IF NOT EXISTS schedules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id INTEGER NOT NULL,
			action TEXT NOT NULL CHECK (action IN ('ON', 'OFF')),
			frequency TEXT NOT NULL CHECK (frequency IN ('ONCE', 'WEEKLY')),
			weekdays TEXT NOT NULL DEFAULT '',
			time_of_day TEXT NOT NULL DEFAULT '',
			scheduled_for_utc TEXT NOT NULL DEFAULT '',
			time_zone TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 1 CHECK (active IN (0, 1)),
			created_at_utc TEXT NOT NULL,
			last_run_at_utc TEXT NOT NULL DEFAULT '',
			last_run_successful INTEGER,
			last_occurrence_key TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (device_id) REFERENCES devices(device_id)
		);
		CREATE INDEX IF NOT EXISTS idx_schedules_active ON schedules(active);

		CREATE TABLE IF NOT EXISTS voice_commands (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			transcript TEXT NOT NULL,
			intent TEXT NOT NULL,
			confidence REAL NOT NULL,
			execute_requested INTEGER NOT NULL CHECK (execute_requested IN (0, 1)),
			executed INTEGER NOT NULL CHECK (executed IN (0, 1)),
			outcome TEXT NOT NULL,
			created_at_utc TEXT NOT NULL,
			created_at_unix_ms INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_voice_commands_created_at ON voice_commands(created_at_unix_ms);
	`)
	if err != nil {
		return fmt.Errorf("migrate SQLite database: %w", err)
	}
	return nil
}

func (s *Store) seedDevices(ctx context.Context) error {
	now := time.Now().UTC()
	for _, device := range defaultDevices {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO devices (
				device_id, name, custom_word, gpio, state,
				created_at_utc, updated_at_utc, updated_at_unix_ms
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(device_id) DO NOTHING`,
			device.Device,
			device.Name,
			device.CustomWord,
			device.GPIO,
			device.State,
			now.Format(time.RFC3339Nano),
			now.Format(time.RFC3339Nano),
			now.UnixMilli(),
		)
		if err != nil {
			return fmt.Errorf("seed device %d: %w", device.Device, err)
		}
	}
	return nil
}

func (s *Store) ListDevices(ctx context.Context) ([]DeviceInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT device_id, name, custom_word, gpio, state, updated_at_utc, updated_at_unix_ms
		FROM devices ORDER BY device_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := make([]DeviceInfo, 0)
	for rows.Next() {
		var device DeviceInfo
		if err := rows.Scan(
			&device.Device,
			&device.Name,
			&device.CustomWord,
			&device.GPIO,
			&device.State,
			&device.UpdatedAtUTC,
			&device.UpdatedAtUnixMS,
		); err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, rows.Err()
}

func (s *Store) GetDevice(ctx context.Context, deviceID int) (DeviceInfo, bool, error) {
	var device DeviceInfo
	err := s.db.QueryRowContext(ctx, `
		SELECT device_id, name, custom_word, gpio, state, updated_at_utc, updated_at_unix_ms
		FROM devices WHERE device_id = ?`, deviceID,
	).Scan(
		&device.Device,
		&device.Name,
		&device.CustomWord,
		&device.GPIO,
		&device.State,
		&device.UpdatedAtUTC,
		&device.UpdatedAtUnixMS,
	)
	if err == sql.ErrNoRows {
		return DeviceInfo{}, false, nil
	}
	if err != nil {
		return DeviceInfo{}, false, err
	}
	return device, true, nil
}

func (s *Store) RenameDevice(ctx context.Context, deviceID int, name string) (DeviceInfo, bool, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE devices SET name = ?, updated_at_utc = ?, updated_at_unix_ms = ?
		WHERE device_id = ?`,
		name,
		now.Format(time.RFC3339Nano),
		now.UnixMilli(),
		deviceID,
	)
	if err != nil {
		return DeviceInfo{}, false, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return DeviceInfo{}, false, err
	}
	if changed == 0 {
		return DeviceInfo{}, false, nil
	}
	return s.GetDevice(ctx, deviceID)
}

func (s *Store) SetDeviceState(ctx context.Context, deviceID int, state string) (DeviceInfo, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		UPDATE devices SET state = ?, updated_at_utc = ?, updated_at_unix_ms = ?
		WHERE device_id = ?`,
		state,
		now.Format(time.RFC3339Nano),
		now.UnixMilli(),
		deviceID,
	)
	if err != nil {
		return DeviceInfo{}, err
	}
	device, exists, err := s.GetDevice(ctx, deviceID)
	if err != nil {
		return DeviceInfo{}, err
	}
	if !exists {
		return DeviceInfo{}, fmt.Errorf("device %d disappeared during state update", deviceID)
	}
	return device, nil
}

func (s *Store) RecordControlAction(ctx context.Context, record ControlActionRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO control_actions (
			action_type, source, device_id, requested_action, previous_state,
			resulting_state, success, http_status, esp_http_status, message,
			started_at_utc, started_at_unix_ms, completed_at_utc,
			completed_at_unix_ms, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ActionType,
		record.Source,
		record.DeviceID,
		record.RequestedAction,
		record.PreviousState,
		record.ResultingState,
		record.Success,
		record.HTTPStatus,
		record.ESPHTTPStatus,
		record.Message,
		record.StartedAt.UTC().Format(time.RFC3339Nano),
		record.StartedAt.UTC().UnixMilli(),
		record.CompletedAt.UTC().Format(time.RFC3339Nano),
		record.CompletedAt.UTC().UnixMilli(),
		record.CompletedAt.Sub(record.StartedAt).Milliseconds(),
	)
	return err
}

func (s *Store) RecordVoiceCommand(ctx context.Context, record VoiceCommandRecord) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO voice_commands (
			source, transcript, intent, confidence, execute_requested, executed,
			outcome, created_at_utc, created_at_unix_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Source,
		record.Transcript,
		record.Intent,
		record.Confidence,
		record.ExecuteRequested,
		record.Executed,
		record.Outcome,
		record.CreatedAt.UTC().Format(time.RFC3339Nano),
		record.CreatedAt.UTC().UnixMilli(),
	)
	return err
}
