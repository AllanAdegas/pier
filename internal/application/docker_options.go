package application

import (
	"net/netip"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"pier/internal/compose"
	"pier/internal/platform/apperr"
)

// Ulimit is one resource limit of the container (soft ≤ hard; -1 is unlimited).
type Ulimit struct {
	Name string `json:"name"`
	Soft int64  `json:"soft"`
	Hard int64  `json:"hard"`
}

// DockerOptions are the extra container settings an application may ask for.
// Every field is validated against an allowlist; the options that give the
// container power over the server (DangerousDockerOptions) also need the
// instance policy (DockerOptionsPolicy) to allow them.
type DockerOptions struct {
	ExtraHosts  []string          `json:"extra_hosts"`
	CapAdd      []string          `json:"cap_add"`
	CapDrop     []string          `json:"cap_drop"`
	Ulimits     []Ulimit          `json:"ulimits"`
	ShmSize     string            `json:"shm_size"`
	Labels      map[string]string `json:"labels"`
	Devices     []string          `json:"devices"`
	Privileged  bool              `json:"privileged"`
	NetworkMode string            `json:"network_mode"`
	PidMode     string            `json:"pid_mode"`
}

const (
	maxDockerOptionItems = 50
	maxLabelValueLength  = 4096
	hostMode             = "host"
)

