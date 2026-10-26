import { controlLED } from "@/api/led";
import { Alert, Button, View } from "react-native";

export default function LEDScreen() {
  async function setLED(
    device: number,
    action: "ON" | "OFF"
  ) {
    try {
      const result = await controlLED(device, action);
      Alert.alert("Success", result.message);
    } catch (error) {
      Alert.alert(
        "Request failed",
        error instanceof Error ? error.message : "Unknown error"
      );
    }
  }

  return (
    <View style={{ padding: 20, gap: 12 }}>
      {[1, 2, 3, 4].map((device) => (
        <View key={device} style={{ gap: 8 }}>
          <Button
            title={`LED ${device} ON`}
            onPress={() => setLED(device, "ON")}
          />
          <Button
            title={`LED ${device} OFF`}
            onPress={() => setLED(device, "OFF")}
          />
        </View>
      ))}
    </View>
  );
}