// config.cpp — config.toml merge injection + backup + validation
#include "config.h"
#include "obf.h"
#include "resources.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
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
            if (c == ']') {
                if (--depth < 0) return false;
            }
        }
    }
    return depth == 0;
}

// Read a simple TOML string assignment without matching comments or table names.
static bool read_string_assignment(const std::string& content, const char* key,
                                   std::string& value) {
    std::istringstream lines(content);
    std::string line;
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        size_t key_start = first;
        size_t key_len = std::strlen(key);
        if (line.compare(key_start, key_len, key) != 0) continue;
        if (key_start + key_len < line.size() &&
            line[key_start + key_len] != ' ' && line[key_start + key_len] != '\t' &&
            line[key_start + key_len] != '=') continue;
        size_t eq = line.find('=', key_start + key_len);
        if (eq == std::string::npos) continue;
        size_t q1 = line.find('"', eq + 1);
        if (q1 == std::string::npos) continue;
        size_t q2 = line.find('"', q1 + 1);
        if (q2 == std::string::npos) continue;
        value = line.substr(q1 + 1, q2 - q1 - 1);
        return true;
    }
    return false;
}

static bool read_top_level_string_assignment(const std::string& content, const char* key,
                                              std::string& value) {
    std::istringstream lines(content);
    std::string line;
    bool in_table = false;
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        if (line[first] == '[') {
            in_table = true;
            continue;
        }
        if (in_table) continue;
        std::string one_line = line;
        if (read_string_assignment(one_line, key, value)) return true;
    }
    return false;
}

static bool provider_base_url(const std::string& content, const std::string& provider,
                              std::string& value) {
    const std::string header = "[model_providers." + provider + "]";
    std::istringstream lines(content);
    std::string line;
    bool in_provider = false;
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        if (line[first] == '[') {
            in_provider = line.compare(first, header.size(), header) == 0 &&
                          (first + header.size() == line.size() ||
                           line[first + header.size()] == ' ' ||
                           line[first + header.size()] == '\t' ||
                           line[first + header.size()] == '\r' ||
                           line[first + header.size()] == '#');
            continue;
        }
        if (in_provider && read_string_assignment(line, "base_url", value)) return true;
    }
    return false;
}

static bool active_provider(const std::string& content, std::string& provider) {
    return read_top_level_string_assignment(content, "model_provider", provider);
}

static bool replace_string_assignment(std::string& content, const char* key,
                                      const std::string& value) {
    size_t offset = 0;
    std::string line;
    std::istringstream lines(content);
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        size_t key_len = std::strlen(key);
        if (first != std::string::npos && line[first] != '#' &&
            line.compare(first, key_len, key) == 0 &&
            (first + key_len == line.size() || line[first + key_len] == ' ' ||
             line[first + key_len] == '\t' || line[first + key_len] == '=')) {
            size_t eq = line.find('=', first + key_len);
            size_t q1 = eq == std::string::npos ? std::string::npos : line.find('"', eq + 1);
            size_t q2 = q1 == std::string::npos ? std::string::npos : line.find('"', q1 + 1);
            if (q1 != std::string::npos && q2 != std::string::npos) {
                content.replace(offset + q1 + 1, q2 - q1 - 1, value);
                return true;
            }
        }
        offset += line.size() + 1;
    }
    return false;
}

static bool replace_provider_base_url(std::string& content, const std::string& provider,
                                      const std::string& value) {
    const std::string header = "[model_providers." + provider + "]";
    size_t offset = 0;
    std::istringstream lines(content);
    std::string line;
    bool in_provider = false;
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first != std::string::npos && line[first] == '[') {
            in_provider = line.compare(first, header.size(), header) == 0 &&
                          (first + header.size() == line.size() ||
                           line[first + header.size()] == ' ' ||
                           line[first + header.size()] == '\t' ||
                           line[first + header.size()] == '\r' ||
                           line[first + header.size()] == '#');
        } else if (in_provider && replace_string_assignment(line, "base_url", value)) {
            content.replace(offset, line.size(), line);
            return true;
        }
        offset += line.size() + 1;
    }
    return false;
}

