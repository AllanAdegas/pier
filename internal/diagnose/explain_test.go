package diagnose

import (
	"slices"
	"testing"

	"pier/internal/platform/i18n"
)

func hintCodes(hints []Hint) []string {
	codes := make([]string, 0, len(hints))
	for _, hint := range hints {
		codes = append(codes, hint.Code)
	}
	return codes
}

func TestExplainRecognizesFailures(t *testing.T) {
	tests := []struct {
		name     string
		log      string
		exitCode int
		want     []string
	}{
		{
			name:     "container killed with exit 137",
			log:      "Step 7/9 : RUN npm run build\nKilled",
			exitCode: 137,
			want:     []string{"out_of_memory"},
		},
		{
			name:     "docker reports OOMKilled in the log",
			log:      "container 9f2c OOMKilled=true after 42s",
			exitCode: 1,
			want:     []string{"out_of_memory"},
		},
		{
			name: "node heap exhausted during build",
			log:  "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory",
			want: []string{"out_of_memory"},
		},
		{
			name: "missing start script in npm",
			log:  "npm ERR! Missing script: \"start\"\nnpm ERR!\nnpm ERR! To see a list of scripts, run:",
			want: []string{"start_script_missing"},
		},
		{
			name: "missing start script in pnpm",
			log:  " ERR_PNPM_NO_SCRIPT  Missing script: start",
			want: []string{"start_script_missing"},
		},
		{
			name: "yarn cannot find start command",
			log:  "error Command \"start\" not found.",
			want: []string{"start_script_missing"},
		},
		{
			name: "nixpacks has no start command",
			log:  "ERROR: Unable to build: no start command could be found",
			want: []string{"start_script_missing"},
		},
		{
			name: "no space left on device",
			log:  "failed to export image: write /var/lib/docker/tmp/layer: no space left on device",
			want: []string{"disk_full"},
		},
		{
			name: "git ssh key rejected",
			log:  "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.",
			want: []string{"git_access_denied"},
		},
		{
			name: "git repository not found",
			log:  "remote: Repository not found.\nfatal: repository 'https://github.com/acme/app.git/' not found",
			want: []string{"git_access_denied"},
		},
		{
			name: "git https prompt for username",
			log:  "fatal: could not read Username for 'https://github.com': terminal prompts disabled",
			want: []string{"git_access_denied"},
		},
		{
			name: "dockerfile path wrong in buildkit",
			log: "failed to solve: failed to read dockerfile: " +
				"open /tmp/buildkit/Dockerfile: no such file or directory",
			want: []string{"dockerfile_missing"},
		},
		{
			name: "dockerfile missing in classic builder context",
			log: "unable to prepare context: unable to evaluate symlinks in Dockerfile path: " +
				"lstat /srv/app/Dockerfile: no such file or directory",
			want: []string{"dockerfile_missing"},
		},
		{
			name: "docker hub anonymous pull limit",
			log:  "toomanyrequests: You have reached your pull rate limit.",
			want: []string{"docker_hub_rate_limit"},
		},
		{
			name: "dns failure while fetching packages",
			log: "npm ERR! request to https://registry.npmjs.org failed, " +
				"reason: getaddrinfo EAI_AGAIN registry.npmjs.org",
			want: []string{"dns_resolution_failed"},
		},
		{
			name: "image built for another architecture",
			log:  "exec /usr/local/bin/app: exec format error",
			want: []string{"architecture_mismatch"},
		},
		{
			name: "port already bound",
			log:  "Error: listen EADDRINUSE: address already in use 0.0.0.0:3000",
			want: []string{"address_in_use"},
		},
		{
			name: "pier healthcheck timeout",
			log: "Aguardando o healthcheck do novo container (até 5 tentativas).\n" +
				"erro: o container não ficou pronto dentro do tempo limite (estado: running|starting|0)",
			want: []string{"healthcheck_failed"},
		},
		{
			name: "pier healthcheck reports unhealthy",
			log:  "erro: o healthcheck do novo container falhou",
			want: []string{"healthcheck_failed"},
		},
		{
			name: "pier healthcheck timeout in English",
			log:  "error: the container did not become ready in time (state: running|starting|0)",
			want: []string{"healthcheck_failed"},
		},
		{
			name: "container exits right after start in English",
			log:  "error: the container exited right after starting",
			want: []string{"container_crash_loop"},
		},
		{
			name: "container restart loop in English",
			log:  "error: the container is in a restart loop",
			want: []string{"container_crash_loop"},
		},
		{
			name: "container exits right after start",
			log:  "erro: o container encerrou logo após iniciar",
			want: []string{"container_crash_loop"},
		},
		{
			name: "container keeps restarting",
			log:  "erro: o container está reiniciando em loop",
			want: []string{"container_crash_loop"},
		},
		{
			name: "python module not installed",
			log: "Traceback (most recent call last):\n  File \"app.py\", line 1, in <module>\n" +
				"ModuleNotFoundError: No module named 'flask'",
			want: []string{"module_missing"},
		},
		{
			name: "node module not installed",
			log:  "Error: Cannot find module 'express'\nRequire stack:",
			want: []string{"module_missing"},
		},
		{
			name: "node EACCES on a mounted volume",
			log:  "Error: EACCES: permission denied, open '/data/uploads/a.png'",
			want: []string{"volume_permission_denied"},
		},
		{
			name: "python errno 13 on a volume",
			log:  "PermissionError: [Errno 13] Permission denied: '/app/storage'",
			want: []string{"volume_permission_denied"},
		},
		{
			name:     "unknown failure has no hint",
			log:      "something unexpected happened",
			exitCode: 2,
			want:     nil,
		},
		{
			name: "empty log without exit status has no hint",
			log:  "",
			want: nil,
		},
		{
			name: "most specific evidence comes first",
			log:  "no space left on device\nremote: Permission denied (publickey).",
			want: []string{"disk_full", "git_access_denied"},
		},
		{
			name: "several matches are capped at three",
			log: "Killed\nno space left on device\nremote: Repository not found.\n" +
				"toomanyrequests: pull rate limit\nexec format error",
			exitCode: 137,
			want:     []string{"out_of_memory", "disk_full", "git_access_denied"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hintCodes(Explain(tt.log, tt.exitCode))
			if !slices.Equal(got, tt.want) {
				t.Errorf("Explain codes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExplainReturnsFilledHints(t *testing.T) {
	hints := Explain("npm ERR! Missing script: \"start\"", 0)
	if len(hints) != 1 {
		t.Fatalf("got %d hints, want 1", len(hints))
	}
	hint := hints[0]
	if hint.Title == "" || hint.Fix == "" || hint.DocURL == "" {
		t.Errorf("hint must carry title, fix and doc URL: %+v", hint)
	}
}

func TestEveryRuleHasUniqueCodeAndDocURL(t *testing.T) {
	seen := map[string]bool{}
	for _, candidate := range failureRules {
		if seen[candidate.hint.Code] {
			t.Errorf("duplicate code %q", candidate.hint.Code)
		}
		seen[candidate.hint.Code] = true
		if candidate.pattern == nil && candidate.exitCode == 0 {
			t.Errorf("rule %q has no evidence", candidate.hint.Code)
		}
		if candidate.hint.DocURL == "" {
			t.Errorf("rule %q has no doc URL", candidate.hint.Code)
		}
	}
}

func TestEveryRuleIsTranslatedToEnglish(t *testing.T) {
	for _, candidate := range failureRules {
		hint := candidate.hint.Localize(i18n.English)
		if hint.Title == candidate.hint.Title {
			t.Errorf("rule %q: title has no English translation: %q", candidate.hint.Code, candidate.hint.Title)
		}
		if hint.Fix == candidate.hint.Fix {
			t.Errorf("rule %q: fix has no English translation", candidate.hint.Code)
		}
	}
}

func TestLocalizeKeepsPortugueseForPortuguese(t *testing.T) {
	hint := Hint{Code: "x", Title: "Dockerfile não encontrado", Fix: "Confirme o caminho.", DocURL: "u"}
	if got := hint.Localize(i18n.Portuguese); got != hint {
		t.Errorf("Localize(pt-BR) = %+v, want %+v", got, hint)
	}
}

func TestLocalizeTranslatesToEnglish(t *testing.T) {
	hint := Hint{Code: "disk_full", Title: "Não há espaço em disco", Fix: "Libere espaço em Servidores.", DocURL: "u"}
	got := hint.Localize(i18n.English)
	if got.Title != "No space left on the server disk" {
		t.Errorf("title = %q, want %q", got.Title, "No space left on the server disk")
	}
	if got.Code != "disk_full" || got.DocURL != "u" {
		t.Errorf("code and doc URL must be kept: %+v", got)
	}
}
