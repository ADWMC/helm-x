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

static bool read_file(const fs::path& path, std::string& content) {
    std::ifstream in(path, std::ios::binary);
    if (!in) return false;
    std::stringstream ss;
    ss << in.rdbuf();
    content = ss.str();
    return in.good() || in.eof();
}

static bool toml_valid_content(const std::string& content) {
    std::istringstream lines(content);
    std::string line;
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        if (line[first] == '[') {
            const bool array_table = first + 1 < line.size() && line[first + 1] == '[';
            size_t close = line.find(array_table ? "]]" : "]", first + 1);
            if (close == std::string::npos || close == first + 1) return false;
            size_t tail = line.find_first_not_of(" \t\r", close + (array_table ? 2 : 1));
            if (tail != std::string::npos && line[tail] != '#') return false;
            continue;
        }
        size_t eq = line.find('=', first);
        if (eq == std::string::npos || eq == first) return false;
        size_t value = line.find_first_not_of(" \t", eq + 1);
        if (value == std::string::npos) return false;
        if (line[value] == '"') {
            bool escaped = false;
            size_t close = std::string::npos;
            for (size_t i = value + 1; i < line.size(); ++i) {
                if (line[i] == '"' && !escaped) {
                    close = i;
                    break;
                }
                escaped = line[i] == '\\' && !escaped;
                if (line[i] != '\\') escaped = false;
            }
            if (close == std::string::npos) return false;
            size_t tail = line.find_first_not_of(" \t\r", close + 1);
            if (tail != std::string::npos && line[tail] != '#') return false;
        }
    }
    return true;
}

static bool atomic_write(const fs::path& path, const std::string& content) {
    if (!toml_valid_content(content)) return false;
    fs::path tmp = path;
    tmp += ".helmx-tmp";
    {
        std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
        if (!out || !out.write(content.data(), static_cast<std::streamsize>(content.size()))) {
            std::error_code ec;
            fs::remove(tmp, ec);
            return false;
        }
    }
#ifdef _WIN32
    if (!MoveFileExW(tmp.c_str(), path.c_str(), MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH)) {
        std::error_code ec;
        fs::remove(tmp, ec);
        return false;
    }
#else
    std::error_code ec;
    fs::rename(tmp, path, ec);
    if (ec) {
        fs::remove(tmp, ec);
        return false;
    }
#endif
    return true;
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
        } else if (in_provider) {
            const size_t original_size = line.size();
            if (!replace_string_assignment(line, "base_url", value)) {
                offset += original_size + 1;
                continue;
            }
            content.replace(offset, original_size, line);
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

static bool has_top_level_assignment(const std::string& content, const char* key) {
    std::istringstream lines(content);
    std::string line;
    bool in_table = false;
    const size_t key_len = std::strlen(key);
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        if (line[first] == '[') {
            in_table = true;
            continue;
        }
        if (in_table || line.compare(first, key_len, key) != 0) continue;
        if (first + key_len < line.size() && line[first + key_len] != ' ' &&
            line[first + key_len] != '\t' && line[first + key_len] != '=') continue;
        return line.find('=', first + key_len) != std::string::npos;
    }
    return false;
}

static bool read_top_level_int_assignment(const std::string& content, const char* key, int& value) {
    std::istringstream lines(content);
    std::string line;
    bool in_table = false;
    const size_t key_len = std::strlen(key);
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first == std::string::npos || line[first] == '#') continue;
        if (line[first] == '[') { in_table = true; continue; }
        if (in_table || line.compare(first, key_len, key) != 0) continue;
        if (first + key_len < line.size() && line[first + key_len] != ' ' &&
            line[first + key_len] != '\t' && line[first + key_len] != '=') continue;
        size_t eq = line.find('=', first + key_len);
        if (eq == std::string::npos) continue;
        try { value = std::stoi(line.substr(eq + 1)); return true; } catch (...) { return false; }
    }
    return false;
}

static bool replace_top_level_assignment(std::string& content, const char* key,
                                         const std::string& value) {
    size_t offset = 0;
    std::istringstream lines(content);
    std::string line;
    bool in_table = false;
    const size_t key_len = std::strlen(key);
    while (std::getline(lines, line)) {
        size_t first = line.find_first_not_of(" \t\r");
        if (first != std::string::npos && line[first] == '[') in_table = true;
        if (!in_table && first != std::string::npos && line[first] != '#' &&
            line.compare(first, key_len, key) == 0 &&
            (first + key_len == line.size() || line[first + key_len] == ' ' ||
             line[first + key_len] == '\t' || line[first + key_len] == '=')) {
            content.replace(offset, line.size(), std::string(key) + " = " + value);
            return true;
        }
        offset += line.size() + 1;
    }
    return false;
}