static bool has_model_provider_custom(const std::string& content) {
    std::string provider;
    return active_provider(content, provider) && provider == "custom";
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
    if (!in) return false;
    std::stringstream ss;
    ss << in.rdbuf();
    std::string content = ss.str();

    // 1. model_provider = "custom" (ensure present)
    if (!has_model_provider_custom(content)) {
        // Keep an existing assignment intact when it has the required value.
        std::string provider;
        if (!read_string_assignment(content, "model_provider", provider)) {
            content = "model_provider = \"custom\"\n" + content;
        } else replace_string_assignment(content, "model_provider", "custom");
    }

    std::ofstream out(cfg, std::ios::trunc);
    if (!out) return false;
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
        if (ec) return false;
    }

    std::ifstream in(cfg);
    if (!in) return false;
    std::stringstream ss;
    ss << in.rdbuf();
    std::string content = ss.str();

    // already pointed at this proxy?
    std::string current_url;
    std::string new_url = "http://127.0.0.1:" + std::to_string(port) + "/v1";
    std::string provider;
    if (!active_provider(content, provider)) return false;
    if (provider_base_url(content, provider, current_url) && current_url == new_url) return true;

    // replace base_url with local proxy
    if (!replace_provider_base_url(content, provider, new_url)) return false;

    std::ofstream out(cfg, std::ios::trunc);
    if (!out) return false;
    out << content;
    out.close();
    return true;
}

bool restore_config_proxy(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path bak = cfg.string() + ".helmx-proxy-bak";
    if (!fs::exists(bak)) return false;

    // Read current config and preserve unrelated user settings.
    std::ifstream cur_f(cfg);
    if (!cur_f) return false;
    std::stringstream cur_ss;
    cur_ss << cur_f.rdbuf();
    std::string current = cur_ss.str();

    // Read backup to get original base_url
    std::ifstream bak_f(bak);
    if (!bak_f) return false;
    std::stringstream bak_ss;
    bak_ss << bak_f.rdbuf();
    std::string backup = bak_ss.str();

    // Extract original base_url from backup
    std::string provider;
    std::string original_url;
    if (!active_provider(current, provider) || !provider_base_url(backup, provider, original_url)) {
        // No base_url in backup, just delete backup
        fs::remove(bak);
        return true;
    }
    // Replace base_url in current config with original
    std::string current_url;
    if (provider_base_url(current, provider, current_url) &&
        replace_provider_base_url(current, provider, original_url)) {
        std::ofstream out(cfg, std::ios::trunc);
        if (!out) return false;
        out << current;
        out.close();
    }

    else return false;

    std::error_code remove_ec;
    fs::remove(bak, remove_ec);
    if (remove_ec) return false;
    return true;
}

std::string read_relay_url(const std::string& home) {
    // Prefer the active provider in the live config. Use the proxy backup only
    // while that same provider is pointed at the local proxy.
    fs::path bak = fs::path(home) / "config.toml.helmx-proxy-bak";
    fs::path cfg = fs::path(home) / "config.toml";

    std::string provider;
    std::string url;
    if (!read_active_provider(home, provider, url)) return "";
    if (url.find("127.0.0.1") == std::string::npos) return url;

    if (!fs::exists(bak)) return "";
    std::ifstream bf(bak);
    if (!bf) return "";
    std::stringstream backup_stream;
    backup_stream << bf.rdbuf();
    if (provider_base_url(backup_stream.str(), provider, url) &&
        url.find("127.0.0.1") == std::string::npos) return url;

    return "";
}

bool read_active_provider(const std::string& home, std::string& provider,
                          std::string& base_url) {
    std::ifstream cf(fs::path(home) / "config.toml");
    if (!cf) return false;
    std::stringstream ss;
    ss << cf.rdbuf();
    const std::string content = ss.str();
    return active_provider(content, provider) &&
           provider_base_url(content, provider, base_url);
}

bool verify_injection(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) return false;
    std::ifstream f(cfg);
    std::stringstream ss;
    ss << f.rdbuf();
    std::string c = ss.str();
    // AGENTS.md is injected by the proxy; config state is the durable marker.
    return has_model_provider_custom(c);
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
    bool ok = true;
    // restore config from backup
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path bak = cfg.string() + ".helmx-bak";
    if (fs::exists(bak)) {
        std::error_code ec;
        fs::copy_file(bak, cfg, fs::copy_options::overwrite_existing, ec);
        if (ec) ok = false;
        std::error_code remove_ec;
        fs::remove(bak, remove_ec);
        if (remove_ec) ok = false;
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
            std::error_code ec;
            fs::remove(agents, ec);
            if (ec) ok = false;
        }
    }
    return ok;
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