var (
	hostAliasPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,62})(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}))*$`)
	labelKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
	devicePattern    = regexp.MustCompile(`^/dev/[A-Za-z0-9._/-]+$`)
	reservedLabels   = []string{"pier.", "traefik.", "com.docker."}
	// safeCapabilities only act inside the container's own namespaces.
	safeCapabilities = []string{"AUDIT_WRITE", "CHOWN", "DAC_OVERRIDE", "FOWNER", "FSETID", "IPC_LOCK", "KILL",
		"MKNOD", "NET_ADMIN", "NET_BIND_SERVICE", "NET_BROADCAST", "NET_RAW", "SETFCAP", "SETGID", "SETPCAP",
		"SETUID", "SYS_CHROOT", "SYS_NICE", "WAKE_ALARM"}
	// dangerousCapabilities reach the kernel or the host beyond the container.
	dangerousCapabilities = []string{"ALL", "AUDIT_CONTROL", "AUDIT_READ", "BLOCK_SUSPEND", "BPF",
		"CHECKPOINT_RESTORE", "DAC_READ_SEARCH", "LEASE", "LINUX_IMMUTABLE", "MAC_ADMIN", "MAC_OVERRIDE",
		"PERFMON", "SYSLOG", "SYS_ADMIN", "SYS_BOOT", "SYS_MODULE", "SYS_PACCT", "SYS_PTRACE", "SYS_RAWIO",
		"SYS_RESOURCE", "SYS_TIME", "SYS_TTY_CONFIG"}
	ulimitNames = []string{"core", "cpu", "data", "fsize", "locks", "memlock", "msgqueue", "nice", "nofile",
		"nproc", "rss", "rtprio", "rttime", "sigpending", "stack"}
	// Raw memory, I/O ports and disks give the container the whole server.
	dangerousDevicePrefixes = []string{"/dev/mem", "/dev/kmem", "/dev/port", "/dev/sd", "/dev/hd", "/dev/vd",
		"/dev/xvd", "/dev/nvme", "/dev/dm-", "/dev/mapper", "/dev/loop", "/dev/md", "/dev/disk"}
	// Binding any of these exposes the Docker daemon, which is root on the server.
	dockerSocketDirectories = []string{"/", "/var", "/var/run", "/run"}
)

func (o *DockerOptions) normalize() {
	o.ExtraHosts = compact(o.ExtraHosts)
	o.CapAdd = normalizeCapabilities(o.CapAdd)
	o.CapDrop = normalizeCapabilities(o.CapDrop)
	o.ShmSize = strings.TrimSpace(o.ShmSize)
	o.Devices = compact(o.Devices)
	o.NetworkMode = strings.TrimSpace(o.NetworkMode)
	o.PidMode = strings.TrimSpace(o.PidMode)
	for index := range o.Ulimits {
		o.Ulimits[index].Name = strings.ToLower(strings.TrimSpace(o.Ulimits[index].Name))
	}
	if o.ExtraHosts == nil {
		o.ExtraHosts = []string{}
	}
}

func normalizeCapabilities(values []string) []string {
	out := []string{}
	for _, value := range compact(values) {
		out = append(out, strings.TrimPrefix(strings.ToUpper(value), "CAP_"))
	}
	return out
}

// validateDockerOptions checks the format of every option. Whether the
// instance allows the dangerous ones is checked by checkDockerOptionsPolicy.
func (c *Config) validateDockerOptions(fields apperr.Fields) {
	options := &c.DockerOptions
	options.normalize()
	validateExtraHosts(options, fields)
	validateCapabilities(options, fields)
	validateUlimits(options.Ulimits, fields)
	validateLabels(options.Labels, fields)
	validateDevices(options.Devices, fields)
	if options.ShmSize != "" && !memoryPattern.MatchString(options.ShmSize) {
		fields.Add("docker_options.shm_size", "Use um valor com unidade, ex.: 256m ou 1g.")
	}
	if options.NetworkMode != "" && options.NetworkMode != hostMode {
		fields.Add("docker_options.network_mode", "Use vazio (rede do Pier) ou host.")
	}
	if options.NetworkMode == hostMode && (len(c.Network.Domains) > 0 || len(c.Network.PortMappings) > 0) {
		fields.Add("docker_options.network_mode", "A rede do host não aceita domínios nem portas publicadas: a aplicação usa as portas do servidor diretamente.")
	}
	if options.PidMode != "" && options.PidMode != hostMode {
		fields.Add("docker_options.pid_mode", "Use vazio (isolado) ou host.")
	}
}

func validateExtraHosts(options *DockerOptions, fields apperr.Fields) {
	if len(options.ExtraHosts) > maxDockerOptionItems {
		fields.Add("docker_options.extra_hosts", "Use no máximo %d entradas.", maxDockerOptionItems)
	}
	for index, entry := range options.ExtraHosts {
		name, address, found := strings.Cut(strings.Replace(entry, "=", ":", 1), ":")
		name, address = strings.TrimSpace(name), strings.TrimSpace(address)
		if _, err := netip.ParseAddr(address); (err != nil && address != "host-gateway") || !found || !hostAliasPattern.MatchString(name) {
			fields.Add("docker_options.extra_hosts", "%q: use nome:IP, ex.: api.interna:10.0.0.5 ou host.docker.internal:host-gateway.", entry)
			continue
		}
		options.ExtraHosts[index] = name + ":" + address
	}
}

func validateCapabilities(options *DockerOptions, fields apperr.Fields) {
	for _, capability := range options.CapAdd {
		if !slices.Contains(safeCapabilities, capability) && !slices.Contains(dangerousCapabilities, capability) {
			fields.Add("docker_options.cap_add", "Capability desconhecida: %s.", capability)
		}
	}
	for _, capability := range options.CapDrop {
		if !slices.Contains(safeCapabilities, capability) && !slices.Contains(dangerousCapabilities, capability) {
			fields.Add("docker_options.cap_drop", "Capability desconhecida: %s.", capability)
		}
	}
}

func validateUlimits(ulimits []Ulimit, fields apperr.Fields) {
	seen := map[string]bool{}
	for _, ulimit := range ulimits {
		if !slices.Contains(ulimitNames, ulimit.Name) || seen[ulimit.Name] {
			fields.Add("docker_options.ulimits", "Limite desconhecido ou repetido: %q.", ulimit.Name)
			continue
		}
		seen[ulimit.Name] = true
		unlimited := ulimit.Hard == -1 && ulimit.Soft >= -1
		if !unlimited && (ulimit.Soft < 0 || ulimit.Hard < ulimit.Soft) {
			fields.Add("docker_options.ulimits", "%s: o limite flexível deve ficar entre 0 e o rígido (-1 é ilimitado).", ulimit.Name)
		}
	}
}

func validateLabels(labels map[string]string, fields apperr.Fields) {
	if len(labels) > maxDockerOptionItems {
		fields.Add("docker_options.labels", "Use no máximo %d labels.", maxDockerOptionItems)
	}
	for key, value := range labels {
		if !labelKeyPattern.MatchString(key) || reservedLabel(key) {
			fields.Add("docker_options.labels", "Label inválido ou reservado ao Pier: %q.", key)
		}
		if len(value) > maxLabelValueLength || strings.ContainsAny(value, "\x00\n\r") {
			fields.Add("docker_options.labels", "O valor do label %q é inválido.", key)
		}
	}
}

func reservedLabel(key string) bool {
	lower := strings.ToLower(key)
	for _, prefix := range reservedLabels {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func validateDevices(devices []string, fields apperr.Fields) {
	for _, device := range devices {
		parts := strings.Split(device, ":")
		valid := len(parts) <= 3 && devicePattern.MatchString(parts[0]) && !hasParentSegment(parts[0])
		if len(parts) >= 2 && !(devicePattern.MatchString(parts[1]) && !hasParentSegment(parts[1])) {
			valid = false
		}
		if len(parts) == 3 && (parts[2] == "" || strings.Trim(parts[2], "rwm") != "") {
			valid = false
		}
		if !valid {
			fields.Add("docker_options.devices", "%q: use /dev/origem[:/dev/destino[:rwm]].", device)
		}
	}
}

// DangerousDockerOptions lists what in the configuration gives the container
// power over the server: privileged mode, the host's network or process
// namespace, kernel-level capabilities, host devices and all host bind mounts.
func (c Config) DangerousDockerOptions() []string {
	options := c.DockerOptions
	options.NetworkMode = strings.TrimSpace(options.NetworkMode)
	options.PidMode = strings.TrimSpace(options.PidMode)
	options.CapAdd = normalizeCapabilities(options.CapAdd)
	found := []string{}
	if options.Privileged {
		found = append(found, "privileged")
	}
	if options.NetworkMode == hostMode {
		found = append(found, "network_mode: host")
	}
	if options.PidMode == hostMode {
		found = append(found, "pid: host")
	}
	for _, capability := range options.CapAdd {
		if slices.Contains(dangerousCapabilities, capability) {
			found = append(found, "cap_add: "+capability)
		}
	}
	for _, device := range options.Devices {
		if device != "" {
			found = append(found, "device: "+strings.Split(device, ":")[0])
		}
	}
	for _, storage := range c.Storages {
		if storage.Kind == StorageBind {
			found = append(found, "volume: "+storage.Source)
		}
	}
	sort.Strings(found)
	return found
}

func dangerousDevice(device string) bool {
	source := path.Clean(strings.Split(device, ":")[0])
	for _, prefix := range dangerousDevicePrefixes {
		if strings.HasPrefix(source, prefix) {
			return true
		}
	}
	return false
}

func exposesDockerSocket(source string) bool {
	cleaned := path.Clean(strings.TrimSpace(source))
	return slices.Contains(dockerSocketDirectories, cleaned) || strings.HasSuffix(cleaned, "docker.sock")
}

// applyDockerOptions adds the validated options to one generated service.
// Pier's own labels always win over user labels with the same key.
func applyDockerOptions(service *compose.Service, options DockerOptions) {
	service.ExtraHosts = append(service.ExtraHosts, options.ExtraHosts...)
	service.CapAdd = slices.Clone(options.CapAdd)
	service.CapDrop = slices.Clone(options.CapDrop)
	service.ShmSize = options.ShmSize
	service.Devices = slices.Clone(options.Devices)
	service.Privileged = service.Privileged || options.Privileged
	if len(options.Ulimits) > 0 {
		service.Ulimits = map[string]compose.Ulimit{}
		for _, ulimit := range options.Ulimits {
			service.Ulimits[ulimit.Name] = compose.Ulimit{Soft: ulimit.Soft, Hard: ulimit.Hard}
		}
	}
	if options.PidMode == hostMode {
		service.Pid = hostMode
	}
	if options.NetworkMode == hostMode {
		service.NetworkMode = hostMode
		service.Networks = nil
		service.Ports = nil
	}
	if service.Labels == nil && len(options.Labels) > 0 {
		service.Labels = map[string]string{}
	}
	for key, value := range options.Labels {
		if _, taken := service.Labels[key]; !taken {
			service.Labels[key] = value
		}
	}
}
