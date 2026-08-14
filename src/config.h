// config.h - Claude Code settings integration and backup handling
#pragma once
#include <string>

namespace helmx {

// Locate Claude Code home (CLAUDE_CONFIG_DIR or ~/.claude).
std::string find_claude_home();

// Back up a settings file once.
bool backup_config(const std::string& cfg_path);

// Prepare Claude Code settings and point it at the default local proxy.
bool inject_config(const std::string& home);

// Point ANTHROPIC_BASE_URL at a local proxy port.
bool inject_config_proxy(const std::string& home, int port);

// Restore ANTHROPIC_BASE_URL from the proxy backup.
bool restore_config_proxy(const std::string& home);

// Read the configured upstream URL, preferring the proxy backup while active.
std::string read_relay_url(const std::string& home);

// Read the active Claude provider and ANTHROPIC_BASE_URL.
bool read_active_provider(const std::string& home, std::string& provider,
                          std::string& base_url);

// Verify Claude Code is pointed at the helm-x proxy.
bool verify_injection(const std::string& home);

// Remove injected settings and restore the original file.
bool remove_all(const std::string& home);

int apply();
int remove();

}  // namespace helmx
