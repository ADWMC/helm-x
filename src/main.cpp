// helm-x — Codex CLI environment control tool (C++17, single binary)
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <chrono>
#include <string>
#include <thread>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>
#include <shellapi.h>
#include <winsock2.h>
#endif

#include "config.h"
#include "log.h"
#include "proxy.h"
#include "resources.h"
#include "ui.h"
#include "verify.h"
#include "watch.h"

// Double-click launch: run proxy + UI in same process, open browser.
static void launch_dashboard() {
    const char* port_env = std::getenv("HELMX_PORT");
    int port = 8090;
    if (port_env && *port_env) {
        int p = std::atoi(port_env);
        if (p > 0) port = p;
    }
    std::string url = "http://127.0.0.1:" + std::to_string(port) + "/";

    std::printf("==============================================\n");
    std::printf("  helm-x  —  Proxy + Web Console\n");
    std::printf("  Proxy   : http://127.0.0.1:1800\n");
    std::printf("  UI      : http://127.0.0.1:%d\n", port);
    std::printf("  Log     : %%USERPROFILE%%\\.codex\\helmx.log\n");
    std::printf("  Close this window to stop all services.\n");
    std::printf("==============================================\n");
    std::fflush(stdout);

    // Start UI in a background thread
    std::thread ui_thread([port]() {
        std::string port_str = std::to_string(port);
        std::vector<std::string> ui_args = {"helmx", "ui", "--port", port_str};
        std::vector<char*> ui_argv;
        for (auto& a : ui_args) ui_argv.push_back(a.data());
        helmx::ui_main((int)ui_argv.size(), ui_argv.data());
    });
    ui_thread.detach();

    // Wait until the UI is accepting connections before opening the browser.
#ifdef _WIN32
    WSADATA wsa{};
    bool ui_ready = WSAStartup(MAKEWORD(2, 2), &wsa) == 0;
    if (ui_ready) {
        ui_ready = false;
        for (int attempt = 0; attempt < 50 && !ui_ready; ++attempt) {
            SOCKET s = ::socket(AF_INET, SOCK_STREAM, IPPROTO_TCP);
            if (s != INVALID_SOCKET) {
                sockaddr_in addr{};
                addr.sin_family = AF_INET;
                addr.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
                addr.sin_port = htons((u_short)port);
                ui_ready = ::connect(s, (sockaddr*)&addr, sizeof(addr)) == 0;
                ::closesocket(s);
            }
            if (!ui_ready) std::this_thread::sleep_for(std::chrono::milliseconds(100));
        }
        WSACleanup();
    }
#else
    bool ui_ready = true;
#endif

    // Open browser
#ifdef _WIN32
    if (ui_ready) ShellExecuteA(nullptr, "open", url.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
    else std::fprintf(stderr, "[helm-x] UI failed to start at %s\n", url.c_str());
#endif

    // Run proxy in main thread (blocks until shutdown)
    std::vector<std::string> proxy_args = {"helmx", "proxy", "--listen", "1800"};
    std::vector<char*> proxy_argv;
    for (auto& a : proxy_args) proxy_argv.push_back(a.data());
    helmx::proxy_main((int)proxy_argv.size(), proxy_argv.data());
}

static void usage() {
    std::printf(
        "helm-x — Codex environment control\n"
        "\n"
        "usage: helmx <command> [args]\n"
        "\n"
        "commands:\n"
        "  apply              deploy AGENTS.md + config injection\n"
        "  verify             self-test injection state [--e2e runs codex check]\n"
        "  activate           send activation word 'helmx' via codex\n"
        "  ui                 web dashboard (status / rules / actions)\n"
        "  watch              self-healing daemon (verify + restore)\n"
        "  proxy              tamper proxy (HTTP MITM inject + rewrite)\n"
        "  remove             uninstall and restore backups\n"
        "\n");
}

int main(int argc, char** argv) {
    if (argc < 2) {
        // double-click: open dashboard in browser
        launch_dashboard();
        helmx::log_info("launch: dashboard requested (double-click)");
        return 0;
    }

    const std::string cmd = argv[1];

    if (cmd == "apply") {
        return helmx::apply();
    } else if (cmd == "verify") {
        return helmx::verify_main(argc, argv);
    } else if (cmd == "activate" || cmd == "zxwn") {
        return helmx::zxwn_cmd();
    } else if (cmd == "ui") {
        return helmx::ui_main(argc, argv);
    } else if (cmd == "watch") {
        return helmx::watch(argc > 2 ? std::atoi(argv[2]) : 60);
    } else if (cmd == "proxy") {
        return helmx::proxy_main(argc, argv);
    } else if (cmd == "remove") {
        return helmx::remove();
    } else if (cmd == "help" || cmd == "-h" || cmd == "--help") {
        usage();
        return 0;
    }

    std::fprintf(stderr, "helm-x: unknown command '%s'\n\n", cmd.c_str());
    usage();
    return 1;
}
