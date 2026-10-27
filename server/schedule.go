package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

const scheduleTimeZone = "Asia/Kolkata"

type CreateScheduleRequest struct {
	DeviceID        int   `json:"deviceId"`
	Action          string `json:"action"`
	Frequency       string `json:"frequency"`
	Weekdays        []int `json:"weekdays"`
	TimeOfDay       string `json:"timeOfDay"`
	ScheduledForUTC string `json:"scheduledForUtc"`
}

type SchedulesResponse struct {
	Success   bool           `json:"success"`
	Schedules []ScheduleInfo `json:"schedules"`
}

type ScheduleResponse struct {
	Success  bool         `json:"success"`
	Schedule ScheduleInfo `json:"schedule"`
}

func weekdaysToString(days []int) string {
	sort.Ints(days)
	parts := make([]string, len(days))
	for index, day := range days {
		parts[index] = strconv.Itoa(day)
	}
	return strings.Join(parts, ",")
}

func weekdaysFromString(value string) []int {
	if value == "" {
		return []int{}
	}
	parts := strings.Split(value, ",")
	days := make([]int, 0, len(parts))
	for _, part := range parts {
		day, err := strconv.Atoi(part)
		if err == nil {
			days = append(days, day)
		}
	}
	return days
}

func scanSchedule(scanner interface{ Scan(...any) error }) (ScheduleInfo, error) {
	var schedule ScheduleInfo
	var weekdays string
	var active int
	var lastRunSuccessful sql.NullBool
	err := scanner.Scan(
		&schedule.ID, &schedule.DeviceID, &schedule.DeviceName, &schedule.Action,
		&schedule.Frequency, &weekdays, &schedule.TimeOfDay, &schedule.ScheduledForUTC,
		&schedule.TimeZone, &active, &schedule.CreatedAtUTC, &schedule.LastRunAtUTC,
		&lastRunSuccessful, &schedule.lastOccurrenceKey,
	)
	if err != nil {
		return ScheduleInfo{}, err
	}
	schedule.Weekdays = weekdaysFromString(weekdays)
	schedule.Active = active == 1
	if lastRunSuccessful.Valid {
		value := lastRunSuccessful.Bool
		schedule.LastRunSuccessful = &value
	}
	return schedule, nil
}

func (s *Store) CreateSchedule(ctx context.Context, request CreateScheduleRequest) (ScheduleInfo, error) {
	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO schedules (
			device_id, action, frequency, weekdays, time_of_day, scheduled_for_utc,
			time_zone, active, created_at_utc
		) VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?)`,
		request.DeviceID, request.Action, request.Frequency, weekdaysToString(request.Weekdays),
		request.TimeOfDay, request.ScheduledForUTC, scheduleTimeZone, now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return ScheduleInfo{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ScheduleInfo{}, err
	}
	return s.GetSchedule(ctx, int(id))
}

func (s *Store) GetSchedule(ctx context.Context, id int) (ScheduleInfo, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT s.id, s.device_id, d.name, s.action, s.frequency, s.weekdays,
			s.time_of_day, s.scheduled_for_utc, s.time_zone, s.active,
			s.created_at_utc, s.last_run_at_utc, s.last_run_successful, s.last_occurrence_key
		FROM schedules s JOIN devices d ON d.device_id = s.device_id WHERE s.id = ?`, id)
	return scanSchedule(row)
}

func (s *Store) ListSchedules(ctx context.Context) ([]ScheduleInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.id, s.device_id, d.name, s.action, s.frequency, s.weekdays,
			s.time_of_day, s.scheduled_for_utc, s.time_zone, s.active,
			s.created_at_utc, s.last_run_at_utc, s.last_run_successful, s.last_occurrence_key
		FROM schedules s JOIN devices d ON d.device_id = s.device_id
		ORDER BY s.active DESC, s.frequency, s.scheduled_for_utc, s.time_of_day, s.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	schedules := make([]ScheduleInfo, 0)
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, schedule)
	}
	return schedules, rows.Err()
}

func (s *Store) dueSchedules(ctx context.Context, now time.Time) ([]ScheduleInfo, error) {
	schedules, err := s.ListSchedules(ctx)
	if err != nil {
		return nil, err
	}
	location, err := time.LoadLocation(scheduleTimeZone)
	if err != nil {
		return nil, err
	}
	localNow := now.In(location)
	currentTime := localNow.Format("15:04")
	currentDay := int(localNow.Weekday())
	due := make([]ScheduleInfo, 0)
	for _, schedule := range schedules {
		if !schedule.Active {
			continue
		}
		if schedule.Frequency == "ONCE" {
			scheduledAt, parseErr := time.Parse(time.RFC3339Nano, schedule.ScheduledForUTC)
			if parseErr == nil && !now.Before(scheduledAt) && schedule.LastRunAtUTC == "" {
				due = append(due, schedule)
			}
			continue
		}
		if schedule.Frequency == "WEEKLY" && schedule.TimeOfDay == currentTime {
			occurrenceKey := localNow.Format("2006-01-02 15:04")
			if schedule.lastOccurrenceKey == occurrenceKey {
				continue
			}
			for _, day := range schedule.Weekdays {
				if day == currentDay {
					due = append(due, schedule)
					break
				}
			}
		}
	}
	return due, nil
}

