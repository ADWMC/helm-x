// config.cpp — config.toml merge injection + backup + validation
#include "config.h"
#include "obf.h"
#include "resources.h"

#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <sstream>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#endif

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
    //    mcp_servers section (idempotent insert) — use THIS exe's absolute path
    if (content.find("[mcp_servers.helmx]") == std::string::npos) {
        // resolve own exe path (so MCP works no matter where helmx.exe lives)
        std::string exe_path = "helmx";
#ifdef _WIN32
        char buf[MAX_PATH] = {0};
        DWORD n = GetModuleFileNameA(nullptr, buf, MAX_PATH);
        if (n > 0 && n < MAX_PATH) {
            exe_path = buf;
            // normalize backslashes for TOML
            for (auto& ch : exe_path) if (ch == '\\') ch = '/';
        }
#endif
        std::string mcp_section =
            "\n[mcp_servers.helmx]\n"
            "command = \"" + exe_path + "\"\n"
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

bool inject_config_proxy(const std::string& home, int port) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) return false;

    // back up original base_url once (proxy-mode backup)
    fs::path bak = cfg.string() + ".helmx-proxy-bak";
    if (!fs::exists(bak)) {
        std::error_code ec;
        fs::copy_file(cfg, bak, fs::copy_options::overwrite_existing, ec);
    }

    std::ifstream in(cfg);
    std::stringstream ss;
    ss << in.rdbuf();
    std::string content = ss.str();

    // already pointed at this proxy?
    std::string needle = "127.0.0.1:" + std::to_string(port);
    if (content.find(needle) != std::string::npos) return true;

    // replace base_url with local proxy
    size_t p = content.find("base_url");
    if (p == std::string::npos) return false;
    size_t eq = content.find('=', p);
    if (eq == std::string::npos) return false;
    size_t q1 = content.find('"', eq);
    if (q1 == std::string::npos) return false;
    size_t q2 = content.find('"', q1 + 1);
    if (q2 == std::string::npos) return false;
    std::string new_url = "http://127.0.0.1:" + std::to_string(port) + "/v1";
    content.replace(q1 + 1, q2 - q1 - 1, new_url);

    std::ofstream out(cfg, std::ios::trunc);
    out << content;
    out.close();
    return true;
}

bool restore_config_proxy(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path bak = cfg.string() + ".helmx-proxy-bak";
    if (!fs::exists(bak)) return false;
    std::error_code ec;
    fs::copy_file(bak, cfg, fs::copy_options::overwrite_existing, ec);
    if (ec) return false;
    fs::remove(bak);
    return true;
}

std::string read_relay_url(const std::string& home) {
    // prefer backup (live config may point at the proxy itself)
    fs::path bak = fs::path(home) / "config.toml.helmx-proxy-bak";
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path src = fs::exists(bak) ? bak : cfg;
    std::ifstream f(src);
    if (!f) return "";
    std::stringstream ss;
    ss << f.rdbuf();
    std::string content = ss.str();
    size_t p = content.find("base_url");
    if (p == std::string::npos) return "";
    size_t q1 = content.find('"', p);
    if (q1 == std::string::npos) return "";
    size_t q2 = content.find('"', q1 + 1);
    if (q2 == std::string::npos) return "";
    return content.substr(q1 + 1, q2 - q1 - 1);
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
        std::printf("[helm-x] apply OK. run: helmx activate  (sends activation word)\n");
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
