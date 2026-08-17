// rewrite.h — request rewriter: sanitize intent wording via LLM API
#pragma once
#include <string>

namespace helmx {

struct RewriterConfig {
    bool enabled = false;
    std::string provider = "klapi";
    std::string base_url = "https://klapi.me/v1";
    std::string api_key;
    std::string model = "mimo-v2.5-pro";
    std::string system_prompt;
    int timeout_sec = 90;
    bool use_proxy = false;
    std::string proxy_url = "http://127.0.0.1:7897";  // HTTP proxy for upstream
    std::string prompt_mode = "default";
    bool context_gardener_enabled = true;
    int context_gardener_threshold_bytes = 32768;
    // Proxy upstream retry policy. max_retries is additional attempts; 0 is unlimited.
    bool upstream_retry_enabled = true;
    int upstream_max_retries = 10;
    int upstream_retry_delay_seconds = 3;
};

// Load config from %APPDATA%/helmx.config.json.
bool load_rewriter_config(RewriterConfig& cfg);

// Save all user-editable settings to %APPDATA%/helmx.config.json.
bool save_rewriter_config(const RewriterConfig& cfg, std::string& path);

// Rewrite a user message through the configured LLM.
// refusal_text: the model's refusal response (for context-aware rewriting).
// context: conversation history for context-aware rewriting.
// Returns true on success, out receives the rewritten message.
bool rewrite_user_message(const RewriterConfig& cfg, const std::string& user_msg,
                          std::string& out, const std::string& refusal_text = "",
                          const std::string& context = "");

// API-based rewrite (internal; used when local rules don't match)
bool rewrite_via_api(const RewriterConfig& cfg, const std::string& user_msg,
                     std::string& out, const std::string& refusal_text = "",
                     const std::string& context = "");

}  // namespace helmx
