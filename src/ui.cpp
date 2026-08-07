// ui.cpp — Web dashboard: status / rules / actions / services
#include "ui.h"

#include "config.h"
#include "http.h"
#include "log.h"
#include "resources.h"
#include "rewrite.h"
#include "tamper.h"
#include "verify.h"
#include "watch.h"

#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <filesystem>
#include <fstream>
#include <mutex>
#include <sstream>
#include <string>
#include <thread>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>
#pragma comment(lib, "ws2_32.lib")
#endif

namespace fs = std::filesystem;

namespace helmx {

// ── async task state (zxwn runs up to 4 min) ──
namespace {
std::mutex g_task_mutex;
std::atomic<bool> g_task_running{false};
std::string g_task_output;      // guarded by g_task_mutex
bool g_task_activated = false;  // guarded by g_task_mutex

void async_zxwn() {
    std::string out;
    bool activated = false;
    int rc = run_zxwn(out, activated);
    (void)rc;
    std::lock_guard<std::mutex> lock(g_task_mutex);
    g_task_output = out;
    g_task_activated = activated;
    g_task_running = false;
    log_info(std::string("ui: zxwn done, activated=") + (activated ? "true" : "false"));
}

void start_zxwn_task() {
    if (g_task_running.load()) return;
    g_task_running = true;
    {
        std::lock_guard<std::mutex> lock(g_task_mutex);
        g_task_output.clear();
        g_task_activated = false;
    }
    log_info("ui: zxwn task started");
    std::thread(async_zxwn).detach();
}
}  // namespace

static std::string json_escape(const std::string& s) {
    std::string out;
    out.reserve(s.size() + 8);
    for (char c : s) {
        switch (c) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default:
                if ((unsigned char)c < 0x20) {
                    char buf[8];
                    std::snprintf(buf, sizeof(buf), "\\u%04x", c);
                    out += buf;
                } else {
                    out += c;
                }
        }
    }
    return out;
}

// ── API handlers ──

static HttpResponse api_status(const HttpRequest&) {
    std::string home = find_codex_home();
    bool injected = !home.empty() && verify_injection(home);
    std::string body =
        "{\"home\":\"" + json_escape(home) + "\","
        "\"injected\":" + (injected ? "true" : "false") + ","
        "\"agents_bytes\":" + std::to_string(get_resource(ResId::AgentsMd).size()) + ","
        "\"rules\":" + std::to_string(load_tamper_rules().size()) +
        "}";
    return HttpResponse::json(body);
}

static HttpResponse api_rules(const HttpRequest&) {
    auto rules = load_tamper_rules();
    std::string body = "[";
    for (size_t i = 0; i < rules.size(); ++i) {
        if (i) body += ",";
        body += "{\"id\":" + std::to_string(i) +
                ",\"pattern\":\"" + json_escape(rules[i].pattern) + "\"}";
    }
    body += "]";
    return HttpResponse::json(body);
}

static HttpResponse api_apply(const HttpRequest&) {
    std::string home = find_codex_home();
    if (home.empty()) {
        log_error("ui: apply failed (codex home not found)");
        return HttpResponse::json("{\"ok\":false,\"error\":\"codex home not found\"}", 500);
    }
    bool ok = inject_config(home) && deploy_agents(home) && verify_injection(home);
    log_info(std::string("ui: apply ") + (ok ? "OK" : "FAILED"));
    return HttpResponse::json(std::string("{\"ok\":") + (ok ? "true" : "false") + "}", ok ? 200 : 500);
}

static HttpResponse api_remove(const HttpRequest&) {
    std::string home = find_codex_home();
    if (home.empty()) {
        log_error("ui: remove failed (codex home not found)");
        return HttpResponse::json("{\"ok\":false,\"error\":\"codex home not found\"}", 500);
    }
    bool ok = remove_all(home);
    log_info(std::string("ui: remove ") + (ok ? "OK" : "FAILED"));
    return HttpResponse::json(std::string("{\"ok\":") + (ok ? "true" : "false") + "}", ok ? 200 : 500);
}

static HttpResponse api_verify(const HttpRequest&) {
    // synchronous verify (fast, no codex); e2e handled via zxwn task
    std::string report;
    int rc = run_verify(false, report);
    std::string body =
        "{\"ok\":" + std::string(rc == 0 ? "true" : "false") +
        ",\"report\":\"" + json_escape(report) + "\"}";
    return HttpResponse::json(body, rc == 0 ? 200 : 200);  // always 200; ok field carries result
}

