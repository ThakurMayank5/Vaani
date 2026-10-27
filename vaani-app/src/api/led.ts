import { Platform } from 'react-native';

export const API_BASE_URL = 'http://192.168.137.1:42069';
export const DEVICE_EVENTS_URL = `${API_BASE_URL.replace(/^http/, 'ws')}/api/events`;

// Change this if Whisper runs on a different computer on your local network.
export const ASR_BASE_URL = 'http://192.168.137.1:43001';

export type DeviceState = 'ON' | 'OFF';

export type Device = {
  device: number;
  name: string;
  customWord: string;
  gpio: number;
  state: DeviceState;
  updatedAtUtc: string;
  updatedAtUnixMs: number;
};

export type DeviceEvent =
  | { type: 'devices.snapshot'; devices: Device[] }
  | { type: 'device.updated'; device: Device };

export type ScheduleFrequency = 'ONCE' | 'WEEKLY';

export type Schedule = {
  id: number;
  deviceId: number;
  deviceName: string;
  action: DeviceState;
  frequency: ScheduleFrequency;
  weekdays: number[];
  timeOfDay?: string;
  scheduledForUtc?: string;
  timeZone: string;
  active: boolean;
  createdAtUtc: string;
  lastRunAtUtc?: string;
  lastRunSuccessful?: boolean;
};

export type CreateSchedule = {
  deviceId: number;
  action: DeviceState;
  frequency: ScheduleFrequency;
  weekdays?: number[];
  timeOfDay?: string;
  scheduledForUtc?: string;
};

export type VoiceIntentResponse = {
  success: boolean;
  message: string;
  intent: { intent: string; confidence: number; rawText: string };
  executed: boolean;
  results?: Array<{ device: number; name: string; state: DeviceState; success: boolean; error?: string }>;
};

type APIResponse = {
  success: boolean;
  message: string;
  device: number;
  action: DeviceState;
  gpio: number;
};

async function readJSON<T>(response: Response): Promise<T> {
  const result = (await response.json()) as T & { message?: string; success?: boolean };
  if (!response.ok || result.success === false) {
    throw new Error(result.message || 'Server request failed');
  }
  return result;
}

export async function getDevices(): Promise<Device[]> {
  const response = await fetch(`${API_BASE_URL}/api/devices`);
  const result = await readJSON<{ success: boolean; devices: Device[] }>(response);
  return result.devices;
}

export async function getDevice(device: number): Promise<Device> {
  const response = await fetch(`${API_BASE_URL}/api/devices/${device}`);
  const result = await readJSON<{ success: boolean; device: Device }>(response);
  return result.device;
}

export async function renameDevice(device: number, name: string): Promise<Device> {
  const response = await fetch(`${API_BASE_URL}/api/devices/${device}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json', 'X-Control-Source': 'mobile_app' },
    body: JSON.stringify({ name }),
  });
  const result = await readJSON<{ success: boolean; device: Device }>(response);
  return result.device;
}

export async function controlLED(device: number, action: DeviceState): Promise<APIResponse> {
  const response = await fetch(`${API_BASE_URL}/api/led`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Control-Source': 'mobile_app' },
    body: JSON.stringify({ device, action }),
  });
  return readJSON<APIResponse>(response);
}

export async function getSchedules(): Promise<Schedule[]> {
  const response = await fetch(`${API_BASE_URL}/api/schedules`);
  const result = await readJSON<{ success: boolean; schedules: Schedule[] }>(response);
  return result.schedules;
}

export async function createSchedule(schedule: CreateSchedule): Promise<Schedule> {
  const response = await fetch(`${API_BASE_URL}/api/schedules`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Control-Source': 'mobile_app' },
    body: JSON.stringify(schedule),
  });
  const result = await readJSON<{ success: boolean; schedule: Schedule }>(response);
  return result.schedule;
}

export async function transcribeAudio(audioData: string | Blob): Promise<string> {
  // 1. Web Platform or raw Blob
  if (Platform.OS === 'web' || audioData instanceof Blob) {
    const body = new FormData();
    let blob: Blob;
    if (typeof audioData === 'string') {
      const response = await fetch(audioData);
      blob = await response.blob();
    } else {
      blob = audioData;
    }
    body.append('audio', blob, 'vaani-command.webm');

    const response = await fetch(`${ASR_BASE_URL}/transcribe`, { method: 'POST', body });
    const result = await readJSON<{ success: boolean; transcript: string }>(response);
    if (!result.transcript || !result.transcript.trim()) {
      throw new Error('No speech was detected. Please try again.');
    }
    return result.transcript.trim();
  }

  // 2. Mobile Native Platform (iOS & Android)
  // Use expo-file-system/legacy uploadAsync to avoid React Native/Expo winter FormData Part error
  try {
    const { uploadAsync, FileSystemUploadType } = require('expo-file-system/legacy');
    const response = await uploadAsync(`${ASR_BASE_URL}/transcribe`, audioData as string, {
      fieldName: 'audio',
      httpMethod: 'POST',
      uploadType: FileSystemUploadType.MULTIPART,
      mimeType: 'audio/m4a',
    });

    if (response.status < 200 || response.status >= 300) {
      throw new Error(`ASR server returned HTTP ${response.status}: ${response.body}`);
    }

    const result = JSON.parse(response.body) as { success: boolean; transcript?: string; message?: string };
    if (!result.transcript || !result.transcript.trim()) {
      throw new Error('No speech was detected. Please speak closer to the microphone.');
    }
    return result.transcript.trim();
  } catch (nativeErr) {
    // If native upload fails, try fetching local URI as Blob
    try {
      const res = await fetch(audioData as string);
      const fileBlob = await res.blob();
      const body = new FormData();
      body.append('audio', fileBlob, 'vaani-command.m4a');
      const response = await fetch(`${ASR_BASE_URL}/transcribe`, { method: 'POST', body });
      const result = await readJSON<{ success: boolean; transcript: string }>(response);
      if (!result.transcript || !result.transcript.trim()) {
        throw new Error('No speech was detected. Please try again.');
      }
      return result.transcript.trim();
    } catch {
      throw nativeErr instanceof Error ? nativeErr : new Error(String(nativeErr));
    }
  }
}

export async function executeVoiceIntent(transcript: string): Promise<VoiceIntentResponse> {
  const response = await fetch(`${API_BASE_URL}/api/intent`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Control-Source': 'voice_app' },
    body: JSON.stringify({ text: transcript, execute: true }),
  });
  return readJSON<VoiceIntentResponse>(response);
}
