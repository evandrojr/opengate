package main

import (
	"crypto/subtle"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var (
	OpenCodeBinary = "opencode"
	OpenCodePath   = ""
)

type ChatMessage struct {
	Role    string      `json:"role"`
	Content interface{} `json:"content"`
}

type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type StreamChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ModelListResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

var availableModels []Model

func init() {
	data, err := os.ReadFile("models.json")
	if err != nil {
		log.Printf("Warning: models.json not found, using default. Error: %v", err)
		availableModels = []Model{
			{ID: "opencode/big-pickle", Object: "model", Created: 1700000000, OwnedBy: "opencode"},
		}
		return
	}
	if err := json.Unmarshal(data, &availableModels); err != nil {
		log.Fatalf("Error parsing models.json: %v", err)
	}
}

func messageText(m ChatMessage) string {
	switch c := m.Content.(type) {
	case string:
		return c
	case []interface{}:
		var parts []string
		for _, p := range c {
			if obj, ok := p.(map[string]interface{}); ok {
				if text, ok := obj["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func buildPrompt(messages []ChatMessage) string {
	var b strings.Builder
	for _, m := range messages {
		text := messageText(m)
		if text == "" {
			continue
		}
		switch m.Role {
		case "system":
			b.WriteString("System: ")
		case "assistant":
			b.WriteString("Assistant: ")
		default:
			b.WriteString("User: ")
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

// resolveModel returns the model ID to pass to `opencode -m`. Short names
// without a provider are prefixed with the default provider so clients can
// send either "deepseek-v4-flash-free" or "opencode/deepseek-v4-flash-free".
func resolveModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" || strings.Contains(model, "/") {
		return model
	}
	return provider + "/" + model
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ModelListResponse{Object: "list", Data: availableModels})
}

func handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	prompt := buildPrompt(req.Messages)
	if prompt == "" {
		http.Error(w, "No messages provided", http.StatusBadRequest)
		return
	}

	model := resolveModel(req.Model)
	opts := RunOptions{
		Model:     model,
		Prompt:    prompt,
		Directory: workingDir,
		Auto:      autoApprove,
		Continue:  continueSession,
		Session:   sessionID,
	}

	// Override with headers if present
	if auto := r.Header.Get("X-Auto-Approve"); auto != "" {
		opts.Auto = (auto == "true")
	}
	if sid := r.Header.Get("X-Session-Id"); sid != "" {
		if sid == "last" {
			opts.Continue = true
			opts.Session = ""
		} else {
			opts.Continue = false
			opts.Session = sid
		}
	}

	if req.Stream {
		handleStream(w, req, opts)
		return
	}
	handleNonStream(w, req, opts)
}

func handleNonStream(w http.ResponseWriter, req ChatCompletionRequest, opts RunOptions) {
	var content strings.Builder
	var usage Usage

	log.Printf("Running opencode (model=%s, session=%s, continue=%v, %d chars)\n",
		opts.Model, opts.Session, opts.Continue, len(opts.Prompt))

	err := runOpenCode(opts, func(ev OpenCodeEvent) error {
		if ev.IsText() {
			content.WriteString(ev.Part.Text)
		}
		if ev.IsFinish() {
			usage.PromptTokens = ev.Part.Tokens.Input
			usage.CompletionTokens = ev.Part.Tokens.Output
			usage.TotalTokens = ev.Part.Tokens.Total
		}
		return nil
	})
	if err != nil {
		log.Printf("Error running opencode: %v\n", err)
		http.Error(w, fmt.Sprintf("Internal server error: %v", err), http.StatusInternalServerError)
		return
	}

	resp := ChatCompletionResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Usage:   usage,
	}
	resp.Choices = append(resp.Choices, Choice{
		Index:        0,
		Message:      ChatMessage{Role: "assistant", Content: content.String()},
		FinishReason: "stop",
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func handleStream(w http.ResponseWriter, req ChatCompletionRequest, opts RunOptions) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	log.Printf("Streaming opencode (model=%s, session=%s, continue=%v, %d chars)\n",
		opts.Model, opts.Session, opts.Continue, len(opts.Prompt))

	writeSSE := func(v interface{}) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Initial role chunk, as real OpenAI streams do.
	stop := "stop"
	if err := writeSSE(map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()),
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   req.Model,
		"choices": []StreamChoice{{Index: 0, Delta: Delta{Role: "assistant"}}},
	}); err != nil {
		return
	}

	var usage Usage
	runErr := runOpenCode(opts, func(ev OpenCodeEvent) error {
		if ev.IsText() {
			usage.CompletionTokens++
			return writeSSE(map[string]interface{}{
				"choices": []StreamChoice{{Index: 0, Delta: Delta{Content: ev.Part.Text}}},
			})
		}
		if ev.IsFinish() {
			usage.PromptTokens = ev.Part.Tokens.Input
			usage.CompletionTokens = ev.Part.Tokens.Output
			usage.TotalTokens = ev.Part.Tokens.Total
		}
		return nil
	})

	if usage.TotalTokens == 0 && usage.CompletionTokens > 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}

	if runErr != nil {
		log.Printf("Error streaming opencode: %v\n", runErr)
		_ = writeSSE(map[string]interface{}{
			"choices": []StreamChoice{{Index: 0, Delta: Delta{}, FinishReason: &stop}},
		})
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	_ = writeSSE(map[string]interface{}{
		"choices": []StreamChoice{{Index: 0, Delta: Delta{}, FinishReason: &stop}},
		"usage":   usage,
	})
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// serviceUser returns the name of the user the systemd service should run as.
// Under sudo the original user is available via SUDO_USER, otherwise USER.
func serviceUser() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return u
	}
	return os.Getenv("USER")
}

// effectiveHome returns the home directory of the user the service runs as.
// Under sudo HOME points to root's home, so we resolve the real user's home.
func effectiveHome() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		if lu, err := user.Lookup(u); err == nil && lu.HomeDir != "" {
			return lu.HomeDir
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// buildServiceFile renders the systemd unit for this executable.
func buildServiceFile() (string, error) {
	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("getting executable path: %w", err)
	}
	exePath, _ = filepath.Abs(exePath)
	workDir := filepath.Dir(exePath)

	ocPath, nodePath := OpenCodeBinary, OpenCodePath
	if cfg, err := loadConfig(filepath.Join(effectiveHome(), ".opengate", "config.json")); err == nil {
		if cfg.OpenCodeBinary != "" {
			ocPath = cfg.OpenCodeBinary
		}
		if cfg.NodePath != "" {
			nodePath = cfg.NodePath
		}
		if cfg.WorkingDir != "" {
			workDir = cfg.WorkingDir
		}
	}
	if ocPath == "" || ocPath == "opencode" {
		if p, err := exec.LookPath("opencode"); err == nil {
			ocPath = p
		}
	}

	pathEnv := "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	if nodePath != "" {
		pathEnv = nodePath + ":" + pathEnv
	}

	user := serviceUser()
	return fmt.Sprintf(`[Unit]
Description=OpenGate OpenAI Proxy Server
After=network.target

[Service]
ExecStart=%s -dir %s
WorkingDirectory=%s
User=%s
Group=%s
Restart=always
Environment=OPENCODE_BINARY=%s
Environment=PATH=%s

[Install]
WantedBy=multi-user.target
`, exePath, workDir, workDir, user, user, ocPath, pathEnv), nil
}

func installService() {
	serviceContent, err := buildServiceFile()
	if err != nil {
		log.Fatalf("Error building service file: %v", err)
	}

	servicePath := "/tmp/opengate.service"
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		log.Fatalf("Error writing temporary service file: %v", err)
	}

	fmt.Printf("Service file generated at: %s\n", servicePath)
	fmt.Println("\nTo install and start the service, run the following commands:")
	fmt.Printf("  sudo cp %s /etc/systemd/system/opengate.service\n", servicePath)
	fmt.Println("  sudo systemctl daemon-reload")
	fmt.Println("  sudo systemctl enable opengate")
	fmt.Println("  sudo systemctl start opengate")
	fmt.Println("  sudo systemctl status opengate")
}

