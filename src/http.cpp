// http.cpp — minimal HTTP/1.1 server (WinSock2, zero external deps)
#include "http.h"

#include "log.h"

#include <cstdio>
#include <cctype>
#include <cstring>
#include <fstream>
#include <sstream>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#endif

namespace helmx {

// ---------- response factories ----------
HttpResponse HttpResponse::json(const std::string& b, int s) {
    HttpResponse r;
    r.status = s;
    r.content_type = "application/json; charset=utf-8";
    r.body = b;
    return r;
}
HttpResponse HttpResponse::html(const std::string& b, int s) {
    HttpResponse r;
    r.status = s;
    r.content_type = "text/html; charset=utf-8";
    r.body = b;
    return r;
}
HttpResponse HttpResponse::text(const std::string& b, int s) {
    HttpResponse r;
    r.status = s;
    r.content_type = "text/plain; charset=utf-8";
    r.body = b;
    return r;
}

// ---------- query parsing ----------
static int hex_val(char c) {
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

std::map<std::string, std::string> parse_query(const std::string& q) {
    std::map<std::string, std::string> out;
    size_t pos = 0;
    while (pos < q.size()) {
        size_t amp = q.find('&', pos);
        std::string pair = q.substr(pos, amp == std::string::npos ? std::string::npos : amp - pos);
        size_t eq = pair.find('=');
        std::string k = eq == std::string::npos ? pair : pair.substr(0, eq);
        std::string v = eq == std::string::npos ? "" : pair.substr(eq + 1);
        // percent-decode
        std::string dk, dv;
        for (size_t i = 0; i < k.size(); ++i) {
            if (k[i] == '%' && i + 2 < k.size()) {
                int h = hex_val(k[i + 1]), l = hex_val(k[i + 2]);
                if (h >= 0 && l >= 0) { dk.push_back((char)(h * 16 + l)); i += 2; continue; }
            }
            dk.push_back(k[i]);
        }
        for (size_t i = 0; i < v.size(); ++i) {
            if (v[i] == '%' && i + 2 < v.size()) {
                int h = hex_val(v[i + 1]), l = hex_val(v[i + 2]);
                if (h >= 0 && l >= 0) { dv.push_back((char)(h * 16 + l)); i += 2; continue; }
            }
            if (v[i] == '+') { dv.push_back(' '); continue; }
            dv.push_back(v[i]);
        }
        out[dk] = dv;
        if (amp == std::string::npos) break;
        pos = amp + 1;
    }
    return out;
}

bool read_file(const std::string& path, std::string& out) {
    std::ifstream f(path, std::ios::binary);
    if (!f) return false;
    std::stringstream ss;
    ss << f.rdbuf();
    out = ss.str();
    return true;
}

// ---------- connection handling ----------
static void send_all(SOCKET s, const char* data, size_t len) {
    size_t sent = 0;
    while (sent < len) {
        int n = ::send(s, data + sent, (int)(len - sent), 0);
        if (n <= 0) break;
        sent += (size_t)n;
    }
}

static void handle_conn(SOCKET client, HttpHandler& handler) {
    char buf[16384];
    std::string recv_data;
    // read request head until \r\n\r\n
    while (recv_data.find("\r\n\r\n") == std::string::npos && recv_data.size() < 65536) {
        int n = ::recv(client, buf, sizeof(buf), 0);
        if (n <= 0) break;
        recv_data.append(buf, (size_t)n);
    }
    size_t head_end = recv_data.find("\r\n\r\n");
    if (head_end == std::string::npos) { ::closesocket(client); return; }

    std::string head = recv_data.substr(0, head_end);
    std::string rest = recv_data.substr(head_end + 4);

    // parse request line
    std::istringstream hss(head);
    std::string method, target, version;
    hss >> method >> target >> version;

    // parse headers
    std::map<std::string, std::string> headers;
    std::string line;
    std::getline(hss, line);  // consume rest of request line
    while (std::getline(hss, line)) {
        if (line.empty() || line == "\r") continue;
        size_t colon = line.find(':');
        if (colon == std::string::npos) continue;
        std::string k = line.substr(0, colon);
        std::string v = line.substr(colon + 1);
        // trim
        size_t s0 = v.find_first_not_of(" \t\r");
        size_t s1 = v.find_last_not_of(" \t\r");
        if (s0 == std::string::npos) v.clear(); else v = v.substr(s0, s1 - s0 + 1);
        for (auto& c : k) c = (char)std::tolower((unsigned char)c);
        headers[k] = v;
    }

    // read body if content-length
    std::string body = rest;
    auto it = headers.find("content-length");
    if (it != headers.end()) {
        char* end = nullptr;
        unsigned long parsed = std::strtoul(it->second.c_str(), &end, 10);
        while (end && *end && std::isspace((unsigned char)*end)) ++end;
        if (!end || end == it->second.c_str() || *end != '\0' || parsed > 16 * 1024 * 1024) {
            ::closesocket(client);
            return;
        }
        size_t need = (size_t)parsed;
        while (body.size() < need) {
            int n = ::recv(client, buf, sizeof(buf), 0);
            if (n <= 0) break;
            body.append(buf, (size_t)n);
        }
        if (body.size() > need) body = body.substr(0, need);
    }

    HttpRequest req;
    req.method = method;
    req.headers = headers;
    req.body = body;
    // split path?query
    size_t qpos = target.find('?');
    if (qpos == std::string::npos) {
        req.path = target;
    } else {
        req.path = target.substr(0, qpos);
        std::string qs = target.substr(qpos + 1);
        // keep query accessible via headers? no — expose in body? For GET we
        // merge query into a synthetic form: simplest is to expose path only.
        // Callers can use parse_query on the raw query — we stash it:
        req.body = qs;  // note: GET handlers must use parse_query(req.body)
    }

    HttpResponse resp;
    try {
        resp = handler(req);
    } catch (...) {
        resp = HttpResponse::text("{\"error\":\"internal\"}", 500);
        resp.content_type = "application/json";
    }

    // real-time log stream: log meaningful events, skip high-frequency
    // polling GETs (status/watch/log/rules refresh every few seconds)
    {
        bool noisy = req.method == "GET" &&
                     (req.path == "/api/status" || req.path == "/api/watch" ||
                      req.path == "/api/log" || req.path == "/api/rules" ||
                      req.path == "/api/proxy" || req.path == "/api/rewriter" ||
                      req.path == "/api/prompt-mode" || req.path == "/api/cyber-log" ||
                      req.path == "/" || req.path == "/index.html");
        bool error = resp.status >= 400;
        if (!noisy || error) {
            char line[256];
            std::snprintf(line, sizeof(line), "req %s %s -> %d", req.method.c_str(), req.path.c_str(), resp.status);
            log_info(line);
        }
    }

    // serialize response
    std::ostringstream out;
    out << "HTTP/1.1 " << resp.status << " " << (resp.status == 200 ? "OK" : resp.status == 404 ? "Not Found" : "Error") << "\r\n";
    out << "Content-Type: " << resp.content_type << "\r\n";
    out << "Content-Length: " << resp.body.size() << "\r\n";
    out << "Connection: close\r\n";
    for (auto& kv : resp.headers) {
        out << kv.first << ": " << kv.second << "\r\n";
    }
    out << "\r\n";
    std::string head_str = out.str();
    send_all(client, head_str.data(), head_str.size());
    send_all(client, resp.body.data(), resp.body.size());
    ::closesocket(client);
}

int run_http_server(int port, HttpHandler handler) {
#ifdef _WIN32
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        std::fprintf(stderr, "[helm-x] WSAStartup failed\n");
        return 1;
    }
#endif

    SOCKET listen_sock = ::socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
    if (listen_sock == INVALID_SOCKET) {
        std::fprintf(stderr, "[helm-x] socket failed\n");
        return 1;
    }
    int reuse = 1;
    ::setsockopt(listen_sock, SOL_SOCKET, SO_REUSEADDR, (const char*)&reuse, sizeof(reuse));

    sockaddr_in addr{};
    addr.sin_family = AF_INET;
    addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
    addr.sin_port = htons((u_short)port);
    if (::bind(listen_sock, (sockaddr*)&addr, sizeof(addr)) != 0) {
        std::fprintf(stderr, "[helm-x] bind 127.0.0.1:%d failed\n", port);
        ::closesocket(listen_sock);
        return 1;
    }
    if (::listen(listen_sock, 16) != 0) {
        std::fprintf(stderr, "[helm-x] listen failed\n");
        ::closesocket(listen_sock);
        return 1;
    }

    std::printf("[helm-x] dashboard: http://127.0.0.1:%d\n", port);
    std::fflush(stdout);

    while (true) {
        SOCKET client = ::accept(listen_sock, nullptr, nullptr);
        if (client == INVALID_SOCKET) break;
        handle_conn(client, handler);
    }
    ::closesocket(listen_sock);
#ifdef _WIN32
    WSACleanup();
#endif
    return 0;
}

}  // namespace helmx
