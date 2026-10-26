import { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Switch,
  Text,
  View,
} from 'react-native';
import { SafeAreaView } from 'react-native-safe-area-context';
import { router, usePathname } from 'expo-router';

import { controlLED, getDevices, type Device } from '@/api/led';
import { Colors, MaxContentWidth, Spacing } from '@/constants/theme';
import { useColorScheme } from '@/hooks/use-color-scheme';

export default function HomeScreen() {
  const colorScheme = useColorScheme();
  const colors = Colors[colorScheme ?? 'light'];
  const pathname = usePathname();
  const [devices, setDevices] = useState<Device[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [updatingDevice, setUpdatingDevice] = useState<number | null>(null);

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

  useEffect(() => {
    void loadDevices();
  }, [loadDevices, pathname]);

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

  return (
    <SafeAreaView style={[styles.safeArea, { backgroundColor: colors.background }]}>
      <View style={styles.content}>
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
      </View>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  safeArea: { flex: 1 },
  content: { flex: 1, width: '100%', maxWidth: MaxContentWidth, alignSelf: 'center', padding: Spacing.four },
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
});
