import { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  Modal,
  Pressable,
  ScrollView,
  StyleSheet,
  Switch,
  Text,
  TextInput,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router, usePathname } from 'expo-router';
import {
  controlLED,
  createSchedule,
  executeVoiceIntent,
  getDevices,
  getSchedules,
  transcribeAudio,
  type Device,
  type DeviceEvent,
  type DeviceState,
  type Schedule,
  type ScheduleFrequency,
  type VoiceIntentResponse,
} from '@/api/led';
import { Colors, MaxContentWidth, Spacing } from '@/constants/theme';
import { useColorScheme } from '@/hooks/use-color-scheme';
import { useDeviceEvents } from '@/hooks/use-device-events';
import { useVoiceRecorder } from '@/hooks/use-voice-recorder';

export default function HomeScreen() {
  const colorScheme = useColorScheme();
  const colors = Colors[colorScheme ?? 'light'];
  const pathname = usePathname();
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [updatingDevice, setUpdatingDevice] = useState<number | null>(null);
  const [schedules, setSchedules] = useState<Schedule[]>([]);
  const [schedulerOpen, setSchedulerOpen] = useState(false);
  const [savingSchedule, setSavingSchedule] = useState(false);
  const [scheduleDevice, setScheduleDevice] = useState<number | null>(null);
  const [scheduleAction, setScheduleAction] = useState<DeviceState>('ON');
  const [scheduleFrequency, setScheduleFrequency] = useState<ScheduleFrequency>('ONCE');
  const [scheduleDate, setScheduleDate] = useState(new Date().toISOString().slice(0, 10));
  const [scheduleTime, setScheduleTime] = useState('19:00');
  const [scheduleWeekdays, setScheduleWeekdays] = useState<number[]>([new Date().getDay()]);
  const [voiceStatus, setVoiceStatus] = useState<string | null>(null);
  const [voiceProcessing, setVoiceProcessing] = useState(false);
  const [lastTranscript, setLastTranscript] = useState<string | null>(null);
  const [lastIntentResult, setLastIntentResult] = useState<VoiceIntentResponse | null>(null);
  const [testCommandInput, setTestCommandInput] = useState('');
  const recorder = useVoiceRecorder();

  const loadDevices = useCallback(async () => {
    try {
      setError(null);
      setDevices(await getDevices());
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Could not load devices');
    } finally {
      setLoading(false);
    }
  }, []);

  const loadSchedules = useCallback(async () => {
    try {
      setSchedules(await getSchedules());
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Could not load schedules');
    }
  }, []);

  useEffect(() => {
    void loadDevices();
    const refreshInterval = setInterval(() => void loadDevices(), 2000);
    return () => clearInterval(refreshInterval);
  }, [loadDevices, pathname]);

  useEffect(() => {
    void loadSchedules();
    const refreshInterval = setInterval(() => void loadSchedules(), 5000);
    return () => clearInterval(refreshInterval);
  }, [loadSchedules]);

  useDeviceEvents((event: DeviceEvent) => {
    if (event.type === 'devices.snapshot') {
      setDevices(event.devices);
      return;
    }
    setDevices((currentDevices) =>
      currentDevices.map((device) => (device.device === event.device.device ? event.device : device)),
    );
  });

  const toggleDevice = async (device: Device, nextState: boolean) => {
    setUpdatingDevice(device.device);
    try {
      const result = await controlLED(device.device, nextState ? 'ON' : 'OFF');
      setDevices((currentDevices) =>
        currentDevices.map((currentDevice) =>
          currentDevice.device === device.device
            ? { ...currentDevice, state: result.action }
            : currentDevice,
        ),
      );
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Could not update device');
    } finally {
      setUpdatingDevice(null);
    }
  };

  const openScheduler = () => {
    setScheduleDevice((current) => current ?? devices[0]?.device ?? null);
    setSchedulerOpen(true);
  };

  const saveSchedule = async () => {
    if (scheduleDevice === null) {
      Alert.alert('Choose a device', 'Select the device this schedule should control.');
      return;
    }
    if (!/^\d{2}:\d{2}$/.test(scheduleTime)) {
      Alert.alert('Invalid time', 'Enter the time as HH:MM, for example 19:30.');
      return;
    }
    let scheduledForUtc: string | undefined;
    if (scheduleFrequency === 'ONCE') {
      const localDate = new Date(`${scheduleDate}T${scheduleTime}:00+05:30`);
      if (Number.isNaN(localDate.getTime())) {
        Alert.alert('Invalid date or time', 'Use YYYY-MM-DD for the date and HH:MM for the time.');
        return;
      }
      scheduledForUtc = localDate.toISOString();
    }
    if (scheduleFrequency === 'WEEKLY' && scheduleWeekdays.length === 0) {
      Alert.alert('Choose a weekday', 'Select at least one day for a weekly schedule.');
      return;
    }

    setSavingSchedule(true);
    try {
      const schedule = await createSchedule({
        deviceId: scheduleDevice,
        action: scheduleAction,
        frequency: scheduleFrequency,
        weekdays: scheduleFrequency === 'WEEKLY' ? scheduleWeekdays : [],
        timeOfDay: scheduleFrequency === 'WEEKLY' ? scheduleTime : '',
        scheduledForUtc: scheduledForUtc ?? '',
      });
      setSchedules((currentSchedules) => [schedule, ...currentSchedules]);
      setSchedulerOpen(false);
    } catch (requestError) {
      Alert.alert('Could not save schedule', requestError instanceof Error ? requestError.message : 'Try again.');
    } finally {
      setSavingSchedule(false);
    }
  };

  const startVoiceCommand = async () => {
    try {
      setVoiceStatus('Listening… speak your command now.');
      await recorder.startRecording();
    } catch (voiceError) {
      const msg = voiceError instanceof Error ? voiceError.message : 'Could not start recording.';
      setVoiceStatus(msg);
      Alert.alert('Microphone Error', msg);
    }
  };

  const stopVoiceCommand = async () => {
    setVoiceProcessing(true);
    setVoiceStatus('Transcribing audio with Whisper ASR…');
    try {
      const payload = await recorder.stopRecording();
      const audioTarget = payload.blob || payload.uri;
      if (!audioTarget) {
        throw new Error('Recording ended but no audio was captured.');
      }
      const transcript = await transcribeAudio(audioTarget);
      setLastTranscript(transcript);
      setVoiceStatus(`Heard: “${transcript}” — Parsing intent…`);

      const result = await executeVoiceIntent(transcript);
      setLastIntentResult(result);
      if (result.success) {
        setVoiceStatus(`“${transcript}” → ${result.message}`);
      } else {
        setVoiceStatus(`“${transcript}” — ${result.message}`);
      }
    } catch (voiceError) {
      const message = voiceError instanceof Error ? voiceError.message : 'Voice command failed. Try again.';
      setVoiceStatus(message);
      Alert.alert('Voice Command Error', message);
    } finally {
      setVoiceProcessing(false);
    }
  };

  const executeDirectCommand = async (phrase: string) => {
    const text = phrase.trim();
    if (!text) return;
    setVoiceProcessing(true);
    setLastTranscript(text);
    setVoiceStatus(`Testing intent for: “${text}”…`);
    try {
      const result = await executeVoiceIntent(text);
      setLastIntentResult(result);
      setVoiceStatus(`“${text}” → ${result.message}`);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not execute intent.';
      setVoiceStatus(message);
    } finally {
      setVoiceProcessing(false);
    }
  };

  return (
    <SafeAreaView style={[styles.safeArea, { backgroundColor: colors.background }]}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.heading}>
          <View>
            <View style={styles.eyebrowRow}>
              <View style={[styles.statusDot, { backgroundColor: '#24a148' }]} />
              <Text style={[styles.eyebrow, { color: colors.textSecondary }]}>VAANI HOME</Text>
            </View>
            <Text style={[styles.title, { color: colors.text }]}>Devices</Text>
            <Text style={[styles.subtitle, { color: colors.textSecondary }]}>Control your connected lights.</Text>
          </View>
          <Pressable onPress={() => void loadDevices()} style={styles.refreshButton}>
            <Text style={{ color: colors.textSecondary }}>Refresh</Text>
          </Pressable>
        </View>

        {/* Voice Command Card */}
        <View style={[styles.voiceCard, { backgroundColor: colors.backgroundElement }]}>
          <View style={styles.voiceHeaderRow}>
            <View style={styles.voiceTitleGroup}>
              <View style={styles.voiceBadgeRow}>
                <View
                  style={[
                    styles.voicePulseDot,
                    { backgroundColor: recorder.isRecording ? '#d12727' : '#0f62fe' },
                  ]}
                />
                <Text style={[styles.voiceCardTitle, { color: colors.text }]}>Voice Control</Text>
                {recorder.isRecording ? (
                  <Text style={[styles.recordingTimer, { color: '#d12727' }]}>
                    {`00:0${recorder.durationSeconds}s`}
                  </Text>
                ) : null}
              </View>
              <Text style={[styles.voiceStatusText, { color: colors.textSecondary }]}>
                {voiceProcessing
                  ? 'Transcribing with Whisper ASR & parsing intent…'
                  : recorder.isRecording
                    ? 'Listening… tap Stop when you finish speaking.'
                    : voiceStatus ?? 'Tap Speak to control devices using speech.'}
              </Text>
            </View>

            <Pressable
              disabled={voiceProcessing}
              onPress={() => void (recorder.isRecording ? stopVoiceCommand() : startVoiceCommand())}
              style={[
                styles.voiceButton,
                {
                  backgroundColor: recorder.isRecording ? '#d12727' : colors.text,
                  opacity: voiceProcessing ? 0.6 : 1,
                },
              ]}>
              {voiceProcessing ? (
                <ActivityIndicator color={colors.background} size="small" />
              ) : (
                <Text style={[styles.voiceButtonText, { color: colors.background }]}>
                  {recorder.isRecording ? 'Stop' : 'Speak'}
                </Text>
              )}
            </Pressable>
          </View>

          {/* Transcript & Intent Result Banner */}
          {lastTranscript && lastIntentResult ? (
            <View style={[styles.resultBox, { backgroundColor: colors.backgroundSelected }]}>
              <View style={styles.resultRow}>
                <Text style={[styles.resultLabel, { color: colors.textSecondary }]}>TRANSCRIPT:</Text>
                <Text style={[styles.resultValue, { color: colors.text }]}>“{lastTranscript}”</Text>
              </View>
              <View style={styles.resultRow}>
                <Text style={[styles.resultLabel, { color: colors.textSecondary }]}>INTENT:</Text>
                <Text
                  style={[
                    styles.resultBadge,
                    {
                      color: lastIntentResult.success ? '#24a148' : '#d12727',
                    },
                  ]}>
                  {lastIntentResult.intent?.intent ?? 'UNKNOWN'} ({Math.round((lastIntentResult.intent?.confidence ?? 0) * 100)}% match)
                </Text>
              </View>
              {lastIntentResult.results && lastIntentResult.results.length > 0 ? (
                <View style={styles.resultsList}>
                  {lastIntentResult.results.map((r) => (
                    <Text key={r.device} style={[styles.deviceOutcome, { color: r.success ? '#24a148' : '#d12727' }]}>
                      {r.success ? '✓' : '✗'} {r.name}: {r.state} {r.error ? `(${r.error})` : ''}
                    </Text>
                  ))}
                </View>
              ) : null}
            </View>
          ) : null}

          {/* Quick Voice Command Suggestion Chips */}
          <View style={styles.suggestedContainer}>
            <Text style={[styles.suggestedTitle, { color: colors.textSecondary }]}>QUICK COMMANDS</Text>
            <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.suggestedScroll}>
              {['Turn on Prism', 'Halo off', 'Turn on all lights', 'Everything off', 'Ember toggle', 'Nova on'].map((phrase) => (
                <Pressable
                  key={phrase}
                  disabled={voiceProcessing || recorder.isRecording}
                  onPress={() => void executeDirectCommand(phrase)}
                  style={[styles.suggestedChip, { backgroundColor: colors.backgroundSelected }]}>
                  <Text style={[styles.suggestedChipText, { color: colors.text }]}>{phrase}</Text>
                </Pressable>
              ))}
            </ScrollView>
          </View>

          {/* Direct Text Command Tester */}
          <View style={styles.textCommandRow}>
            <TextInput
              placeholder="Or type a command (e.g. 'desk light on')…"
              placeholderTextColor={colors.textSecondary}
              value={testCommandInput}
              onChangeText={setTestCommandInput}
              onSubmitEditing={() => {
                void executeDirectCommand(testCommandInput);
                setTestCommandInput('');
              }}
              style={[styles.textCommandInput, { backgroundColor: colors.backgroundSelected, color: colors.text }]}
            />
            <Pressable
              disabled={voiceProcessing || !testCommandInput.trim()}
              onPress={() => {
                void executeDirectCommand(testCommandInput);
                setTestCommandInput('');
              }}
              style={[styles.sendButton, { backgroundColor: colors.text, opacity: testCommandInput.trim() ? 1 : 0.4 }]}>
              <Text style={{ color: colors.background, fontWeight: '700' }}>Send</Text>
            </Pressable>
          </View>
        </View>

        {loading ? (
          <ActivityIndicator color={colors.text} style={styles.loader} />
        ) : (
          <View style={styles.list}>
            {devices.map((device) => {
              const isOn = device.state === 'ON';
              const isUpdating = updatingDevice === device.device;
              return (
                <View
                  key={device.device}
                  style={[styles.deviceCard, { backgroundColor: colors.backgroundElement }]}>
                  <Pressable
                    accessibilityRole="button"
                    accessibilityLabel={`Open ${device.name}`}
                    onPress={() => router.push({ pathname: '/device/[id]', params: { id: device.device } })}
                    style={styles.deviceInfo}>
                    <View
                      style={[
                        styles.deviceIcon,
                        { backgroundColor: isOn ? '#f9d66b' : colors.backgroundSelected },
                      ]}>
                      <Text style={{ color: isOn ? '#5d4700' : colors.textSecondary, fontWeight: '700' }}>
                        {device.name.slice(0, 1).toUpperCase()}
                      </Text>
                    </View>
                    <View style={styles.deviceCopy}>
                      <Text style={[styles.deviceName, { color: colors.text }]}>{device.name}</Text>
                      <Text style={{ color: colors.textSecondary }}>
                        {device.customWord} · GPIO {device.gpio}
                      </Text>
                    </View>
                  </Pressable>
                  <View style={styles.toggleColumn}>
                    <Text style={[styles.stateLabel, { color: isOn ? '#24a148' : colors.textSecondary }]}>
                      {isUpdating ? 'SAVING' : device.state}
                    </Text>
                    <Switch
                      value={isOn}
                      disabled={isUpdating}
                      onValueChange={(nextState) => void toggleDevice(device, nextState)}
                      trackColor={{ false: colors.backgroundSelected, true: '#62b879' }}
                    />
                  </View>
                </View>
              );
            })}
          </View>
        )}

        {error ? <Text style={styles.error}>{error}</Text> : null}
        {!loading && devices.length === 0 && !error ? (
          <Text style={{ color: colors.textSecondary }}>No devices were returned by the server.</Text>
        ) : null}

        <View style={styles.scheduleHeader}>
          <View>
            <Text style={[styles.sectionTitle, { color: colors.text }]}>Schedules</Text>
            <Text style={{ color: colors.textSecondary }}>Automate a device action in India time.</Text>
          </View>
          <Pressable onPress={openScheduler} style={[styles.scheduleButton, { backgroundColor: colors.text }]}>
            <Text style={{ color: colors.background, fontWeight: '700' }}>Schedule action</Text>
          </Pressable>
        </View>

        <View style={styles.scheduleList}>
          {schedules.map((schedule) => (
            <View key={schedule.id} style={[styles.scheduleCard, { backgroundColor: colors.backgroundElement }]}>
              <View style={styles.scheduleCopy}>
                <Text style={[styles.deviceName, { color: colors.text }]}>
                  {schedule.deviceName} · {schedule.action}
                </Text>
                <Text style={{ color: colors.textSecondary }}>{formatSchedule(schedule)}</Text>
                {schedule.lastRunAtUtc ? (
                  <Text style={{ color: schedule.lastRunSuccessful ? '#24a148' : '#d12727', fontSize: 12 }}>
                    Last attempt: {schedule.lastRunSuccessful ? 'completed' : 'failed'} · {new Date(schedule.lastRunAtUtc).toLocaleString()}
                  </Text>
                ) : null}
              </View>
              <Text style={{ color: schedule.active ? '#24a148' : colors.textSecondary, fontWeight: '700', fontSize: 12 }}>
                {schedule.active ? 'ACTIVE' : 'DONE'}
              </Text>
            </View>
          ))}
          {schedules.length === 0 ? <Text style={{ color: colors.textSecondary }}>No scheduled actions yet.</Text> : null}
        </View>
      </ScrollView>

      <Modal visible={schedulerOpen} animationType="slide" transparent onRequestClose={() => setSchedulerOpen(false)}>
        <View style={styles.modalBackdrop}>
          <View style={[styles.modalCard, { backgroundColor: colors.background }]}>
            <View style={styles.modalTitleRow}>
              <Text style={[styles.modalTitle, { color: colors.text }]}>Schedule action</Text>
              <Pressable onPress={() => setSchedulerOpen(false)}><Text style={{ color: colors.textSecondary }}>Close</Text></Pressable>
            </View>
            <Text style={[styles.formLabel, { color: colors.textSecondary }]}>DEVICE</Text>
            <View style={styles.choiceRow}>
              {devices.map((device) => <Choice key={device.device} label={device.name} selected={scheduleDevice === device.device} onPress={() => setScheduleDevice(device.device)} colors={colors} />)}
            </View>
            <Text style={[styles.formLabel, { color: colors.textSecondary }]}>ACTION</Text>
            <View style={styles.choiceRow}>
              <Choice label="Turn on" selected={scheduleAction === 'ON'} onPress={() => setScheduleAction('ON')} colors={colors} />
              <Choice label="Turn off" selected={scheduleAction === 'OFF'} onPress={() => setScheduleAction('OFF')} colors={colors} />
            </View>
            <Text style={[styles.formLabel, { color: colors.textSecondary }]}>REPEAT</Text>
            <View style={styles.choiceRow}>
              <Choice label="Once" selected={scheduleFrequency === 'ONCE'} onPress={() => setScheduleFrequency('ONCE')} colors={colors} />
              <Choice label="Every week" selected={scheduleFrequency === 'WEEKLY'} onPress={() => setScheduleFrequency('WEEKLY')} colors={colors} />
            </View>
            {scheduleFrequency === 'WEEKLY' ? <>
              <Text style={[styles.formLabel, { color: colors.textSecondary }]}>WEEKDAYS</Text>
              <View style={styles.choiceRow}>{WEEKDAYS.map((day, index) => <Choice key={day} label={day} selected={scheduleWeekdays.includes(index)} onPress={() => setScheduleWeekdays((current) => current.includes(index) ? current.filter((value) => value !== index) : [...current, index])} colors={colors} />)}</View>
            </> : <>
              <Text style={[styles.formLabel, { color: colors.textSecondary }]}>DATE (YYYY-MM-DD)</Text>
              <TextInput value={scheduleDate} onChangeText={setScheduleDate} style={[styles.input, { backgroundColor: colors.backgroundElement, color: colors.text }]} />
            </>}
            <Text style={[styles.formLabel, { color: colors.textSecondary }]}>TIME — INDIA (HH:MM)</Text>
            <TextInput value={scheduleTime} onChangeText={setScheduleTime} keyboardType="numbers-and-punctuation" style={[styles.input, { backgroundColor: colors.backgroundElement, color: colors.text }]} />
            <Pressable onPress={() => void saveSchedule()} disabled={savingSchedule} style={[styles.createButton, { backgroundColor: colors.text, opacity: savingSchedule ? 0.55 : 1 }]}>
              <Text style={{ color: colors.background, fontWeight: '700' }}>{savingSchedule ? 'Saving…' : 'Save schedule'}</Text>
            </Pressable>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

