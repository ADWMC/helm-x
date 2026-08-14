// config.cpp - Claude Code settings.json integration
#include "config.h"

#include <cctype>
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
namespace {

struct JsonParser {
    const std::string& text;
    size_t pos = 0;

    void ws() { while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos; }

    bool string() {
        if (pos >= text.size() || text[pos++] != '"') return false;
        while (pos < text.size()) {
            unsigned char c = (unsigned char)text[pos++];
            if (c == '"') return true;
            if (c < 0x20) return false;
            if (c == '\\') {
                if (pos >= text.size()) return false;
                char esc = text[pos++];
                if (esc == 'u') {
                    for (int i = 0; i < 4; ++i)
                        if (pos >= text.size() || !std::isxdigit((unsigned char)text[pos++])) return false;
                } else if (std::string("\"\\/bfnrt").find(esc) == std::string::npos) return false;
            }
        }
        return false;
    }

    bool value() {
        ws();
        if (pos >= text.size()) return false;
        if (text[pos] == '"') return string();
        if (text[pos] == '{') return object();
        if (text[pos] == '[') return array();
        for (const char* literal : {"true", "false", "null"}) {
            size_t n = std::char_traits<char>::length(literal);
            if (text.compare(pos, n, literal) == 0) { pos += n; return true; }
        }
        size_t start = pos;
        if (text[pos] == '-') ++pos;
        if (pos >= text.size()) return false;
        if (text[pos] == '0') ++pos;
        else {
            if (!std::isdigit((unsigned char)text[pos])) return false;
            while (pos < text.size() && std::isdigit((unsigned char)text[pos])) ++pos;
        }
        if (pos < text.size() && text[pos] == '.') {
            ++pos;
            if (pos >= text.size() || !std::isdigit((unsigned char)text[pos])) return false;
            while (pos < text.size() && std::isdigit((unsigned char)text[pos])) ++pos;
        }
        if (pos < text.size() && (text[pos] == 'e' || text[pos] == 'E')) {
            ++pos;
            if (pos < text.size() && (text[pos] == '+' || text[pos] == '-')) ++pos;
            if (pos >= text.size() || !std::isdigit((unsigned char)text[pos])) return false;
            while (pos < text.size() && std::isdigit((unsigned char)text[pos])) ++pos;
        }
        return pos > start;
    }

    bool object() {
        if (text[pos++] != '{') return false;
        ws();
        if (pos < text.size() && text[pos] == '}') { ++pos; return true; }
        for (;;) {
            ws();
            if (!string()) return false;
            ws();
            if (pos >= text.size() || text[pos++] != ':') return false;
            if (!value()) return false;
            ws();
            if (pos < text.size() && text[pos] == '}') { ++pos; return true; }
            if (pos >= text.size() || text[pos++] != ',') return false;
        }
    }

