import { useEffect, useRef, useState } from 'react';
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { useLocalSearchParams } from 'expo-router';

import { getDevice, renameDevice, type Device, type DeviceEvent } from '@/api/led';
import { Colors, MaxContentWidth, Spacing } from '@/constants/theme';
import { useColorScheme } from '@/hooks/use-color-scheme';
import { useDeviceEvents } from '@/hooks/use-device-events';

export default function DeviceDetailsScreen() {
  const { id } = useLocalSearchParams<{ id: string }>();
  const colorScheme = useColorScheme();
  const colors = Colors[colorScheme ?? 'light'];
  const [device, setDevice] = useState<Device | null>(null);
  const [name, setName] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const lastServerName = useRef('');

  useEffect(() => {
    let disposed = false;
    const loadDevice = async () => {
      const deviceID = Number(id);
      if (!Number.isInteger(deviceID)) {
        setError('This device does not exist.');
        setLoading(false);
        return;
      }
      try {
        const result = await getDevice(deviceID);
        if (!disposed) {
          setDevice(result);
          setName((currentName) => {
            if (currentName === '' || currentName === lastServerName.current) {
              lastServerName.current = result.name;
              return result.name;
            }
            return currentName;
          });
        }
      } catch (requestError) {
        if (!disposed) {
          setError(requestError instanceof Error ? requestError.message : 'Could not load device');
        }
      } finally {
        if (!disposed) {
          setLoading(false);
        }
      }
    };
    void loadDevice();
    const refreshInterval = setInterval(() => void loadDevice(), 2000);
    return () => {
      disposed = true;
      clearInterval(refreshInterval);
    };
  }, [id]);

  useDeviceEvents((event: DeviceEvent) => {
    const updatedDevice = event.type === 'device.updated'
      ? event.device
      : event.devices.find((candidate) => candidate.device === Number(id));
    if (!updatedDevice || updatedDevice.device !== Number(id)) return;

    setDevice(updatedDevice);
    setName((currentName) => {
      if (currentName === '' || currentName === lastServerName.current) {
        lastServerName.current = updatedDevice.name;
        return updatedDevice.name;
      }
      return currentName;
    });
  });

  const saveName = async () => {
    if (!device) return;
    setSaving(true);
    setError(null);
    try {
      const result = await renameDevice(device.device, name);
      setDevice(result);
      setName(result.name);
      lastServerName.current = result.name;
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Could not save device name');
    } finally {
      setSaving(false);
    }
  };

  return (
    <SafeAreaView style={[styles.safeArea, { backgroundColor: colors.background }]} edges={['bottom']}>
      <View style={styles.content}>
        {loading ? <ActivityIndicator color={colors.text} /> : null}
        {device ? (
          <>
            <View style={[styles.statusCard, { backgroundColor: colors.backgroundElement }]}>
              <Text style={[styles.deviceName, { color: colors.text }]}>{device.name}</Text>
              <Text style={{ color: device.state === 'ON' ? '#24a148' : colors.textSecondary }}>
                {device.state === 'ON' ? 'Currently on' : 'Currently off'}
              </Text>
            </View>

            <Text style={[styles.sectionTitle, { color: colors.text }]}>Device information</Text>
            <View style={[styles.infoCard, { backgroundColor: colors.backgroundElement }]}>
              <InfoRow label="Custom word" value={device.customWord} color={colors.text} />
              <InfoRow label="Device number" value={String(device.device)} color={colors.text} />
              <InfoRow label="ESP GPIO" value={String(device.gpio)} color={colors.text} />
            </View>

            <Text style={[styles.sectionTitle, { color: colors.text }]}>Device name</Text>
            <TextInput
              value={name}
              onChangeText={setName}
              maxLength={60}
              placeholder="Enter a device name"
              placeholderTextColor={colors.textSecondary}
              style={[styles.input, { backgroundColor: colors.backgroundElement, color: colors.text }]}
            />
            <Pressable
              disabled={saving || !name.trim() || name.trim() === device.name}
              onPress={() => void saveName()}
              style={({ pressed }) => [
                styles.saveButton,
                {
                  backgroundColor: colors.text,
                  opacity: pressed || saving || !name.trim() || name.trim() === device.name ? 0.55 : 1,
                },
              ]}>
              <Text style={[styles.saveButtonText, { color: colors.background }]}>
                {saving ? 'Saving…' : 'Save name'}
              </Text>
            </Pressable>
          </>
        ) : null}
        {error ? <Text style={styles.error}>{error}</Text> : null}
      </View>
    </SafeAreaView>
  );
}

function InfoRow({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <View style={styles.infoRow}>
      <Text style={[styles.infoLabel, { color }]}>{label}</Text>
      <Text style={{ color }}>{value}</Text>
    </View>
  );
}

const styles = StyleSheet.create({
  safeArea: { flex: 1 },
  content: { flex: 1, width: '100%', maxWidth: MaxContentWidth, alignSelf: 'center', padding: Spacing.four },
  statusCard: { padding: Spacing.four, borderRadius: 18, gap: Spacing.one },
  deviceName: { fontSize: 26, fontWeight: '700' },
  sectionTitle: { fontSize: 16, fontWeight: '700', marginTop: Spacing.five, marginBottom: Spacing.two },
  infoCard: { borderRadius: 18, overflow: 'hidden' },
  infoRow: { minHeight: 52, paddingHorizontal: Spacing.three, flexDirection: 'row', alignItems: 'center', justifyContent: 'space-between', borderBottomWidth: StyleSheet.hairlineWidth, borderBottomColor: '#9a9a9a55' },
  infoLabel: { opacity: 0.65 },
  input: { minHeight: 52, borderRadius: 12, paddingHorizontal: Spacing.three, fontSize: 16 },
  saveButton: { minHeight: 52, borderRadius: 12, marginTop: Spacing.two, alignItems: 'center', justifyContent: 'center' },
  saveButtonText: { fontSize: 16, fontWeight: '700' },
  error: { color: '#d12727', marginTop: Spacing.three },
});
