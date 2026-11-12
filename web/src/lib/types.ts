// Shapes of the JSON exchanged with the backend. They mirror the Go structs
// one to one (snake_case included) so there is no translation layer.

export interface User {
  id: string;
  email: string;
  name: string;
  created_at: string;
}

export interface AuthStatus {
  needs_setup: boolean;
  authenticated: boolean;
  user: User | null;
}

export type Ability = "read" | "write" | "deploy";

export interface ApiToken {
  id: string;
  name: string;
  abilities: Ability[];
  last_used_at: string | null;
  created_at: string;
  secret?: string;
}

export interface ProxySettings {
  http_port: number;
  https_port: number;
  acme_email: string;
  /** A Caddy that owns ports 80/443 in front of Pier's proxy; empty container = none. */
  edge: EdgeProxy;
}

export interface EdgeProxy {
  container: string;
  caddyfile: string;
}

export interface EdgeSync {
  addresses: string[];
  caddyfile: string;
  upstream: string;
  changed: boolean;
}

export interface ServerCheck {
  code: string;
  status: "pass" | "warning" | "error" | "info";
  message: string;
  fix: string;
}

export interface DomainCheck {
  ok: boolean;
  addresses: string[];
  expected: string[];
  record_hint: string;
}

export interface Server {
  id: string;
  name: string;
  description: string;
  kind: "local" | "ssh";
  host: string;
  port: number;
  user: string;
  private_key_id: string;
  host_key: string;
  data_dir: string;
  proxy: ProxySettings;
  status: "unknown" | "reachable" | "unreachable";
  status_message: string;
  docker_version: string;
  checks?: ServerCheck[];
  last_checked_at: string | null;
  created_at: string;
}

export interface ServerInput {
  name: string;
  description: string;
  host: string;
  port: number;
  user: string;
  private_key_id: string;
  data_dir: string;
  proxy: ProxySettings;
}

export interface ContainerUsage {
  name: string;
  resource_type: string;
  resource_id: string;
  cpu_percent: string;
  memory_usage: string;
  memory_percent: string;
  state: string;
  status: string;
}

export interface ServerMetrics {
  collected_at: string;
  cpu_percent: number | null;
  memory_used_bytes: number | null;
  memory_available_bytes: number | null;
  swap_total_bytes: number | null;
  swap_used_bytes: number | null;
  uptime_seconds: number | null;
  load_average: number[] | null;
  disk_available_bytes: number | null;
  containers_total: number;
  containers_stopped: number;
  containers_paused: number;
  host_error: string;
  disk_error: string;
  operating_system: string;
  architecture: string;
  cpus: number;
  memory_total_bytes: number;
  disk_total_bytes: number;
  disk_used_bytes: number;
  containers_running: number;
  images: number;
  containers: ContainerUsage[];
}

export interface ProxyInfo {
  state: { running: boolean; status: string; image: string };
  configuration: string;
  /** Caddy containers running on the server; the Caddy-in-front setting only shows up when there is one. */
  edge_candidates: string[];
}

export interface PrivateKey {
  id: string;
  name: string;
  description: string;
  public_key: string;
  fingerprint: string;
  created_at: string;
}

export interface Environment {
  protected: boolean;
  frozen: boolean;
  id: string;
  project_id: string;
  name: string;
  created_at: string;
}

export interface Project {
  id: string;
  name: string;
  description: string;
  created_at: string;
  environments: Environment[];
}

export type BuildPack = "dockerfile" | "nixpacks" | "static" | "image";

export interface Storage {
  kind: "volume" | "bind";
  source: string;
  target: string;
}

