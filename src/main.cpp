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
#endif

#include "config.h"
#include "log.h"
#include "mcp.h"
#include "proxy.h"
#include "resources.h"
#include "ui.h"
#include "verify.h"
#include "watch.h"

// Double-click launch: open the web dashboard in the default browser.
// Spawns a detached process so the console window can close.
static void launch_dashboard() {
    const char* port_env = std::getenv("HELMX_PORT");
    int port = 8090;
    if (port_env && *port_env) {
        int p = std::atoi(port_env);
        if (p > 0) port = p;
    }
    std::string url = "http://127.0.0.1:" + std::to_string(port) + "/";

#ifdef _WIN32
    char exe[MAX_PATH] = {0};
    GetModuleFileNameA(nullptr, exe, MAX_PATH);

    // 1. start proxy (local mapping; upstream auto-read from config)
    {
        std::string cmd = std::string("\"") + exe + "\" proxy --listen 1800";
        STARTUPINFOA si{};
        si.cb = sizeof(si);
        PROCESS_INFORMATION pi{};
        BOOL ok = CreateProcessA(nullptr, cmd.data(), nullptr, nullptr, FALSE,
                                 CREATE_NEW_CONSOLE, nullptr, nullptr, &si, &pi);
        if (ok) { CloseHandle(pi.hProcess); CloseHandle(pi.hThread); }
    }
    // 2. start ui server in a NEW visible console window (real-time log stream)
    {
        std::string cmd = std::string("\"") + exe + "\" ui --port " + std::to_string(port);
        STARTUPINFOA si{};
        si.cb = sizeof(si);
        PROCESS_INFORMATION pi{};
        BOOL ok = CreateProcessA(nullptr, cmd.data(), nullptr, nullptr, FALSE,
                                 CREATE_NEW_CONSOLE, nullptr, nullptr, &si, &pi);
        if (ok) { CloseHandle(pi.hProcess); CloseHandle(pi.hThread); }
    }
    // 3. open browser after a short delay so servers are up
    std::thread([url] {
        std::this_thread::sleep_for(std::chrono::milliseconds(1200));
        ShellExecuteA(nullptr, "open", url.c_str(), nullptr, nullptr, SW_SHOWNORMAL);
    }).detach();
#else
    (void)url;
#endif
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
        "  mcp                built-in MCP server (stdio JSON-RPC)\n"
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
    } else if (cmd == "mcp") {
        return helmx::mcp_main();
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
