// tamper.h — TAMPER_RULES engine
#pragma once
#include <string>
#include <vector>

namespace helmx {

struct TamperRule {
    std::string pattern;   // regex pattern
    std::string replace;   // replacement text
};

// Load rules from embedded resource
std::vector<TamperRule> load_tamper_rules();

// Check if response text matches any refusal pattern
bool is_refusal(const std::string& text);

// Rewrite a refusal into a compliant continuation. Returns true if tampered.
bool try_tamper(std::string& text);

}  // namespace helmx
