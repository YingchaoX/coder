package lsp

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"coder/internal/config"
)

// Manager manages multiple LSP clients for different languages
// Manager 管理多种语言的 LSP 客户端
type Manager struct {
	configs   map[string]config.LSPServerConfig
	workspace string

	clients   map[string]*Client
	clientsMu sync.RWMutex

	// Track which servers are available
	available map[string]bool
	mu        sync.RWMutex

	// Installation hints for missing servers
	installHints map[string]string
}

// Install hints for LSP servers
// LSP 服务器的安装提示
var defaultInstallHints = map[string]string{
	"sh": "npm install -g bash-language-server",
	"py": "pip install python-lsp-server",
}

// NewManager creates a new LSP manager
// NewManager 创建一个新的 LSP 管理器
func NewManager(cfg config.LSPConfig, workspace string) *Manager {
	return &Manager{
		configs:      cfg.Servers,
		workspace:    workspace,
		clients:      make(map[string]*Client),
		available:    make(map[string]bool),
		installHints: defaultInstallHints,
	}
}

// DetectServers checks which LSP servers are available
// DetectServers 检查哪些 LSP 服务器可用
func (m *Manager) DetectServers() []string {
	var missing []string

	for lang, serverCfg := range m.configs {
		if !serverCfg.Enabled {
			continue
		}

		// Check if command exists in PATH
		_, err := exec.LookPath(serverCfg.Command)
		m.mu.Lock()
		if err != nil {
			m.available[lang] = false
			missing = append(missing, lang)
		} else {
			m.available[lang] = true
		}
		m.mu.Unlock()
	}

	return missing
}

// GetMissingServers returns a list of missing servers with install hints
// GetMissingServers 返回缺失的服务器列表及安装提示
func (m *Manager) GetMissingServers() []struct {
	Lang        string
	Command     string
	InstallHint string
} {
	var missing []struct {
		Lang        string
		Command     string
		InstallHint string
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for lang, serverCfg := range m.configs {
		if !serverCfg.Enabled {
			continue
		}
		if !m.available[lang] {
			hint := m.installHints[lang]
			if hint == "" {
				hint = fmt.Sprintf("Please install %s", serverCfg.Command)
			}
			missing = append(missing, struct {
				Lang        string
				Command     string
				InstallHint string
			}{
				Lang:        lang,
				Command:     serverCfg.Command,
				InstallHint: hint,
			})
		}
	}

	return missing
}

// IsAvailable checks if a language server is available
// IsAvailable 检查语言服务器是否可用
func (m *Manager) IsAvailable(lang string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.available[lang]
}

// GetClient gets or creates a client for a language
// GetClient 获取或创建某个语言的客户端
func (m *Manager) GetClient(lang string) (*Client, error) {
	m.clientsMu.RLock()
	client, exists := m.clients[lang]
	m.clientsMu.RUnlock()

	if exists {
		return client, nil
	}

	// Check if available
	if !m.IsAvailable(lang) {
		return nil, fmt.Errorf("LSP server for %s is not available", lang)
	}

	// Get server config
	serverCfg, ok := m.configs[lang]
	if !ok {
		return nil, fmt.Errorf("no LSP configuration for language: %s", lang)
	}

	// Create new client
	client = NewClient(lang, serverCfg.Command, serverCfg.Args, m.workspace)

	// Start the client
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start LSP client for %s: %w", lang, err)
	}

	// Store the client
	m.clientsMu.Lock()
	m.clients[lang] = client
	m.clientsMu.Unlock()

	return client, nil
}

// Stop stops all LSP clients
// Stop 停止所有 LSP 客户端
func (m *Manager) Stop() {
	m.clientsMu.Lock()
	defer m.clientsMu.Unlock()

	for _, client := range m.clients {
		client.Stop()
	}
	m.clients = make(map[string]*Client)
}

// GetLanguageFromPath detects language from file path
// GetLanguageFromPath 从文件路径检测语言
func (m *Manager) GetLanguageFromPath(path string) string {
	ext := strings.ToLower(filepath.Ext(path))

	// Check if we have a config for this extension
	for lang := range m.configs {
		if m.isLanguageMatch(lang, ext) {
			return lang
		}
	}

	return ""
}

// isLanguageMatch checks if an extension matches a language
// isLanguageMatch 检查扩展名是否匹配语言
func (m *Manager) isLanguageMatch(lang, ext string) bool {
	switch lang {
	case "py":
		return ext == ".py"
	case "sh":
		return ext == ".sh" || ext == ".bash" || ext == ".zsh"
	default:
		return false
	}
}

// IsLSPFile checks if a file path is supported by LSP
// IsLSPFile 检查文件路径是否被 LSP 支持
func (m *Manager) IsLSPFile(path string) bool {
	return m.GetLanguageFromPath(path) != ""
}
