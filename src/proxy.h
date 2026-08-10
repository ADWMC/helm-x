// proxy.h — HTTP MITM tamper proxy (WinHTTP upstream, SSE-safe)
#pragma once
#include <string>
namespace helmx {

// proxy --listen <port> --upstream <relay-url>
//   Local mapping: codex -> 127.0.0.1:port -> upstream relay.
//   Injects embedded AGENTS into requests, tamper-rewrites refusals.
//   Forcing stream=false avoids the SSE-stall bug of the Python original.
int proxy_main(int argc, char** argv);

// Get the relay URL that the proxy resolved at startup.
// Returns empty string if proxy hasn't started yet.
std::string get_relay_url();

}  // namespace helmx
