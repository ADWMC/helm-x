// ui.cpp — Web dashboard: status / rules / actions / services
#include "ui.h"

#include "config.h"
#include "http.h"
#include "log.h"
#include "resources.h"
#include "tamper.h"
#include "verify.h"
#include "watch.h"

#include <atomic>
#include <cstdio>
#include <cstdlib>
#include <mutex>
#include <string>
#include <thread>

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