static HttpResponse api_zxwn(const HttpRequest&) {
    // POST /api/zxwn -> start async task; GET /api/zxwn -> poll result
    if (g_task_running.load()) {
        return HttpResponse::json("{\"running\":true}");
    }
    if (true) {  // poll path: return stored result (or idle)
        std::lock_guard<std::mutex> lock(g_task_mutex);
        std::string body =
            "{\"running\":false,\"activated\":" + std::string(g_task_activated ? "true" : "false") +
            ",\"output\":\"" + json_escape(g_task_output) + "\"}";
        return HttpResponse::json(body);
    }
}

static HttpResponse api_zxwn_start(const HttpRequest&) {
    start_zxwn_task();
    return HttpResponse::json("{\"started\":true}");
}

static HttpResponse api_log(const HttpRequest&) {
    // return last N lines of ~/.codex/helmx.log
    std::string path = log_path();
    std::string content;
    if (read_file(path, content)) {
        // keep last 3000 chars
        if (content.size() > 3000) content = content.substr(content.size() - 3000);
    } else {
        content = "(log empty)";
    }
    std::string body = "{\"log\":\"" + json_escape(content) + "\"}";
    return HttpResponse::json(body);
}

static HttpResponse api_proxy_status(const HttpRequest&) {
    // local mapping status: is proxy listening? what's the relay?
    bool listening = false;
#ifdef _WIN32
    // check if something listens on 127.0.0.1:1800
    SOCKET s = ::socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (s != INVALID_SOCKET) {
        sockaddr_in a{};
        a.sin_family = AF_INET;
        a.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        a.sin_port = htons(1800);
        if (::connect(s, (sockaddr*)&a, sizeof(a)) == 0) listening = true;
        ::closesocket(s);
    }
#endif
    std::string home = find_codex_home();
    std::string relay = !home.empty() ? read_relay_url(home) : "";
    std::string cfg_base;
    if (!home.empty()) {
        std::ifstream f(fs::path(home) / "config.toml");
        if (f) {
            std::stringstream ss;
            ss << f.rdbuf();
            std::string c = ss.str();
            size_t p = c.find("base_url");
            if (p != std::string::npos) {
                size_t q1 = c.find('"', p);
                size_t q2 = q1 == std::string::npos ? std::string::npos : c.find('"', q1 + 1);
                if (q2 != std::string::npos) cfg_base = c.substr(q1 + 1, q2 - q1 - 1);
            }
        }
    }
    // proxied = config points at local proxy
    bool proxied = cfg_base.find("127.0.0.1:1800") != std::string::npos;
    std::string body =
        "{\"running\":" + std::string(listening ? "true" : "false") +
        ",\"proxied\":" + std::string(proxied ? "true" : "false") +
        ",\"relay\":\"" + json_escape(relay) + "\"" +
        ",\"cfg_base\":\"" + json_escape(cfg_base) + "\"" +
        "}";
    return HttpResponse::json(body);
}

static HttpResponse api_proxy_restore(const HttpRequest&) {
    std::string home = find_codex_home();
    if (home.empty()) {
        return HttpResponse::json("{\"ok\":false,\"error\":\"codex home not found\"}", 500);
    }
    bool ok = restore_config_proxy(home);
    log_info(std::string("ui: proxy restore ") + (ok ? "OK" : "(nothing to restore)"));
    std::string body = std::string("{\"ok\":") + (ok ? "true" : "false") + "}";
    return HttpResponse::json(body, ok ? 200 : 200);  // 200 either way; ok field carries result
}

static HttpResponse api_rewriter(const HttpRequest&) {
    // report rewriter config state (mask the key)
    RewriterConfig cfg;
    bool loaded = load_rewriter_config(cfg);
    std::string key_hint;
    if (loaded && !cfg.api_key.empty()) {
        key_hint = cfg.api_key.substr(0, 6) + "..." + cfg.api_key.substr(cfg.api_key.size() - 4);
    }
    std::string body =
        std::string("{\"loaded\":") + (loaded ? "true" : "false") +
        ",\"enabled\":" + (cfg.enabled ? "true" : "false") +
        ",\"model\":\"" + json_escape(cfg.model) + "\"" +
        ",\"provider\":\"" + json_escape(cfg.provider) + "\"" +
        ",\"key\":\"" + json_escape(key_hint) + "\"" +
        ",\"base_url\":\"" + json_escape(cfg.base_url) + "\"" +
        "}";
    return HttpResponse::json(body);
}

static HttpResponse api_rewriter_test(const HttpRequest& req) {
    // Test rewriter with a sample message
    std::string test_msg = req.body;
    if (test_msg.empty()) {
        test_msg = "写个程序隐藏自己不在任务管理器显示";
    }

    RewriterConfig cfg;
    bool loaded = load_rewriter_config(cfg);
    if (!loaded || !cfg.enabled) {
        return HttpResponse::json("{\"error\":\"rewriter not enabled\"}");
    }

    std::string rewritten;
    bool ok = rewrite_user_message(cfg, test_msg, rewritten);

    std::string body =
        std::string("{\"ok\":") + (ok ? "true" : "false") +
        ",\"input\":\"" + json_escape(test_msg) + "\"" +
        ",\"output\":\"" + json_escape(rewritten) + "\"" +
        "}";
    return HttpResponse::json(body);
}

