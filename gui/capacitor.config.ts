import type { CapacitorConfig } from "@capacitor/cli";

const config: CapacitorConfig = {
  appId: "com.opengui.app",
  appName: "PrAImate GUI",
  webDir: "dist",
  server: {
    cleartext: true,
    androidScheme: "http",
  },
};

export default config;
