// log.h — file logging
#pragma once
#include <string>

namespace helmx {

std::string log_path();
std::string cyber_log_path();

void log_info(const std::string& msg);
void log_error(const std::string& msg);

// Cyber event context
struct CyberContext {
    std::string original;       // user's original request
    std::string rewritten;      // rewritten request (if any)
    std::string refusal_text;   // model's refusal response
    std::string trigger_words;  // detected trigger words
    std::string prompt_mode;    // "default" or "v45"
    int upstream_status = 0;    // HTTP status from upstream
    int rewrite_status = 0;     // 0=not attempted, 1=success, 2=failed
    std::string result;         // "pass" / "blocked" / "rewritten_pass" / "rewritten_fail"
};

// Cyber event logging (writes to helmx-cyber.log)
void log_cyber(const CyberContext& ctx);

}  // namespace helmx
