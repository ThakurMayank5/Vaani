import { useCallback, useEffect, useRef, useState } from 'react';
import { Platform } from 'react-native';

export type VoiceAudioPayload = {
  uri?: string;
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
  const mediaRecorderRef = useRef<any>(null);
  const webAudioChunksRef = useRef<Blob[]>([]);
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
      setDurationSeconds((prev) => prev + 1);
    }, 1000);
  }, [stopTimer]);

  // Clean up resources on unmount
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

    // Web Platform Implementation
    if (Platform.OS === 'web') {
      try {
        if (!navigator?.mediaDevices?.getUserMedia) {
          throw new Error('Microphone access is not supported in this browser.');
        }

        const stream = await navigator.mediaDevices.getUserMedia({ audio: true });
        webStreamRef.current = stream;
        webAudioChunksRef.current = [];

        // Check supported MIME types for Whisper compatibility
        let mimeType = 'audio/webm';
        if (typeof MediaRecorder !== 'undefined') {
          if (MediaRecorder.isTypeSupported('audio/webm;codecs=opus')) {
            mimeType = 'audio/webm;codecs=opus';
          } else if (MediaRecorder.isTypeSupported('audio/mp4')) {
            mimeType = 'audio/mp4';
          } else if (MediaRecorder.isTypeSupported('audio/ogg')) {
            mimeType = 'audio/ogg';
          }
        }

        const recorder = new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
        mediaRecorderRef.current = recorder;

        recorder.ondataavailable = (event: BlobEvent) => {
          if (event.data && event.data.size > 0) {
            webAudioChunksRef.current.push(event.data);
          }
        };

        recorder.start(250); // Record in 250ms chunks
        setIsRecording(true);
        startTimer();
      } catch (err) {
        const message = err instanceof Error ? err.message : 'Could not access microphone.';
        setError(message);
        throw new Error(message);
      }
      return;
    }

    // Native Mobile Implementation (iOS & Android)
    try {
      const expoAudio = require('expo-audio');
      const { AudioModule, RecordingPresets, setAudioModeAsync } = expoAudio;

      const permission = await AudioModule.requestRecordingPermissionsAsync();
      if (!permission.granted) {
        throw new Error('Microphone permission was not granted. Please allow access in device settings.');
      }

      await setAudioModeAsync({
        allowsRecording: true,
        playsInSilentMode: true,
      });

      const recorder = new expoAudio.AudioRecorder(RecordingPresets.HIGH_QUALITY);
      nativeRecorderRef.current = recorder;

      await recorder.prepareToRecordAsync();
      recorder.record();

      setIsRecording(true);
      startTimer();
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not start mobile audio recorder.';
      setError(message);
      throw new Error(message);
    }
  }, [startTimer]);

  const stopRecording = useCallback(async (): Promise<VoiceAudioPayload> => {
    stopTimer();
    setIsRecording(false);

    // Web Platform Stop
    if (Platform.OS === 'web') {
      return new Promise<VoiceAudioPayload>((resolve, reject) => {
        const recorder = mediaRecorderRef.current;
        if (!recorder) {
          reject(new Error('No active web recorder found.'));
          return;
        }

        recorder.onstop = () => {
          try {
            const blobType = recorder.mimeType || 'audio/webm';
            const audioBlob = new Blob(webAudioChunksRef.current, { type: blobType });
            const audioUrl = URL.createObjectURL(audioBlob);

            if (webStreamRef.current) {
              webStreamRef.current.getTracks().forEach((track) => track.stop());
              webStreamRef.current = null;
            }
            mediaRecorderRef.current = null;

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

    // Native Mobile Stop
    try {
      const recorder = nativeRecorderRef.current;
      if (!recorder) {
        throw new Error('No active mobile recorder found.');
      }

      await recorder.stop();
      const uri = recorder.uri;
      nativeRecorderRef.current = null;

      if (!uri) {
        throw new Error('Recording ended but no audio file was generated.');
      }

      return { uri };
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not stop audio recording.';
      setError(message);
      throw new Error(message);
    }
  }, [stopTimer]);

  const cancelRecording = useCallback(async () => {
    stopTimer();
    setIsRecording(false);
    setDurationSeconds(0);

    if (Platform.OS === 'web') {
      if (mediaRecorderRef.current && mediaRecorderRef.current.state !== 'inactive') {
        try {
          mediaRecorderRef.current.stop();
        } catch (_) {}
      }
      if (webStreamRef.current) {
        webStreamRef.current.getTracks().forEach((track) => track.stop());
        webStreamRef.current = null;
      }
      mediaRecorderRef.current = null;
      webAudioChunksRef.current = [];
      return;
    }

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
