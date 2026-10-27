import { useCallback, useEffect, useRef, useState } from 'react';
import { Platform } from 'react-native';

export type VoiceAudioPayload = {
  uri: string;
  blob?: Blob;
};

export type VoiceRecorderHook = {
  isRecording: boolean;
  durationSeconds: number;
  startRecording: () => Promise<void>;
  stopRecording: () => Promise<VoiceAudioPayload>;
  cancelRecording: () => Promise<void>;
  error: string | null;
};

export function useVoiceRecorder(): VoiceRecorderHook {
  const [isRecording, setIsRecording] = useState(false);
  const [durationSeconds, setDurationSeconds] = useState(0);
  const [error, setError] = useState<string | null>(null);

  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const webRecorderRef = useRef<any>(null);
  const webChunksRef = useRef<Blob[]>([]);
  const webStreamRef = useRef<MediaStream | null>(null);
  const nativeRecorderRef = useRef<any>(null);

  const stopTimer = useCallback(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const startTimer = useCallback(() => {
    stopTimer();
    setDurationSeconds(0);
    timerRef.current = setInterval(() => {
      setDurationSeconds((d) => d + 1);
    }, 1000);
  }, [stopTimer]);

  useEffect(() => {
    return () => {
      stopTimer();
      if (webStreamRef.current) {
        webStreamRef.current.getTracks().forEach((track) => track.stop());
        webStreamRef.current = null;
      }
    };
  }, [stopTimer]);

  const startRecording = useCallback(async () => {
    setError(null);

    // 1. Web Platform (Browser)
    if (Platform.OS === 'web' || (typeof window !== 'undefined' && !!navigator?.mediaDevices?.getUserMedia)) {
      try {
        if (!navigator?.mediaDevices?.getUserMedia) {
          throw new Error('Microphone access is not supported in this browser.');
        }

        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        webStreamRef.current = stream;
        webChunksRef.current = [];

        let mimeType = 'audio/webm';
        if (typeof MediaRecorder !== 'undefined') {
          if (MediaRecorder.isTypeSupported('audio/webm;codecs=opus')) {
            mimeType = 'audio/webm;codecs=opus';
          } else if (MediaRecorder.isTypeSupported('audio/webm')) {
            mimeType = 'audio/webm';
          } else if (MediaRecorder.isTypeSupported('audio/mp4')) {
            mimeType = 'audio/mp4';
          } else if (MediaRecorder.isTypeSupported('audio/ogg')) {
            mimeType = 'audio/ogg';
          }
        }

        const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
        webRecorderRef.current = recorder;

        recorder.ondataavailable = (event: BlobEvent) => {
          if (event.data && event.data.size > 0) {
            webChunksRef.current.push(event.data);
          }
        };

        recorder.start(250);
        setIsRecording(true);
        startTimer();
        return;
      } catch (webErr) {
        const message = webErr instanceof Error ? webErr.message : 'Could not access browser microphone.';
        setError(message);
        throw new Error(message);
      }
    }

    // 2. Mobile Native Platform (iOS / Android)
    try {
      let expoAudio: any;
      try {
        expoAudio = require('expo-audio');
      } catch {
        throw new Error('expo-audio package is not available in this environment.');
      }

      const { AudioModule, RecordingPresets, setAudioModeAsync } = expoAudio;
      if (!AudioModule) {
        throw new Error('AudioModule is not available in this build. Please test in a web browser or development build.');
      }

      const permission = await AudioModule.requestRecordingPermissionsAsync();
      if (!permission.granted) {
        throw new Error('Microphone permission denied. Please allow microphone access in device settings.');
      }

      await setAudioModeAsync({
        allowsRecording: true,
        playsInSilentMode: true,
      });

      // Check if native AudioRecorder is supported
      if (typeof AudioModule.AudioRecorder !== 'function') {
        throw new Error(
          'expo-audio native recording is not available in standard Expo Go. Please run via web browser (npm run web) or build a development client (npx expo run:android).'
        );
      }

      const recorder = new AudioModule.AudioRecorder(RecordingPresets.HIGH_QUALITY);
      nativeRecorderRef.current = recorder;

      await recorder.prepareToRecordAsync();
      recorder.record();

      setIsRecording(true);
      startTimer();
    } catch (nativeErr) {
      const message = nativeErr instanceof Error ? nativeErr.message : 'Could not start mobile audio recorder.';
      setError(message);
      throw new Error(message);
    }
  }, [startTimer]);

  const stopRecording = useCallback(async (): Promise<VoiceAudioPayload> => {
    stopTimer();
    setIsRecording(false);

    // 1. Web Platform Stop
    if (Platform.OS === 'web' || webRecorderRef.current) {
      return new Promise<VoiceAudioPayload>((resolve, reject) => {
        const recorder = webRecorderRef.current;
        if (!recorder) {
          reject(new Error('No active web recorder found.'));
          return;
        }

        recorder.onstop = () => {
          try {
            const blobType = recorder.mimeType || 'audio/webm';
            const audioBlob = new Blob(webChunksRef.current, { type: blobType });
            const audioUrl = URL.createObjectURL(audioBlob);

            if (webStreamRef.current) {
              webStreamRef.current.getTracks().forEach((track) => track.stop());
              webStreamRef.current = null;
            }
            webRecorderRef.current = null;

            resolve({ uri: audioUrl, blob: audioBlob });
          } catch (err) {
            reject(err);
          }
        };

        try {
          recorder.stop();
        } catch (err) {
          reject(err);
        }
      });
    }

    // 2. Native Mobile Stop
    try {
      const recorder = nativeRecorderRef.current;
      if (!recorder) {
        throw new Error('No active mobile recorder found.');
      }

      await recorder.stop();
      const uri = recorder.uri;
      nativeRecorderRef.current = null;

      if (!uri) {
        throw new Error('Recording ended, but audio data is unavailable.');
      }

      return { uri };
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not stop recording.';
      setError(message);
      throw new Error(message);
    }
  }, [stopTimer]);

  const cancelRecording = useCallback(async () => {
    stopTimer();
    setIsRecording(false);
    setDurationSeconds(0);

    if (webRecorderRef.current && webRecorderRef.current.state !== 'inactive') {
      try {
        webRecorderRef.current.stop();
      } catch (_) {}
    }
    if (webStreamRef.current) {
      webStreamRef.current.getTracks().forEach((track) => track.stop());
      webStreamRef.current = null;
    }
    webRecorderRef.current = null;
    webChunksRef.current = [];

    if (nativeRecorderRef.current) {
      try {
        await nativeRecorderRef.current.stop();
      } catch (_) {}
      nativeRecorderRef.current = null;
    }
  }, [stopTimer]);

  return {
    isRecording,
    durationSeconds,
    startRecording,
    stopRecording,
    cancelRecording,
    error,
  };
}