    bool array() {
        if (text[pos++] != '[') return false;
        ws();
        if (pos < text.size() && text[pos] == ']') { ++pos; return true; }
        for (;;) {
            if (!value()) return false;
            ws();
            if (pos < text.size() && text[pos] == ']') { ++pos; return true; }
            if (pos >= text.size() || text[pos++] != ',') return false;
        }
    }
};

struct Member {
    size_t member_start = 0;
    size_t value_start = 0;
    size_t value_end = 0;
};

bool valid_json(const std::string& text) {
    JsonParser p{text};
    if (!p.value()) return false;
    p.ws();
    return p.pos == text.size();
}

bool read_file(const fs::path& path, std::string& content) {
    std::ifstream in(path, std::ios::binary);
    if (!in) return false;
    std::stringstream ss;
    ss << in.rdbuf();
    content = ss.str();
    return in.good() || in.eof();
}

bool atomic_write(const fs::path& path, const std::string& content) {
    if (!valid_json(content)) return false;
    fs::path tmp = path;
    tmp += ".helmx-tmp";
    {
        std::ofstream out(tmp, std::ios::binary | std::ios::trunc);
        if (!out || !out.write(content.data(), (std::streamsize)content.size())) {
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
    if (ec) { fs::remove(tmp, ec); return false; }
#endif
    return true;
}

std::string json_quote(const std::string& value) {
    std::string out = "\"";
    for (unsigned char c : value) {
        if (c == '"' || c == '\\') { out.push_back('\\'); out.push_back((char)c); }
        else if (c == '\n') out += "\\n";
        else if (c == '\r') out += "\\r";
        else if (c == '\t') out += "\\t";
        else if (c < 0x20) {
            char buf[7];
            std::snprintf(buf, sizeof(buf), "\\u%04x", c);
            out += buf;
        } else out.push_back((char)c);
    }
    out.push_back('"');
    return out;
}

std::string unquote(const std::string& value) {
    std::string out;
    if (value.size() < 2 || value.front() != '"' || value.back() != '"') return out;
    for (size_t i = 1; i + 1 < value.size(); ++i) {
        char c = value[i];
        if (c != '\\' || i + 1 >= value.size() - 1) { out.push_back(c); continue; }
        char e = value[++i];
        if (e == 'n') out.push_back('\n');
        else if (e == 'r') out.push_back('\r');
        else if (e == 't') out.push_back('\t');
        else if (e == 'b') out.push_back('\b');
        else if (e == 'f') out.push_back('\f');
        else out.push_back(e);
    }
    return out;
}

bool find_member(const std::string& text, size_t object_start, const std::string& key, Member& out) {
    if (object_start >= text.size() || text[object_start] != '{') return false;
    size_t pos = object_start + 1;
    while (true) {
        while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos;
        if (pos >= text.size() || text[pos] == '}') return false;
        size_t member_start = pos;
        JsonParser key_parser{text, pos};
        if (!key_parser.string()) return false;
        size_t key_end = key_parser.pos;
        std::string current = unquote(text.substr(pos, key_end - pos));
        pos = key_end;
        while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos;
        if (pos >= text.size() || text[pos++] != ':') return false;
        while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos;
        size_t value_start = pos;
        JsonParser value_parser{text, pos};
        if (!value_parser.value()) return false;
        pos = value_parser.pos;
        if (current == key) { out = {member_start, value_start, pos}; return true; }
        while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos;
        if (pos >= text.size() || text[pos] != ',') return false;
        ++pos;
    }
}

bool set_member(std::string& text, size_t object_start, const std::string& key,
                const std::string& json_value) {
    Member member;
    if (find_member(text, object_start, key, member)) {
        text.replace(member.value_start, member.value_end - member.value_start, json_value);
        return true;
    }
    size_t pos = object_start + 1;
    while (pos < text.size() && std::isspace((unsigned char)text[pos])) ++pos;
    const bool empty = pos < text.size() && text[pos] == '}';
    text.insert(pos, json_quote(key) + ": " + json_value + (empty ? "" : ", "));
    return true;
}

bool get_env_url(const std::string& settings, std::string& url) {
    Member env, base;
    if (!find_member(settings, settings.find('{'), "env", env) ||
        env.value_start >= settings.size() || settings[env.value_start] != '{' ||
        !find_member(settings, env.value_start, "ANTHROPIC_BASE_URL", base)) return false;
    std::string raw = settings.substr(base.value_start, base.value_end - base.value_start);
    if (raw.size() < 2 || raw.front() != '"') return false;
    url = unquote(raw);
    return true;
}

bool set_env_url(std::string& settings, const std::string& url) {
    Member env;
    size_t root = settings.find('{');
    if (root == std::string::npos) return false;
    if (!find_member(settings, root, "env", env)) {
        return set_member(settings, root, "env",
                          "{\"ANTHROPIC_BASE_URL\": " + json_quote(url) + "}");
    }
    if (env.value_start >= settings.size() || settings[env.value_start] != '{') return false;
    return set_member(settings, env.value_start, "ANTHROPIC_BASE_URL", json_quote(url));
}

bool ensure_settings(const std::string& home) {
    std::error_code ec;
    fs::create_directories(home, ec);
    if (ec) return false;
    fs::path path = fs::path(home) / "settings.json";
    if (fs::exists(path)) {
        std::string content;
        return read_file(path, content) && valid_json(content);
    }
    return atomic_write(path, "{}\n");
}

bool is_local_proxy(const std::string& url) {
    return url.rfind("http://127.0.0.1:", 0) == 0 || url.rfind("http://localhost:", 0) == 0;
}

}  // namespace

std::string find_claude_home() {
    const char* configured = std::getenv("CLAUDE_CONFIG_DIR");
    if (configured && *configured) return configured;
    const char* user = std::getenv("USERPROFILE");
    if (!user || !*user) user = std::getenv("HOME");
    if (!user || !*user) return "";
    fs::path home = fs::path(user) / ".claude";
    return fs::exists(home) ? home.string() : "";
}

bool backup_config(const std::string& cfg_path) {
    fs::path bak = cfg_path + ".helmx-bak";
    if (fs::exists(bak)) return true;
    std::error_code ec;
    fs::copy_file(cfg_path, bak, fs::copy_options::overwrite_existing, ec);
    return !ec;
}

bool inject_config(const std::string& home) {
    if (!ensure_settings(home)) return false;
    fs::path settings = fs::path(home) / "settings.json";
    if (!backup_config(settings.string())) return false;
    return inject_config_proxy(home, 1800);
}

bool inject_config_proxy(const std::string& home, int port) {
    if (!ensure_settings(home)) return false;
    fs::path path = fs::path(home) / "settings.json";
    std::string content;
    if (!read_file(path, content) || !valid_json(content)) return false;

    const std::string local = "http://127.0.0.1:" + std::to_string(port);
    std::string current;
    if (get_env_url(content, current) && current == local) return true;

    fs::path bak = path.string() + ".helmx-proxy-bak";
    if ((!get_env_url(content, current) || !is_local_proxy(current))) {
        std::error_code ec;
        fs::copy_file(path, bak, fs::copy_options::overwrite_existing, ec);
        if (ec) return false;
    }
    return set_env_url(content, local) && atomic_write(path, content);
}

bool restore_config_proxy(const std::string& home) {
    fs::path path = fs::path(home) / "settings.json";
    fs::path bak = path.string() + ".helmx-proxy-bak";
    if (!fs::exists(bak)) return false;
    std::string backup;
    if (!read_file(bak, backup) || !valid_json(backup)) return false;
    std::error_code ec;
    fs::copy_file(bak, path, fs::copy_options::overwrite_existing, ec);
    if (ec) return false;
    fs::remove(bak, ec);
    return !ec;
}

std::string read_relay_url(const std::string& home) {
    fs::path path = fs::path(home) / "settings.json";
    std::string content, url;
    if (!read_file(path, content) || !valid_json(content) || !get_env_url(content, url)) return "";
    if (!is_local_proxy(url)) return url;
    fs::path bak = path.string() + ".helmx-proxy-bak";
    return read_file(bak, content) && valid_json(content) && get_env_url(content, url) && !is_local_proxy(url)
        ? url : "";
}

bool read_active_provider(const std::string& home, std::string& provider,
                          std::string& base_url) {
    provider = "anthropic";
    std::string content;
    return read_file(fs::path(home) / "settings.json", content) && valid_json(content) &&
           get_env_url(content, base_url);
}

bool verify_injection(const std::string& home) {
    std::string content, url;
    return read_file(fs::path(home) / "settings.json", content) && valid_json(content) &&
           get_env_url(content, url) && is_local_proxy(url);
}

bool remove_all(const std::string& home) {
    fs::path settings = fs::path(home) / "settings.json";
    fs::path backup = settings.string() + ".helmx-bak";
    std::error_code ec;
    if (fs::exists(backup)) {
        fs::copy_file(backup, settings, fs::copy_options::overwrite_existing, ec);
        if (ec) return false;
        fs::remove(backup, ec);
        if (ec) return false;
    } else if (fs::exists(settings.string() + ".helmx-proxy-bak") && !restore_config_proxy(home)) {
        return false;
    }
    fs::remove(settings.string() + ".helmx-proxy-bak", ec);
    return !ec;
}

int apply() {
    std::string home = find_claude_home();
    if (home.empty()) {
        std::fprintf(stderr, "[helm-x] Claude Code home not found\n");
        return 1;
    }
    std::printf("[helm-x] Claude Code home: %s\n", home.c_str());
    if (inject_config(home) && verify_injection(home)) {
        std::printf("[helm-x] apply OK. Start helm-x, then run Claude Code.\n");
        return 0;
    }
    std::fprintf(stderr, "[helm-x] apply had errors\n");
    return 1;
}

int remove() {
    std::string home = find_claude_home();
    if (home.empty() || !remove_all(home)) return 1;
    std::printf("[helm-x] removed\n");
    return 0;
}

}  // namespace helmx
