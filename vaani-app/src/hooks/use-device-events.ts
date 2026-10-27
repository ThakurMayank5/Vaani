import { useEffect, useRef } from 'react';

import { DEVICE_EVENTS_URL, type DeviceEvent } from '@/api/led';

/** Keeps the active screen synchronized with server-side device updates. */
export function useDeviceEvents(onEvent: (event: DeviceEvent) => void) {
  const eventHandler = useRef(onEvent);
  eventHandler.current = onEvent;

  useEffect(() => {
    let socket: WebSocket | null = null;
    let reconnectTimer: ReturnType<typeof setTimeout> | null = null;
    let stopped = false;

    const connect = () => {
      if (stopped) return;
      socket = new WebSocket(DEVICE_EVENTS_URL);
      socket.onmessage = (message) => {
        try {
          eventHandler.current(JSON.parse(String(message.data)) as DeviceEvent);
        } catch {
          // Ignore malformed events and wait for the next valid server update.
        }
      };
      socket.onerror = () => socket?.close();
      socket.onclose = () => {
        if (!stopped) {
          reconnectTimer = setTimeout(connect, 2000);
        }
      };
    };

    connect();
    return () => {
      stopped = true;
      if (reconnectTimer) clearTimeout(reconnectTimer);
      socket?.close();
    };
  }, []);
}