export interface ApplicationConfig {
  source: {
    repository: string;
    branch: string;
    commit: string;
    private_key_id: string;
    image: string;
    watch_paths: string[];
    tag: string;
    tag_pattern: string;
  };
  build: {
    pack: BuildPack;
    base_directory: string;
    dockerfile_path: string;
    publish_directory: string;
    spa: boolean;
    install_command: string;
    build_command: string;
    start_command: string;
  };
  network: { domains: string[]; port: number; port_mappings: string[] };
  health: {
    enabled: boolean;
    path: string;
    command: string;
    interval_seconds: number;
    timeout_seconds: number;
    retries: number;
    start_period_seconds: number;
  };
  limits: { cpus: string; memory: string };
  hooks: { pre_deploy: string; post_deploy: string };
  release: { command: string };
  storages: Storage[];
  auto_deploy: boolean;
  /** Empty or null means a single "web" process. */
  processes?: Process[] | null;
}

export interface Process {
  name: string;
  command: string;
  replicas: number;
  limits: { cpus: string; memory: string };
}

export interface Application {
  id: string;
  environment_id: string;
  server_id: string;
  name: string;
  description: string;
  config: ApplicationConfig;
  status: string;
  current_image: string;
  created_at: string;
  updated_at: string;
}

export type DeploymentStatus = "queued" | "in_progress" | "finished" | "failed" | "cancelled";

export interface Deployment {
  id: string;
  application_id: string;
  application_name: string;
  status: DeploymentStatus;
  trigger: "manual" | "webhook" | "api" | "rollback" | "archive" | "promotion";
  source: string;
  ref: string;
  promoted_from: string;
  commit_sha: string;
  commit_message: string;
  image: string;
  rollback_image: string;
  error: string;
  hints: { Code: string; Title: string; Fix: string; DocURL: string }[];
  suggested_port?: number;
  retired_logs: { container: string; output: string }[];
  created_at: string;
  started_at: string | null;
  finished_at: string | null;
}

export interface LogEntry {
  id: number;
  level: "info" | "command" | "output" | "error" | "success";
  line: string;
  created_at: string;
}

export interface Container {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  type: string;
  resource_id: string;
  deployment: string;
  /** Application containers only: the process the container runs. */
  process?: string;
}

export interface EnvVariable {
  id: string;
  key: string;
  value: string;
  build_time: boolean;
  secret_build: boolean;
}

export interface WebhookInfo {
  secret: string;
  deploy_url: string;
  github_url: string;
  gitlab_url: string;
  gitea_url: string;
}

export interface ScheduledTask {
  id: string;
  application_id: string;
  name: string;
  command: string;
  cron: string;
  enabled: boolean;
  created_at: string;
}

export interface TaskExecution {
  id: string;
  task_id: string;
  status: string;
  output: string;
  started_at: string;
  finished_at: string | null;
}

export interface EngineInfo {
  engine: string;
  label: string;
  description: string;
  default_image: string;
  port: number;
  uses_account: boolean;
  supports_backup: boolean;
}

export interface DatabaseConfig {
  image: string;
  public_port: number;
  cpus: string;
  memory: string;
  backup: { enabled: boolean; cron: string; retention: number; s3?: boolean; restore_test?: boolean; restore_test_cron?: string };
}

