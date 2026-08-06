// resources.h — embedded encrypted resource layer
#pragma once
#include <string>
#include <vector>

namespace helmx {

// Embedded resource ids
enum class ResId {
    AgentsMd,       // AGENTS.md content
    TamperRules,    // TAMPER_RULES pattern list
    ToolsJson,      // MCP tool definitions
    DashboardHtml,  // embedded web dashboard
    SkillsIndex,    // skills manifest (name -> content)
};

// Decrypt and return an embedded resource (runtime XOR key derived in code)
std::string get_resource(ResId id);

// Decrypt skill content by name
std::string get_skill(const std::string& name);

// List all skill names from embedded index
std::vector<std::string> list_skills();

}  // namespace helmx