function Choice({ label, selected, onPress, colors }: { label: string; selected: boolean; onPress: () => void; colors: typeof Colors.light }) {
  return <Pressable onPress={onPress} style={[styles.choice, { backgroundColor: selected ? colors.text : colors.backgroundElement }]}><Text style={{ color: selected ? colors.background : colors.textSecondary, fontWeight: '700' }}>{label}</Text></Pressable>;
}

function formatSchedule(schedule: Schedule) {
  if (schedule.frequency === 'ONCE') return `Once · ${new Date(schedule.scheduledForUtc ?? '').toLocaleString()}`;
  return `Weekly · ${schedule.weekdays.map((day) => WEEKDAYS[day]).join(', ')} · ${schedule.timeOfDay} IST`;
}

const styles = StyleSheet.create({
  safeArea: { flex: 1 },
  content: { width: '100%', maxWidth: MaxContentWidth, alignSelf: 'center', padding: Spacing.four, paddingBottom: Spacing.six },
  heading: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: Spacing.five },
  eyebrowRow: { flexDirection: 'row', alignItems: 'center', gap: Spacing.one },
  eyebrow: { fontSize: 12, letterSpacing: 1, fontWeight: '700' },
  statusDot: { width: 8, height: 8, borderRadius: 4 },
  title: { fontSize: 34, lineHeight: 42, marginTop: Spacing.one, fontWeight: '700' },
  subtitle: { marginTop: Spacing.half },
  refreshButton: { paddingVertical: Spacing.one, paddingHorizontal: Spacing.two },
  loader: { marginTop: Spacing.five },
  list: { gap: Spacing.two },
  deviceCard: { minHeight: 94, borderRadius: 18, flexDirection: 'row', alignItems: 'center', padding: Spacing.three },
  deviceInfo: { flex: 1, flexDirection: 'row', alignItems: 'center', minWidth: 0 },
  deviceIcon: { width: 48, height: 48, borderRadius: 24, justifyContent: 'center', alignItems: 'center', marginRight: Spacing.two },
  deviceCopy: { flex: 1, gap: Spacing.half },
  deviceName: { fontSize: 17, fontWeight: '700' },
  toggleColumn: { alignItems: 'flex-end', gap: Spacing.half, marginLeft: Spacing.two },
  stateLabel: { fontSize: 11, letterSpacing: 0.8, fontWeight: '700' },
  error: { color: '#d12727', marginTop: Spacing.three },
  voiceCard: {
    borderRadius: 18,
    padding: Spacing.three,
    gap: Spacing.two,
    marginBottom: Spacing.four,
  },
  voiceHeaderRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: Spacing.two,
  },
  voiceTitleGroup: {
    flex: 1,
    gap: Spacing.half,
  },
  voiceBadgeRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.one,
  },
  voicePulseDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
  },
  voiceCardTitle: {
    fontSize: 16,
    fontWeight: '700',
  },
  recordingTimer: {
    fontSize: 12,
    fontWeight: '700',
  },
  voiceStatusText: {
    fontSize: 14,
    lineHeight: 18,
  },
  voiceButton: {
    minWidth: 84,
    minHeight: 44,
    borderRadius: 22,
    alignItems: 'center',
    justifyContent: 'center',
    paddingHorizontal: Spacing.three,
  },
  voiceButtonText: {
    fontSize: 15,
    fontWeight: '700',
  },
  resultBox: {
    borderRadius: 12,
    padding: Spacing.two,
    gap: Spacing.one,
  },
  resultRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.one,
  },
  resultLabel: {
    fontSize: 11,
    fontWeight: '700',
    letterSpacing: 0.8,
  },
  resultValue: {
    fontSize: 13,
    fontWeight: '600',
    flex: 1,
  },
  resultBadge: {
    fontSize: 12,
    fontWeight: '700',
  },
  resultsList: {
    marginTop: Spacing.half,
    gap: 2,
  },
  deviceOutcome: {
    fontSize: 12,
    fontWeight: '600',
  },
  suggestedContainer: {
    gap: Spacing.one,
    marginTop: Spacing.half,
  },
  suggestedTitle: {
    fontSize: 11,
    fontWeight: '700',
    letterSpacing: 0.8,
  },
  suggestedScroll: {
    gap: Spacing.one,
    paddingVertical: 2,
  },
  suggestedChip: {
    paddingHorizontal: Spacing.two,
    paddingVertical: 6,
    borderRadius: 8,
  },
  suggestedChipText: {
    fontSize: 12,
    fontWeight: '600',
  },
  textCommandRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: Spacing.one,
    marginTop: Spacing.half,
  },
  textCommandInput: {
    flex: 1,
    minHeight: 40,
    borderRadius: 10,
    paddingHorizontal: Spacing.two,
    fontSize: 14,
  },
  sendButton: {
    minHeight: 40,
    paddingHorizontal: Spacing.two,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  scheduleHeader: { marginTop: Spacing.five, flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', gap: Spacing.two },
  sectionTitle: { fontSize: 21, fontWeight: '700', marginBottom: Spacing.half },
  scheduleButton: { paddingHorizontal: Spacing.three, paddingVertical: Spacing.two, borderRadius: 12 },
  scheduleList: { marginTop: Spacing.three, gap: Spacing.two },
  scheduleCard: { borderRadius: 16, padding: Spacing.three, flexDirection: 'row', justifyContent: 'space-between', gap: Spacing.two },
  scheduleCopy: { flex: 1, gap: Spacing.half },
  modalBackdrop: { flex: 1, justifyContent: 'flex-end', backgroundColor: '#00000066' },
  modalCard: { borderTopLeftRadius: 24, borderTopRightRadius: 24, padding: Spacing.four, gap: Spacing.two },
  modalTitleRow: { flexDirection: 'row', justifyContent: 'space-between', alignItems: 'center', marginBottom: Spacing.one },
  modalTitle: { fontSize: 24, fontWeight: '700' },
  formLabel: { fontSize: 11, fontWeight: '700', letterSpacing: 0.8, marginTop: Spacing.one },
  choiceRow: { flexDirection: 'row', flexWrap: 'wrap', gap: Spacing.one },
  choice: { paddingHorizontal: Spacing.two, paddingVertical: Spacing.one, borderRadius: 10 },
  input: { minHeight: 48, borderRadius: 12, paddingHorizontal: Spacing.three, fontSize: 16 },
  createButton: { minHeight: 52, borderRadius: 12, alignItems: 'center', justifyContent: 'center', marginTop: Spacing.two },
});
