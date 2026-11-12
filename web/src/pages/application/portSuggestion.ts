import type { ApplicationConfig } from "../../lib/types";

/** Keep the latest editable config and change only the diagnosed runtime port. */
export function withSuggestedPort(config: ApplicationConfig, port: number): ApplicationConfig {
  if (!Number.isInteger(port) || port < 1 || port > 65535) throw new RangeError("Invalid port");
  return { ...config, network: { ...config.network, port } };
}
