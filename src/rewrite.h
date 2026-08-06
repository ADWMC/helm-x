// rewrite.h — request rewriter: sanitize intent wording via LLM API
#pragma once
#include <string>

namespace helmx {

struct RewriterConfig {
    bool enabled = false;
    std::string base_url = "https://klapi.me/v1";
    std::string api_key;
    std::string model = "mimo-v2.5-pro";
    std::string system_prompt;
    int timeout_sec = 90;
    bool use_proxy = false;  // klapi direct; nvidia needs 127.0.0.1:7897
};

// Load config from helmx.config.json next to the exe (or ./helmx.config.json)
bool load_rewriter_config(RewriterConfig& cfg);

// Rewrite a user message through the configured LLM.
// Returns true on success, out receives the rewritten message.
bool rewrite_user_message(const RewriterConfig& cfg, const std::string& user_msg, std::string& out);

// API-based rewrite (internal; used when local rules don't match)
bool rewrite_via_api(const RewriterConfig& cfg, const std::string& user_msg, std::string& out);

}  // namespace helmx
