// config.cpp — config.toml merge injection + backup + validation
#include "config.h"
#include "obf.h"
#include "resources.h"

#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <sstream>

namespace fs = std::filesystem;

namespace helmx {

std::string find_codex_home() {
    const char* env = std::getenv("CODEX_HOME");
    if (env && *env && fs::exists(fs::path(env) / "config.toml")) {
        return env;
    }
    const char* user = std::getenv("USERPROFILE");
    if (user && *user) {
        fs::path home(user);
        for (const char* sub : {".codex", "codex"}) {
            if (fs::exists(home / sub / "config.toml")) {
                return (home / sub).string();
            }
        }
    }
    return "";
}

bool backup_config(const std::string& cfg_path) {
    fs::path bak = cfg_path + ".helmx-bak";
    if (fs::exists(bak)) return true;  // already backed up
    std::error_code ec;
    fs::copy_file(cfg_path, bak, fs::copy_options::overwrite_existing, ec);
    return !ec;
}

static bool toml_valid(const std::string& path) {
    // Minimal validation: braces balance per line-section. Full TOML parse
    // would need a parser; we keep injection line-based and validate by
    // re-reading key presence + bracket balance.
    std::ifstream f(path);
    if (!f) return false;
    std::string line;
    int depth = 0;
    while (std::getline(f, line)) {
        // strip comments (naive: outside quotes)
        size_t comment = line.find('#');
        std::string core = comment == std::string::npos ? line : line.substr(0, comment);
        for (char c : core) {
            if (c == '[') depth++;
            if (c == ']') depth--;
        }
    }
    return depth == 0;
}

bool inject_config(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) {
        std::fprintf(stderr, "[helm-x] config.toml not found at %s\n", cfg.string().c_str());
        return false;
    }
    if (!backup_config(cfg.string())) {
        std::fprintf(stderr, "[helm-x] backup failed\n");
        return false;
    }

    std::ifstream in(cfg);
    std::stringstream ss;
    ss << in.rdbuf();
    std::string content = ss.str();

    // 1. model_provider = "custom" (ensure present)
    if (content.find("model_provider") == std::string::npos) {
        content = "model_provider = \"custom\"\n" + content;
    }
    // 2. AGENTS.md is auto-read by codex; we only need skills dir + mcp_servers.
    //    mcp_servers section (idempotent insert)
    if (content.find("[mcp_servers.helmx]") == std::string::npos) {
        std::string mcp_section =
            "\n[mcp_servers.helmx]\n"
            "command = \"helmx\"\n"
            "args = [\"mcp\"]\n"
            "startup_timeout_sec = 30\n";
        // insert before end of file (after any trailing newline)
        if (!content.empty() && content.back() != '\n') content.push_back('\n');
        content += mcp_section;
    }

    std::ofstream out(cfg, std::ios::trunc);
    out << content;
    out.close();

    if (!toml_valid(cfg.string())) {
        std::fprintf(stderr, "[helm-x] TOML invalid after inject, restoring\n");
        fs::path bak = cfg.string() + ".helmx-bak";
        if (fs::exists(bak)) {
            std::error_code ec;
            fs::copy_file(bak, cfg, fs::copy_options::overwrite_existing, ec);
        }
        return false;
    }
    return true;
}

bool verify_injection(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) return false;
    std::ifstream f(cfg);
    std::stringstream ss;
    ss << f.rdbuf();
    std::string c = ss.str();
    bool ok = true;
    if (c.find("[mcp_servers.helmx]") == std::string::npos) ok = false;
    if (!fs::exists(fs::path(home) / "AGENTS.md")) ok = false;
    return ok;
}

bool deploy_agents(const std::string& home) {
    std::string content = get_resource(ResId::AgentsMd);
    if (content.empty()) {
        std::fprintf(stderr, "[helm-x] AGENTS.md resource empty\n");
        return false;
    }
    fs::path dst = fs::path(home) / "AGENTS.md";
    // binary mode: never translate \n -> \r\n (content must byte-match resource)
    std::ofstream out(dst, std::ios::trunc | std::ios::binary);
    out << content;
    out.close();
    return fs::exists(dst);
}

bool remove_all(const std::string& home) {
    // restore config from backup
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path bak = cfg.string() + ".helmx-bak";
    if (fs::exists(bak)) {
        std::error_code ec;
        fs::copy_file(bak, cfg, fs::copy_options::overwrite_existing, ec);
        fs::remove(bak);
    }
    // remove AGENTS.md only if it matches our embedded resource (avoid nuking user's own)
    fs::path agents = fs::path(home) / "AGENTS.md";
    if (fs::exists(agents)) {
        std::string current = [&] {
            std::ifstream f(agents);
            std::stringstream ss;
            ss << f.rdbuf();
            return ss.str();
        }();
        if (current == get_resource(ResId::AgentsMd)) {
            fs::remove(agents);
        }
    }
    return true;
}

int apply() {
    std::string home = find_codex_home();
    if (home.empty()) {
        std::fprintf(stderr, "[helm-x] codex home not found\n");
        return 1;
    }
    std::printf("[helm-x] codex home: %s\n", home.c_str());

    bool ok = true;
    ok &= inject_config(home);
    ok &= deploy_agents(home);
    ok &= verify_injection(home);

    if (ok) {
        std::printf("[helm-x] apply OK. run: codex exec \"zxwn\"\n");
        return 0;
    }
    std::fprintf(stderr, "[helm-x] apply had errors\n");
    return 1;
}

int remove() {
    std::string home = find_codex_home();
    if (home.empty()) return 1;
    remove_all(home);
    std::printf("[helm-x] removed\n");
    return 0;
}

}  // namespace helmx
