// resources.h — embedded encrypted resource layer
#pragma once
#include <string>
#include <vector>

namespace helmx {

// Embedded resource ids
enum class ResId {
    AgentsMd,       // AGENTS.md content
    AgentsV45,      // v45 prompt (gpt-5.6-instruct)
    TamperRules,    // TAMPER_RULES pattern list
    DashboardHtml,  // embedded web dashboard
    SkillsIndex,    // skills manifest (name -> content)
    RewritePrompt,  // rewriter system prompt
    RewriterBuiltin, // built-in rewriter config (free, not in git)
};

// Decrypt and return an embedded resource (runtime XOR key derived in code)
std::string get_resource(ResId id);

// Decrypt skill content by name
std::string get_skill(const std::string& name);

// List all skill names from embedded index
std::vector<std::string> list_skills();

}  // namespace helmx
