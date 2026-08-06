// ui.cpp — Web dashboard: status / rules / actions
#include "ui.h"

#include "config.h"
#include "http.h"
#include "resources.h"
#include "tamper.h"

#include <cstdio>
#include <cstdlib>
#include <string>

namespace helmx {

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
        return HttpResponse::json("{\"ok\":false,\"error\":\"codex home not found\"}", 500);
    }
    bool ok = inject_config(home) && deploy_agents(home) && verify_injection(home);
    std::string body = std::string("{\"ok\":") + (ok ? "true" : "false") + "}";
    return HttpResponse::json(body, ok ? 200 : 500);
}

static HttpResponse api_remove(const HttpRequest&) {
    std::string home = find_codex_home();
    if (home.empty()) {
        return HttpResponse::json("{\"ok\":false,\"error\":\"codex home not found\"}", 500);
    }
    bool ok = remove_all(home);
    return HttpResponse::json(std::string("{\"ok\":") + (ok ? "true" : "false") + "}", ok ? 200 : 500);
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
        if (req.method == "POST" && req.path == "/api/apply") return api_apply(req);
        if (req.method == "POST" && req.path == "/api/remove") return api_remove(req);
        return HttpResponse::text("not found", 404);
    };

    return run_http_server(port, handler);
}

}  // namespace helmx
