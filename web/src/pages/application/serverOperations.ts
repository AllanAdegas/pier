/** Application-scoped operations preserve project authorization on every server. */
export function serverOperationPath(applicationId: string, serverId: string, operation: string): string {
  return `/applications/${encodeURIComponent(applicationId)}/${operation}?server_id=${encodeURIComponent(serverId)}`;
}