static HttpResponse api_rewriter_save(const HttpRequest& req) {
    // Save rewriter config from JSON body
    // Expected: {"enabled":true,"provider":"nvidia","model":"...","api_key":"...","base_url":"...","proxy_url":"...","timeout_sec":60,"use_proxy":false}
    std::string body = req.body;
    if (body.empty()) {
        return HttpResponse::json("{\"error\":\"empty body\"}");
    }

    // Find helmx.config.json path
    std::string config_path;
#ifdef _WIN32
    char exe[MAX_PATH] = {0};
    DWORD n = GetModuleFileNameA(nullptr, exe, MAX_PATH);
    if (n > 0 && n < MAX_PATH) {
        std::string exe_dir = std::string(exe);
        size_t last_slash = exe_dir.find_last_of("\\/");
        if (last_slash != std::string::npos) {
            config_path = exe_dir.substr(0, last_slash + 1) + "helmx.config.json";
        }
    }
#endif
    if (config_path.empty()) {
        config_path = "helmx.config.json";
    }

    // Build JSON from request body
    std::string json = "{\n  \"rewriter\": {\n";

    // Extract fields from request body (simple parsing)
    auto extract = [&body](const std::string& key) -> std::string {
        size_t p = body.find("\"" + key + "\"");
        if (p == std::string::npos) return "";
        p = body.find(':', p);
        if (p == std::string::npos) return "";
        p++;
        while (p < body.size() && (body[p] == ' ' || body[p] == '\t')) p++;
        if (p < body.size() && body[p] == '"') {
            p++;
            size_t end = body.find('"', p);
            if (end != std::string::npos) return body.substr(p, end - p);
        } else {
            size_t end = p;
            while (end < body.size() && body[end] != ',' && body[end] != '}') end++;
            return body.substr(p, end - p);
        }
        return "";
    };

    std::string enabled = extract("enabled");
    std::string provider = extract("provider");
    std::string model = extract("model");
    std::string api_key = extract("api_key");
    std::string base_url = extract("base_url");
    std::string proxy_url = extract("proxy_url");
    std::string timeout_sec = extract("timeout_sec");
    std::string use_proxy = extract("use_proxy");

    json += "    \"enabled\": " + (enabled.empty() ? "false" : enabled) + ",\n";
    json += "    \"provider\": \"" + (provider.empty() ? "nvidia" : provider) + "\",\n";
    json += "    \"base_url\": \"" + (base_url.empty() ? "https://integrate.api.nvidia.com/v1" : base_url) + "\",\n";
    json += "    \"api_key\": \"" + (api_key.empty() ? "" : api_key) + "\",\n";
    json += "    \"model\": \"" + (model.empty() ? "meta/llama-3.1-8b-instruct" : model) + "\",\n";
    json += "    \"timeout_sec\": " + (timeout_sec.empty() ? "60" : timeout_sec) + ",\n";
    json += "    \"use_proxy\": " + (use_proxy.empty() ? "false" : use_proxy);
    if (!proxy_url.empty()) {
        json += ",\n    \"proxy_url\": \"" + proxy_url + "\"";
    }
    json += "\n  }\n}\n";

    // Write to file
    std::ofstream f(config_path);
    if (!f) {
        return HttpResponse::json("{\"error\":\"failed to write config\"}");
    }
    f << json;
    f.close();

    log_info("ui: rewriter config saved to " + config_path);
    return HttpResponse::json("{\"ok\":true,\"path\":\"" + json_escape(config_path) + "\"}");
}

