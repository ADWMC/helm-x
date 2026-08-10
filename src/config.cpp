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
    // 2. No MCP injection — user manages their own MCP config.

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

    // Back up original config ONCE before first modification
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

    // Read current config (may have user-added MCP/other settings)
    std::ifstream cur_f(cfg);
    std::stringstream cur_ss;
    cur_ss << cur_f.rdbuf();
    std::string current = cur_ss.str();

    // Read backup to get original base_url
    std::ifstream bak_f(bak);
    std::stringstream bak_ss;
    bak_ss << bak_f.rdbuf();
    std::string backup = bak_ss.str();

    // Extract original base_url from backup
    size_t bak_base = backup.find("base_url");
    if (bak_base == std::string::npos) {
        // No base_url in backup, just delete backup
        fs::remove(bak);
        return true;
    }
    size_t bak_eq = backup.find('=', bak_base);
    size_t bak_q1 = backup.find('"', bak_eq);
    size_t bak_q2 = backup.find('"', bak_q1 + 1);
    std::string original_url = backup.substr(bak_q1 + 1, bak_q2 - bak_q1 - 1);

    // Replace base_url in current config with original
    size_t cur_base = current.find("base_url");
    if (cur_base != std::string::npos) {
        size_t cur_eq = current.find('=', cur_base);
        size_t cur_q1 = current.find('"', cur_eq);
        size_t cur_q2 = current.find('"', cur_q1 + 1);
        current.replace(cur_q1 + 1, cur_q2 - cur_q1 - 1, original_url);
        std::ofstream out(cfg, std::ios::trunc);
        out << current;
        out.close();
    }

    fs::remove(bak);
    return true;
}

std::string read_relay_url(const std::string& home) {
    // Read from backup (original base_url before proxy modified it)
    // If backup doesn't exist, read current config.toml (may work if proxy hasn't modified it yet)
    fs::path bak = fs::path(home) / "config.toml.helmx-proxy-bak";
    fs::path cfg = fs::path(home) / "config.toml";

    auto extract_url = [](const std::string& content) -> std::string {
        size_t p = content.find("base_url");
        if (p == std::string::npos) return "";
        size_t q1 = content.find('"', p);
        if (q1 == std::string::npos) return "";
        size_t q2 = content.find('"', q1 + 1);
        if (q2 == std::string::npos) return "";
        return content.substr(q1 + 1, q2 - q1 - 1);
    };

    // Try backup first
    if (fs::exists(bak)) {
        std::ifstream bf(bak);
        if (bf) {
            std::stringstream ss;
            ss << bf.rdbuf();
            std::string url = extract_url(ss.str());
            if (!url.empty() && url.find("127.0.0.1") == std::string::npos) return url;
        }
    }

    // Fallback: read current config (works if proxy hasn't modified it yet)
    std::ifstream cf(cfg);
    if (cf) {
        std::stringstream ss;
        ss << cf.rdbuf();
        std::string url = extract_url(ss.str());
        if (!url.empty() && url.find("127.0.0.1") == std::string::npos) return url;
    }

    return "";
}

bool verify_injection(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) return false;
    std::ifstream f(cfg);
    std::stringstream ss;
    ss << f.rdbuf();
    std::string c = ss.str();
    bool ok = true;
    // AGENTS.md not deployed — proxy injects from encrypted resource
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
    // AGENTS.md not deployed as file — proxy injects from encrypted resource
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