// installAsService writes the unit to /etc/systemd/system and enables and
// starts it. Requires root; run with sudo.
func installAsService() {
	if runtime.GOOS != "linux" {
		log.Fatalf("systemd service installation is only supported on Linux")
	}
	if os.Geteuid() != 0 {
		log.Fatalf("root privileges required, run with sudo")
	}

	serviceContent, err := buildServiceFile()
	if err != nil {
		log.Fatalf("Error building service file: %v", err)
	}

	servicePath := "/etc/systemd/system/opengate.service"
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		log.Fatalf("Error writing service file: %v", err)
	}
	fmt.Printf("Service file written to %s\n", servicePath)

	for _, step := range []struct {
		args []string
		desc string
	}{
		{[]string{"daemon-reload"}, "daemon-reload"},
		{[]string{"enable", "opengate"}, "enable"},
		{[]string{"start", "opengate"}, "start"},
	} {
		if out, err := exec.Command("systemctl", step.args...).CombinedOutput(); err != nil {
			log.Printf("systemctl %s failed: %v\n%s", step.desc, err, out)
		} else {
			fmt.Printf("systemctl %s: ok\n", step.desc)
		}
	}
}

var (
	listenAddr      string
	token           string
	workingDir      string
	provider        string
	autoApprove     bool
	continueSession bool
	sessionID       string
)

