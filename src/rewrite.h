// rewrite.h — request rewriter: sanitize intent wording via LLM API
#pragma once
#include <string>

namespace helmx {

struct RewriterConfig {
    bool enabled = false;
    std::string base_url = "https://integrate.api.nvidia.com/v1";
    std::string api_key;
    std::string model = "z-ai/glm-5.2";
    std::string system_prompt;
    int timeout_sec = 20;
};

// Load config from helmx.config.json next to the exe (or ./helmx.config.json)
bool load_rewriter_config(RewriterConfig& cfg);

// Rewrite a user message through the configured LLM.
// Returns true on success, out receives the rewritten message.
bool rewrite_user_message(const RewriterConfig& cfg, const std::string& user_msg, std::string& out);

// API-based rewrite (internal; used when local rules don't match)
bool rewrite_via_api(const RewriterConfig& cfg, const std::string& user_msg, std::string& out);

}  // namespace helmx
