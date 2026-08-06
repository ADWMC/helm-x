// mcp.cpp — built-in MCP server (stdio JSON-RPC)
#include "mcp.h"

#include <cstdio>
#include <string>

namespace helmx {

// Minimal MCP stdio loop. Phase P4: full JSON-RPC + tools from embedded
// tools.json. Skeleton: echo protocol handshake.
int mcp_main() {
    std::printf("[helm-x] mcp skeleton (stdio JSON-RPC) — Phase P4\n");
    std::fflush(stdout);

    std::string line;
    char buf[8192];
    while (std::fgets(buf, sizeof(buf), stdin)) {
        line = buf;
        if (line.find("\"initialize\"") != std::string::npos) {
            std::printf("{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"protocolVersion\":\"2024-11-05\",\"capabilities\":{\"tools\":{}},\"serverInfo\":{\"name\":\"helmx\",\"version\":\"0.1.0\"}}}\n");
            std::fflush(stdout);
        } else if (line.find("\"tools/list\"") != std::string::npos) {
            std::printf("{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"tools\":[]}}\n");
            std::fflush(stdout);
        }
    }
    return 0;
}

}  // namespace helmx