func main() {
	help := flag.Bool("help", false, "Show help")
	h := flag.Bool("h", false, "Show help")
	install := flag.Bool("install-service", false, "Generate systemd service file")
	installNow := flag.Bool("install", false, "Install and start as a systemd service (requires root)")
	autoCfg := flag.Bool("auto-config", false, "Resolve opencode binary and node path, write ~/.opengate/config.json and exit")
	configFlag := flag.String("config", "", "Path to config file (default: ~/.opengate/config.json)")
	runFlag := flag.String("run", "", "Execute a single prompt via opencode CLI and exit (no HTTP server)")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1:2211", "Address to listen on (e.g. 127.0.0.1:2211, :2211 for all interfaces)")
	flag.StringVar(&token, "token", os.Getenv("OPENGATE_TOKEN"), "Bearer token required for API access (default: $OPENGATE_TOKEN)")
	flag.StringVar(&workingDir, "dir", "", "Working directory for opencode sessions (default: current directory)")
	flag.StringVar(&provider, "provider", "opencode", "Default provider prefix for model IDs without a provider")
	defaultBinary := os.Getenv("OPENCODE_BINARY")
	if defaultBinary == "" {
		defaultBinary = "opencode"
	}
	flag.StringVar(&OpenCodeBinary, "opencode-binary", defaultBinary, "Path to the opencode binary")
	flag.StringVar(&OpenCodePath, "node-path", "", "Extra PATH entry (e.g. fnm node bin dir) appended for node subprocesses")
	flag.BoolVar(&autoApprove, "auto", false, "Auto-approve opencode tool permissions")
	flag.BoolVar(&continueSession, "continue", false, "Continue the last opencode session for every request")
	flag.StringVar(&sessionID, "session", "", "Continue the given opencode session ID for every request")

	flag.Parse()

	if *help || *h {
		fmt.Println("OpenGate OpenAI Proxy Server")
		fmt.Println("\nUsage:")
		flag.PrintDefaults()
		return
	}

	if *autoCfg {
		if err := autoConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Load config file and use it as defaults for flags not explicitly set.
	cfgPath := *configFlag
	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if cfg, err := loadConfig(cfgPath); err == nil {
		if !set["opencode-binary"] {
			OpenCodeBinary = cfg.OpenCodeBinary
		}
		if !set["node-path"] {
			OpenCodePath = cfg.NodePath
		}
		if !set["dir"] {
			workingDir = cfg.WorkingDir
		}
	}

	if *install {
		installService()
		return
	}

	if *installNow {
		installAsService()
		return
	}

	if OpenCodeBinary == "" {
		OpenCodeBinary = "opencode"
	}
	if bin, err := exec.LookPath(OpenCodeBinary); err == nil {
		OpenCodeBinary = bin
	}
	log.Printf("Using opencode binary: %s\n", OpenCodeBinary)

	if *runFlag != "" {
		if err := runCLI(*runFlag); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	http.HandleFunc("/v1/models", withAuth(handleModels))
	http.HandleFunc("/v1/chat/completions", withAuth(handleChatCompletions))

	log.Printf("Server starting on %s...\n", listenAddr)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// runCLI executes a single prompt directly via opencode and prints the
// assistant text to stdout, bypassing the HTTP server entirely.
// withAuth protects handlers with a Bearer token when one is configured.
func withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if token != "" {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") ||
				subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(auth, "Bearer ")), []byte(token)) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="opengate"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

func runCLI(prompt string) error {
	var out strings.Builder
	var usage Usage

	err := runOpenCode(RunOptions{
		Model:     resolveModel(""),
		Prompt:    prompt,
		Directory: workingDir,
		Auto:      autoApprove,
		Continue:  continueSession,
		Session:   sessionID,
	}, func(ev OpenCodeEvent) error {
		if ev.IsText() {
			out.WriteString(ev.Part.Text)
		}
		if ev.IsFinish() {
			usage.PromptTokens = ev.Part.Tokens.Input
			usage.CompletionTokens = ev.Part.Tokens.Output
			usage.TotalTokens = ev.Part.Tokens.Total
		}
		return nil
	})
	if err != nil {
		return err
	}

	fmt.Println(strings.TrimSpace(out.String()))
	log.Printf("Usage: %d prompt / %d completion / %d total tokens\n",
		usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens)
	return nil
}
