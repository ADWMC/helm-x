// mcp.cpp — built-in MCP server (stdio JSON-RPC, Content-Length framing)
//
// MCP stdio transport: each message is framed as
//   Content-Length: <N>\r\n\r\n<JSON body of N bytes>
// Codex (MCP client) spawns `helmx mcp` and speaks this protocol.
// This server implements initialize / tools/list / tools/call / ping
// with built-in helmx tools (verify / activate / status / watch).
#include "mcp.h"

#include "config.h"
#include "log.h"
#include "resources.h"
#include "verify.h"
#include "version.h"
#include "watch.h"

#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <string>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <fcntl.h>
#include <io.h>
#endif

namespace helmx {

namespace {

// ── MCP framing ──
// Read exactly n bytes from stdin (binary). Returns false on EOF/error.
bool read_exact(char* buf, size_t n) {
    size_t got = 0;
    while (got < n) {
        size_t r = fread(buf + got, 1, n - got, stdin);
        if (r == 0) return false;  // EOF / error
        got += r;
    }
    return true;
}

// Read one MCP frame from stdin. Returns false on EOF/parse error.
// body receives the JSON payload.
bool read_frame(std::string& body) {
    // read headers until \r\n\r\n
    std::string headers;
    char c;
    while (headers.find("\r\n\r\n") == std::string::npos) {
        if (fread(&c, 1, 1, stdin) != 1) return false;
        headers.push_back(c);
        if (headers.size() > 8192) return false;  // header too long
    }
    size_t header_end = headers.find("\r\n\r\n");
    std::string head = headers.substr(0, header_end);

    // parse Content-Length
    size_t len = 0;
    size_t pos = 0;
    while (pos < head.size()) {
        size_t eol = head.find("\r\n", pos);
        std::string line = head.substr(pos, eol == std::string::npos ? std::string::npos : eol - pos);
        if (line.size() > 15 && line.compare(0, 15, "Content-Length:") == 0) {
            len = (size_t)std::strtoull(line.c_str() + 15, nullptr, 10);
        }
        if (eol == std::string::npos) break;
        pos = eol + 2;
    }
    if (len == 0 || len > 16 * 1024 * 1024) return false;

    body.resize(len);
    return read_exact(&body[0], len);
}

// Write one MCP frame to stdout.
void write_frame(const std::string& body) {
    char head[64];
    int n = std::snprintf(head, sizeof(head), "Content-Length: %zu\r\n\r\n", body.size());
    fwrite(head, 1, (size_t)n, stdout);
    fwrite(body.data(), 1, body.size(), stdout);
    fflush(stdout);
}

// ── minimal JSON field extraction (no external parser) ──
// Find a top-level string field value: {"field":"value"}
std::string json_field(const std::string& s, const std::string& field) {
    std::string key = "\"" + field + "\"";
    size_t p = s.find(key);
    if (p == std::string::npos) return "";
    p = s.find(':', p + key.size());
    if (p == std::string::npos) return "";
    p++;  // after ':'
    while (p < s.size() && (s[p] == ' ' || s[p] == '\t')) p++;
    if (p < s.size() && s[p] == '"') {
        p++;
        std::string out;
        while (p < s.size() && s[p] != '"') {
            if (s[p] == '\\' && p + 1 < s.size()) {
                p++;
                out.push_back(s[p] == 'n' ? '\n' : s[p] == 't' ? '\t' : s[p]);
            } else {
                out.push_back(s[p]);
            }
            p++;
        }
        return out;
    }
    // number or bool
    size_t start = p;
    while (p < s.size() && (s[p] != ',' && s[p] != '}' && s[p] != ' ')) p++;
    return s.substr(start, p - start);
}

// Extract JSON-RPC id (number or string). Returns "" if absent.
std::string json_id(const std::string& s) {
    size_t p = s.find("\"id\"");
    if (p == std::string::npos) return "";
    p = s.find(':', p + 4);
    if (p == std::string::npos) return "";
    p++;
    while (p < s.size() && (s[p] == ' ' || s[p] == '\t')) p++;
    size_t start = p;
    if (p < s.size() && s[p] == '"') {  // string id
        p++;
        start = p;
        while (p < s.size() && s[p] != '"') p++;
        return s.substr(start, p - start);
    }
    while (p < s.size() && s[p] != ',' && s[p] != '}') p++;
    return s.substr(start, p - start);
}

std::string json_escape(const std::string& s) {
    std::string out;
    for (char ch : s) {
        switch (ch) {
            case '"': out += "\\\""; break;
            case '\\': out += "\\\\"; break;
            case '\n': out += "\\n"; break;
            case '\r': out += "\\r"; break;
            case '\t': out += "\\t"; break;
            default: out += ch;
        }
    }
    return out;
}

std::string rpc_result(const std::string& id, const std::string& result_json) {
    if (id.empty()) {
        return "{\"jsonrpc\":\"2.0\",\"result\":" + result_json + "}";
    }
    return "{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"result\":" + result_json + "}";
}

std::string rpc_error(const std::string& id, int code, const std::string& msg) {
    if (id.empty()) {
        return "{\"jsonrpc\":\"2.0\",\"error\":{\"code\":" + std::to_string(code) +
               ",\"message\":\"" + json_escape(msg) + "\"}}";
    }
    return "{\"jsonrpc\":\"2.0\",\"id\":" + id + ",\"error\":{\"code\":" + std::to_string(code) +
           ",\"message\":\"" + json_escape(msg) + "\"}}";
}

// ── tools ──
struct ToolDef {
    const char* name;
    const char* description;
};

const ToolDef kTools[] = {
    {"verify", "Run helmx integrity self-test (7 checks). Returns report text."},
    {"activate", "Send activation word 'helmx' to codex and check the reply."},
    {"status", "Get injection status (codex home, AGENTS bytes, injected)."},
    {"watch_start", "Start the self-healing watch daemon. Args: {\"interval\":60}."},
    {"watch_stop", "Stop the self-healing watch daemon."},
    {"watch_status", "Get watch daemon status (running, restore count)."},
};

constexpr size_t kToolCount = sizeof(kTools) / sizeof(kTools[0]);

std::string tools_list_json() {
    std::string out = "{\"tools\":[";
    for (size_t i = 0; i < kToolCount; ++i) {
        if (i) out += ",";
        out += "{\"name\":\"" + std::string(kTools[i].name) +
               "\",\"description\":\"" + json_escape(kTools[i].description) + "\",";
        // inputSchema: minimal open (no required params) except watch_start
        if (std::string(kTools[i].name) == "watch_start") {
            out += "\"inputSchema\":{\"type\":\"object\",\"properties\":{\"interval\":{\"type\":\"number\",\"description\":\"check period in seconds (>=5)\"}}}";
        } else {
            out += "\"inputSchema\":{\"type\":\"object\",\"properties\":{}}";
        }
        out += "}";
    }
    out += "]}";
    return out;
}

std::string call_tool(const std::string& name, const std::string& args_json) {
    if (name == "verify") {
        std::string report;
        int rc = run_verify(false, report);
        std::string content = "verify: " + std::string(rc == 0 ? "PASS" : "FAIL") + "\n" + report;
        return "{\"content\":[{\"type\":\"text\",\"text\":\"" + json_escape(content) + "\"}]}";
    }
    if (name == "activate") {
        std::string out;
        bool activated = false;
        int rc = run_zxwn(out, activated);
        (void)rc;
        std::string content = (activated ? "[OK] activation confirmed\n" : "[WARN] not activated\n") + out;
        return "{\"content\":[{\"type\":\"text\",\"text\":\"" + json_escape(content) + "\"}]}";
    }
    if (name == "status") {
        std::string home = find_codex_home();
        bool injected = !home.empty() && verify_injection(home);
        std::string content = "home: " + home + "\ninjected: " + (injected ? "true" : "false") +
                              "\nagents_bytes: " + std::to_string(get_resource(ResId::AgentsMd).size());
        return "{\"content\":[{\"type\":\"text\",\"text\":\"" + json_escape(content) + "\"}]}";
    }
    if (name == "watch_start") {
        int interval = 60;
        // args_json contains {"interval":N} — extract number after "interval"
        size_t p = args_json.find("interval");
        if (p != std::string::npos) {
            p = args_json.find(':', p);
            if (p != std::string::npos) {
                interval = std::atoi(args_json.c_str() + p + 1);
            }
        }
        watch_start(interval);
        return "{\"content\":[{\"type\":\"text\",\"text\":\"watch started (interval " +
               std::to_string(interval) + "s)\"}]}";
    }
    if (name == "watch_stop") {
        watch_stop();
        return "{\"content\":[{\"type\":\"text\",\"text\":\"watch stopped\"}]}";
    }
    if (name == "watch_status") {
        std::string content = "running: " + std::string(watch_running() ? "true" : "false") +
                              "\nrestores: " + std::to_string(watch_restores());
        return "{\"content\":[{\"type\":\"text\",\"text\":\"" + json_escape(content) + "\"}]}";
    }
    return "";
}

}  // namespace

int mcp_main() {
    // MCP stdio transport is binary — CRLF must survive; disable text-mode
    // translation on Windows CRT (otherwise \r\n becomes \n and framing breaks).
#ifdef _WIN32
    _setmode(_fileno(stdin), _O_BINARY);
    _setmode(_fileno(stdout), _O_BINARY);
#endif
    std::string body;
    while (read_frame(body)) {
        std::string method = json_field(body, "method");
        std::string id = json_id(body);

        if (method.empty()) {
            // notification (no method match should not happen; ignore)
            continue;
        }
        if (method == "initialize") {
            log_info("mcp: initialize (client connected)");
            // protocol negotiation
            std::string result =
                "{\"protocolVersion\":\"2024-11-05\","
                "\"capabilities\":{\"tools\":{\"listChanged\":false}},"
                "\"serverInfo\":{\"name\":\"helmx\",\"version\":\"" HELMX_VERSION "\"}}";
            write_frame(rpc_result(id, result));
        } else if (method == "notifications/initialized") {
            log_info("mcp: initialized notification");
            // no response for notifications
            continue;
        } else if (method == "ping") {
            write_frame(rpc_result(id, "{}"));
        } else if (method == "tools/list") {
            log_info("mcp: tools/list -> " + std::to_string(kToolCount) + " tools");
            write_frame(rpc_result(id, tools_list_json()));
        } else if (method == "tools/call") {
            std::string name = json_field(body, "name");
            // arguments object — extract the {...} block after "arguments"
            std::string args;
            size_t p = body.find("\"arguments\"");
            if (p != std::string::npos) {
                p = body.find('{', p);
                if (p != std::string::npos) {
                    args = body.substr(p);
                }
            }
            std::string result = call_tool(name, args);
            if (!result.empty()) {
                write_frame(rpc_result(id, result));
            } else {
                write_frame(rpc_error(id, -32601, "tool not found: " + name));
            }
        } else {
            write_frame(rpc_error(id, -32601, "method not found: " + method));
        }
    }
    return 0;
}

}  // namespace helmx