export interface Database {
  id: string;
  environment_id: string;
  server_id: string;
  name: string;
  description: string;
  engine: string;
  config: DatabaseConfig;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface DatabaseConnection {
  host: string;
  port: number;
  credentials: { username: string; password: string; database: string; root_password: string };
  internal_url: string;
  public_url: string;
}

export interface BackupExecution {
  id: string;
  database_id: string;
  trigger: string;
  status: "running" | "succeeded" | "failed";
  file_path: string;
  size_bytes: number;
  message: string;
  started_at: string;
  finished_at: string | null;
}

export interface StackRoute {
  service: string;
  port: number;
  domain: string;
}

export interface Stack {
  id: string;
  environment_id: string;
  server_id: string;
  name: string;
  description: string;
  template: string;
  /** Empty for a stack that runs from a folder of the server. */
  compose: string;
  directory: string;
  compose_file: string;
  project_name: string;
  routes: StackRoute[];
  status: string;
  created_at: string;
  updated_at: string;
}

export interface StackTemplateRoute {
  service: string;
  port: number;
  /** Prefix this service answers under when several share one domain. */
  path: string;
}

/** A variable the compose file of a stack or template interpolates. */
export interface ComposeVariable {
  name: string;
  default: string;
  required: boolean;
  kind: "user" | "generated" | "address";
}

export interface StackVariable extends ComposeVariable {
  /** True when a value will be there on the next deployment. */
  set: boolean;
  /** Where the value comes from; empty when there is none. */
  source: "" | "pier" | "folder" | "address";
}

/** Catalog entry. Listings omit `compose` and `variables`. */
export interface StackTemplate {
  auth?: string;
  host_ports?: string[];
  slug: string;
  name: string;
  description: string;
  category: string;
  website: string;
  tags: string[];
  routes: StackTemplateRoute[];
  /** Deployed and checked by hand; the others are only known to be valid compose files. */
  verified: boolean;
  /** Powers over the server the template asks for (docker-socket, privileged, host-network, host-pid). */
  risks: string[];
  compose?: string;
  variables?: ComposeVariable[];
}

/** The compose file of a stack as it is on the server right now. */
export interface StackSource {
  directory: string;
  compose_file: string;
  content: string;
  env_file: boolean;
}

/** What a folder of a server holds that matters for a compose project. */
export interface FolderListing {
  path: string;
  parent: string;
  directories: string[];
  compose_files: string[];
  env_file: boolean;
  truncated: boolean;
}

export interface OperationSnapshot {
  title: string;
  running: boolean;
  error: string;
  lines: string[];
  started_at: string;
  finished_at: string | null;
}

export interface Resources {
  applications: Application[];
  databases: Database[];
  stacks: Stack[];
}

export interface Dashboard {
  servers: Server[];
  projects: number;
  applications: number;
  databases: number;
  stacks: number;
  running: number;
  unhealthy: number;
  deployments: Deployment[];
}

export type ChannelKind = "discord" | "slack" | "telegram" | "webhook" | "email";

export interface NotificationChannel {
  id: string;
  name: string;
  kind: ChannelKind;
  config: Record<string, string>;
  events: string[];
  enabled: boolean;
  created_at: string;
}

export interface InstanceSettings {
  instance_name: string;
  public_url: string;
  /** Language of deploy logs, notifications and API answers without Accept-Language. */
  language: "pt-BR" | "en";
}

/* ---------- Plain Docker ---------- */

export interface DockerContainer {
  id: string;
  name: string;
  image: string;
  command: string;
  state: string;
  status: string;
  ports: string;
  networks: string;
  created: string;
  managed: boolean;
  owner: { type: string; id: string } | null;
  compose_project: string;
}

export interface DockerImage {
  id: string;
  repository: string;
  tag: string;
  size: string;
  created: string;
}

export interface DockerVolume {
  name: string;
  driver: string;
  scope: string;
}

export interface DockerNetwork {
  id: string;
  name: string;
  driver: string;
  scope: string;
  reserved: boolean;
}

/* ---------- Kubernetes ---------- */

export interface Cluster {
  id: string;
  name: string;
  description: string;
  source: "imported" | "k3s";
  server_id: string;
  k3s: { image: string; api_port: number; http_port: number };
  endpoint: string;
  status: "unknown" | "provisioning" | "ready" | "unreachable";
  status_message: string;
  version: string;
  nodes: number;
  last_checked_at: string | null;
  created_at: string;
}

export interface KubeResource {
  group: string;
  version: string;
  name: string;
  kind: string;
  namespaced: boolean;
  verbs: string[];
}

export interface KubeRow {
  name: string;
  namespace: string;
  created: string;
  status: string;
  health: "" | "ok" | "progressing" | "error";
  info: string;
}

export interface ClusterOverview {
  nodes: KubeRow[];
  namespaces: number;
  pods: number;
  pods_running: number;
  deployments: number;
  events: KubeRow[];
}

export interface KubeApplied {
  kind: string;
  name: string;
  namespace: string;
}
