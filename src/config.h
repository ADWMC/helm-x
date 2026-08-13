// config.h — config.toml merge injection + backup + validation
#pragma once
#include <string>

namespace helmx {

struct ContextRequestConfig {
    int tool_output_token_limit = 8000;
    int model_auto_compact_token_limit = 180000;
    std::string model_auto_compact_token_limit_scope = "body_after_prefix";
};

// Locate codex home (CODEX_HOME env or ~/.codex)
std::string find_codex_home();

// Back up config.toml to config.toml.helmx-bak if not already backed up
bool backup_config(const std::string& cfg_path);

// Merge-inject required settings into config.toml (TOML-safe, validate after write)
bool inject_config(const std::string& home);

bool read_context_request_config(const std::string& home, ContextRequestConfig& cfg);
bool write_context_request_config(const std::string& home, const ContextRequestConfig& cfg);

// Point codex base_url at a local proxy port; back up original to .helmx-proxy-bak
bool inject_config_proxy(const std::string& home, int port);

// Restore base_url from .helmx-proxy-bak (called on proxy exit)
bool restore_config_proxy(const std::string& home);

// Read the active provider's relay base_url. Uses .helmx-proxy-bak only when
// that provider currently points at the local proxy.
std::string read_relay_url(const std::string& home);

// Read the active provider and its base_url from the live config.
bool read_active_provider(const std::string& home, std::string& provider,
                          std::string& base_url);

// Verify injected settings are still present
bool verify_injection(const std::string& home);

// Deploy AGENTS.md (decrypted from embedded resources)
bool deploy_agents(const std::string& home);

// Remove all injected artifacts, restore backup
bool remove_all(const std::string& home);

int apply();
int remove();

}  // namespace helmx