func (s *Store) MarkScheduleAttempt(ctx context.Context, schedule ScheduleInfo, attemptedAt time.Time, success bool) error {
	active := 1
	if schedule.Frequency == "ONCE" {
		active = 0
	}
	location, _ := time.LoadLocation(scheduleTimeZone)
	_, err := s.db.ExecContext(ctx, `
		UPDATE schedules SET active = ?, last_run_at_utc = ?, last_run_successful = ?,
			last_occurrence_key = ? WHERE id = ?`,
		active, attemptedAt.UTC().Format(time.RFC3339Nano), success,
		attemptedAt.In(location).Format("2006-01-02 15:04"), schedule.ID,
	)
	return err
}

func validateSchedule(request *CreateScheduleRequest) error {
	request.Action = strings.ToUpper(strings.TrimSpace(request.Action))
	request.Frequency = strings.ToUpper(strings.TrimSpace(request.Frequency))
	request.TimeOfDay = strings.TrimSpace(request.TimeOfDay)
	if request.DeviceID < 1 || (request.Action != "ON" && request.Action != "OFF") {
		return fmt.Errorf("choose a valid device and ON or OFF action")
	}
	switch request.Frequency {
	case "ONCE":
		scheduledAt, err := time.Parse(time.RFC3339Nano, request.ScheduledForUTC)
		if err != nil {
			return fmt.Errorf("one-time schedules require a valid UTC date and time")
		}
		if !scheduledAt.After(time.Now().UTC()) {
			return fmt.Errorf("one-time schedule must be in the future")
		}
	case "WEEKLY":
		if len(request.Weekdays) == 0 {
			return fmt.Errorf("weekly schedules require at least one weekday")
		}
		for _, day := range request.Weekdays {
			if day < 0 || day > 6 {
				return fmt.Errorf("weekday must be between 0 (Sunday) and 6 (Saturday)")
			}
		}
		if _, err := time.Parse("15:04", request.TimeOfDay); err != nil {
			return fmt.Errorf("weekly schedules require time in HH:MM format")
		}
	default:
		return fmt.Errorf("frequency must be ONCE or WEEKLY")
	}
	return nil
}

func schedulesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		schedules, err := store.ListSchedules(r.Context())
		if err != nil {
			log.Printf("Could not list schedules: %v", err)
			writeJSON(w, http.StatusInternalServerError, APIResponse{Message: "Could not load schedules"})
			return
		}
		writeJSON(w, http.StatusOK, SchedulesResponse{Success: true, Schedules: schedules})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		writeJSON(w, http.StatusMethodNotAllowed, APIResponse{Message: "Use GET or POST for schedules"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request CreateScheduleRequest
	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Invalid schedule JSON"})
		return
	}
	if err := validateSchedule(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: err.Error()})
		return
	}
	if _, exists, err := store.GetDevice(r.Context(), request.DeviceID); err != nil || !exists {
		writeJSON(w, http.StatusBadRequest, APIResponse{Message: "Choose a configured device"})
		return
	}
	schedule, err := store.CreateSchedule(r.Context(), request)
	if err != nil {
		log.Printf("Could not create schedule: %v", err)
		writeJSON(w, http.StatusInternalServerError, APIResponse{Message: "Could not save schedule"})
		return
	}
	writeJSON(w, http.StatusCreated, ScheduleResponse{Success: true, Schedule: schedule})
}

func runScheduler(stop <-chan struct{}) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-ticker.C:
			due, err := store.dueSchedules(context.Background(), now.UTC())
			if err != nil {
				log.Printf("Could not find due schedules: %v", err)
				continue
			}
			for _, schedule := range due {
				_, err := executeESP32Command(context.Background(), schedule.DeviceID, schedule.Action, fmt.Sprintf("scheduler:%d", schedule.ID))
				if markErr := store.MarkScheduleAttempt(context.Background(), schedule, now.UTC(), err == nil); markErr != nil {
					log.Printf("Could not mark schedule %d attempt: %v", schedule.ID, markErr)
				}
			}
		}
	}
}
