// http.h — minimal HTTP/1.1 server (WinSock2, zero external deps)
// Shared by `helmx ui` (dashboard) and `helmx proxy` (MITM relay).
#pragma once
#include <functional>
#include <map>
#include <string>

namespace helmx {

struct HttpRequest {
    std::string method;   // GET / POST / ...
    std::string path;     // /api/status
    std::map<std::string, std::string> headers;  // lowercased keys
    std::string body;
};

struct HttpResponse {
    int status = 200;
    std::string content_type = "text/plain; charset=utf-8";
    std::string body;
    std::map<std::string, std::string> headers;  // extra headers

    static HttpResponse json(const std::string& body, int status = 200);
    static HttpResponse html(const std::string& body, int status = 200);
    static HttpResponse text(const std::string& body, int status = 200);
};

// Handler: return response for request. Throwing => 500.
using HttpHandler = std::function<HttpResponse(const HttpRequest&)>;

// Start blocking HTTP server on 127.0.0.1:port. Returns on shutdown signal.
// handler is invoked per request (serialized; fine for a dashboard).
int run_http_server(int port, HttpHandler handler);

// --- helpers ---
// URL-decode a query string into a map (e.g. "a=1&b=2" -> {a:1, b:2})
std::map<std::string, std::string> parse_query(const std::string& query);

// Read whole file (returns false if missing)
bool read_file(const std::string& path, std::string& out);

}  // namespace helmx