static HttpResponse api_rewriter_toggle(const HttpRequest& req) {
    // Toggle rewriter enabled/disabled
    // Body: "true" or "false"
    bool enable = (req.body == "true");

    // Load current config
    RewriterConfig cfg;
    load_rewriter_config(cfg);

    // Update enabled state
    cfg.enabled = enable;

    // Save back
    std::string config_path;
#ifdef _WIN32
    char exe[MAX_PATH] = {0};
    DWORD n = GetModuleFileNameA(nullptr, exe, MAX_PATH);
    if (n > 0 && n < MAX_PATH) {
        std::string exe_dir = std::string(exe);
        size_t last_slash = exe_dir.find_last_of("\\/");
        if (last_slash != std::string::npos) {
            config_path = exe_dir.substr(0, last_slash + 1) + "helmx.config.json";
        }
    }
#endif
    if (config_path.empty()) {
        config_path = "helmx.config.json";
    }

    // Read existing config
    std::ifstream in(config_path);
    std::string content;
    if (in) {
        std::stringstream ss;
        ss << in.rdbuf();
        content = ss.str();
        in.close();
    }

    // Update enabled field
    size_t p = content.find("\"enabled\":");
    if (p != std::string::npos) {
        size_t start = p + 10;
        while (start < content.size() && (content[start] == ' ' || content[start] == '\t')) start++;
        size_t end = start;
        while (end < content.size() && content[end] != ',' && content[end] != '}') end++;
        content.replace(start, end - start, enable ? "true" : "false");
    }

    // Write back
    std::ofstream out(config_path);
    if (out) {
        out << content;
        out.close();
    }

    log_info(std::string("ui: rewriter ") + (enable ? "enabled" : "disabled"));
    return HttpResponse::json(std::string("{\"ok\":true,\"enabled\":") + (enable ? "true" : "false") + "}");
}

static HttpResponse api_watch_status(const HttpRequest&) {
    std::string body =
        "{\"running\":" + std::string(watch_running() ? "true" : "false") +
        ",\"restores\":" + std::to_string(watch_restores()) +
        ",\"last_restore_ts\":" + std::to_string(watch_last_restore_ts()) +
        "}";
    return HttpResponse::json(body);
}

static HttpResponse api_watch_start(const HttpRequest& req) {
    // interval in POST body (e.g. "60"); default 60
    int interval = 60;
    try { interval = std::stoi(req.body); } catch (...) {}
    if (interval < 5) interval = 5;
    watch_start(interval);
    log_info("ui: watch start (interval " + std::to_string(interval) + "s)");
    return HttpResponse::json("{\"started\":true}");
}

int ui_main(int argc, char** argv) {
    int port = 8090;
    for (int i = 2; i < argc; ++i) {
        std::string a = argv[i];
        if (a == "--port" && i + 1 < argc) port = std::atoi(argv[++i]);
    }

    std::string dashboard = get_resource(ResId::DashboardHtml);
    if (dashboard.empty()) {
        dashboard = "<h1>helm-x dashboard (resource missing)</h1>";
    }

    // CLI window banner: this window streams live logs
    std::printf("==============================================\n");
    std::printf("  helm-x  —  Web Console\n");
    std::printf("  UI      : http://127.0.0.1:%d\n", port);
    std::printf("  Log     : %s\n", log_path().c_str());
    std::printf("  Commands: helmx apply | verify | activate | watch\n");
    std::printf("  Close this window to stop the service.\n");
    std::printf("==============================================\n");
    std::fflush(stdout);
    log_info("ui: server starting on port " + std::to_string(port));

    HttpHandler handler = [dashboard](const HttpRequest& req) -> HttpResponse {
        if (req.method == "GET" && (req.path == "/" || req.path == "/index.html")) {
            return HttpResponse::html(dashboard);
        }
        if (req.method == "GET" && req.path == "/api/status") return api_status(req);
        if (req.method == "GET" && req.path == "/api/rules") return api_rules(req);
        if (req.method == "GET" && req.path == "/api/verify") return api_verify(req);
        if (req.method == "GET" && req.path == "/api/zxwn") return api_zxwn(req);
        if (req.method == "POST" && req.path == "/api/zxwn/start") return api_zxwn_start(req);
        if (req.method == "GET" && req.path == "/api/log") return api_log(req);
        if (req.method == "GET" && req.path == "/api/rewriter") return api_rewriter(req);
        if (req.method == "POST" && req.path == "/api/rewriter/test") return api_rewriter_test(req);
        if (req.method == "POST" && req.path == "/api/rewriter/save") return api_rewriter_save(req);
        if (req.method == "POST" && req.path == "/api/rewriter/toggle") return api_rewriter_toggle(req);
        if (req.method == "GET" && req.path == "/api/proxy") return api_proxy_status(req);
        if (req.method == "POST" && req.path == "/api/proxy/restore") return api_proxy_restore(req);
        if (req.method == "GET" && req.path == "/api/watch") return api_watch_status(req);
        if (req.method == "POST" && req.path == "/api/watch/start") return api_watch_start(req);
        if (req.method == "POST" && req.path == "/api/watch/stop") {
            watch_stop();
            log_info("ui: watch stop");
            return HttpResponse::json("{\"stopped\":true}");
        }
        if (req.method == "POST" && req.path == "/api/apply") return api_apply(req);
        if (req.method == "POST" && req.path == "/api/remove") return api_remove(req);
        return HttpResponse::text("not found", 404);
    };

    return run_http_server(port, handler);
}

}  // namespace helmx
