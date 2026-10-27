const API_BASE_URL = 'http://192.168.137.1:42069';

export type DeviceState = 'ON' | 'OFF';

export type Device = {
  device: number;
  name: string;
  customWord: string;
  gpio: number;
  state: DeviceState;
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
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name }),
  });
  const result = await readJSON<{ success: boolean; device: Device }>(response);
  return result.device;
}

export async function controlLED(device: number, action: DeviceState): Promise<APIResponse> {
  const response = await fetch(`${API_BASE_URL}/api/led`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ device, action }),
  });
  return readJSON<APIResponse>(response);
}