static void inject_context_request_defaults(std::string& content) {
    const char* eol = content.find("\r\n") != std::string::npos ? "\r\n" : "\n";
    std::string defaults;
    if (!has_top_level_assignment(content, "tool_output_token_limit"))
        defaults += std::string("tool_output_token_limit = 8000") + eol;
    if (!has_top_level_assignment(content, "model_auto_compact_token_limit"))
        defaults += std::string("model_auto_compact_token_limit = 180000") + eol;
    if (!has_top_level_assignment(content, "model_auto_compact_token_limit_scope"))
        defaults += std::string("model_auto_compact_token_limit_scope = \"body_after_prefix\"") + eol;
    content.insert(0, defaults);
}

bool read_context_request_config(const std::string& home, ContextRequestConfig& cfg) {
    std::string content;
    if (!read_file(fs::path(home) / "config.toml", content)) return false;
    read_top_level_int_assignment(content, "tool_output_token_limit", cfg.tool_output_token_limit);
    read_top_level_int_assignment(content, "model_auto_compact_token_limit", cfg.model_auto_compact_token_limit);
    read_top_level_string_assignment(content, "model_auto_compact_token_limit_scope",
                                     cfg.model_auto_compact_token_limit_scope);
    return true;
}

bool write_context_request_config(const std::string& home, const ContextRequestConfig& cfg) {
    fs::path path = fs::path(home) / "config.toml";
    std::string content;
    if (!read_file(path, content) || !toml_valid_content(content)) return false;
    const char* eol = content.find("\r\n") != std::string::npos ? "\r\n" : "\n";
    auto set = [&](const char* key, const std::string& value) {
        if (!replace_top_level_assignment(content, key, value))
            content.insert(0, std::string(key) + " = " + value + eol);
    };
    set("tool_output_token_limit", std::to_string(cfg.tool_output_token_limit));
    set("model_auto_compact_token_limit", std::to_string(cfg.model_auto_compact_token_limit));
    set("model_auto_compact_token_limit_scope",
        "\"" + cfg.model_auto_compact_token_limit_scope + "\"");
    return toml_valid_content(content) && atomic_write(path, content);
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

    std::string content;
    if (!read_file(cfg, content) || !toml_valid_content(content)) return false;

    // 1. model_provider = "custom" (ensure present)
    if (!has_model_provider_custom(content)) {
        // Keep an existing assignment intact when it has the required value.
        std::string provider;
        if (!read_string_assignment(content, "model_provider", provider)) {
            content = "model_provider = \"custom\"\n" + content;
        } else replace_string_assignment(content, "model_provider", "custom");
    }

    // Keep Codex request history bounded before large tool observations make
    // every subsequent Responses API request grow by several megabytes.
    // Existing user values are authoritative and are never overwritten.
    inject_context_request_defaults(content);

    return atomic_write(cfg, content);
}

bool inject_config_proxy(const std::string& home, int port) {
    fs::path cfg = fs::path(home) / "config.toml";
    if (!fs::exists(cfg)) return false;

    std::string content;
    if (!read_file(cfg, content) || !toml_valid_content(content)) return false;
    const std::string original_content = content;

    // Double-click startup reaches this path without requiring a prior `apply`.
    inject_context_request_defaults(content);

    std::string current_url;
    std::string new_url = "http://127.0.0.1:" + std::to_string(port) + "/v1";
    std::string provider;
    if (!active_provider(content, provider) ||
        !provider_base_url(content, provider, current_url)) return false;
    if (current_url == new_url) {
        return content == original_content || atomic_write(cfg, content);
    }

    // Refresh the restore point from each valid, non-proxy configuration.
    fs::path bak = cfg.string() + ".helmx-proxy-bak";
    if (current_url.find("127.0.0.1") == std::string::npos &&
        !atomic_write(bak, content)) return false;

    // replace base_url with local proxy
    if (!replace_provider_base_url(content, provider, new_url)) return false;
    return atomic_write(cfg, content);
}

bool restore_config_proxy(const std::string& home) {
    fs::path cfg = fs::path(home) / "config.toml";
    fs::path bak = cfg.string() + ".helmx-proxy-bak";
    if (!fs::exists(bak)) return false;

    // Read current config and preserve unrelated user settings.
    std::string current;
    if (!read_file(cfg, current) || !toml_valid_content(current)) return false;

    // Read backup to get original base_url
    std::string backup;
    if (!read_file(bak, backup) || !toml_valid_content(backup)) return false;

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
        if (!atomic_write(cfg, current)) return false;
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
    return has_model_provider_custom(c) &&
           has_top_level_assignment(c, "tool_output_token_limit") &&
           has_top_level_assignment(c, "model_auto_compact_token_limit") &&
           has_top_level_assignment(c, "model_auto_compact_token_limit_scope");
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
