// config.h — config.toml merge injection + backup + validation
#pragma once
#include <string>

namespace helmx {

// Locate codex home (CODEX_HOME env or ~/.codex)
std::string find_codex_home();

// Back up config.toml to config.toml.helmx-bak if not already backed up
bool backup_config(const std::string& cfg_path);

// Merge-inject required settings into config.toml (TOML-safe, validate after write)
bool inject_config(const std::string& home);

// Point codex base_url at a local proxy port; back up original to .helmx-proxy-bak
bool inject_config_proxy(const std::string& home, int port);

// Restore base_url from .helmx-proxy-bak (called on proxy exit)
bool restore_config_proxy(const std::string& home);

// Verify injected settings are still present
bool verify_injection(const std::string& home);

// Deploy AGENTS.md (decrypted from embedded resources)
bool deploy_agents(const std::string& home);

// Remove all injected artifacts, restore backup
bool remove_all(const std::string& home);

int apply();
int remove();

}  // namespace helmx
