// helm-x — Codex CLI environment control tool (C++17, single binary)
#include <cstdio>
#include <cstring>
#include <string>

#include "config.h"
#include "mcp.h"
#include "proxy.h"
#include "resources.h"
#include "ui.h"
#include "verify.h"
#include "watch.h"

static void usage() {
    std::printf(
        "helm-x — Codex environment control\n"
        "\n"
        "usage: helmx <command> [args]\n"
        "\n"
        "commands:\n"
        "  apply              deploy AGENTS.md + config injection\n"
        "  verify             self-test injection state [--e2e runs codex check]\n"
        "  zxwn               send activation via codex (codex exec helmx)\n"
        "  ui                 web dashboard (status / rules / actions)\n"
        "  watch              self-healing daemon (verify + restore)\n"
        "  proxy              tamper proxy (HTTP MITM inject + rewrite)\n"
        "  mcp                built-in MCP server (stdio JSON-RPC)\n"
        "  remove             uninstall and restore backups\n"
        "\n");
}

int main(int argc, char** argv) {
    if (argc < 2) {
        usage();
        return 1;
    }

    const std::string cmd = argv[1];

    if (cmd == "apply") {
        return helmx::apply();
    } else if (cmd == "verify") {
        return helmx::verify_main(argc, argv);
    } else if (cmd == "zxwn") {
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
