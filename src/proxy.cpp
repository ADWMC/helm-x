// proxy.cpp — HTTP MITM tamper proxy (WinSock2, zero external deps)
#include "proxy.h"

#include <cstdio>
#include <cstdlib>
#include <string>

#ifdef _WIN32
#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <winsock2.h>
#include <ws2tcpip.h>
#pragma comment(lib, "ws2_32.lib")
#endif

namespace helmx {

// NOTE: full MITM implementation (request inject + SSE tamper + relay) is
// Phase P5. This skeleton wires the CLI surface and validates WinSock init.
int proxy_main(int argc, char** argv) {
    int listen_port = 1800;
    std::string upstream;

    for (int i = 2; i < argc; ++i) {
        std::string a = argv[i];
        if (a == "--listen" && i + 1 < argc) listen_port = std::atoi(argv[++i]);
        else if (a == "--upstream" && i + 1 < argc) upstream = argv[++i];
    }

#ifdef _WIN32
    WSADATA wsa;
    if (WSAStartup(MAKEWORD(2, 2), &wsa) != 0) {
        std::fprintf(stderr, "[helm-x] WSAStartup failed\n");
        return 1;
    }
#endif

    std::printf("[helm-x] proxy skeleton: listen=127.0.0.1:%d upstream=%s\n",
                listen_port, upstream.empty() ? "(none)" : upstream.c_str());
    std::printf("[helm-x] full MITM in Phase P5; exiting\n");

#ifdef _WIN32
    WSACleanup();
#endif
    return 0;
}

}  // namespace helmx
